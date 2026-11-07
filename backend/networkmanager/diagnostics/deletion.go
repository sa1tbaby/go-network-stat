package diagnostics

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) Delete(id, expectedDirectory string) (any, error) {
	root := s.host.LogDirectory()
	if expectedDirectory != "" && !strings.EqualFold(filepath.Clean(root), filepath.Clean(expectedDirectory)) {
		return nil, errors.New("The log folder changed. Try deleting again.")
	}

	if !diagnosisSafeID(id) {
		return nil, errors.New("Specify the history record ID.")
	}

	if strings.HasPrefix(id, "legacy-") {
		return s.deleteLegacy(id, root)
	}

	dir := filepath.Join(root, "diagnosis-runs", id)
	if _, err := diagnosisManifest(dir, id); err != nil {
		return nil, err
	}

	if diagnosisRecoveredAlive(dir) {
		return nil, errors.New("Stop the running test first.")
	}

	s.mu.Lock()
	j := s.jobs[dir]
	s.mu.Unlock()
	if j != nil {
		j.mu.Lock()
		running := j.running
		j.mu.Unlock()
		if running {
			return nil, errors.New("Stop the running test first.")
		}
	}

	if err := deleteDiagnosisOwned(dir); err != nil {
		return nil, err
	}

	index := filepath.Join(root, "diagnosis-index.json")
	savedIndex := diagnosisReadJSON(index)
	if savedIndex["appId"] == "codex-network-diagnosis-index" && savedIndex["activeRunId"] == id {
		if err := os.Remove(index); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	s.mu.Lock()
	delete(s.jobs, dir)
	s.mu.Unlock()
	s.host.Event("diagnosis_deleted", "Deleted history record "+id)

	return map[string]any{"ok": true, "runId": id}, nil
}

func deleteDiagnosisOwned(dir string) error {
	if err := diagnosisOrdinaryPath(dir); err != nil {
		return err
	}

	remove := func(path string) error {
		if err := diagnosisOrdinaryPath(path); err != nil {
			return err
		}

		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}
	clearData := func(path, kind string) error {
		summary := diagnosisReadJSON(filepath.Join(path, "summary.json"))
		owned := summary["kind"] == kind
		if kind == "network" {
			owned = summary["appId"] == "codex-single-pc-diagnostics"
		}

		if !owned {
			return nil
		}

		names := []string{"summary.json"}
		switch kind {
		case "network":
			names = append(names, "probes.csv")
		case "wifi_radio":
			names = append(names, "radio.csv", "radio-raw.jsonl")
		case "ethernet_line":
			names = append(names, "ethernet.csv", "ethernet-events.jsonl")
		}

		for _, name := range names {
			if err := remove(filepath.Join(path, name)); err != nil {
				return err
			}
		}

		_ = os.Remove(path)

		return nil
	}
	for folder, kind := range map[string]string{"radio": "wifi_radio", "ethernet": "ethernet_line"} {
		if err := clearData(filepath.Join(dir, folder), kind); err != nil {
			return err
		}
	}

	parent := filepath.Join(dir, "diagnostics")
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if entry.IsDir() {
			if err := clearData(filepath.Join(parent, entry.Name()), "network"); err != nil {
				return err
			}
		}
	}

	_ = os.Remove(parent)
	for filename, marker := range map[string]string{
		"diagnostics-live.json": "codex-single-pc-diagnostics",
		"radio-live.json":       "wifi_radio",
		"ethernet-live.json":    "ethernet_line",
		"runtime.json":          "codex-diagnosis-runtime",
	} {
		path := filepath.Join(dir, filename)
		value := diagnosisReadJSON(path)
		if value["appId"] == marker || value["kind"] == marker {
			if err := remove(path); err != nil {
				return err
			}
		}
	}

	stop := filepath.Join(dir, "diagnostics-stop.flag")
	if data, err := os.ReadFile(stop); err == nil && strings.TrimSpace(string(data)) == "stop" {
		if err := remove(stop); err != nil {
			return err
		}
	}

	if err := remove(filepath.Join(dir, "run.json")); err != nil {
		return err
	}

	_ = os.Remove(dir)

	return nil
}

func (s *Service) deleteLegacy(id, root string) (any, error) {
	var selected map[string]any
	if id == "legacy-current" {
		selected = diagnosisReadJSON(filepath.Join(root, "diagnostics-live.json"))
	} else {
		selected = diagnosisReadJSON(filepath.Join(root, "diagnostics", strings.TrimPrefix(id, "legacy-"), "summary.json"))
	}

	if selected["appId"] != "codex-single-pc-diagnostics" {
		return nil, errors.New("History record not found.")
	}

	parent := filepath.Join(root, "diagnostics")
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(parent, entry.Name())
		summary := diagnosisReadJSON(filepath.Join(dir, "summary.json"))
		if summary["appId"] == selected["appId"] && summary["started"] == selected["started"] && summary["adapterName"] == selected["adapterName"] {
			if err := diagnosisOrdinaryPath(dir); err != nil {
				return nil, err
			}

			for _, name := range []string{"summary.json", "probes.csv"} {
				if err := diagnosisOrdinaryPath(filepath.Join(dir, name)); err != nil {
					return nil, err
				}

				if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
					return nil, err
				}
			}

			_ = os.Remove(dir)
		}
	}

	live := filepath.Join(root, "diagnostics-live.json")
	n := diagnosisReadJSON(live)
	if n["started"] == selected["started"] && n["adapterName"] == selected["adapterName"] {
		if err := os.Remove(live); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	// Older radio/Ethernet workers stored their histories beside diagnostics/.
	// Pair by adapter and start time, as the original archive did.
	for folder, kind := range map[string]string{"radio": "wifi_radio", "ethernet": "ethernet_line"} {
		matches := func(value map[string]any) bool {
			if value["kind"] != kind || value["adapterName"] != selected["adapterName"] {
				return false
			}

			a, ea := time.Parse(time.RFC3339Nano, diagnosisString(value, "started"))
			b, eb := time.Parse(time.RFC3339Nano, diagnosisString(selected, "started"))

			return ea == nil && eb == nil && math.Abs(a.Sub(b).Seconds()) <= 5
		}
		live := filepath.Join(root, folder+"-live.json")
		if matches(diagnosisReadJSON(live)) {
			if err := diagnosisOrdinaryPath(live); err != nil {
				return nil, err
			}

			if err := os.Remove(live); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}

		entries, _ := os.ReadDir(filepath.Join(root, folder))
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			dir := filepath.Join(root, folder, entry.Name())
			if !matches(diagnosisReadJSON(filepath.Join(dir, "summary.json"))) {
				continue
			}

			names := []string{"summary.json", folder + ".csv"}
			if folder == "radio" {
				names = append(names, "radio-raw.jsonl")
			} else {
				names = append(names, "ethernet-events.jsonl")
			}

			for _, name := range names {
				path := filepath.Join(dir, name)
				if err := diagnosisOrdinaryPath(path); err != nil {
					return nil, err
				}

				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return nil, err
				}
			}

			_ = os.Remove(dir)
		}
	}

	return map[string]any{"ok": true, "runId": id}, nil
}
