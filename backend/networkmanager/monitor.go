package networkmanager

import (
	"fmt"
	"strconv"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func (m *Manager) monitor() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	refresh := time.Now()
	save := time.Now()
	previous := time.Now()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-ticker.C:
			if now.Sub(previous) > 2*time.Second {
				m.Event("sampling_gap", fmt.Sprintf("Collector sampling gap: %d ms", now.Sub(previous).Milliseconds()))
			}

			previous = now
			if !now.Before(refresh) {
				refresh = now.Add(500 * time.Millisecond)
				m.refresh()
			}

			m.schedule(now)
			if !now.Before(save) {
				save = now.Add(2 * time.Second)
				m.mu.Lock()
				_ = m.saveSnapshotLocked()
				m.mu.Unlock()
			}
		}
	}
}

func (m *Manager) schedule(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.measuring || m.stopping {
		return
	}

	for _, monitoredTarget := range m.targets {
		adapter, ok := m.adapters[monitoredTarget.AdapterID]
		if !ok || !adapter.Available || monitoredTarget.HistoricalOnly || monitoredTarget.Pending || now.Before(monitoredTarget.NextDue) || (monitoredTarget.Kind == "gateway" && monitoredTarget.Address != adapter.Gateway) || (monitoredTarget.Protocol == "icmp" && !adapter.ICMPSafe) {
			continue
		}

		monitoredTarget.Pending = true
		interval := time.Duration(m.interval) * time.Millisecond
		if monitoredTarget.Protocol == "tcp" {
			interval = 5 * time.Second
		}

		monitoredTarget.NextDue = now.Add(interval)
		m.wg.Add(1)
		go func(adapter collectors.Adapter, monitoredTarget *target) {
			defer m.wg.Done()
			request := collectors.ProbeRequest{
				Source:         adapter.IPv4,
				Address:        monitoredTarget.Address,
				InterfaceIndex: adapter.Index,
				PayloadBytes:   32,
				TimeoutMs:      1200,
			}
			sentAt := time.Now().UnixMilli()
			var reply collectors.ProbeResult
			if monitoredTarget.Protocol == "tcp" {
				reply = m.collector.TCP(m.ctx, request, monitoredTarget.Port)
			} else {
				reply = m.collector.ICMP(m.ctx, request)
			}

			rttMs := reply.RTTMs
			if !reply.OK {
				rttMs = nil
			}

			m.record(adapter, monitoredTarget, Point{
				T:         sentAt,
				OK:        reply.OK,
				RTTMs:     rttMs,
				Status:    reply.Status,
				ToolError: reply.ToolError,
			})
		}(adapter, monitoredTarget)
	}
}

func (m *Manager) record(adapter collectors.Adapter, monitoredTarget *target, point Point) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.targets[monitoredTarget.ID] != monitoredTarget {
		return
	}

	monitoredTarget.Pending = false
	restorePoint(monitoredTarget, point, time.Now().UnixMilli())
	cutoff := time.Now().UnixMilli() - 360000
	recent := monitoredTarget.Recent[:0]
	for _, row := range monitoredTarget.Recent {
		if row.T >= cutoff {
			recent = append(recent, row)
		}
	}

	monitoredTarget.Recent = recent
	if point.ToolError {
		m.eventLocked("probe_tool_error", adapter.Name+" "+monitoredTarget.Address+": "+point.Status)
	} else if !point.OK {
		m.eventLocked("probe_failure", adapter.Name+" "+monitoredTarget.Protocol+" "+monitoredTarget.Address+": "+point.Status)
	}

	if m.writers != nil {
		rtt := ""
		if point.RTTMs != nil {
			rtt = number(*point.RTTMs)
		}

		_ = m.writers.AppendProbe([]string{
			iso(),
			fmt.Sprint(point.T),
			adapter.Name,
			adapter.IPv4,
			fmt.Sprint(adapter.Index),
			monitoredTarget.Protocol,
			monitoredTarget.Address,
			fmt.Sprint(monitoredTarget.Port),
			strconv.FormatBool(point.OK),
			point.Status,
			rtt,
			strconv.FormatBool(point.ToolError),
		})
	}
}
