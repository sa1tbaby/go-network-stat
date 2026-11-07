package diagnostics

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func (s *Service) Status() any {
	root := s.host.LogDirectory()
	parent := filepath.Join(root, "diagnosis-runs")
	runs := []map[string]any{}
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if !entry.IsDir() || !diagnosisSafeID(entry.Name()) {
			continue
		}

		id := entry.Name()
		dir := filepath.Join(parent, id)
		meta, err := diagnosisManifest(dir, id)
		if err != nil {
			continue
		}

		network := diagnosisReadJSON(filepath.Join(dir, "diagnostics-live.json"))
		radio := diagnosisReadJSON(filepath.Join(dir, "radio-live.json"))
		ethernet := diagnosisReadJSON(filepath.Join(dir, "ethernet-live.json"))
		runtime := diagnosisReadJSON(filepath.Join(dir, "runtime.json"))
		running := false
		stopRequested, _ := runtime["stopRequested"].(bool)
		errorText := diagnosisString(runtime, "error")
		s.mu.Lock()
		j := s.jobs[dir]
		s.mu.Unlock()
		if j != nil {
			j.mu.Lock()
			running = j.running
			stopRequested = j.stopRequested
			errorText = j.err
			j.mu.Unlock()
		} else {
			running = diagnosisRecoveredAlive(dir)
			previous, _ := runtime["running"].(bool)
			networkRunning, _ := network["running"].(bool)
			if !running && (previous || networkRunning) {
				errorText = "The previous process exited before the test finished."
			}
		}

		if network["appId"] != "codex-single-pc-diagnostics" {
			network = nil
		}

		if radio["kind"] != "wifi_radio" {
			radio = nil
		}

		if ethernet["kind"] != "ethernet_line" {
			ethernet = nil
		}

		if meta["kind"] != "wifi" {
			radio = nil
		}

		if meta["kind"] != "ethernet" {
			ethernet = nil
		}

		if errorText == "" {
			errorText = diagnosisString(network, "error")
		}

		started := diagnosisString(meta, "started")
		label := diagnosisString(meta, "adapterName")
		if at, e := time.Parse(time.RFC3339Nano, started); e == nil {
			label = at.Local().Format("02.01 15:04:05.000") + " · " + label
		}

		runs = append(runs, map[string]any{
			"id":            id,
			"label":         label,
			"adapterName":   meta["adapterName"],
			"started":       started,
			"running":       running,
			"stopRequested": stopRequested,
			"error":         nullableDiagnosisError(errorText),
			"network":       network,
			"radio":         radio,
			"ethernet":      s.correlateSaved(ethernet, network, dir),
		})
	}
	// Older stores used a top-level live file and diagnostics/*/summary.json.
	addLegacy := func(n, r map[string]any, id string) {
		if n["appId"] != "codex-single-pc-diagnostics" {
			return
		}

		for _, run := range runs {
			if run["started"] == n["started"] && run["adapterName"] == n["adapterName"] {
				return
			}
		}

		if r["kind"] != "wifi_radio" || r["adapterName"] != n["adapterName"] {
			r = nil
		}

		if r != nil {
			a, ea := time.Parse(time.RFC3339Nano, diagnosisString(r, "started"))
			b, eb := time.Parse(time.RFC3339Nano, diagnosisString(n, "started"))
			if ea != nil || eb != nil || math.Abs(a.Sub(b).Seconds()) > 5 {
				r = nil
			}
		}

		runs = append(runs, map[string]any{
			"id":            id,
			"label":         n["adapterName"],
			"adapterName":   n["adapterName"],
			"started":       n["started"],
			"running":       false,
			"stopRequested": false,
			"error":         n["error"],
			"network":       n,
			"radio":         r,
			"ethernet":      nil,
		})
	}
	addLegacy(diagnosisReadJSON(filepath.Join(root, "diagnostics-live.json")), diagnosisReadJSON(filepath.Join(root, "radio-live.json")), "legacy-current")
	legacyEntries, _ := os.ReadDir(filepath.Join(root, "diagnostics"))
	for _, entry := range legacyEntries {
		if entry.IsDir() && diagnosisSafeID(entry.Name()) {
			addLegacy(diagnosisReadJSON(filepath.Join(root, "diagnostics", entry.Name(), "summary.json")), nil, "legacy-"+entry.Name())
		}
	}

	sort.Slice(runs, func(i, j int) bool {
		return diagnosisString(runs[i], "started") > diagnosisString(runs[j], "started")
	})
	archived := 0
	visible := make([]map[string]any, 0, len(runs))
	active := 0
	for _, run := range runs {
		if run["running"] == true {
			active++
			visible = append(visible, run)
		} else if archived < 20 {
			archived++
			visible = append(visible, run)
		}
	}

	index := diagnosisReadJSON(filepath.Join(root, "diagnosis-index.json"))
	activeID := diagnosisString(index, "activeRunId")
	var current map[string]any
	for _, run := range visible {
		if run["id"] == activeID {
			current = run
			break
		}
	}

	if current == nil && len(visible) > 0 {
		current = visible[0]
	}

	return map[string]any{
		"running":          active > 0,
		"activeCount":      active,
		"maxConcurrent":    4,
		"storageDirectory": root,
		"started":          current["started"],
		"error":            current["error"],
		"currentRunId":     current["id"],
		"network":          current["network"],
		"radio":            current["radio"],
		"ethernet":         current["ethernet"],
		"runs":             visible,
	}
}
