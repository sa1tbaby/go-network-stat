package diagnostics

import (
	"context"
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

const diagnosisNote = "ICMP and valid UDP DNS replies compare network segments. They do not measure game UDP loss. No reply from a target that has never responded means the measurement is unavailable; an RTT threshold exceedance indicates delay."

func (s *Service) run(ctx context.Context, job *diagnosisJob) {
	defer close(job.done)
	defer job.cancel()
	networkDir := filepath.Join(job.dir, "diagnostics", job.start.Format("20060102-150405")+"-"+strconv.Itoa(os.Getpid()))
	err := os.MkdirAll(networkDir, 0700)
	var file *os.File
	if err == nil {
		file, err = os.Create(filepath.Join(networkDir, "probes.csv"))
	}

	if err != nil {
		job.mu.Lock()
		job.err = err.Error()
		job.running = false
		_ = job.saveRuntime()
		job.mu.Unlock()

		return
	}

	defer file.Close()
	csvWriter := csv.NewWriter(file)
	_ = csvWriter.Write([]string{
		"event_time",
		"sent_time",
		"target_id",
		"protocol",
		"address",
		"source_ip",
		"interface_index",
		"payload_bytes",
		"status",
		"rtt_ms",
		"scheduler_lag_ms",
	})
	csvWriter.Flush()
	var wg sync.WaitGroup
	for _, target := range job.targets {
		wg.Add(1)
		go func(t *diagnosisTarget) {
			defer wg.Done()
			s.probeLoop(ctx, job, t, csvWriter)
		}(target)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.extraLoop(ctx, job)
	}()
	workersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(workersDone)
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	loop := true
	for loop {
		select {
		case <-ctx.Done():
			loop = false
		case <-workersDone:
			loop = false
		case <-ticker.C:
			adapters, readErr := s.collector.Adapters(ctx)
			if readErr != nil && ctx.Err() == nil {
				job.mu.Lock()
				job.err = "Failed to verify the selected connection: " + readErr.Error()
				job.mu.Unlock()
				job.cancel()
			} else if readErr == nil {
				valid := false
				for _, a := range adapters {
					if a.ID == job.adapter.ID && a.Available && a.IPv4 == job.adapter.IPv4 && a.Index == job.adapter.Index && a.ICMPSafe {
						valid = true
						break
					}
				}

				if !valid {
					job.mu.Lock()
					job.err = "The selected connection changed during the measurement."
					job.mu.Unlock()
					job.cancel()
				}
			}

			if _, e := os.Stat(filepath.Join(job.dir, "diagnostics-stop.flag")); e == nil {
				job.cancel()
			}

			if e := s.saveNetwork(job, networkDir, true); e != nil {
				job.mu.Lock()
				job.err = e.Error()
				job.mu.Unlock()
				job.cancel()
			}
		}
	}

	wg.Wait()
	job.mu.Lock()
	csvWriter.Flush()
	if csvWriter.Error() != nil && job.err == "" {
		job.err = csvWriter.Error().Error()
	}

	if err := file.Sync(); err != nil && job.err == "" {
		job.err = err.Error()
	}

	if err := file.Close(); err != nil && job.err == "" {
		job.err = err.Error()
	}

	job.mu.Unlock()
	if err := s.saveNetwork(job, networkDir, false); err != nil {
		job.mu.Lock()
		job.err = err.Error()
		job.mu.Unlock()
	}
	// Storage switching/reset must remain blocked until every final snapshot
	// and CSV handle has finished; an idle flag before this point races cleanup.
	job.mu.Lock()
	job.running = false
	_ = job.saveRuntime()
	job.mu.Unlock()
	s.host.Event("diagnosis_complete", "Test completed: "+job.adapter.Name)
}

func (s *Service) probeLoop(ctx context.Context, job *diagnosisJob, probeTarget *diagnosisTarget, writer *csv.Writer) {
	if probeTarget.Protocol == "udp_dns" {
		if stream, ok := s.collector.(collectors.DNSStreamCollector); ok {
			remaining := time.Until(job.start.Add(s.duration))
			if remaining <= 0 {
				return
			}

			err := stream.StreamDNS(ctx, collectors.ProbeRequest{
				Source:         job.adapter.IPv4,
				Address:        probeTarget.Address,
				InterfaceIndex: job.adapter.Index,
				PayloadBytes:   29,
				TimeoutMs:      1200,
				DurationMs:     max(1, int(remaining/time.Millisecond)),
				OnPending: func(count int) {
					job.mu.Lock()
					probeTarget.Pending = count
					job.mu.Unlock()
				},
			}, probeTarget.PPS, func(observation collectors.DNSObservation) {
				result := observation.Result
				result.Bytes = 29
				s.saveProbe(job, probeTarget, writer, diagnosisProbe{
					Sent:   observation.Sent,
					At:     observation.At,
					Lag:    observation.Lag,
					Result: result,
				})
			})
			if err != nil && ctx.Err() == nil {
				s.saveProbe(job, probeTarget, writer, diagnosisProbe{
					Sent: time.Now(),
					At:   time.Now(),
					Result: collectors.ProbeResult{
						Status:    "ToolError:" + err.Error(),
						ToolError: true,
						Bytes:     29,
					},
				})
			}

			return
		}
	}

	step := time.Second / time.Duration(probeTarget.PPS)
	ticker := time.NewTicker(step)
	defer ticker.Stop()
	due := time.Now()
	sequence := 0
	var probes sync.WaitGroup
	defer probes.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if time.Since(job.start) >= s.duration {
			return
		}

		job.mu.Lock()
		pending := probeTarget.Pending
		if pending < 20 {
			probeTarget.Pending++
		}

		job.mu.Unlock()
		bytes := 29
		if probeTarget.Protocol == "icmp" {
			bytes = []int{64, 600, 1200}[sequence%3]
		}

		sequence++
		lag := math.Max(0, float64(time.Since(due))/float64(time.Millisecond))
		sent := time.Now()
		if pending >= 20 {
			s.saveProbe(job, probeTarget, writer, diagnosisProbe{
				Sent: sent,
				At:   sent,
				Lag:  lag,
				Result: collectors.ProbeResult{
					Status:    "LocalConcurrencyLimit",
					ToolError: true,
					Bytes:     bytes,
				},
			})
		} else {
			probes.Add(1)
			go func(payload int, sent time.Time, lag float64) {
				defer probes.Done()
				request := collectors.ProbeRequest{
					Source:         job.adapter.IPv4,
					Address:        probeTarget.Address,
					InterfaceIndex: job.adapter.Index,
					TTL:            128,
					PayloadBytes:   payload,
					TimeoutMs:      1200,
				}
				actualLag := lag + float64(time.Since(sent))/float64(time.Millisecond)
				var result collectors.ProbeResult
				if probeTarget.Protocol == "icmp" {
					result = s.collector.ICMP(ctx, request)
				} else {
					result = s.collector.DNS(ctx, request)
				}

				result.Bytes = payload
				job.mu.Lock()
				probeTarget.Pending--
				job.mu.Unlock()
				s.saveProbe(job, probeTarget, writer, diagnosisProbe{
					Sent:   sent,
					At:     time.Now(),
					Lag:    actualLag,
					Result: result,
				})
			}(bytes, sent, lag)
		}

		due = due.Add(step)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if time.Since(due) > 4*step {
			due = time.Now()
		}
	}
}

func (s *Service) saveProbe(job *diagnosisJob, probeTarget *diagnosisTarget, writer *csv.Writer, row diagnosisProbe) {
	job.mu.Lock()
	defer job.mu.Unlock()
	probeTarget.Rows = append(probeTarget.Rows, row)
	job.maxLag = math.Max(job.maxLag, row.Lag)
	rtt := ""
	if row.Result.RTTMs != nil {
		rtt = strconv.FormatFloat(*row.Result.RTTMs, 'f', 3, 64)
	}

	sent := ""
	if !row.Sent.IsZero() {
		sent = row.Sent.Format(time.RFC3339Nano)
	}

	err := writer.Write([]string{
		row.At.Format(time.RFC3339Nano),
		sent,
		probeTarget.ID,
		probeTarget.Protocol,
		probeTarget.Address,
		job.adapter.IPv4,
		strconv.Itoa(job.adapter.Index),
		strconv.Itoa(row.Result.Bytes),
		row.Result.Status,
		rtt,
		strconv.FormatFloat(row.Lag, 'f', 3, 64),
	})
	writer.Flush()
	if err == nil {
		err = writer.Error()
	}

	if err != nil && job.err == "" {
		job.err = err.Error()
		job.cancel()
	}
}

func (s *Service) saveNetwork(job *diagnosisJob, dir string, running bool) error {
	job.mu.Lock()
	targets := make([]any, 0, len(job.targets))
	for _, t := range job.targets {
		targets = append(targets, diagnosisMetrics(t))
	}

	network := map[string]any{
		"appId":             "codex-single-pc-diagnostics",
		"running":           running,
		"pid":               os.Getpid(),
		"started":           job.started,
		"now":               time.Now().Format(time.RFC3339Nano),
		"sourceIp":          job.adapter.IPv4,
		"interfaceIndex":    job.adapter.Index,
		"adapterName":       job.adapter.Name,
		"durationSeconds":   s.duration.Seconds(),
		"elapsedSeconds":    time.Since(job.start).Seconds(),
		"deadlineMs":        1200,
		"maxSchedulerLagMs": job.maxLag,
		"logDir":            dir,
		"error":             nullableDiagnosisError(job.err),
		"note":              diagnosisNote,
		"targets":           targets,
	}
	job.network = network
	job.mu.Unlock()
	if err := diagnosisWriteJSON(filepath.Join(job.dir, "diagnostics-live.json"), network); err != nil {
		return err
	}

	return diagnosisWriteJSON(filepath.Join(dir, "summary.json"), network)
}
