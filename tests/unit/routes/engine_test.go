package routes_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/routes"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

type host struct {
	dir string
}

func (h host) Adapter(string) (collectors.Adapter, bool) {
	return testsupport.Adapter(), true
}

func (h host) HasICMPTarget(string, string) bool {
	return true
}

func (h host) LogDirectory() string {
	return h.dir
}

func (h host) Event(string, string) {}

func TestRouteAssessmentDoesNotClaimTransitLoss(t *testing.T) {
	fake := &testsupport.FakeCollector{Probe: func(r collectors.ProbeRequest) collectors.ProbeResult {
		if r.TTL == 1 {
			return collectors.ProbeResult{Status: "TimedOut", IPStatus: 11010}
		}

		rtt := 1.0

		return collectors.ProbeResult{
			OK:           true,
			Received:     true,
			Status:       "Success",
			ReplyAddress: r.Address,
			RTTMs:        &rtt,
		}
	}}
	engine := routes.New(context.Background(), host{t.TempDir()}, fake, routes.Options{
		SampleGoal:       2,
		MaxHops:          2,
		SamplingInterval: time.Millisecond,
	})
	defer engine.Close()
	if _, err := engine.Start("Ethernet", "203.0.113.10"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for engine.Active() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	status := testsupport.JSON(t, engine.Status("Ethernet", "203.0.113.10"))
	if status["state"] != "complete" {
		t.Fatal(status)
	}

	hops := status["hops"].([]any)
	if hops[0].(map[string]any)["assessment"] != "response_limit_possible" || hops[1].(map[string]any)["assessment"] != "destination" || !strings.Contains(status["conclusion"].(string), "do not prove") {
		t.Fatal("silent hop was reported as transit loss", status)
	}
}

func TestRouteToolErrorsExcludedAndActiveRestoreRejected(t *testing.T) {
	release := make(chan struct{})
	fake := &testsupport.FakeCollector{Probe: func(r collectors.ProbeRequest) collectors.ProbeResult {
		<-release

		return collectors.ProbeResult{ToolError: true, Status: "FixtureToolError"}
	}}
	engine := routes.New(context.Background(), host{t.TempDir()}, fake, routes.Options{SampleGoal: 1})
	defer engine.Close()
	if _, err := engine.Start("Ethernet", "203.0.113.10"); err != nil {
		t.Fatal(err)
	}

	if err := engine.Restore(map[string]*routes.Job{}); err == nil {
		t.Fatal("restored a new store while route worker active")
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for engine.Active() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	status := testsupport.JSON(t, engine.Status("Ethernet", "203.0.113.10"))
	hop := status["hops"].([]any)[0].(map[string]any)
	if status["state"] != "error" || hop["noReplyPct"] != nil || hop["toolErrors"] != float64(1) || !strings.Contains(status["conclusion"].(string), "Tool errors are excluded") {
		t.Fatal(status)
	}
}
