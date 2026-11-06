// Package routes owns bounded TTL route jobs independently of HTTP and monitoring.
package routes

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

type Host interface {
	Adapter(string) (collectors.Adapter, bool)
	HasICMPTarget(string, string) bool
	LogDirectory() string
	Event(string, string)
}

type Options struct {
	SampleGoal       int
	TimeoutMs        int
	MaxHops          int
	SamplingInterval time.Duration
}

type Engine struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	host      Host
	collector collectors.Collector
	routes    map[string]*Job
	options   Options
	wg        sync.WaitGroup
	closed    bool
	workers   int
}

func New(ctx context.Context, host Host, collector collectors.Collector, options Options) *Engine {
	if options.SampleGoal <= 0 {
		options.SampleGoal = 20
	}

	if options.TimeoutMs <= 0 {
		options.TimeoutMs = 700
	}

	if options.MaxHops <= 0 {
		options.MaxHops = 25
	}

	if options.SamplingInterval <= 0 {
		options.SamplingInterval = time.Second
	}

	ctx, cancel := context.WithCancel(ctx)

	return &Engine{
		ctx:       ctx,
		cancel:    cancel,
		host:      host,
		collector: collector,
		routes:    map[string]*Job{},
		options:   options,
	}
}

func (engine *Engine) Active() bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.workers > 0 {
		return true
	}

	for _, job := range engine.routes {
		if job.State == "running" {
			return true
		}
	}

	return false
}

func (engine *Engine) Restore(jobs map[string]*Job) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.workers > 0 {
		return errors.New("A route test is still running.")
	}

	engine.routes = jobs
	if engine.routes == nil {
		engine.routes = map[string]*Job{}
	}

	return nil
}

func (engine *Engine) Close() {
	engine.mu.Lock()
	engine.closed = true
	engine.cancel()
	engine.mu.Unlock()
	engine.wg.Wait()
}

func iso() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func floatPtr(v float64) *float64 {
	return &v
}

func stringPtr(v string) *string {
	return &v
}
