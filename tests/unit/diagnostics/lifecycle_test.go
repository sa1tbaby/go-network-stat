package diagnostics_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/diagnostics"
)

func TestConcurrentRunsHaveIndependentStopAndSafeDeletion(t *testing.T) {
	s, h := fixture(t, time.Minute)
	ids := []string{}
	for i := 0; i < 4; i++ {
		ids = append(ids, start(t, s, h.adapter.ID))
	}

	if _, err := s.Start(h.adapter.ID); err == nil {
		t.Fatal("fifth diagnosis accepted")
	}

	if _, err := s.Stop(ids[0]); err != nil {
		t.Fatal(err)
	}

	waitRun(t, s, ids[0])
	if !s.Active() || object(t, s.Status())["activeCount"] != float64(3) {
		t.Fatal("stopping one run stopped other runs")
	}

	dir := filepath.Join(h.root, "diagnosis-runs", ids[0])
	runtime := readJSON(t, filepath.Join(dir, "runtime.json"))
	if runtime["executionModel"] != "go-goroutines" || runtime["stopRequested"] != true || runtime["running"] != false {
		t.Fatalf("runtime: %#v", runtime)
	}

	if _, err := s.Delete(ids[1], h.root); err == nil {
		t.Fatal("deleted running diagnosis")
	}

	foreign := filepath.Join(dir, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Delete(ids[0], h.root); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("foreign file removed")
	}

	if _, err := os.Stat(filepath.Join(dir, "run.json")); !os.IsNotExist(err) {
		t.Fatal("owned manifest retained")
	}
}

func TestSourceVerificationFailureStopsRun(t *testing.T) {
	_, h := fixture(t, time.Second)
	s := diagnostics.New(context.Background(), h, &collector{adapter: h.adapter, adapterError: errors.New("adapter query failed")}, diagnostics.Options{Duration: 2 * time.Second})
	defer s.Close()
	id := start(t, s, h.adapter.ID)
	run := waitRun(t, s, id)
	network := run["network"].(map[string]any)
	if text, _ := network["error"].(string); !strings.Contains(text, "adapter query failed") {
		t.Fatalf("missing topology error: %#v", network)
	}
}

func TestParentCancellationStopsWorkersAndRejectsNewRuns(t *testing.T) {
	_, h := fixture(t, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	s := diagnostics.New(ctx, h, &collector{adapter: h.adapter}, diagnostics.Options{})
	defer s.Close()
	id := start(t, s, h.adapter.ID)
	cancel()
	waitRun(t, s, id)
	if s.Active() {
		t.Fatal("workers survived parent cancellation")
	}

	if _, err := s.Start(h.adapter.ID); err == nil {
		t.Fatal("started a run after application cancellation")
	}
}
