package diagnostics_test

import (
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/diagnostics"
)

func TestEthernetTrackerKeepsUnknownSeparateFromDownAndResetsBaseline(t *testing.T) {
	tracker := &diagnostics.EthernetTracker{}
	a := collectors.Adapter{
		ID:                "eth",
		State:             "Up",
		SpeedMbps:         1000,
		CountersAvailable: true,
		RxErrors:          10,
	}
	tracker.Observe(a, 0, "start")
	tracker.Observe(collectors.Adapter{State: "Unknown", Error: "query"}, 500, "unknown")
	a.RxErrors = 12
	tracker.Observe(a, 1000, "same")
	summary := tracker.Summary(1000)
	if summary["linkDownCount"] != 0 || summary["rxErrorsDelta"] != int64(2) {
		t.Fatalf("query error created a link incident: %#v", summary)
	}

	a.RxErrors = 1
	tracker.Observe(a, 1500, "reset")
	a.RxErrors = 3
	a.SpeedMbps = 100
	events := tracker.Observe(a, 2000, "speed")
	summary = tracker.Summary(2000)
	if summary["counterResets"] != 1 || summary["rxErrorsDelta"] != int64(4) || summary["speedChangeCount"] != 1 {
		t.Fatalf("counter reset handling: %#v", summary)
	}

	if len(events) < 1 || events[0]["kind"] != "speed_change" {
		t.Fatal("speed event missing")
	}

	a.State = "Down"
	tracker.Observe(a, 2500, "down")
	a.State = "Up"
	tracker.Observe(a, 3500, "up")
	if tracker.Summary(3500)["totalLinkDownMs"] != float64(1000) {
		t.Fatal("observed outage duration incorrect")
	}
}

func TestRadioTrackerUsesPHY0AndExcludesResetFromTraffic(t *testing.T) {
	tracker := &diagnostics.RadioTracker{}
	first := collectors.PHYStats{TxFrames: 100, RetryFrames: 10}
	tracker.Observe(collectors.RadioSample{
		StatisticsAvailable:  true,
		Phys:                 []collectors.PHYStats{first, first},
		DuplicatePHYCounters: true,
	})
	next := collectors.PHYStats{TxFrames: 200, RetryFrames: 30}
	tracker.Observe(collectors.RadioSample{StatisticsAvailable: true, Phys: []collectors.PHYStats{next, next}})
	summary := tracker.Summary(1)
	if summary["retryPctSuccessfulTx"] != float64(20) || summary["txFrames"] != uint64(100) || summary["duplicatePhyCounters"] != true {
		t.Fatalf("duplicate PHY counters were summed: %#v", summary)
	}

	tracker.Observe(collectors.RadioSample{StatisticsAvailable: true, Phys: []collectors.PHYStats{{TxFrames: 1, RetryFrames: 1}}})
	summary = tracker.Summary(2)
	if summary["counterResets"] != 1 || summary["txFrames"] != uint64(100) {
		t.Fatalf("reset counted as traffic: %#v", summary)
	}
}
