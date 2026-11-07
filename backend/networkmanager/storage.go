package networkmanager

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func (m *Manager) initializeStorage() error {
	dir := m.cfg.LogDirectory
	if dir == "" {
		settings := filepath.Join(m.cfg.StateDirectory, "log-settings.json")
		if err := storage.NoLink(settings); err != nil {
			return err
		}

		if b, err := os.ReadFile(settings); err == nil {
			var data struct {
				AppID     string `json:"appId"`
				Directory string `json:"directory"`
			}
			if json.Unmarshal(storage.WithoutBOM(b), &data) != nil || data.AppID != appID {
				return errors.New("Unknown log-settings.json format.")
			}

			dir = data.Directory
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	if dir == "" {
		dir = filepath.Join(m.cfg.StateDirectory, "logs", time.Now().UTC().Format("20060102-150405")+"-"+strconv.Itoa(os.Getpid()))
	}

	var err error
	dir, err = storage.FullPath(dir)
	if err != nil {
		return err
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

	m.logDir = dir
	m.writers = writers
	m.applyStoreLocked(data)

	return nil
}

func (m *Manager) saveStoreSettings(dir string) error {
	if err := storage.PublishJSON(filepath.Join(dir, "log-store.json"), map[string]any{
		"appId":     appID,
		"version":   1,
		"createdAt": iso(),
	}); err != nil {
		return err
	}

	return storage.PublishJSON(filepath.Join(m.cfg.StateDirectory, "log-settings.json"), map[string]any{"appId": appID, "directory": dir})
}

func (m *Manager) saveSnapshotLocked() error {
	return storage.PublishJSON(filepath.Join(m.logDir, "status.json"), m.snapshotLocked())
}
