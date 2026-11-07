package diagnostics_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyBOMHistoryIsRecoveredWithoutRevivingDeadWorkers(t *testing.T) {
	s, h := fixture(t, time.Second)
	id := "20261107-120000-legacy"
	dir := filepath.Join(h.root, "diagnosis-runs", id)
	writeJSON(t, filepath.Join(dir, "run.json"), map[string]any{
		"appId":       "codex-network-diagnosis-run",
		"id":          id,
		"adapterName": "Ethernet",
		"kind":        "ethernet",
		"started":     "2026-11-07T12:00:00+03:00",
	}, true)
	writeJSON(t, filepath.Join(dir, "runtime.json"), map[string]any{
		"appId":   "codex-diagnosis-runtime",
		"id":      id,
		"running": true,
		"children": []any{map[string]any{
			"pid":               2147483647,
			"startTimeUtcTicks": int64(638000000000000001),
			"exitCode":          nil,
		}},
	}, true)
	status := object(t, s.Status())
	if status["activeCount"] != float64(0) {
		t.Fatal("dead worker revived")
	}

	runs := status["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["error"] == nil {
		t.Fatalf("interrupted BOM history missing: %#v", status)
	}
}

func TestSavedEthernetCorrelationAcceptsBOMAndRejectsLinkedCSV(t *testing.T) {
	s, h := fixture(t, time.Second)
	id := "20261107-120000-saved"
	runDir := filepath.Join(h.root, "diagnosis-runs", id)
	dir := filepath.Join(runDir, "diagnostics", "data")
	at := "2026-11-07T12:00:00Z"
	writeJSON(t, filepath.Join(runDir, "run.json"), map[string]any{
		"appId":       "codex-network-diagnosis-run",
		"id":          id,
		"adapterName": "Ethernet",
		"kind":        "ethernet",
		"started":     at,
	}, false)
	writeJSON(t, filepath.Join(runDir, "diagnostics-live.json"), map[string]any{
		"appId":   "codex-single-pc-diagnostics",
		"logDir":  dir,
		"targets": []any{map[string]any{"scope": "local_gateway", "address": "192.168.1.1"}},
	}, false)
	writeJSON(t, filepath.Join(runDir, "ethernet-live.json"), map[string]any{
		"kind": "ethernet_line",
		"events": []any{map[string]any{
			"time":    at,
			"kind":    "link_down",
			"message": "link",
		}},
	}, false)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "probes.csv")
	data := "\ufeffevent_time,sent_time,target_id,protocol,address,source_ip,interface_index,payload_bytes,status,rtt_ms,scheduler_lag_ms\n" + at + "," + at + ",gateway-icmp,icmp,192.168.1.1,192.168.1.2,4,64,Success,30,0\n" + at + "," + at + ",cloudflare-icmp,icmp,1.1.1.1,192.168.1.2,4,64,TimedOut,,0\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	run := waitRun(t, s, id)
	ethernet := run["ethernet"].(map[string]any)
	incidents := ethernet["gatewayIncidents"].(map[string]any)
	if incidents["slowReplies"] != float64(1) || incidents["matchedLineEvents"] != float64(1) {
		t.Fatalf("incorrect correlation: %#v", ethernet)
	}

	foreign := filepath.Join(h.root, "foreign.csv")
	if err := os.WriteFile(foreign, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(foreign, path); err != nil {
		t.Skip("symlink creation unavailable: " + err.Error())
	}

	ethernet = waitRun(t, s, id)["ethernet"].(map[string]any)
	if _, ok := ethernet["correlations"]; ok {
		t.Fatal("followed symlinked probes.csv")
	}
}
