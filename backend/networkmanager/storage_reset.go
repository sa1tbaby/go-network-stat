package networkmanager

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/routes"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func (m *Manager) ResetLogs(expected string) (any, error) {
	return m.command(func() (any, error) {
		return m.resetStore(expected)
	})
}

func ownedPath(dir, path string) error {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("Deletion outside the selected folder is not allowed.")
	}

	return storage.NoLink(path)
}

// Only recognized application files are removed. Unrelated user files survive.
func ownedExtraFiles(dir string) ([]string, error) {
	files := []string{}
	add := func(path string) error {
		if err := ownedPath(dir, path); err != nil {
			return err
		}

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files = append(files, path)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}

		return nil
	}
	for _, item := range [][3]string{
		{"diagnostics-live.json", "appId", "codex-single-pc-diagnostics"},
		{"radio-live.json", "kind", "wifi_radio"},
		{"ethernet-live.json", "kind", "ethernet_line"},
		{"diagnosis-index.json", "appId", "codex-network-diagnosis-index"},
	} {
		path := filepath.Join(dir, item[0])
		if storage.JSONHas(path, item[1], item[2]) {
			if err := add(path); err != nil {
				return nil, err
			}
		}
	}

	stop := filepath.Join(dir, "diagnostics-stop.flag")
	if b, _ := os.ReadFile(stop); strings.TrimSpace(string(b)) == "stop" {
		if err := add(stop); err != nil {
			return nil, err
		}
	}

	routeFiles, _ := filepath.Glob(filepath.Join(dir, "routes", "*.json"))
	for _, path := range routeFiles {
		if err := ownedPath(dir, path); err != nil {
			return nil, err
		}

		var job routes.Job
		b, _ := os.ReadFile(path)
		if storage.JSONHas(path, "appId", "codex-internet-stability-route") || (json.Unmarshal(storage.WithoutBOM(b), &job) == nil && job.AdapterID != "" && storage.SamePath(job.LogFile, path)) {
			if err := add(path); err != nil {
				return nil, err
			}
		}
	}

	for _, parent := range []string{"diagnostics", "radio", "ethernet", "diagnosis-runs"} {
		base := filepath.Join(dir, parent)
		if err := ownedPath(dir, base); err != nil {
			return nil, err
		}

		entries, err := os.ReadDir(base)
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}

			child := filepath.Join(base, entry.Name())
			if err := ownedPath(dir, child); err != nil {
				return nil, err
			}

			if parent == "diagnosis-runs" {
				meta := filepath.Join(child, "run.json")
				if storage.JSONHas(meta, "appId", "codex-network-diagnosis-run") && storage.JSONHas(meta, "id", entry.Name()) {
					if err := add(meta); err != nil {
						return nil, err
					}

					runtime := filepath.Join(child, "runtime.json")
					if err := ownedPath(dir, runtime); err != nil {
						return nil, err
					}

					if storage.JSONHas(runtime, "appId", "codex-diagnosis-runtime") && storage.JSONHas(runtime, "id", entry.Name()) {
						if err := add(runtime); err != nil {
							return nil, err
						}
					}

					for _, item := range [][3]string{
						{"diagnostics-live.json", "appId", "codex-single-pc-diagnostics"},
						{"radio-live.json", "kind", "wifi_radio"},
						{"ethernet-live.json", "kind", "ethernet_line"},
					} {
						path := filepath.Join(child, item[0])
						if err := ownedPath(dir, path); err != nil {
							return nil, err
						}

						if storage.JSONHas(path, item[1], item[2]) {
							if err := add(path); err != nil {
								return nil, err
							}
						}
					}

					stop := filepath.Join(child, "diagnostics-stop.flag")
					if err := ownedPath(dir, stop); err != nil {
						return nil, err
					}

					if b, _ := os.ReadFile(stop); strings.TrimSpace(string(b)) == "stop" {
						if err := add(stop); err != nil {
							return nil, err
						}
					}

					network := filepath.Join(child, "diagnostics")
					if err := ownedPath(dir, network); err != nil {
						return nil, err
					}

					entries, readErr := os.ReadDir(network)
					if readErr != nil && !os.IsNotExist(readErr) {
						return nil, readErr
					}

					for _, entry := range entries {
						path := filepath.Join(network, entry.Name())
						if err := ownedPath(dir, path); err != nil {
							return nil, err
						}

						if entry.IsDir() {
							if err := ownedDataFiles(dir, path, "network", add); err != nil {
								return nil, err
							}
						}
					}

					for _, kind := range []string{"radio", "ethernet"} {
						if err := ownedDataFiles(dir, filepath.Join(child, kind), kind, add); err != nil {
							return nil, err
						}
					}
				}
			} else {
				kind := parent
				if kind == "diagnostics" {
					kind = "network"
				}

				if err := ownedDataFiles(dir, child, kind, add); err != nil {
					return nil, err
				}
			}
		}
	}

	return files, nil
}

func ownedDataFiles(root, directory, kind string, add func(string) error) error {
	if err := ownedPath(root, directory); err != nil {
		return err
	}

	summary := filepath.Join(directory, "summary.json")
	if err := ownedPath(root, summary); err != nil {
		return err
	}

	names := []string{}
	switch {
	case kind == "network" && storage.JSONHas(summary, "appId", "codex-single-pc-diagnostics"):
		names = []string{"summary.json", "probes.csv"}
	case kind == "radio" && storage.JSONHas(summary, "kind", "wifi_radio"):
		names = []string{"summary.json", "radio.csv", "radio-raw.jsonl"}
	case kind == "ethernet" && storage.JSONHas(summary, "kind", "ethernet_line"):
		names = []string{"summary.json", "ethernet.csv", "ethernet-events.jsonl"}
	}

	for _, name := range names {
		if err := add(filepath.Join(directory, name)); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) resetStore(expected string) (any, error) {
	if err := m.storageIdle(false); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	dir, err := storage.FullPath(expected)
	if err != nil || !storage.SamePath(dir, m.logDir) {
		return nil, errors.New("The folder changed after confirmation. Repeat the reset for the current folder.")
	}

	if err = storage.Validate(m.logDir); err != nil {
		return nil, err
	}

	files, err := ownedExtraFiles(m.logDir)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if err = os.Remove(file); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	for _, file := range files {
		for parent := filepath.Dir(file); !storage.SamePath(parent, m.logDir); parent = filepath.Dir(parent) {
			if err := os.Remove(parent); err != nil {
				break
			}
		}
	}

	if err = m.writers.Reset(); err != nil {
		return nil, err
	}

	m.applyStoreLocked(emptyStore())
	m.selectionState = "idle"
	m.selectionError = ""
	m.selectionDirectory = m.logDir
	m.eventLocked("logs_reset", "Logs and statistics reset in folder: "+m.logDir)
	if err = m.notifyStateLocked(); err != nil {
		return nil, err
	}

	if err = m.saveSnapshotLocked(); err != nil {
		return nil, err
	}

	return map[string]any{"ok": true, "directory": m.logDir}, nil
}
