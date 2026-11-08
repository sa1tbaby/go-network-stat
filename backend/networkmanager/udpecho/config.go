package udpecho

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

type TestConfig struct {
	Server         string
	AdapterName    string
	Source         string
	LogDirectory   string
	Port           int
	InterfaceIndex int
	PPS            int
	PayloadBytes   int
	Duration       time.Duration
}

func selectSource(ctx context.Context, cfg *TestConfig) error {
	if cfg.Source != "" {
		return nil
	}

	if cfg.AdapterName == "Loopback" {
		interfaces, err := net.Interfaces()
		if err != nil {
			return err
		}

		for _, adapter := range interfaces {
			if adapter.Flags&net.FlagLoopback == 0 {
				continue
			}

			addresses, err := adapter.Addrs()
			if err != nil {
				continue
			}

			for _, address := range addresses {
				ip, _, _ := net.ParseCIDR(address.String())
				if ip.To4() != nil {
					cfg.Source = ip.String()
					cfg.InterfaceIndex = adapter.Index

					return nil
				}
			}
		}

		return errors.New("no IPv4 loopback interface found")
	}

	adapters, err := collectors.New().Adapters(ctx)
	if err != nil {
		return err
	}

	sort.SliceStable(adapters, func(i, j int) bool {
		return adapters[i].Kind == "wifi" && adapters[j].Kind != "wifi"
	})
	for _, adapter := range adapters {
		if adapter.Available && (cfg.AdapterName == "" || cfg.AdapterName == adapter.Name) {
			cfg.Source = adapter.IPv4
			cfg.InterfaceIndex = adapter.Index
			cfg.AdapterName = adapter.Name

			return nil
		}
	}

	return errors.New("no selected active physical adapter; specify its exact name (Loopback is for local QA)")
}

func normalizeTest(ctx context.Context, cfg TestConfig) (TestConfig, error) {
	if cfg.Port == 0 {
		cfg.Port = 39091
	}

	if cfg.PPS == 0 {
		cfg.PPS = 60
	}

	if cfg.PayloadBytes == 0 {
		cfg.PayloadBytes = 256
	}

	if cfg.Duration == 0 {
		cfg.Duration = time.Minute
	}

	if cfg.Server == "" {
		return cfg, errors.New("controlled UDP server is required")
	}

	if cfg.Port < 1024 || cfg.Port > 65535 {
		return cfg, errors.New("UDP port must be within 1024..65535")
	}

	if cfg.PPS < 1 || cfg.PPS > 120 {
		return cfg, errors.New("UDP sampling rate must be within 1..120 packets per second")
	}

	if cfg.PayloadBytes < 32 || cfg.PayloadBytes > 1200 {
		return cfg, errors.New("UDP payload must be within 32..1200 bytes")
	}

	if cfg.Duration < 0 || cfg.Duration > 10*time.Minute {
		return cfg, errors.New("UDP test duration must be within 0..600 seconds")
	}

	if err := selectSource(ctx, &cfg); err != nil {
		return cfg, err
	}

	if net.ParseIP(cfg.Source).To4() == nil {
		return cfg, errors.New("UDP source must be IPv4")
	}

	if ip := net.ParseIP(cfg.Server).To4(); ip != nil {
		cfg.Server = ip.String()
	} else {
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, cfg.Server)
		if err != nil {
			return cfg, err
		}

		found := false
		for _, address := range addresses {
			if address.IP.To4() != nil {
				cfg.Server = address.IP.String()
				found = true
				break
			}
		}

		if !found {
			return cfg, errors.New("server has no IPv4 address")
		}
	}

	if cfg.LogDirectory == "" {
		layout, err := data.Resolve("")
		if err != nil {
			return cfg, err
		}

		cfg.LogDirectory = filepath.Join(layout.UDP, fmt.Sprintf("udp-%s-%d", time.Now().Format("20060102-150405"), os.Getpid()))
	}

	return cfg, nil
}
