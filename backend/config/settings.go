// Package config owns persistent application settings and legacy layout imports.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

type Config struct {
	LogDirectory        string `json:"logDirectory"`
	StateDirectory      string `json:"-"`
	RuntimeDirectory    string `json:"-"`
	ServiceLogDirectory string `json:"-"`
	DataDirectory       string `json:"-"`
	IntervalMs          int    `json:"intervalMs"`
	StartPaused         bool   `json:"startPaused"`
	Port                int    `json:"port"`
}

// Load uses the new layout first, then imports configuration from the supplied
// legacy directories. Existing measurement files are never moved or rewritten.
func Load(layout data.Layout, legacy []string) (Config, error) {
	cfg := Config{
		LogDirectory: layout.Statistics,
		IntervalMs:   1000,
		Port:         8765,
	}
	if err := layout.Create(); err != nil {
		return cfg, err
	}
	// An explicit --data-dir can also be an old, flat directory.
	for _, dir := range append([]string{layout.Root}, legacy...) {
		if err := importConfig(layout.Config, dir); err != nil {
			return cfg, err
		}
	}

	settingsPath := filepath.Join(layout.Config, "settings.json")
	content, err := os.ReadFile(settingsPath)
	if err == nil {
		if err := json.Unmarshal(bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf}), &cfg); err != nil {
			return cfg, fmt.Errorf("settings.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return cfg, err
	}

	cfg.StateDirectory, cfg.RuntimeDirectory = layout.Config, layout.Runtime
	cfg.ServiceLogDirectory, cfg.DataDirectory = layout.Logs, layout.Root
	recoverLogDirectory(&cfg, layout.Config, settingsPath)
	if !filepath.IsAbs(cfg.LogDirectory) {
		return cfg, errors.New("The log path in settings must be absolute.")
	}

	if cfg.Port < 1024 || cfg.Port > 65515 {
		cfg.Port = 8765
	}

	if cfg.IntervalMs != 250 {
		cfg.IntervalMs = 1000
	}

	return cfg, nil
}

func Save(layout data.Layout, cfg Config) error {
	if !filepath.IsAbs(cfg.LogDirectory) {
		return errors.New("log directory must be absolute")
	}

	return fileutil.WriteJSON(filepath.Join(layout.Config, "settings.json"), cfg)
}
