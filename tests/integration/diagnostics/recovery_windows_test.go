//go:build windows

package diagnostics_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/diagnostics"
)

type savedHost struct {
	root string
}

func (h savedHost) Adapter(string) (collectors.Adapter, bool) {
	return collectors.Adapter{}, false
}

func (h savedHost) LogDirectory() string {
	return h.root
}

func (h savedHost) StateDirectory() string {
	return h.root
}

func (savedHost) Event(string, string) {}

func (savedHost) AddDetectedTarget(string, contracts.DetectedTarget, string) error {
	return nil
}

// This reads this test process identity only. It neither starts a collector nor
// terminates a process; the stop operation is the legacy per-run stop flag.
func TestLegacyPIDRecoveryPreservesExactNativeStartTicks(t *testing.T) {
	pid, ticks, err := collectors.CurrentProcessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	id := "native-history"
	dir := filepath.Join(root, "diagnosis-runs", id)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	write := func(name string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}

		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("run.json", map[string]any{
		"appId":       "codex-network-diagnosis-run",
		"id":          id,
		"adapterName": "Ethernet",
		"kind":        "ethernet",
		"started":     time.Now().Format(time.RFC3339Nano),
	})
	runtime := func(startTicks int64) map[string]any {
		return map[string]any{
			"appId": "codex-diagnosis-runtime",
			"id":    id,
			"children": []any{map[string]any{
				"pid":               pid,
				"startTimeUtcTicks": startTicks,
				"role":              "network",
				"exitCode":          nil,
			}},
		}
	}
	write("runtime.json", runtime(ticks))
	service := diagnostics.New(context.Background(), savedHost{root}, collectors.New(), diagnostics.Options{})
	defer service.Close()
	if !service.Active() {
		t.Fatal("live PID/start-time identity not recovered")
	}

	if _, err = service.Delete(id, root); err == nil {
		t.Fatal("live external collector record deleted")
	}

	if _, err = service.Stop(id); err != nil {
		t.Fatal(err)
	}

	if _, err = os.Stat(filepath.Join(dir, "diagnostics-stop.flag")); err != nil {
		t.Fatal("legacy stop flag missing")
	}

	write("runtime.json", runtime(ticks+1))
	if service.Active() {
		t.Fatal("reused PID with different creation time accepted")
	}

	if _, err = service.Delete(id, root); err != nil {
		t.Fatal(err)
	}
}
