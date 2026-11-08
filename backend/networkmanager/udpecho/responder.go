package udpecho

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

type ResponderConfig struct {
	BindAddress string
	Port        int
	Duration    time.Duration
}

func RunResponder(ctx context.Context, cfg ResponderConfig, out io.Writer) error {
	if cfg.BindAddress == "" {
		cfg.BindAddress = "0.0.0.0"
	}

	if cfg.Port == 0 {
		cfg.Port = 39091
	}

	ip := net.ParseIP(cfg.BindAddress).To4()
	if ip == nil {
		return errors.New("responder bind address must be IPv4")
	}

	if cfg.Port < 1024 || cfg.Port > 65535 {
		return errors.New("responder port must be within 1024..65535")
	}

	if cfg.Duration < 0 || cfg.Duration > 24*time.Hour {
		return errors.New("responder duration must be within 0..24 hours")
	}

	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: cfg.Port})
	if err != nil {
		return err
	}

	defer connection.Close()

	return Serve(ctx, connection, cfg.Duration, out)
}

// Serve runs the controlled echo responder on an already-bound UDP socket.
// This supports caller-selected ports and socket activation. It owns the
// connection and closes it on return or cancellation.
func Serve(ctx context.Context, connection *net.UDPConn, duration time.Duration, out io.Writer) error {
	if connection == nil {
		return errors.New("UDP responder socket is required")
	}

	defer connection.Close()
	if out == nil {
		out = io.Discard
	}

	if duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, duration)
		defer cancel()
	}

	stopClose := context.AfterFunc(ctx, func() {
		connection.Close()
	})
	defer stopClose()
	if err := connection.SetReadBuffer(1 << 20); err != nil {
		return err
	}

	fmt.Fprintf(out, "UDP responder %s; max1200 bytes; max200pps/IP. Ctrl+C stops. No firewall changes.\n", connection.LocalAddr())
	var echoed, rejected, limited uint64
	defer func() {
		fmt.Fprintf(out, "Stopped. Echoed=%d rejected=%d rateLimited=%d\n", echoed, rejected, limited)
	}()
	limiter := RateLimiter{}
	buffer := make([]byte, 65535)
	nextReport := time.Now()
	for {
		if ctx.Err() != nil {
			return nil
		}

		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, peer, err := connection.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				continue
			}

			return err
		}

		if _, valid := ParsePacket(buffer[:n]); !valid {
			rejected++
			continue
		}

		now := time.Now()
		if !limiter.Allow(peer.IP.String(), now) {
			limited++
			continue
		}

		if _, err = connection.WriteToUDP(buffer[:n], peer); err != nil {
			rejected++
		} else {
			echoed++
		}

		if !now.Before(nextReport) {
			fmt.Fprintf(out, "Echoed=%d rejected=%d rateLimited=%d\n", echoed, rejected, limited)
			nextReport = now.Add(10 * time.Second)
		}
	}
}
