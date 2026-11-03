package collectors

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"time"
)

type dnsPending struct {
	sent time.Time
	lag  float64
}

type dnsDatagram struct {
	data []byte
	at   time.Time
	err  error
}

func (c *nativeCollector) StreamDNS(ctx context.Context, request ProbeRequest, pps int, emit func(DNSObservation)) error {
	port, err := dnsPort(request)
	if err != nil {
		return err
	}

	return c.streamDNS(ctx, request, pps, port, emit)
}

func (c *nativeCollector) streamDNS(ctx context.Context, request ProbeRequest, pps, port int, emit func(DNSObservation)) error {
	notifyPending := func(count int) {
		if request.OnPending != nil {
			request.OnPending(count)
		}
	}
	notifyPending(0)
	defer notifyPending(0)
	if pps < 1 || pps > 100 {
		return errors.New("DNS sampling rate must be within 1..100 packets per second")
	}

	if emit == nil {
		return errors.New("DNS stream callback is required")
	}

	src, dst, err := probeIPs(request)
	if err != nil {
		return err
	}

	if err = validateSocketSource(request); err != nil {
		return err
	}

	dialer := net.Dialer{LocalAddr: &net.UDPAddr{IP: src}, Control: socketControl(request.InterfaceIndex)}
	connection, err := dialer.DialContext(ctx, "udp4", net.JoinHostPort(dst.String(), strconv.Itoa(port)))
	if err != nil {
		return err
	}

	defer connection.Close()
	readerCtx, cancelReader := context.WithCancel(ctx)
	defer cancelReader()
	stopClose := context.AfterFunc(readerCtx, func() {
		connection.Close()
	})
	defer stopClose()
	packets := make(chan dnsDatagram, 128)
	go readDNSPackets(readerCtx, connection, packets)
	var random [2]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}

	sequence := binary.BigEndian.Uint16(random[:])
	pending := make(map[uint16]dnsPending)
	expired := make(map[uint16]dnsPending)
	step := time.Second / time.Duration(pps)
	timeout := time.Duration(request.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}

	if timeout > time.Minute {
		timeout = time.Minute
	}

	started := time.Now()
	next := started
	var sendDeadline time.Time
	if request.DurationMs > 0 {
		sendDeadline = started.Add(time.Duration(request.DurationMs) * time.Millisecond)
	}

	tick := time.NewTicker(min(10*time.Millisecond, step))
	defer tick.Stop()
	// The scheduler owns the maps and callback. Reader timestamps are captured
	// before channel delivery so scheduler delay cannot turn an on-time reply
	// into measured network loss.
	for {
		if ctx.Err() != nil {
			return nil
		}

		now := time.Now()
		sending := sendDeadline.IsZero() || now.Before(sendDeadline)
		if sending && !now.Before(next) {
			lag := max(0, float64(now.Sub(next))/float64(time.Millisecond))
			id := sequence
			found := false
			for attempts := 0; attempts < 65536; attempts++ {
				id = sequence
				sequence++
				if _, used := pending[id]; used {
					continue
				}

				if _, used := expired[id]; used {
					continue
				}

				found = true
				break
			}

			if !found {
				return errors.New("DNS stream transaction space exhausted")
			}

			if _, err = connection.Write(DNSQuery(id)); err != nil {
				if ctx.Err() != nil {
					return nil
				}

				result := failedProbe(err, 29)
				result.ToolError = true
				result.Status = "LocalSendError: " + result.Status
				emit(DNSObservation{
					Sent:   now,
					At:     time.Now(),
					Lag:    lag,
					Result: result,
				})
			} else {
				pending[id] = dnsPending{sent: now, lag: lag}
				notifyPending(len(pending))
			}

			next = next.Add(step)
			if now.Sub(next) > 4*step {
				next = now.Add(step)
			}
		}
		// Process any queued datagrams before expiring requests. Their arrival
		// time, not the time this goroutine was scheduled, determines the RTT.
		for drain := 0; drain < 128; drain++ {
			select {
			case packet := <-packets:
				if packet.err != nil {
					if ctx.Err() != nil {
						return nil
					}
					// A connected UDP socket reports ICMP port-unreachable as a
					// receive error. Keep pending deadlines and continue reading.
					if status, tool, ok := socketError(packet.err); ok && !tool && (status == "ConnectionReset" || status == "ConnectionRefused") {
						go readDNSPackets(readerCtx, connection, packets)
						continue
					}

					return packet.err
				}

				if len(packet.data) < 2 {
					continue
				}

				id := binary.BigEndian.Uint16(packet.data)
				if !ValidDNSReply(packet.data, id) {
					continue
				}

				if request, ok := pending[id]; ok {
					delete(pending, id)
					notifyPending(len(pending))
					rtt := float64(packet.at.Sub(request.sent)) / float64(time.Millisecond)
					result := ProbeResult{
						OK:           packet.at.Before(request.sent.Add(timeout)),
						Received:     true,
						RTTMs:        &rtt,
						Status:       "Success_DNS_RCODE_" + strconv.Itoa(int(packet.data[3]&15)),
						ReplyAddress: dst.String(),
						Bytes:        29,
					}
					if !result.OK {
						result.Status = "LateAfterDeadline"
					}

					emit(DNSObservation{
						Sent:   request.sent,
						At:     packet.at,
						Lag:    request.lag,
						Result: result,
					})
				} else if _, ok := expired[id]; ok {
					delete(expired, id)
					emit(DNSObservation{
						At:   packet.at,
						Late: true,
						Result: ProbeResult{
							Received:     true,
							Status:       "LateReplyAfterTimeout",
							ReplyAddress: dst.String(),
							Bytes:        29,
						},
					})
				}
			default:
				drain = 128
			}
		}

		now = time.Now()
		for id, request := range pending {
			if !now.Before(request.sent.Add(timeout)) {
				delete(pending, id)
				notifyPending(len(pending))
				expired[id] = request
				emit(DNSObservation{
					Sent:   request.sent,
					At:     now,
					Lag:    request.lag,
					Result: ProbeResult{Status: "TimedOut", Bytes: 29},
				})
			}
		}

		if !sending && len(pending) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// readDNSPackets restarts reading after Windows/Unix surfaces an asynchronous
// ICMP error for an earlier UDP datagram. It never creates a second live reader.
func readDNSPackets(ctx context.Context, connection net.Conn, packets chan<- dnsDatagram) {
	buffer := make([]byte, 4096)
	for {
		n, err := connection.Read(buffer)
		packet := dnsDatagram{at: time.Now(), err: err}
		if err == nil {
			packet.data = append([]byte(nil), buffer[:n]...)
		}

		select {
		case packets <- packet:
		case <-ctx.Done():
			return
		}

		if err != nil {
			return
		}
	}
}
