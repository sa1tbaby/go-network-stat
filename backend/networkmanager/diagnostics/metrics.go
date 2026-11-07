package diagnostics

import (
	"math"
	"sort"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

type diagnosisProbe struct {
	Sent   time.Time
	At     time.Time
	Result collectors.ProbeResult
	Lag    float64
}

type diagnosisTarget struct {
	ID       string
	Address  string
	Protocol string
	Scope    string
	PPS      int
	Pending  int
	Rows     []diagnosisProbe
}

func diagnosisMetrics(probeTarget *diagnosisTarget) map[string]any {
	completed, received, errors, late := 0, 0, 0, 0
	maxLag := 0.0
	var rtts []float64
	byPayload := map[int][]diagnosisProbe{}
	for _, row := range probeTarget.Rows {
		maxLag = math.Max(maxLag, row.Lag)
		if row.Result.ToolError {
			errors++
			continue
		}

		if row.Result.Status == "LateReplyAfterTimeout" {
			late++
			continue
		}

		completed++
		byPayload[row.Result.Bytes] = append(byPayload[row.Result.Bytes], row)
		if row.Result.OK && row.Result.RTTMs != nil {
			received++
			rtts = append(rtts, *row.Result.RTTMs)
		}
	}

	sort.Float64s(rtts)
	var avg, p95, maxRTT, loss any
	if received > 0 {
		sum := 0.0
		for _, v := range rtts {
			sum += v
		}

		avg, p95, maxRTT = sum/float64(received), rtts[int(math.Ceil(float64(received)*.95))-1], rtts[len(rtts)-1]
		loss = 100 * float64(completed-received) / float64(completed)
	}

	reachability := "waiting"
	if completed > 0 {
		reachability = "no_response_yet"
	}

	if received > 0 {
		reachability = "responding"
	}

	thresholds := make([]any, 0, 4)
	for _, threshold := range []int{20, 50, 100, 200} {
		above := 0
		for _, r := range rtts {
			if r > float64(threshold) {
				above++
			}
		}

		thresholds = append(thresholds, map[string]any{
			"thresholdMs": threshold,
			"above":       above,
			"ofReplies":   received,
		})
	}

	payloads := make([]int, 0, len(byPayload))
	for bytes := range byPayload {
		payloads = append(payloads, bytes)
	}

	sort.Ints(payloads)
	by := make([]any, 0, len(payloads))
	for _, bytes := range payloads {
		rows := byPayload[bytes]
		good := 0
		var max any
		for _, row := range rows {
			if row.Result.OK && row.Result.RTTMs != nil {
				good++
				if max == nil || *row.Result.RTTMs > max.(float64) {
					max = *row.Result.RTTMs
				}
			}
		}

		by = append(by, map[string]any{
			"payloadBytes":         bytes,
			"completed":            len(rows),
			"received":             good,
			"unansweredByDeadline": len(rows) - good,
			"maxMs":                max,
		})
	}

	return map[string]any{
		"id":                      probeTarget.ID,
		"address":                 probeTarget.Address,
		"protocol":                probeTarget.Protocol,
		"scope":                   probeTarget.Scope,
		"pps":                     probeTarget.PPS,
		"pending":                 probeTarget.Pending,
		"completed":               completed,
		"received":                received,
		"unansweredByDeadline":    completed - received,
		"lossPct":                 loss,
		"reachability":            reachability,
		"toolErrors":              errors,
		"lateRepliesAfterTimeout": late,
		"avgMs":                   avg,
		"p95Ms":                   p95,
		"maxMs":                   maxRTT,
		"latencyThresholds":       thresholds,
		"byPayload":               by,
		"maxSchedulerLagMs":       maxLag,
	}
}
