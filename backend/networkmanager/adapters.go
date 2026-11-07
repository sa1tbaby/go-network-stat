package networkmanager

import (
	"fmt"
	"strconv"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func (m *Manager) Adapter(id string) (collectors.Adapter, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.adapters[id]

	return a, ok
}

func (m *Manager) refresh() {
	adapters, err := m.collector.Adapters(m.ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.eventLocked("adapter_read_error", err.Error())

		return
	}

	presentAdapters := map[string]bool{}
	for _, adapter := range adapters {
		presentAdapters[adapter.ID] = true
		previousAdapter, exists := m.adapters[adapter.ID]
		adapter.LastChange = previousAdapter.LastChange
		if !exists {
			adapter.LastChange = iso()
			m.eventLocked("adapter_detected", adapter.Name+": "+adapter.State)
		} else {
			if previousAdapter.State != adapter.State || previousAdapter.IPv4 != adapter.IPv4 || previousAdapter.Gateway != adapter.Gateway || (adapter.Kind == "ethernet" && previousAdapter.SpeedMbps != adapter.SpeedMbps) {
				adapter.LastChange = iso()
				m.eventLocked("link_change", fmt.Sprintf("%s: %s → %s; IP %s; %.0f Mbps", adapter.Name, previousAdapter.State, adapter.State, adapter.IPv4, adapter.SpeedMbps))
			}

			if previousAdapter.RxErrors != adapter.RxErrors || previousAdapter.TxErrors != adapter.TxErrors || previousAdapter.RxDiscards != adapter.RxDiscards || previousAdapter.TxDiscards != adapter.TxDiscards {
				m.eventLocked("adapter_counters", fmt.Sprintf("%s: Δ errors RX=%d, TX=%d, discards RX=%d, TX=%d", adapter.Name, adapter.RxErrors-previousAdapter.RxErrors, adapter.TxErrors-previousAdapter.TxErrors, adapter.RxDiscards-previousAdapter.RxDiscards, adapter.TxDiscards-previousAdapter.TxDiscards))
			}
		}

		m.adapters[adapter.ID] = adapter
		m.ensureAdapterTargetsLocked(adapter)
		if m.writers != nil {
			_ = m.writers.AppendAdapter([]string{
				iso(),
				strconv.FormatInt(m.writers.Elapsed().Milliseconds(), 10),
				adapter.Name,
				strconv.Itoa(adapter.Index),
				adapter.State,
				adapter.IPv4,
				adapter.Gateway,
				number(adapter.SpeedMbps),
				fmt.Sprint(adapter.RxErrors),
				fmt.Sprint(adapter.TxErrors),
				fmt.Sprint(adapter.RxDiscards),
				fmt.Sprint(adapter.TxDiscards),
			})
		}
	}

	for id, adapter := range m.adapters {
		if !presentAdapters[id] && adapter.State != "Historical" && adapter.State != "Missing" {
			adapter.State = "Missing"
			adapter.Available = false
			adapter.LastChange = iso()
			m.adapters[id] = adapter
			m.eventLocked("link_change", adapter.Name+": device not present")
		}
	}
}
