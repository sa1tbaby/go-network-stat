package collectors

import (
	"context"
	"errors"
	"net"
	"time"
)

func probeContext(ctx context.Context, request ProbeRequest) (context.Context, context.CancelFunc) {
	timeout := request.TimeoutMs
	if timeout <= 0 {
		timeout = 1200
	}

	if timeout > 60000 {
		timeout = 60000
	}

	return context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
}

func probeIPs(request ProbeRequest) (net.IP, net.IP, error) {
	source, destination := net.ParseIP(request.Source).To4(), net.ParseIP(request.Address).To4()
	if source == nil || source.IsUnspecified() {
		return nil, nil, errors.New("source must be a specific IPv4 address")
	}

	if destination == nil || destination.IsUnspecified() {
		return nil, nil, errors.New("target must be a specific IPv4 address")
	}

	return source, destination, nil
}

func failedProbe(err error, payload int) ProbeResult {
	result := ProbeResult{
		Status:    "ToolError: " + err.Error(),
		ToolError: true,
		Bytes:     payload,
	}
	if errors.Is(err, context.Canceled) {
		result.Status = "Cancelled"

		return result
	}

	if errors.Is(err, context.DeadlineExceeded) {
		result.Status = "TimedOut"
		result.ToolError = false

		return result
	}

	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		result.Status = "TimedOut"
		result.ToolError = false

		return result
	}

	if status, tool, ok := socketError(err); ok {
		result.Status, result.ToolError = status, tool
	}

	return result
}
