package diagnostics

import (
	"math"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

// RadioTracker accumulates PHY0 driver deltas without summing duplicated PHYs.
type RadioTracker struct {
	previous  *collectors.PHYStats
	totals    [6]uint64
	samples   int
	valid     int
	resets    int
	duplicate bool
	min       *int32
	max       *int32
}

func radioCounters(p collectors.PHYStats) [6]uint64 {
	return [6]uint64{p.TxFrames, p.RetryFrames, p.MultipleRetryFrames, p.FailedFrames, p.ACKFailure, p.FCSErrors}
}

// Observe adds a radio reading; decreasing counters start a new base.
func (t *RadioTracker) Observe(sample collectors.RadioSample) {
	t.samples++
	t.duplicate = t.duplicate || sample.DuplicatePHYCounters
	if sample.RSSIDbm != nil {
		if t.min == nil || *sample.RSSIDbm < *t.min {
			v := *sample.RSSIDbm
			t.min = &v
		}

		if t.max == nil || *sample.RSSIDbm > *t.max {
			v := *sample.RSSIDbm
			t.max = &v
		}
	}

	if !sample.StatisticsAvailable || len(sample.Phys) == 0 {
		return
	}

	t.valid++
	p := sample.Phys[0]
	if t.previous != nil {
		next, prev := radioCounters(p), radioCounters(*t.previous)
		reset := false
		for i := range next {
			reset = reset || next[i] < prev[i]
		}

		if reset {
			t.resets++
		} else {
			for i := range next {
				t.totals[i] += next[i] - prev[i]
			}
		}
	}

	t.previous = &p
}

// Summary returns legacy-compatible radio totals at the elapsed seconds.
func (t *RadioTracker) Summary(elapsed float64) map[string]any {
	var pct any
	if t.totals[0] > 0 {
		pct = math.Round(100000*float64(t.totals[1])/float64(t.totals[0])) / 1000
	}

	return map[string]any{
		"samples":              t.samples,
		"validStatsSamples":    t.valid,
		"elapsedSeconds":       math.Round(elapsed*1000) / 1000,
		"counterResets":        t.resets,
		"phyIndex":             0,
		"duplicatePhyCounters": t.duplicate,
		"txFrames":             t.totals[0],
		"retryFrames":          t.totals[1],
		"multipleRetryFrames":  t.totals[2],
		"failedFrames":         t.totals[3],
		"ackFailures":          t.totals[4],
		"fcsErrors":            t.totals[5],
		"retryPctSuccessfulTx": pct,
		"rssiMinDbm":           t.min,
		"rssiMaxDbm":           t.max,
	}
}
