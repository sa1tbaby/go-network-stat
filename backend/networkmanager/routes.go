package networkmanager

func (m *Manager) StartRoute(adapter, address string) (any, error) {
	return m.command(func() (any, error) {
		if err := m.CheckReady(); err != nil {
			return nil, err
		}

		return m.routeEngine.Start(adapter, address)
	})
}

func (m *Manager) RouteStatus(adapter, address string) any {
	return m.routeEngine.Status(adapter, address)
}

// HasICMPTarget is the route engine's read-only domain port.
func (m *Manager) HasICMPTarget(adapter, address string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.targets {
		if t.AdapterID == adapter && t.Address == address && t.Protocol == "icmp" {
			return true
		}
	}

	return false
}
