package networkmanager

func (m *Manager) Event(kind, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventLocked(kind, message)
}

func (m *Manager) eventLocked(kind, message string) {
	event := EventRow{
		Time:    iso(),
		Kind:    kind,
		Message: message,
	}
	m.events = append(m.events, event)
	if len(m.events) > 100 {
		m.events = m.events[len(m.events)-100:]
	}

	if m.writers != nil {
		_ = m.writers.AppendEvent(event)
	}
}

type EventRow struct {
	Time    string `json:"time"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (m *Manager) Mark() (any, error) {
	return m.command(func() (any, error) {
		m.Event("user_mark", "User marked an issue")

		return map[string]bool{"ok": true}, nil
	})
}
