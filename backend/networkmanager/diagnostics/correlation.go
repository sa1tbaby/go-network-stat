package diagnostics

import (
	"encoding/csv"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const ethernetCorrelationNote = "Ethernet events and ICMP probes are correlated within a ±1 second window. Matching times do not prove causation. Unavailable CRC/FCS counters do not mean zero CRC/FCS errors; errors from the PC to the router may only be counted by the receiving router port."

type diagnosisComparison struct {
	at      time.Time
	gateway bool
	failed  bool
	rtt     *float64
}

type diagnosisComparisonCache struct {
	stamp int64
	size  int64
	rows  []diagnosisComparison
}

func diagnosisWindow(rows []diagnosisComparison, gateway bool, threshold float64) map[string]any {
	count, slow, failed := 0, 0, 0
	var max any
	for _, row := range rows {
		if row.gateway != gateway {
			continue
		}

		count++
		if row.failed {
			failed++
		}

		if row.rtt != nil {
			if *row.rtt > threshold {
				slow++
			}

			if max == nil || *row.rtt > max.(float64) {
				max = *row.rtt
			}
		}
	}

	return map[string]any{
		"probes":       count,
		"slowReplies":  slow,
		"thresholdMs":  threshold,
		"failedProbes": failed,
		"maxMs":        max,
	}
}

func diagnosisUnder(root, path string) bool {
	rel, err := filepath.Rel(root, path)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (s *Service) correlateSaved(ethernet, network map[string]any, runDir string) map[string]any {
	if ethernet == nil {
		return nil
	}

	dir := diagnosisString(network, "logDir")
	if dir == "" || !diagnosisUnder(runDir, dir) || diagnosisOrdinaryPath(dir) != nil {
		return ethernet
	}

	path := filepath.Join(dir, "probes.csv")
	if diagnosisOrdinaryPath(path) != nil {
		return ethernet
	}

	info, err := os.Stat(path)
	if err != nil {
		return ethernet
	}

	s.mu.Lock()
	cached, hit := s.probeCache[path]
	s.mu.Unlock()
	rows := cached.rows
	if !hit || cached.stamp != info.ModTime().UnixNano() || cached.size != info.Size() {
		file, err := os.Open(path)
		if err != nil {
			return ethernet
		}

		defer file.Close()
		reader := csv.NewReader(file)
		reader.FieldsPerRecord = -1
		header, err := reader.Read()
		if len(header) > 0 {
			header[0] = strings.TrimPrefix(header[0], "\ufeff")
		}

		if err != nil || strings.Join(header, ",") != "event_time,sent_time,target_id,protocol,address,source_ip,interface_index,payload_bytes,status,rtt_ms,scheduler_lag_ms" {
			return ethernet
		}

		gateways := map[string]bool{}
		if targets, ok := network["targets"].([]any); ok {
			for _, value := range targets {
				target, ok := value.(map[string]any)
				if ok && target["scope"] == "local_gateway" {
					gateways[diagnosisString(target, "address")] = true
				}
			}
		}

		rows = []diagnosisComparison{}
		for {
			c, e := reader.Read()
			if e == io.EOF {
				break
			}

			if e != nil {
				var parseError *csv.ParseError
				if errors.As(e, &parseError) {
					continue
				}

				break
			}

			if len(c) != 11 || c[3] != "icmp" {
				continue
			}

			status := c[8]
			if strings.HasPrefix(status, "ToolError") || status == "LocalConcurrencyLimit" || diagnosisToolStatus(status) {
				continue
			}

			at, e := time.Parse(time.RFC3339Nano, c[1])
			if e != nil {
				continue
			}

			row := diagnosisComparison{
				at:      at,
				gateway: gateways[c[4]],
				failed:  status != "Success",
			}
			if !row.failed {
				if ms, e := strconv.ParseFloat(c[9], 64); e == nil && !math.IsNaN(ms) && !math.IsInf(ms, 0) {
					row.rtt = &ms
				}
			}

			rows = append(rows, row)
		}

		s.mu.Lock()
		if s.probeCache == nil || len(s.probeCache) >= 50 {
			s.probeCache = map[string]diagnosisComparisonCache{}
		}

		s.probeCache[path] = diagnosisComparisonCache{
			stamp: info.ModTime().UnixNano(),
			size:  info.Size(),
			rows:  rows,
		}
		s.mu.Unlock()
	}

	correlations := []any{}
	matched := 0
	if events, ok := ethernet["events"].([]any); ok {
		for _, value := range events {
			event, ok := value.(map[string]any)
			if !ok {
				continue
			}

			at, err := time.Parse(time.RFC3339Nano, diagnosisString(event, "time"))
			if err != nil {
				continue
			}

			near := []diagnosisComparison{}
			match := false
			for _, row := range rows {
				if math.Abs(row.at.Sub(at).Seconds()) <= 1 {
					near = append(near, row)
					match = match || row.gateway && (row.failed || row.rtt != nil && *row.rtt > 20)
				}
			}

			if match {
				matched++
			}

			correlations = append(correlations, map[string]any{
				"time":     event["time"],
				"kind":     event["kind"],
				"message":  event["message"],
				"gateway":  diagnosisWindow(near, true, 20),
				"external": diagnosisWindow(near, false, 100),
			})
		}
	}

	local := diagnosisWindow(rows, true, 20)
	ethernet["correlations"] = correlations
	ethernet["gatewayIncidents"] = map[string]any{
		"total":             local["slowReplies"].(int) + local["failedProbes"].(int),
		"slowReplies":       local["slowReplies"],
		"failedProbes":      local["failedProbes"],
		"matchedLineEvents": matched,
	}
	ethernet["correlationNote"] = ethernetCorrelationNote

	return ethernet
}

func diagnosisToolStatus(status string) bool {
	for _, code := range []int{5, 6, 8, 50, 87, 11001, 11006, 11007, 11011, 11015, 11017, 11018} {
		if status == "ICMP_"+strconv.Itoa(code) {
			return true
		}
	}

	return false
}

// Remove only files whose ownership was established by the run/summary markers.
// Unknown files and links are preserved instead of recursively deleting a folder.
