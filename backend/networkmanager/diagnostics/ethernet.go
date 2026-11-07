package diagnostics

import (
	"math"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

// EthernetTracker tracks observed link transitions and counter deltas. Query
// failures retain the last known link state; counter resets establish a new base.
type EthernetTracker struct {
	previous      *collectors.Adapter
	baseline      [4]*int64
	deltas        [4]int64
	seen          [4]bool
	downStart     *float64
	downMs        float64
	lastSpeed     *float64
	samples       int
	valid         int
	validCounters int
	resets        int
	downs         int
	ups           int
	speeds        int
	events        []map[string]any
}

// Observe adds one adapter sample with a monotonic elapsed time in milliseconds.
func (t *EthernetTracker) Observe(adapter collectors.Adapter, elapsed float64, timestamp string) []map[string]any {
	t.samples++
	if adapter.ID != "" {
		t.valid++
	}

	added := []map[string]any{}
	known := adapter.State != "Unknown" && adapter.State != ""
	up := adapter.State == "Up" && adapter.ID != ""
	create := func(kind, message string) map[string]any {
		e := map[string]any{
			"time":              timestamp,
			"kind":              kind,
			"message":           message,
			"state":             adapter.State,
			"speedMbps":         diagnosisSpeed(adapter),
			"previousState":     nil,
			"previousSpeedMbps": nil,
			"observedDownMs":    nil,
			"rxErrorsDelta":     nil,
			"txErrorsDelta":     nil,
			"rxDiscardsDelta":   nil,
			"txDiscardsDelta":   nil,
		}
		if t.previous != nil {
			e["previousState"] = t.previous.State
			e["previousSpeedMbps"] = diagnosisSpeed(*t.previous)
		}

		added = append(added, e)

		return e
	}
	if t.previous == nil {
		if known && !up {
			v := elapsed
			t.downStart = &v
		}
	} else if known {
		wasUp := t.previous.State == "Up"
		if wasUp && !up {
			t.downs++
			v := elapsed
			t.downStart = &v
			create("link_down", "The Ethernet link is no longer Up.")
		}

		if !wasUp && up {
			t.ups++
			e := create("link_up", "The Ethernet link is restored.")
			if t.downStart != nil {
				duration := math.Max(0, elapsed-*t.downStart)
				t.downMs += duration
				e["observedDownMs"] = duration
				t.downStart = nil
			}
		}
	}

	if up && adapter.SpeedMbps > 0 {
		if t.lastSpeed != nil && *t.lastSpeed != adapter.SpeedMbps {
			t.speeds++
			e := create("speed_change", "The negotiated Ethernet speed changed.")
			e["previousSpeedMbps"] = *t.lastSpeed
		}

		v := adapter.SpeedMbps
		t.lastSpeed = &v
	}

	if adapter.CountersAvailable {
		t.validCounters++
		values := [4]int64{adapter.RxErrors, adapter.TxErrors, adapter.RxDiscards, adapter.TxDiscards}
		step := [4]int64{}
		increased := false
		reset := false
		for i, v := range values {
			if v < 0 {
				t.baseline[i] = nil
				continue
			}

			t.seen[i] = true
			if t.baseline[i] != nil {
				if v < *t.baseline[i] {
					t.resets++
					reset = true
				} else {
					step[i] = v - *t.baseline[i]
					t.deltas[i] += step[i]
					increased = increased || step[i] > 0
				}
			}

			value := v
			t.baseline[i] = &value
		}

		if reset {
			create("counter_reset", "A counter decreased; a new baseline was started.")
		}

		if increased {
			e := create("counter_increase", "Windows error or discard counters increased.")
			for i, key := range []string{"rxErrorsDelta", "txErrorsDelta", "rxDiscardsDelta", "txDiscardsDelta"} {
				e[key] = step[i]
			}
		}
	} else if adapter.Error != "" && (t.previous == nil || t.previous.Error != adapter.Error) {
		create("statistics_unavailable", "Failed to read Windows counters: "+adapter.Error)
	}

	if known {
		copy := adapter
		t.previous = &copy
	}

	t.events = append(t.events, added...)
	if len(t.events) > 250 {
		t.events = t.events[len(t.events)-250:]
	}

	return added
}

// Summary returns legacy-compatible totals at the supplied elapsed milliseconds.
func (t *EthernetTracker) Summary(elapsed float64) map[string]any {
	down := t.downMs
	if t.downStart != nil {
		down += math.Max(0, elapsed-*t.downStart)
	}

	s := map[string]any{
		"samples":             t.samples,
		"validSamples":        t.valid,
		"validCounterSamples": t.validCounters,
		"elapsedSeconds":      elapsed / 1000,
		"counterResets":       t.resets,
		"linkDownCount":       t.downs,
		"linkUpCount":         t.ups,
		"speedChangeCount":    t.speeds,
		"totalLinkDownMs":     down,
	}
	for i, key := range []string{"rxErrorsDelta", "txErrorsDelta", "rxDiscardsDelta", "txDiscardsDelta"} {
		s[key] = nil
		if t.seen[i] {
			s[key] = t.deltas[i]
		}
	}

	return s
}
