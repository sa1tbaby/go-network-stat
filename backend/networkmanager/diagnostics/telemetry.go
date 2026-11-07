package diagnostics

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

const radioNote = "Wi-Fi driver radio frame counters. Retries are not a game UDP loss percentage. PHY0 is selected; duplicate counters from other PHYs are not summed. Direction: PC to access point."

func (s *Service) extraLoop(ctx context.Context, job *diagnosisJob) {
	kind, folder, interval := "ethernet_line", "ethernet", 500*time.Millisecond
	if job.adapter.Kind == "wifi" {
		kind, folder, interval = "wifi_radio", "radio", time.Second
	}

	dir := filepath.Join(job.dir, folder)
	if err := os.MkdirAll(dir, 0700); err != nil {
		job.mu.Lock()
		job.err = err.Error()
		job.mu.Unlock()
		job.cancel()

		return
	}

	file, err := os.Create(filepath.Join(dir, folder+".csv"))
	if err != nil {
		job.mu.Lock()
		job.err = err.Error()
		job.mu.Unlock()
		job.cancel()

		return
	}

	defer file.Close()
	writer := csv.NewWriter(file)
	rawName := "ethernet-events.jsonl"
	if folder == "radio" {
		rawName = "radio-raw.jsonl"
	}

	raw, err := os.Create(filepath.Join(dir, rawName))
	if err != nil {
		job.mu.Lock()
		job.err = err.Error()
		job.mu.Unlock()
		job.cancel()

		return
	}

	defer raw.Close()
	encoder := json.NewEncoder(raw)
	if folder == "radio" {
		_ = writer.Write([]string{
			"time",
			"statisticsError",
			"rssiDbm",
			"qualityPct",
			"txRateNative",
			"rxRateNative",
			"txFrames",
			"retryFrames",
			"multipleRetryFrames",
			"failedFrames",
			"ackFailure",
			"fcsErrors",
		})
	} else {
		_ = writer.Write([]string{
			"time",
			"elapsed_ms",
			"state",
			"speed_mbps",
			"rx_errors",
			"tx_errors",
			"rx_discards",
			"tx_discards",
			"counters_available",
			"adapter_found",
			"error",
		})
	}

	ethernet, radio := &EthernetTracker{}, &RadioTracker{}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last map[string]any
	save := func(running bool) {
		if last == nil {
			return
		}

		last["running"] = running
		last["updated"] = time.Now().Format(time.RFC3339Nano)
		if err := diagnosisWriteJSON(filepath.Join(job.dir, folder+"-live.json"), last); err != nil {
			job.mu.Lock()
			if job.err == "" {
				job.err = err.Error()
			}

			job.mu.Unlock()
			job.cancel()
		}

		if err := diagnosisWriteJSON(filepath.Join(dir, "summary.json"), last); err != nil {
			job.mu.Lock()
			if job.err == "" {
				job.err = err.Error()
			}

			job.mu.Unlock()
			job.cancel()
		}

		job.mu.Lock()
		job.extra = last
		job.mu.Unlock()
	}
	defer func() {
		if err != nil && last != nil {
			last["error"] = err.Error()
		}

		save(false)
		writer.Flush()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if time.Since(job.start) >= s.duration {
			return
		}

		timestamp := time.Now().Format(time.RFC3339Nano)
		elapsed := float64(time.Since(job.start)) / float64(time.Millisecond)
		if folder == "radio" {
			sample, readErr := s.collector.Radio(ctx, job.adapter)
			if readErr != nil {
				sample.Error = readErr.Error()
			}

			if sample.Time == "" {
				sample.Time = timestamp
			}

			radio.Observe(sample)
			rawSample := sample.Raw
			if rawSample == nil {
				rawSample = map[string]any{
					"time":       sample.Time,
					"openError":  sample.StatisticsError,
					"statistics": map[string]any{"error": sample.StatisticsError, "phys": sample.Phys},
				}
			}

			if err = encoder.Encode(rawSample); err == nil {
				row := []string{
					sample.Time,
					strconv.FormatUint(uint64(sample.StatisticsError), 10),
					diagnosisNumber(sample.RSSIDbm),
					diagnosisNumber(sample.QualityPct),
					diagnosisNumber(sample.TxRateNative),
					diagnosisNumber(sample.RxRateNative),
				}
				if sample.StatisticsAvailable && len(sample.Phys) > 0 {
					for _, v := range radioCounters(sample.Phys[0]) {
						row = append(row, strconv.FormatUint(v, 10))
					}
				} else {
					row = append(row, "", "", "", "", "", "")
				}

				err = writer.Write(row)
			}

			last = map[string]any{
				"schemaVersion":        1,
				"kind":                 kind,
				"running":              true,
				"started":              job.started,
				"updated":              timestamp,
				"adapterName":          job.adapter.Name,
				"interfaceGuid":        sample.InterfaceGUID,
				"available":            radio.valid > 0,
				"latestStatsAvailable": sample.StatisticsAvailable,
				"logDirectory":         dir,
				"error":                nullableDiagnosisError(sample.Error),
				"latest": map[string]any{
					"time":         sample.Time,
					"rssiDbm":      sample.RSSIDbm,
					"qualityPct":   sample.QualityPct,
					"txRateNative": sample.TxRateNative,
					"rxRateNative": sample.RxRateNative,
				},
				"summary": radio.Summary(elapsed / 1000),
				"note":    radioNote,
			}
		} else {
			adapter := collectors.Adapter{State: "Unknown", Error: "AdapterNotFound"}
			adapters, readErr := s.collector.Adapters(ctx)
			if readErr != nil {
				adapter.Error = readErr.Error()
			} else {
				adapter.State = "Missing"
				for _, item := range adapters {
					if item.ID == job.adapter.ID {
						adapter = item
						break
					}
				}
			}

			for _, event := range ethernet.Observe(adapter, elapsed, timestamp) {
				if err = encoder.Encode(event); err != nil {
					break
				}
			}

			if err == nil {
				err = writer.Write([]string{
					timestamp,
					fmt.Sprintf("%.3f", elapsed),
					adapter.State,
					diagnosisCSVOptional(diagnosisSpeed(adapter)),
					diagnosisCSVOptional(diagnosisCounter(adapter.RxErrors, adapter.CountersAvailable)),
					diagnosisCSVOptional(diagnosisCounter(adapter.TxErrors, adapter.CountersAvailable)),
					diagnosisCSVOptional(diagnosisCounter(adapter.RxDiscards, adapter.CountersAvailable)),
					diagnosisCSVOptional(diagnosisCounter(adapter.TxDiscards, adapter.CountersAvailable)),
					strconv.FormatBool(adapter.CountersAvailable),
					strconv.FormatBool(adapter.ID != ""),
					adapter.Error,
				})
			}

			events := ethernet.events
			if events == nil {
				events = []map[string]any{}
			}

			last = map[string]any{
				"schemaVersion":        1,
				"kind":                 kind,
				"pid":                  os.Getpid(),
				"running":              true,
				"started":              job.started,
				"updated":              timestamp,
				"adapterName":          job.adapter.Name,
				"available":            ethernet.valid > 0,
				"latestStatsAvailable": adapter.CountersAvailable,
				"intervalMs":           500,
				"logDirectory":         dir,
				"error":                nil,
				"latest": map[string]any{
					"time":              timestamp,
					"elapsedMs":         elapsed,
					"state":             adapter.State,
					"speedMbps":         diagnosisSpeed(adapter),
					"adapterFound":      adapter.ID != "",
					"countersAvailable": adapter.CountersAvailable,
					"rxErrors":          diagnosisCounter(adapter.RxErrors, adapter.CountersAvailable),
					"txErrors":          diagnosisCounter(adapter.TxErrors, adapter.CountersAvailable),
					"rxDiscards":        diagnosisCounter(adapter.RxDiscards, adapter.CountersAvailable),
					"txDiscards":        diagnosisCounter(adapter.TxDiscards, adapter.CountersAvailable),
					"crcFcsAvailable":   false,
					"crcFcsErrors":      nil,
					"error":             nullableDiagnosisError(adapter.Error),
				},
				"summary": ethernet.Summary(elapsed),
				"events":  events,
				"note":    "Windows counters cover all adapter traffic; separate CRC/FCS counters are unavailable.",
			}
		}

		writer.Flush()
		if err == nil {
			err = writer.Error()
		}

		if err != nil {
			job.mu.Lock()
			if job.err == "" {
				job.err = err.Error()
			}

			job.mu.Unlock()
			job.cancel()

			return
		}

		save(true)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func diagnosisNumber(value any) string {
	switch v := value.(type) {
	case *uint32:
		if v != nil {
			return strconv.FormatUint(uint64(*v), 10)
		}
	case *int32:
		if v != nil {
			return strconv.FormatInt(int64(*v), 10)
		}
	}

	return ""
}

func diagnosisCounter(value int64, available bool) any {
	if !available || value < 0 {
		return nil
	}

	return value
}

func diagnosisSpeed(a collectors.Adapter) any {
	if a.ID == "" {
		return nil
	}

	return a.SpeedMbps
}

func diagnosisCSVOptional(value any) string {
	if value == nil {
		return ""
	}

	return fmt.Sprint(value)
}
