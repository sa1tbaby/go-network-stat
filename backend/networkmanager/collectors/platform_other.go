//go:build !windows

package collectors

import (
	"context"
	"errors"
	"syscall"
)

type nativeCollector struct{}

func New() Collector {
	return &nativeCollector{}
}

func (c *nativeCollector) Adapters(ctx context.Context) ([]Adapter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return nil, ErrWindowsRequired
}

func (c *nativeCollector) ICMP(ctx context.Context, r ProbeRequest) ProbeResult {
	if err := ctx.Err(); err != nil {
		return failedProbe(err, r.PayloadBytes)
	}

	return failedProbe(ErrWindowsRequired, r.PayloadBytes)
}

func (c *nativeCollector) Radio(ctx context.Context, a Adapter) (RadioSample, error) {
	return RadioSample{}, ErrWindowsRequired
}

func socketControl(index int) func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		if index != 0 {
			return ErrWindowsRequired
		}

		return nil
	}
}

func validateSocketSource(request ProbeRequest) error {
	return nil
}

func socketError(err error) (string, bool, bool) {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return "", false, false
	}

	switch errno {
	case syscall.ECONNREFUSED:
		return "ConnectionRefused", false, true
	case syscall.ECONNRESET:
		return "ConnectionReset", false, true
	case syscall.ENETUNREACH:
		return "NetworkUnreachable", false, true
	case syscall.EHOSTUNREACH:
		return "HostUnreachable", false, true
	case syscall.EACCES, syscall.EPERM:
		return "AccessDenied", true, true
	default:
		return "SocketError: " + errno.Error(), true, true
	}
}
