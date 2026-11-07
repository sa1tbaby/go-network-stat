package networkmanager

import (
	"encoding/json"
	"sort"
	"time"
)

type targetSnapshot struct {
	AdapterID    string        `json:"adapterId"`
	AdapterName  string        `json:"adapterName"`
	Kind         string        `json:"kind"`
	Address      string        `json:"address"`
	Protocol     string        `json:"protocol"`
	Port         int           `json:"port"`
	Details      *CustomTarget `json:"details"`
	Reachability string        `json:"reachability"`
	All          Metrics       `json:"all"`
	Last60       Metrics       `json:"last60"`
	Last300      Metrics       `json:"last300"`
	InFlight     bool          `json:"inFlight"`
	History      []Point       `json:"history"`
}

func (m *Manager) snapshotLocked() any {
	now := time.Now().UnixMilli()
	adapters := make([]map[string]any, 0, len(m.adapters))
	for _, key := range sortedKeys(m.adapters) {
		adapter := m.adapters[key]
		encodedAdapter, _ := json.Marshal(adapter)
		var view map[string]any
		_ = json.Unmarshal(encodedAdapter, &view)
		view["ipv4"] = nullableString(adapter.IPv4)
		view["gateway"] = nullableString(adapter.Gateway)
		view["lastChange"] = nullableString(adapter.LastChange)
		adapters = append(adapters, view)
	}

	targets := make([]targetSnapshot, 0, len(m.targets))
	for _, key := range sortedKeys(m.targets) {
		monitoredTarget := m.targets[key]
		reach := "waiting"
		if monitoredTarget.Totals.Received > 0 {
			reach = "responding"
		} else if monitoredTarget.Totals.Sent >= 5 {
			reach = "no_response_yet"
		}

		history := []Point{}
		for _, point := range monitoredTarget.Recent {
			if point.T >= now-300000 {
				history = append(history, point)
			}
		}

		sort.Slice(history, func(i, j int) bool {
			return history[i].T < history[j].T
		})
		var details *CustomTarget
		if customTarget, ok := m.custom.Get(monitoredTarget.Address); ok {
			copy := customTarget
			details = &copy
		}

		targets = append(targets, targetSnapshot{
			AdapterID:    monitoredTarget.AdapterID,
			AdapterName:  monitoredTarget.AdapterName,
			Kind:         monitoredTarget.Kind,
			Address:      monitoredTarget.Address,
			Protocol:     monitoredTarget.Protocol,
			Port:         monitoredTarget.Port,
			Details:      details,
			Reachability: reach,
			All:          allMetrics(monitoredTarget),
			Last60:       windowMetrics(monitoredTarget.Recent, now-60000),
			Last300:      windowMetrics(monitoredTarget.Recent, now-300000),
			InFlight:     monitoredTarget.Pending,
			History:      history,
		})
	}

	custom := m.custom.All()
	events := []EventRow{}
	for i := len(m.events) - 1; i >= 0 && len(events) < 40; i-- {
		events = append(events, m.events[i])
	}

	return map[string]any{
		"appId":         appID,
		"running":       m.measuring && !m.stopping && m.ctx.Err() == nil,
		"serverRunning": !m.stopping && m.ctx.Err() == nil,
		"customTargets": custom,
		"started":       m.started,
		"now":           iso(),
		"intervalMs":    m.interval,
		"timeoutMs":     1200,
		"logDir":        m.logDir,
		"adapters":      adapters,
		"targets":       targets,
		"recentEvents":  events,
	}
}

func (m *Manager) Snapshot() any {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.snapshotLocked()
}
