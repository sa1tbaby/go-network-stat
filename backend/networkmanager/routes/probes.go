package routes

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func (engine *Engine) routeAdapterValid(job *Job) bool {
	a, ok := engine.host.Adapter(job.AdapterID)

	return engine.ctx.Err() == nil && ok && a.Available && a.IPv4 == job.source && a.Index == job.index && a.ICMPSafe
}

func (engine *Engine) ttlProbe(job *Job, ttl int) collectors.ProbeResult {
	r := engine.collector.ICMP(engine.ctx, collectors.ProbeRequest{
		Source:         job.source,
		Address:        job.Address,
		InterfaceIndex: job.index,
		TTL:            ttl,
		PayloadBytes:   32,
		TimeoutMs:      job.TimeoutMs,
	})
	switch {
	case r.OK:
		r.Status = "EchoReply"
	case r.IPStatus == 11013:
		r.Status = "TimeExceeded"
	case r.IPStatus == 11010:
		r.Status = "TimedOut"
	case r.IPStatus == 11002 || r.IPStatus == 11003 || r.IPStatus == 11004 || r.IPStatus == 11005 || r.IPStatus == 11012:
		r.Status = fmt.Sprintf("Unreachable_%d", r.IPStatus)
	case r.IPStatus != 0:
		r.Status = fmt.Sprintf("ICMP_%d", r.IPStatus)
	}

	return r
}

func pauseContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (engine *Engine) collectRoute(job *Job) error {
	started := time.Now()
	for ttl := 1; ttl <= job.MaxHops; ttl++ {
		if !engine.routeAdapterValid(job) {
			return errors.New("The selected connection disconnected or changed its IP address. Repeat the route test after it recovers.")
		}

		hop := &Hop{
			TTL:           ttl,
			Addresses:     []string{},
			ProbeStatuses: []string{},
			Assessment:    "no_reply",
		}
		engine.mu.Lock()
		job.Hops = append(job.Hops, hop)
		engine.mu.Unlock()
		reply := engine.ttlProbe(job, ttl)
		engine.addHopReply(job, hop, reply)
		if err := engine.saveRoute(job); err != nil {
			return err
		}

		if reply.ToolError {
			return errors.New("Failed to run a TTL probe: " + reply.Status)
		}

		if reply.OK {
			break
		}

		if !pauseContext(engine.ctx, 80*time.Millisecond) {
			return engine.ctx.Err()
		}
	}

	engine.mu.Lock()
	job.Phase = "sampling"
	engine.mu.Unlock()
	for round := 1; round < job.SampleGoal && time.Since(started) < 90*time.Second; round++ {
		roundStarted := time.Now()
		if !engine.routeAdapterValid(job) {
			return errors.New("The connection changed during measurement.")
		}

		engine.mu.Lock()
		hops := append([]*Hop(nil), job.Hops...)
		engine.mu.Unlock()
		var wg sync.WaitGroup
		limit := make(chan struct{}, 8)
		for i, hop := range hops {
			select {
			case limit <- struct{}{}:
			case <-engine.ctx.Done():
				wg.Wait()

				return engine.ctx.Err()
			}

			wg.Add(1)
			go func(i int, hop *Hop) {
				defer wg.Done()
				defer func() {
					<-limit
				}()
				if !pauseContext(engine.ctx, time.Duration(i%4)*25*time.Millisecond) {
					return
				}

				if engine.routeAdapterValid(job) {
					engine.addHopReply(job, hop, engine.ttlProbe(job, hop.TTL))
				}
			}(i, hop)
		}

		wg.Wait()
		if err := engine.saveRoute(job); err != nil {
			return err
		}

		rest := engine.options.SamplingInterval - time.Since(roundStarted)
		if rest > 0 && !pauseContext(engine.ctx, rest) {
			return engine.ctx.Err()
		}
	}

	return nil
}
