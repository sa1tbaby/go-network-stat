package networkmanager

import (
	"strconv"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

type target struct {
	ID             string
	AdapterID      string
	AdapterName    string
	Kind           string
	Address        string
	Protocol       string
	Port           int
	Pending        bool
	HistoricalOnly bool
	NextDue        time.Time
	Recent         []Point
	Totals         Metrics
	Sum            float64
	JitterSum      float64
	JitterCount    int64
	Previous       *float64
	Histogram      [10001]int64
}

func targetID(adapter, protocol, address string, port int) string {
	return adapter + "|" + protocol + "|" + address + "|" + strconv.Itoa(port)
}

func (m *Manager) ensureTargetLocked(adapter collectors.Adapter, kind, address, protocol string, port int) {
	id := targetID(adapter.ID, protocol, address, port)
	monitoredTarget, ok := m.targets[id]
	if !ok {
		m.targets[id] = &target{
			ID:          id,
			AdapterID:   adapter.ID,
			AdapterName: adapter.Name,
			Kind:        kind,
			Address:     address,
			Protocol:    protocol,
			Port:        port,
			Recent:      []Point{},
		}
	} else {
		monitoredTarget.HistoricalOnly = false
		monitoredTarget.Kind = kind
		monitoredTarget.AdapterName = adapter.Name
	}
}

func (m *Manager) ensureAdapterTargetsLocked(adapter collectors.Adapter) {
	if !adapter.Available {
		return
	}

	if adapter.ICMPSafe {
		if adapter.Gateway != "" {
			m.ensureTargetLocked(adapter, "gateway", adapter.Gateway, "icmp", 0)
		}

		m.ensureTargetLocked(adapter, "internet", "1.1.1.1", "icmp", 0)
		m.ensureTargetLocked(adapter, "internet", "8.8.8.8", "icmp", 0)
	}

	m.ensureTargetLocked(adapter, "internet", "1.1.1.1", "tcp", 443)
	for _, custom := range m.custom.All() {
		if adapter.ICMPSafe {
			m.ensureTargetLocked(adapter, "custom", custom.Address, "icmp", 0)
		}

		if custom.Port > 0 {
			m.ensureTargetLocked(adapter, "custom", custom.Address, "tcp", custom.Port)
		}
	}
}
