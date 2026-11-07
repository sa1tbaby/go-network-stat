package networkmanager

import (
	"math"
	"sort"
)

type Point struct {
	T         int64    `json:"t"`
	OK        bool     `json:"ok"`
	RTTMs     *float64 `json:"rttMs"`
	Status    string   `json:"status"`
	ToolError bool     `json:"toolError"`
}

type Metrics struct {
	Sent       int64    `json:"sent"`
	Received   int64    `json:"received"`
	Lost       int64    `json:"lost"`
	Errors     int64    `json:"errors"`
	LossPct    *float64 `json:"lossPct"`
	AvgMs      *float64 `json:"avgMs"`
	P95Ms      *float64 `json:"p95Ms"`
	MaxMs      *float64 `json:"maxMs"`
	JitterMs   *float64 `json:"jitterMs"`
	Above20Ms  int64    `json:"above20Ms"`
	Above50Ms  int64    `json:"above50Ms"`
	Above100Ms int64    `json:"above100Ms"`
	Above200Ms int64    `json:"above200Ms"`
}

func restorePoint(monitoredTarget *target, point Point, now int64) {
	if point.T >= now-360000 {
		monitoredTarget.Recent = append(monitoredTarget.Recent, point)
	}

	if point.ToolError {
		monitoredTarget.Totals.Errors++

		return
	}

	monitoredTarget.Totals.Sent++
	if !point.OK || point.RTTMs == nil {
		monitoredTarget.Totals.Lost++
		monitoredTarget.Previous = nil

		return
	}

	rttMs := *point.RTTMs
	monitoredTarget.Totals.Received++
	monitoredTarget.Sum += rttMs
	if monitoredTarget.Totals.MaxMs == nil || rttMs > *monitoredTarget.Totals.MaxMs {
		monitoredTarget.Totals.MaxMs = floatPtr(rttMs)
	}

	if rttMs > 20 {
		monitoredTarget.Totals.Above20Ms++
	}

	if rttMs > 50 {
		monitoredTarget.Totals.Above50Ms++
	}

	if rttMs > 100 {
		monitoredTarget.Totals.Above100Ms++
	}

	if rttMs > 200 {
		monitoredTarget.Totals.Above200Ms++
	}

	bucket := int(math.RoundToEven(rttMs))
	if bucket > 10000 {
		bucket = 10000
	}

	if bucket < 0 {
		bucket = 0
	}

	monitoredTarget.Histogram[bucket]++
	if monitoredTarget.Previous != nil {
		monitoredTarget.JitterSum += math.Abs(rttMs - *monitoredTarget.Previous)
		monitoredTarget.JitterCount++
	}

	monitoredTarget.Previous = floatPtr(rttMs)
}

func allMetrics(monitoredTarget *target) Metrics {
	s := monitoredTarget.Totals
	if s.Sent > 0 {
		s.LossPct = floatPtr(100 * float64(s.Lost) / float64(s.Sent))
	}

	if s.Received > 0 {
		s.AvgMs = floatPtr(monitoredTarget.Sum / float64(s.Received))
		threshold := int64(math.Ceil(float64(s.Received) * .95))
		n := int64(0)
		for bucket, count := range monitoredTarget.Histogram {
			n += count
			if n >= threshold {
				s.P95Ms = floatPtr(float64(bucket))
				break
			}
		}
	}

	if monitoredTarget.JitterCount > 0 {
		s.JitterMs = floatPtr(monitoredTarget.JitterSum / float64(monitoredTarget.JitterCount))
	}

	return s
}

func windowMetrics(rows []Point, cutoff int64) Metrics {
	var s Metrics
	values := []float64{}
	sum, jitter := 0.0, 0.0
	var previous *float64
	jn := 0
	ordered := append([]Point(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].T < ordered[j].T
	})
	for _, p := range ordered {
		if p.T < cutoff {
			continue
		}

		if p.ToolError {
			s.Errors++
			continue
		}

		s.Sent++
		if !p.OK || p.RTTMs == nil {
			s.Lost++
			previous = nil
			continue
		}

		r := *p.RTTMs
		s.Received++
		values = append(values, r)
		sum += r
		if previous != nil {
			jitter += math.Abs(r - *previous)
			jn++
		}

		previous = floatPtr(r)
		if r > 20 {
			s.Above20Ms++
		}

		if r > 50 {
			s.Above50Ms++
		}

		if r > 100 {
			s.Above100Ms++
		}

		if r > 200 {
			s.Above200Ms++
		}
	}

	if s.Sent > 0 {
		s.LossPct = floatPtr(100 * float64(s.Lost) / float64(s.Sent))
	}

	if len(values) > 0 {
		sort.Float64s(values)
		s.AvgMs = floatPtr(sum / float64(len(values)))
		s.MaxMs = floatPtr(values[len(values)-1])
		s.P95Ms = floatPtr(values[int(math.Ceil(float64(len(values))*.95))-1])
	}

	if jn > 0 {
		s.JitterMs = floatPtr(jitter / float64(jn))
	}

	return s
}
