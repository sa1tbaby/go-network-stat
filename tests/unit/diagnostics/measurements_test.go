package diagnostics_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/diagnostics"
)

func TestCompletionPersistsNetworkSnapshotAndCSV(t *testing.T) {
	s, h := fixture(t, 120*time.Millisecond)
	id := start(t, s, h.adapter.ID)
	run := waitRun(t, s, id)
	network := run["network"].(map[string]any)
	if network["running"] != false || network["appId"] != "codex-single-pc-diagnostics" {
		t.Fatalf("network: %#v", network)
	}

	targets := network["targets"].([]any)
	if len(targets) != 6 {
		t.Fatal("expected ICMP and DNS targets for gateway and two controls")
	}

	for _, value := range targets {
		target := value.(map[string]any)
		if target["received"].(float64) < 1 || target["lossPct"] != float64(0) {
			t.Fatalf("invalid target: %#v", target)
		}
	}

	paths, err := filepath.Glob(filepath.Join(h.root, "diagnosis-runs", id, "diagnostics", "*", "probes.csv"))
	if err != nil || len(paths) != 1 {
		t.Fatal("probe CSV missing")
	}

	data, _ := os.ReadFile(paths[0])
	if !strings.HasPrefix(string(data), "event_time,sent_time,target_id,protocol,address,source_ip,interface_index,payload_bytes,status,rtt_ms,scheduler_lag_ms\n") {
		t.Fatalf("incompatible CSV: %s", data)
	}
}

func TestStreamingDNSExcludesToolErrorsAndDoesNotCountLateReplyTwice(t *testing.T) {
	_, h := fixture(t, time.Second)
	s := diagnostics.New(context.Background(), h, &streamCollector{collector{adapter: h.adapter}}, diagnostics.Options{Duration: 100 * time.Millisecond})
	defer s.Close()
	id := start(t, s, h.adapter.ID)
	network := waitRun(t, s, id)["network"].(map[string]any)
	for _, value := range network["targets"].([]any) {
		target := value.(map[string]any)
		if target["protocol"] == "udp_dns" && (target["completed"] != float64(2) || target["received"] != float64(1) || target["lossPct"] != float64(50) || target["toolErrors"] != float64(1) || target["lateRepliesAfterTimeout"] != float64(1) || target["pending"] != float64(0)) {
			t.Fatalf("invalid DNS accounting: %#v", target)
		}
	}

	paths, _ := filepath.Glob(filepath.Join(h.root, "diagnosis-runs", id, "diagnostics", "*", "probes.csv"))
	data, _ := os.ReadFile(paths[0])
	if !strings.Contains(string(data), ",,gateway-udp-dns,") {
		t.Fatal("late observation must retain empty sent_time")
	}
}

func TestUnavailableEthernetCountersStayNullAndCSVValuesStayBlank(t *testing.T) {
	_, h := fixture(t, time.Second)
	a := h.adapter
	a.CountersAvailable = false
	a.Error = "CountersUnavailable"
	s := diagnostics.New(context.Background(), h, &collector{adapter: a}, diagnostics.Options{Duration: 100 * time.Millisecond})
	defer s.Close()
	id := start(t, s, h.adapter.ID)
	ethernet := waitRun(t, s, id)["ethernet"].(map[string]any)
	latest := ethernet["latest"].(map[string]any)
	for _, key := range []string{"rxErrors", "txErrors", "rxDiscards", "txDiscards", "crcFcsErrors"} {
		if latest[key] != nil {
			t.Errorf("%s should be null", key)
		}
	}

	for _, key := range []string{"time", "elapsedMs", "state", "speedMbps", "adapterFound", "countersAvailable", "error"} {
		if _, ok := latest[key]; !ok {
			t.Errorf("missing legacy field %s", key)
		}
	}

	if ethernet["pid"] == nil || ethernet["intervalMs"] != float64(500) || ethernet["error"] != nil {
		t.Fatalf("invalid envelope: %#v", ethernet)
	}

	data, _ := os.ReadFile(filepath.Join(h.root, "diagnosis-runs", id, "ethernet", "ethernet.csv"))
	if !strings.Contains(string(data), ",,,,false,true,CountersUnavailable") {
		t.Fatalf("unavailable counters were recorded as zeros: %s", data)
	}
}
