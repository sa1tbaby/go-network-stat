package diagnostics_test

import (
	"context"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/diagnostics"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func TestSnapshotsMatchFullFrozenLegacyContract(t *testing.T) {
	shapes := testsupport.LoadContract(t).Objects
	for _, kind := range []string{"ethernet", "wifi"} {
		t.Run(kind, func(t *testing.T) {
			_, h := fixture(t, time.Second)
			h.adapter.Kind = kind
			s := diagnostics.New(context.Background(), h, &collector{adapter: h.adapter}, diagnostics.Options{Duration: time.Second})
			defer s.Close()
			runID := start(t, s, h.adapter.ID)
			waitRun(t, s, runID)
			status := object(t, s.Status())
			testsupport.AssertShape(t, shapes, "diagnostics", status)
			for _, value := range status["runs"].([]any) {
				run := value.(map[string]any)
				testsupport.AssertShape(t, shapes, "diagnosisRun", run)
				network := run["network"].(map[string]any)
				testsupport.AssertShape(t, shapes, "network", network)
				for _, value := range network["targets"].([]any) {
					testsupport.AssertShape(t, shapes, "networkTarget", value.(map[string]any))
				}

				if kind == "wifi" {
					testsupport.AssertShape(t, shapes, "radio", run["radio"].(map[string]any))
				} else {
					ethernet := run["ethernet"].(map[string]any)
					testsupport.AssertShape(t, shapes, "ethernet", ethernet)
					for _, event := range ethernet["events"].([]any) {
						testsupport.AssertShape(t, shapes, "ethernetEvent", event.(map[string]any))
					}
				}
			}
		})
	}
}
