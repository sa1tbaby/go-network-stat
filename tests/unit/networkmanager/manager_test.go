package networkmanager_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func newManager(t *testing.T, cfg networkmanager.Config, fake *testsupport.FakeCollector) *networkmanager.Manager {
	t.Helper()
	if cfg.Root == "" {
		cfg.Root = filepath.Join(testsupport.Root(), "frontend")
	}

	m, err := networkmanager.New(context.Background(), cfg, fake)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})

	return m
}

func wait(t *testing.T, label string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("timed out: " + label)
}

func TestMonitorProbeSourceAndUnsafeICMP(t *testing.T) {
	base := t.TempDir()
	a := testsupport.Adapter()
	fake := &testsupport.FakeCollector{Interfaces: []collectors.Adapter{a}}
	m := newManager(t, networkmanager.Config{
		StateDirectory: filepath.Join(base, "config"),
		LogDirectory:   filepath.Join(base, "statistics"),
		IntervalMs:     250,
	}, fake)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	wait(t, "initial source probes", func() bool {
		r, _ := fake.Observed()

		return len(r) >= 4
	})
	requests, ports := fake.Observed()
	if len(ports) != 1 || ports[0] != 443 {
		t.Fatal("base TCP probe missing", ports)
	}

	for _, r := range requests {
		if r.Source != a.IPv4 || r.InterfaceIndex != a.Index || r.TimeoutMs != 1200 {
			t.Fatalf("probe not bound to selected source: %#v", r)
		}
	}

	a.ICMPSafe = false
	fake.SetAdapters([]collectors.Adapter{a})
	wait(t, "weak-host adapter refresh", func() bool {
		snapshot := testsupport.JSON(t, m.Snapshot())

		return snapshot["adapters"].([]any)[0].(map[string]any)["icmpSafe"] == false
	})
	before, beforeTCP := fake.Observed()
	icmpBefore := len(before) - len(beforeTCP)
	if _, err := m.AddTarget("203.0.113.10", "9090"); err != nil {
		t.Fatal(err)
	}

	wait(t, "TCP continues on unsafe ICMP interface", func() bool {
		_, ports := fake.Observed()

		return len(ports) > len(beforeTCP)
	})
	after, afterTCP := fake.Observed()
	if len(after)-len(afterTCP) != icmpBefore {
		t.Fatal("ICMP scheduled on weak-host interface")
	}
}
