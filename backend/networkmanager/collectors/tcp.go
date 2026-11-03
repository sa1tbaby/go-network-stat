package collectors

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"
)

func (c *nativeCollector) TCP(ctx context.Context, request ProbeRequest, port int) ProbeResult {
	source, destination, err := probeIPs(request)
	if err != nil {
		return failedProbe(err, 0)
	}

	if port < 1 || port > 65535 {
		return failedProbe(errors.New("TCP port is outside 1..65535"), 0)
	}

	if err = validateSocketSource(request); err != nil {
		return failedProbe(err, 0)
	}

	ctx, cancel := probeContext(ctx, request)
	defer cancel()
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: source}, Control: socketControl(request.InterfaceIndex)}
	started := time.Now()
	connection, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(destination.String(), strconv.Itoa(port)))
	if err != nil {
		return failedProbe(err, 0)
	}

	defer connection.Close()
	rtt := float64(time.Since(started)) / float64(time.Millisecond)

	return ProbeResult{
		OK:           true,
		Received:     true,
		RTTMs:        &rtt,
		Status:       "Success",
		ReplyAddress: destination.String(),
	}
}
