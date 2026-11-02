package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

func recoverLogDirectory(cfg *Config, directory, settingsPath string) {
	// Recover a browser folder selection made after the last desktop save.
	backendSettings := filepath.Join(directory, "log-settings.json")
	if backendInfo, e := os.Stat(backendSettings); e == nil {
		clientInfo, ce := os.Stat(settingsPath)
		if ce != nil || backendInfo.ModTime().After(clientInfo.ModTime()) {
			var saved struct {
				AppID     string `json:"appId"`
				Directory string `json:"directory"`
			}
			if raw, e := os.ReadFile(backendSettings); e == nil && json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &saved) == nil && saved.AppID == "codex-internet-stability" && filepath.IsAbs(saved.Directory) {
				cfg.LogDirectory = saved.Directory
			}
		}
	}
}
