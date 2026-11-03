package collectors

import (
	"context"
	"errors"
	"net"
	"strconv"
)

// DialUDP creates a connected IPv4 UDP socket with the same explicit source and
// Windows IP_UNICAST_IF binding used by diagnostics. The caller owns Close.
func DialUDP(ctx context.Context, request ProbeRequest, port int) (*net.UDPConn, error) {
	source, destination, err := probeIPs(request)
	if err != nil {
		return nil, err
	}

	if port < 1 || port > 65535 {
		return nil, errors.New("UDP port is outside 1..65535")
	}

	if err = validateSocketSource(request); err != nil {
		return nil, err
	}

	dialer := net.Dialer{LocalAddr: &net.UDPAddr{IP: source}, Control: socketControl(request.InterfaceIndex)}
	connection, err := dialer.DialContext(ctx, "udp4", net.JoinHostPort(destination.String(), strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}

	return connection.(*net.UDPConn), nil
}
