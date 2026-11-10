// Package client implements the local panel API's verified HTTP client.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
)

// PauseFromStateFiles ignores stale/unrelated endpoints and verifies application identity.
func (c *Client) PauseFromStateFiles(paths []string) error {
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		var state struct {
			AppID string `json:"appId"`
			URL   string `json:"url"`
		}
		if err != nil || json.Unmarshal(raw, &state) != nil || state.AppID != appID || !ValidBackendURL(state.URL) {
			continue
		}

		if _, err = c.Status(state.URL); err != nil {
			continue
		}

		if err = c.command(state.URL, "api/stop"); err != nil {
			return fmt.Errorf("pause: %w", err)
		}

		return nil
	}

	return errors.New("The dashboard is not running. Open InternetStability.exe first.")
}

// PauseMonitor locates portable/current/legacy runtime state without opening a UI.
func PauseMonitor() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	paths := []string{filepath.Join(data.DefaultRoot(executable), "runtime", "run-state.json")}
	if layout, err := data.Resolve(""); err == nil {
		paths = append(paths, filepath.Join(layout.Runtime, "run-state.json"))
	}

	for _, legacy := range data.LegacyDirectories(executable) {
		paths = append(paths, filepath.Join(legacy, "run-state.json"))
	}

	return New().PauseFromStateFiles(paths)
}
