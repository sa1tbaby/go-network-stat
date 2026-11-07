package networkmanager

import (
	"errors"
	"os"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func (m *Manager) LogStatus() any {
	return m.logStatus()
}

func (m *Manager) SelectLogDirectory(path string) (any, error) {
	return m.command(func() (any, error) {
		if err := m.switchStore(path, false); err != nil {
			return nil, err
		}

		return map[string]any{"ok": true, "directory": m.LogDirectory()}, nil
	})
}

func (m *Manager) PickLogDirectory() (any, error) {
	return m.command(m.pickStore)
}

func (m *Manager) storageIdle(selecting bool) error {
	m.mu.Lock()
	choosing := m.choosing
	stopping := m.stopping || m.ctx.Err() != nil
	guard := m.activityGuard
	m.mu.Unlock()
	if stopping {
		return errors.New("The collector is shutting down.")
	}

	if choosing && !selecting {
		return errors.New("The folder picker is already open. Finish or cancel the selection.")
	}

	if guard != nil && guard() {
		return errors.New("Wait for detailed diagnostics, route tests, and game UDP detection to finish before changing the folder or resetting logs.")
	}

	if m.routeEngine.Active() {
		return errors.New("Wait for detailed diagnostics, route tests, and game UDP detection to finish before changing the folder or resetting logs.")
	}

	return nil
}

func (m *Manager) switchStore(requested string, selecting bool) error {
	if err := m.storageIdle(selecting); err != nil {
		return err
	}

	dir, err := storage.FullPath(requested)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if storage.SamePath(dir, m.logDir) {
		m.selectionState = "complete"
		m.selectionDirectory = dir
		m.selectionError = ""

		return nil
	}

	if err = storage.NoLink(dir); err != nil {
		return err
	}

	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	if err = storage.Validate(dir); err != nil {
		return err
	}

	data, err := readStore(dir)
	if err != nil {
		return err
	}

	writers, err := storage.Open(dir)
	if err != nil {
		return err
	}

	if err = m.saveStoreSettings(dir); err != nil {
		writers.Close()

		return err
	}

	old := m.writers
	m.writers = writers
	m.logDir = dir
	m.applyStoreLocked(data)
	_ = old.Close()
	m.selectionState = "complete"
	m.selectionDirectory = dir
	m.selectionError = ""
	if err = m.notifyStateLocked(); err != nil {
		return err
	}

	if err = m.saveSnapshotLocked(); err != nil {
		return err
	}

	m.eventLocked("log_folder", "Selected log folder: "+dir)
	if m.storageWarning != "" {
		m.eventLocked("log_import_warning", m.storageWarning)
	}

	return nil
}

func (m *Manager) logStatus() any {
	m.mu.Lock()
	defer m.mu.Unlock()

	return map[string]any{
		"directory": m.logDir,
		"choosing":  m.choosing,
		"busy":      m.choosing,
		"warning":   nullableString(m.storageWarning),
		"selection": map[string]any{
			"state":     m.selectionState,
			"directory": nullableString(m.selectionDirectory),
			"error":     nullableString(m.selectionError),
		},
	}
}

func (m *Manager) pickStore() (any, error) {
	if err := m.storageIdle(false); err != nil {
		return nil, err
	}

	if m.cfg.PickFolder == nil {
		return nil, errors.New("The folder picker is unavailable.")
	}

	m.mu.Lock()
	m.choosing = true
	m.selectionState = "choosing"
	m.selectionError = ""
	initial := m.logDir
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		dir, err := m.cfg.PickFolder(m.ctx, initial)
		m.opMu.Lock()
		defer m.opMu.Unlock()
		if err == nil && dir != "" {
			err = m.switchStore(dir, true)
		}

		m.mu.Lock()
		defer m.mu.Unlock()
		m.choosing = false
		if err != nil {
			m.selectionState = "error"
			m.selectionError = err.Error()
		} else if dir == "" {
			m.selectionState = "cancelled"
		}
	}()

	return map[string]any{"ok": true}, nil
}
