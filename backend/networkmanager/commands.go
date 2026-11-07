package networkmanager

import (
	"errors"
	"time"
)

func (m *Manager) command(fn func() (any, error)) (any, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.Stopping() {
		return nil, errors.New("The collector is shutting down.")
	}

	return fn()
}

func (m *Manager) SetInterval(ms int) (any, error) {
	return m.command(func() (any, error) {
		if ms != 250 && ms != 1000 {
			return nil, errors.New("The interval must be 250 or 1000 ms.")
		}

		m.mu.Lock()
		m.interval = ms
		m.eventLocked("interval", "ICMP interval: "+fmtInterval(ms)+" ms")
		m.mu.Unlock()

		return map[string]bool{"ok": true}, nil
	})
}

func fmtInterval(ms int) string {
	if ms == 250 {
		return "250"
	}

	return "1000"
}

func (m *Manager) SetMeasuring(enabled bool) (any, error) {
	return m.command(func() (any, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.measuring != enabled {
			m.measuring = enabled
			for _, target := range m.targets {
				target.Previous = nil
				target.NextDue = time.Time{}
			}

			if enabled {
				m.eventLocked("measurement_start", "Measurements started")
			} else {
				m.eventLocked("measurement_pause", "Measurements paused; the dashboard remains available")
			}
		}

		return map[string]bool{"ok": true}, nil
	})
}
