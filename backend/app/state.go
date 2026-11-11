package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func (a *App) publishState(state networkmanager.State) error {
	a.mu.Lock()
	port := a.port
	a.mu.Unlock()
	value := map[string]any{
		"appId":   "codex-internet-stability",
		"pid":     os.Getpid(),
		"started": state.Started,
		"url":     fmt.Sprintf("http://127.0.0.1:%d/", port),
		"logDir":  state.LogDirectory,
	}
	if err := storage.PublishJSON(filepath.Join(a.cfg.RuntimeDirectory, "run-state.json"), value); err != nil {
		return err
	}

	if a.cfg.ReadyFile != "" {
		return storage.PublishJSON(a.cfg.ReadyFile, value)
	}

	return nil
}
