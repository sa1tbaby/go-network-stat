package networkmanager

import (
	"errors"
)

type State struct {
	Started      string
	LogDirectory string
	IntervalMs   int
	Measuring    bool
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.stateLocked()
}

func (m *Manager) stateLocked() State {
	return State{
		Started:      m.started,
		LogDirectory: m.logDir,
		IntervalMs:   m.interval,
		Measuring:    m.measuring && !m.stopping,
	}
}

func (m *Manager) notifyStateLocked() error {
	if m.cfg.StateChanged != nil {
		return m.cfg.StateChanged(m.stateLocked())
	}

	return nil
}

func (m *Manager) LogDirectory() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.logDir
}

func (m *Manager) StateDirectory() string {
	return m.cfg.StateDirectory
}

func (m *Manager) CheckReady() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopping || m.ctx.Err() != nil {
		return errors.New("The collector is shutting down.")
	}

	if m.choosing {
		return errors.New("Finish selecting the log folder first.")
	}

	return nil
}

func (m *Manager) Stopping() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.stopping
}
