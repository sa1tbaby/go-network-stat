package relay_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/relay"
)

type host struct {
	root     string
	mu       sync.Mutex
	targets  []contracts.DetectedTarget
	evidence []string
}

func (h *host) Adapter(string) (collectors.Adapter, bool) {
	return collectors.Adapter{}, false
}

func (h *host) LogDirectory() string {
	return h.root
}

func (h *host) StateDirectory() string {
	return h.root
}

func (h *host) Event(string, string) {}

func (h *host) AddDetectedTarget(_ string, target contracts.DetectedTarget, evidence string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.targets = append(h.targets, target)
	h.evidence = append(h.evidence, evidence)

	return nil
}

type engine struct {
	started       chan struct{}
	release       chan struct{}
	analysis      *collectors.UDPAnalysis
	findErr       error
	runErr        error
	helperError   *string
	wrongIdentity bool
	mu            sync.Mutex
	findCalls     int
}

func (e *engine) FindGameProcess(string) (int, int64, error) {
	e.mu.Lock()
	e.findCalls++
	e.mu.Unlock()

	return 42, 638000000000000001, e.findErr
}

func (e *engine) CurrentProcessIdentity() (int, int64, error) {
	return 99, 638000000000000003, nil
}

func (e *engine) Run(ctx context.Context, dir string, request collectors.CaptureRequest, _ int, started func(int)) error {
	started(5555)
	close(e.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.release:
	}

	if e.analysis == nil && e.helperError == nil {
		return e.runErr
	}

	state := "error"
	message := "helper failed"
	if e.analysis != nil {
		state = e.analysis.State
		message = e.analysis.Message
	}

	report := collectors.CaptureReport{
		AppID:          "codex-cod-relay-capture",
		Game:           request.Game,
		RunID:          request.RunID,
		GamePID:        request.GamePID,
		GameStartTicks: request.GameStartTicks,
		State:          state,
		Message:        message,
		Error:          e.helperError,
		Analysis:       e.analysis,
	}
	if e.wrongIdentity {
		report.GameStartTicks++
	}

	data, err := json.Marshal(report)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(dir, "capture-*.tmp")
	if err != nil {
		return err
	}

	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()

		return err
	}

	if err = file.Close(); err != nil {
		return err
	}

	if err = os.Rename(name, filepath.Join(dir, "capture.json")); err != nil {
		return err
	}

	return e.runErr
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	return result
}

func newService(t *testing.T, analysis *collectors.UDPAnalysis) (*relay.Service, *host, *engine) {
	t.Helper()
	h := &host{root: t.TempDir()}
	e := &engine{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		analysis: analysis,
	}
	s := relay.NewWithEngine(context.Background(), h, e)
	t.Cleanup(s.Close)

	return s, h, e
}

func waitStatus(t *testing.T, s *relay.Service, game string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := object(t, s.Status(game))
		if status["running"] == false {
			return status
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("relay did not finish")

	return nil
}

func detected() *collectors.UDPAnalysis {
	return &collectors.UDPAnalysis{
		State:         "detected",
		Message:       "peer found",
		GamePID:       42,
		GameUDPEvents: 400,
		Selected: &collectors.UDPPeer{
			RemoteIP:   "8.8.4.4",
			RemotePort: 3074,
			LocalIP:    "192.168.1.2",
			LocalPort:  50000,
			InEvents:   200,
			OutEvents:  200,
			Eligible:   true,
		},
		Candidates: []collectors.UDPPeer{},
	}
}
