package collectors

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// DNSQuery sends a real UDP question rather than timing an OS resolver/cache.
// The same question is used by the original diagnostics: example.com IN A.
func DNSQuery(id uint16) []byte {
	query := []byte{0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	binary.BigEndian.PutUint16(query, id)

	return query
}

// ValidDNSReply requires the matching transaction, response bit, standard
// opcode, and original question. A DNS failure RCODE still proves a response
// from the selected DNS peer and is recorded with that RCODE by DNS().
func ValidDNSReply(packet []byte, id uint16) bool {
	question := DNSQuery(id)

	return len(packet) >= len(question) && binary.BigEndian.Uint16(packet) == id &&
		packet[2]&0x80 != 0 && packet[2]&0x78 == 0 && binary.BigEndian.Uint16(packet[4:6]) == 1 && bytes.Equal(packet[12:len(question)], question[12:])
}

func (c *nativeCollector) DNS(ctx context.Context, request ProbeRequest) ProbeResult {
	port, err := dnsPort(request)
	if err != nil {
		return failedProbe(err, 29)
	}

	return c.dnsProbe(ctx, request, port)
}

func dnsPort(request ProbeRequest) (int, error) {
	port := request.DNSPort
	if port == 0 {
		port = 53
	}

	if port < 1 || port > 65535 {
		return 0, errors.New("DNS port must be within 1..65535")
	}

	return port, nil
}

func (c *nativeCollector) dnsProbe(ctx context.Context, request ProbeRequest, port int) ProbeResult {
	source, destination, err := probeIPs(request)
	if err != nil {
		return failedProbe(err, 29)
	}

	if err = validateSocketSource(request); err != nil {
		return failedProbe(err, 29)
	}

	ctx, cancel := probeContext(ctx, request)
	defer cancel()
	dialer := net.Dialer{LocalAddr: &net.UDPAddr{IP: source}, Control: socketControl(request.InterfaceIndex)}
	connection, err := dialer.DialContext(ctx, "udp4", net.JoinHostPort(destination.String(), strconv.Itoa(port)))
	if err != nil {
		return failedProbe(err, 29)
	}

	defer connection.Close()
	deadline, _ := ctx.Deadline()
	if err = connection.SetDeadline(deadline); err != nil {
		return failedProbe(err, 29)
	}
	// Closing the socket also interrupts a read when the owner stops the job.
	stopClose := context.AfterFunc(ctx, func() {
		connection.Close()
	})
	defer stopClose()
	var random [2]byte
	if _, err = rand.Read(random[:]); err != nil {
		return failedProbe(err, 29)
	}

	id := binary.BigEndian.Uint16(random[:])
	query := DNSQuery(id)
	started := time.Now()
	if _, err = connection.Write(query); err != nil {
		return failedProbe(err, len(query))
	}

	buffer := make([]byte, 4096)
	for {
		read, err := connection.Read(buffer)
		if err != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			}

			return failedProbe(err, len(query))
		}

		if !ValidDNSReply(buffer[:read], id) {
			continue
		}

		rtt := float64(time.Since(started)) / float64(time.Millisecond)
		if ctx.Err() == context.Canceled {
			return failedProbe(ctx.Err(), len(query))
		}

		if !time.Now().Before(deadline) {
			return ProbeResult{
				Received:     true,
				RTTMs:        &rtt,
				Status:       "LateAfterDeadline",
				ReplyAddress: destination.String(),
				Bytes:        len(query),
			}
		}

		return ProbeResult{
			OK:           true,
			Received:     true,
			RTTMs:        &rtt,
			Status:       fmt.Sprintf("Success_DNS_RCODE_%d", buffer[3]&15),
			ReplyAddress: destination.String(),
			Bytes:        len(query),
		}
	}
}
