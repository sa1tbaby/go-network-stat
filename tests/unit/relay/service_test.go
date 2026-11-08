package relay_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func TestRelayPortKeepsSingleCaptureAndPersistsDetectedTarget(t *testing.T) {
	s, h, e := newService(t, detected())
	result, err := s.Start("cod")
	if err != nil {
		t.Fatal(err)
	}

	id := object(t, result)["runId"].(string)
	select {
	case <-e.started:
	case <-time.After(time.Second):
		t.Fatal("capture did not start")
	}

	if !s.Active() {
		t.Fatal("capture not active")
	}

	again, err := s.Start("cod")
	if err != nil || object(t, again)["reused"] != true || object(t, again)["runId"] != id {
		t.Fatalf("active capture was not reused: %#v %v", again, err)
	}

	if _, err := s.Start("cs2"); err == nil {
		t.Fatal("parallel game capture allowed")
	}

	e.mu.Lock()
	calls := e.findCalls
	e.mu.Unlock()
	if calls != 1 {
		t.Fatal("reusing a capture repeated process discovery")
	}

	close(e.release)
	status := waitStatus(t, s, "cod")
	if status["state"] != "complete" || status["addedAddress"] != "8.8.4.4" || status["helperPid"] != float64(5555) {
		t.Fatalf("unexpected result: %#v", status)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.targets) != 1 || h.targets[0].UDPPort != 3074 || h.targets[0].LocalPort != 50000 || h.evidence[0] != filepath.Join(h.root, "cod-relay-runs", id, "capture.json") {
		t.Fatalf("target/evidence mismatch: %#v %#v", h.targets, h.evidence)
	}

	data, err := os.ReadFile(filepath.Join(h.root, "cod-relay-runs", id, "request.json"))
	if err != nil {
		t.Fatal(err)
	}

	var request collectors.CaptureRequest
	if err = json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}

	if request.GameStartTicks != 638000000000000001 || request.OwnerStartTicks != 638000000000000003 {
		t.Fatal("process identity precision lost")
	}
}

func TestRelayDoesNotAddAmbiguousOrForgedEvidence(t *testing.T) {
	for _, test := range []struct {
		name     string
		analysis *collectors.UDPAnalysis
		forged   bool
		state    string
	}{{"ambiguous", &collectors.UDPAnalysis{
		State:      "ambiguous",
		Message:    "multiple peers",
		Candidates: []collectors.UDPPeer{},
	}, false, "ambiguous"}, {"forged", detected(), true, "error"}} {
		t.Run(test.name, func(t *testing.T) {
			s, h, e := newService(t, test.analysis)
			e.wrongIdentity = test.forged
			if _, err := s.Start("cs2"); err != nil {
				t.Fatal(err)
			}

			<-e.started
			close(e.release)
			status := waitStatus(t, s, "cs2")
			if status["state"] != test.state {
				t.Fatalf("unexpected state: %#v", status)
			}

			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.targets) != 0 {
				t.Fatal("unverified address added")
			}
		})
	}
}

func TestRelayKeepsNativeHelperErrorAndUACCancellation(t *testing.T) {
	for _, test := range []struct {
		name    string
		message *string
		err     error
		state   string
	}{{"helper", func() *string {
		v := "ETW provider unavailable"

		return &v
	}(), errors.New("exit code 1"), "error"}, {"uac", nil, syscall.Errno(1223), "cancelled"}} {
		t.Run(test.name, func(t *testing.T) {
			s, _, e := newService(t, nil)
			e.runErr = test.err
			e.helperError = test.message
			if _, err := s.Start("cod"); err != nil {
				t.Fatal(err)
			}

			<-e.started
			close(e.release)
			status := waitStatus(t, s, "cod")
			if status["state"] != test.state {
				t.Fatalf("unexpected state: %#v", status)
			}

			if test.message != nil && status["error"] != *test.message {
				t.Fatalf("specific helper error was replaced: %#v", status)
			}
		})
	}
}

func TestRelayCloseCancelsOwnedCaptureAndRejectsRestart(t *testing.T) {
	s, h, e := newService(t, nil)
	result, err := s.Start("cod")
	if err != nil {
		t.Fatal(err)
	}

	id := object(t, result)["runId"].(string)
	<-e.started
	s.Close()
	status := waitStatus(t, s, "cod")
	if s.Active() || status["state"] != "cancelled" {
		t.Fatalf("capture survived shutdown: %#v", status)
	}

	if _, err := os.Stat(filepath.Join(h.root, "cod-relay-runs", id, "cancel.json")); err != nil {
		t.Fatal("owned capture cancellation file missing")
	}

	if _, err := s.Start("cod"); err == nil {
		t.Fatal("capture restarted after close")
	}
}

func TestRelayRejectsUnsupportedGameBeforeProcessDiscovery(t *testing.T) {
	s, _, e := newService(t, nil)
	if _, err := s.Start("other"); err == nil {
		t.Fatal("unsupported game accepted")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.findCalls != 0 {
		t.Fatal("invalid game reached native discovery")
	}

	if status := object(t, s.Status("cod")); status["state"] != "idle" || status["running"] != false {
		t.Fatalf("unexpected initial state: %#v", status)
	}
}
