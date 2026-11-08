package udpecho

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

type pendingEcho struct {
	sent time.Time
	tick uint64
}

type echoStats struct {
	sent          uint64
	received      uint64
	lost          uint64
	duplicates    uint64
	reorder       uint64
	late          uint64
	ignored       uint64
	localErrors   uint64
	highest       uint32
	rtts          []float64
	jitterSum     float64
	jitterCount   uint64
	previousRTT   *float64
	pacingMax     float64
	loopMax       float64
	receiveGapMax float64
}

// Test verifies a nonce-bound handshake before recording any probe failures.
// Its summary and CSV preserve the standalone PowerShell utility's fields.
func Test(ctx context.Context, config TestConfig, out io.Writer) (path string, resultError error) {
	cfg, err := normalizeTest(ctx, config)
	if err != nil {
		return "", err
	}

	if out == nil {
		out = io.Discard
	}

	if err = os.MkdirAll(cfg.LogDirectory, 0700); err != nil {
		return "", err
	}

	path = filepath.Join(cfg.LogDirectory, "summary.json")
	file, err := os.OpenFile(filepath.Join(cfg.LogDirectory, "probes.csv"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return path, err
	}

	defer file.Close()
	_, _ = file.Write([]byte{0xef, 0xbb, 0xbf})
	csvWriter := csv.NewWriter(file)
	defer csvWriter.Flush()
	_ = csvWriter.Write([]string{"event_time", "sent_time", "sequence", "status", "rtt_ms", "source_ip", "interface_index", "server", "port"})
	stats := echoStats{}
	started := time.Now()
	clockStart := started
	verified := false
	pending := map[uint32]pendingEcho{}
	summary := map[string]any{
		"started":                  started.Format(time.RFC3339Nano),
		"server":                   cfg.Server,
		"port":                     cfg.Port,
		"sourceIp":                 cfg.Source,
		"interfaceIndex":           cfg.InterfaceIndex,
		"adapter":                  cfg.AdapterName,
		"ppsRequested":             cfg.PPS,
		"payloadBytes":             cfg.PayloadBytes,
		"durationRequestedSeconds": cfg.Duration.Seconds(),
		"deadlineMs":               1200,
		"protocol":                 "Controlled UDP echo; not a game protocol",
		"directionNote":            "Echo failures combine outbound and return paths; upload/download loss is not separated.",
		"handshakeVerified":        false,
	}
	row := func(sequence uint32, status string, rtt *float64, sent time.Time) {
		value, stamp := "", ""
		if rtt != nil {
			value = strconv.FormatFloat(*rtt, 'f', -1, 64)
		}

		if !sent.IsZero() {
			stamp = sent.Format(time.RFC3339Nano)
		}

		_ = csvWriter.Write([]string{
			time.Now().Format(time.RFC3339Nano),
			stamp,
			strconv.FormatUint(uint64(sequence), 10),
			status,
			value,
			cfg.Source,
			strconv.Itoa(cfg.InterfaceIndex),
			cfg.Server,
			strconv.Itoa(cfg.Port),
		})
		csvWriter.Flush()
	}
	defer func() {
		elapsed := time.Since(clockStart).Seconds()
		completed := stats.received + stats.lost
		observed := stats.received + stats.late
		summary["handshakeVerified"] = verified
		summary["completed"] = time.Now().Format(time.RFC3339Nano)
		summary["elapsedSeconds"] = elapsed
		summary["stoppedByUser"] = ctx.Err() != nil
		summary["sentDatagrams"] = stats.sent
		summary["completedProbes"] = completed
		summary["timelyEchoes"] = stats.received
		summary["echoFailuresByDeadline"] = stats.lost
		summary["echoFailurePct"] = nil
		if completed > 0 {
			summary["echoFailurePct"] = 100 * float64(stats.lost) / float64(completed)
		}

		summary["pendingAtExit"] = len(pending)
		summary["duplicates"] = stats.duplicates
		summary["outOfOrderFirstReplies"] = stats.reorder
		summary["lateReplies"] = stats.late
		summary["ignoredDatagrams"] = stats.ignored
		summary["localSendErrors"] = stats.localErrors
		summary["avgRttMs"] = nil
		summary["p95RttMs"] = nil
		summary["maxRttMs"] = nil
		summary["rttJitterMs"] = nil
		sort.Float64s(stats.rtts)
		if len(stats.rtts) > 0 {
			sum := 0.0
			for _, rtt := range stats.rtts {
				sum += rtt
			}

			summary["avgRttMs"] = sum / float64(len(stats.rtts))
			summary["p95RttMs"] = stats.rtts[int(math.Ceil(float64(len(stats.rtts))*.95))-1]
			summary["maxRttMs"] = stats.rtts[len(stats.rtts)-1]
		}

		if stats.jitterCount > 0 {
			summary["rttJitterMs"] = stats.jitterSum / float64(stats.jitterCount)
		}

		summary["maxClientLoopGapMs"] = stats.loopMax
		summary["maxSendPacingLateMs"] = stats.pacingMax
		summary["maxReceiveGapMs"] = stats.receiveGapMax
		summary["actualPps"] = 0.0
		if elapsed > 0 {
			summary["actualPps"] = float64(stats.sent) / math.Min(cfg.Duration.Seconds(), elapsed)
		}

		thresholds := []any{}
		for _, threshold := range []float64{50, 100, 200} {
			above := stats.late
			for _, rtt := range stats.rtts {
				if rtt > threshold {
					above++
				}
			}

			var pct any
			if observed > 0 {
				pct = 100 * float64(above) / float64(observed)
			}

			thresholds = append(thresholds, map[string]any{
				"rttThresholdMs":      threshold,
				"observedEchoesAbove": above,
				"observedEchoes":      observed,
				"pctOfObservedEchoes": pct,
			})
		}

		summary["latencyThresholds"] = thresholds
		summary["lateObservationNote"] = "Late replies are observed only until test closure; missing replies are not proven permanent drops."
		if resultError != nil {
			summary["error"] = resultError.Error()
		}

		content, e := json.MarshalIndent(summary, "", "  ")
		if e == nil {
			e = os.WriteFile(path, content, 0600)
		}

		if resultError == nil && e != nil {
			resultError = e
		}

		fmt.Fprintf(out, "Report: %s\n", path)
	}()
	connection, err := collectors.DialUDP(ctx, collectors.ProbeRequest{
		Source:         cfg.Source,
		Address:        cfg.Server,
		InterfaceIndex: cfg.InterfaceIndex,
	}, cfg.Port)
	if err != nil {
		return path, err
	}

	defer connection.Close()
	if err = connection.SetReadBuffer(1 << 20); err != nil {
		return path, err
	}

	stopClose := context.AfterFunc(ctx, func() {
		connection.Close()
	})
	defer stopClose()
	var nonceBytes [8]byte
	if _, err = rand.Read(nonceBytes[:]); err != nil {
		return path, err
	}

	nonce := binary.LittleEndian.Uint64(nonceBytes[:])
	tickEpoch := time.Now()
	handshakeTick := uint64(time.Since(tickEpoch)) + 1
	handshake, _ := Packet(32, 0, handshakeTick, nonce)
	if _, err = connection.Write(handshake); err != nil {
		return path, err
	}

	buffer := make([]byte, 65535)
	handshakeDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(handshakeDeadline) && ctx.Err() == nil {
		_ = connection.SetReadDeadline(handshakeDeadline)
		n, readErr := connection.Read(buffer)
		if readErr != nil {
			break
		}

		header, valid := ParsePacket(buffer[:n])
		if valid && header.Nonce == nonce && header.Sequence == 0 && header.Tick == handshakeTick {
			verified = true
			break
		}
	}

	if !verified {
		return path, errors.New("no valid controlled-responder handshake; test was not started and this is not measured 100% loss")
	}
	// Ctrl+C stops sending, then drains the requests already sent to their
	// deadlines, matching the original controlled echo utility. Cancellation
	// during the handshake still closes the blocking socket immediately.
	if !stopClose() && ctx.Err() != nil {
		return path, nil
	}

	fmt.Fprintf(out, "Verified UDP responder. %s /if%d -> %s:%d; %dpps; %d bytes; %s. Ctrl+C stops.\n", cfg.Source, cfg.InterfaceIndex, cfg.Server, cfg.Port, cfg.PPS, cfg.PayloadBytes, cfg.Duration)
	clockStart = time.Now()
	end := clockStart.Add(cfg.Duration)
	nextDue := clockStart
	step := time.Second / time.Duration(cfg.PPS)
	next := uint32(1)
	seen := map[uint32]bool{}
	expired := map[uint32]pendingEcho{}
	lateSeen := map[uint32]bool{}
	var lastLoop, lastReceive time.Time
	nextReport := clockStart
	for (ctx.Err() == nil && time.Now().Before(end)) || len(pending) > 0 {
		now := time.Now()
		if !lastLoop.IsZero() {
			stats.loopMax = math.Max(stats.loopMax, float64(now.Sub(lastLoop))/float64(time.Millisecond))
		}

		lastLoop = now
		if ctx.Err() == nil && now.Before(end) && !now.Before(nextDue) {
			stats.pacingMax = math.Max(stats.pacingMax, float64(now.Sub(nextDue))/float64(time.Millisecond))
			sequence := next
			next++
			tick := uint64(now.Sub(tickEpoch)) + 1
			packet, _ := Packet(cfg.PayloadBytes, sequence, tick, nonce)
			if _, err = connection.Write(packet); err != nil {
				if ctx.Err() != nil {
					return path, nil
				}

				stats.localErrors++
				row(sequence, "LocalSendError:"+err.Error(), nil, now)
			} else {
				pending[sequence] = pendingEcho{sent: now, tick: tick}
				stats.sent++
			}

			nextDue = nextDue.Add(step)
			if now.Sub(nextDue) > 4*step {
				nextDue = now.Add(step)
			}
		}

		_ = connection.SetReadDeadline(time.Now().Add(2 * time.Millisecond))
		n, readErr := connection.Read(buffer)
		at := time.Now()
		if readErr == nil {
			header, valid := ParsePacket(buffer[:n])
			if !valid || header.Nonce != nonce || header.Sequence == 0 || header.Sequence >= next {
				stats.ignored++
			} else if request, ok := pending[header.Sequence]; ok && header.Tick != request.tick {
				stats.ignored++
			} else if request, ok := expired[header.Sequence]; ok && header.Tick != request.tick {
				stats.ignored++
			} else if seen[header.Sequence] {
				stats.duplicates++
				row(header.Sequence, "Duplicate", nil, time.Time{})
			} else {
				seen[header.Sequence] = true
				if header.Sequence < stats.highest {
					stats.reorder++
				} else {
					stats.highest = header.Sequence
				}

				if request, ok := pending[header.Sequence]; ok {
					delete(pending, header.Sequence)
					rtt := float64(at.Sub(request.sent)) / float64(time.Millisecond)
					if rtt > 1200 {
						stats.lost++
						stats.late++
						expired[header.Sequence] = request
						lateSeen[header.Sequence] = true
						row(header.Sequence, "LateAfterDeadline", &rtt, request.sent)
					} else {
						stats.received++
						stats.rtts = append(stats.rtts, rtt)
						if stats.previousRTT != nil {
							stats.jitterSum += math.Abs(rtt - *stats.previousRTT)
							stats.jitterCount++
						}

						stats.previousRTT = &rtt
						row(header.Sequence, "Success", &rtt, request.sent)
					}

					if !lastReceive.IsZero() {
						stats.receiveGapMax = math.Max(stats.receiveGapMax, float64(at.Sub(lastReceive))/float64(time.Millisecond))
					}

					lastReceive = at
				} else if _, ok := expired[header.Sequence]; ok {
					if !lateSeen[header.Sequence] {
						stats.late++
						lateSeen[header.Sequence] = true
						row(header.Sequence, "LateAfterTimeout", nil, time.Time{})
					}
				} else {
					stats.ignored++
				}
			}
		} else {
			var nerr net.Error
			if !errors.As(readErr, &nerr) || !nerr.Timeout() {
				var errno syscall.Errno
				if errors.As(readErr, &errno) && (errno == syscall.ECONNRESET || errno == syscall.ECONNREFUSED || errno == 10054) {
					stats.ignored++
				} else {
					return path, readErr
				}
			}
		}

		check := time.Now()
		for sequence, request := range pending {
			if check.Sub(request.sent) >= 1200*time.Millisecond {
				delete(pending, sequence)
				expired[sequence] = request
				stats.lost++
				row(sequence, "TimedOut", nil, request.sent)
			}
		}

		if !check.Before(nextReport) {
			done := stats.received + stats.lost
			pct := "n/a"
			if done > 0 {
				pct = fmt.Sprintf("%.2f", 100*float64(stats.lost)/float64(done))
			}

			fmt.Fprintf(out, "t=%.1fs sent=%d completed=%d echoFailures=%d (%s%%) pending=%d reorder=%d\n", check.Sub(clockStart).Seconds(), stats.sent, done, stats.lost, pct, len(pending), stats.reorder)
			nextReport = check.Add(time.Second)
		}
	}

	if err = csvWriter.Error(); err != nil {
		return path, err
	}

	return path, nil
}
