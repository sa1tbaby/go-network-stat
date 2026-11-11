package app

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sa1tbaby/go-network-stat/backend/api/client"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/udpecho"
)

// HandleMode handles server/worker commands before a caller opens any desktop UI.
// The UI supplies only its native folder-selection port; backend imports no frontend package.
func HandleMode(arguments []string, pickFolder func(context.Context, string) (string, error)) (handled bool, err error) {
	if len(arguments) == 0 {
		return false, nil
	}

	switch arguments[0] {
	case "--backend":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return true, runBackendMode(ctx, arguments[1:], pickFolder)
	case "--capture-worker":
		return true, collectors.RunCaptureWorker(arguments[1:])
	case "--pause-monitor":
		return true, client.PauseMonitor()
	case "--udp-test", "--udp-responder":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		command := "test"
		if arguments[0] == "--udp-responder" {
			command = "responder"
		}

		return true, udpecho.RunCLI(ctx, append([]string{command}, arguments[1:]...), os.Stdout)
	default:
		return false, nil
	}
}

func runBackendMode(ctx context.Context, arguments []string, pickFolder func(context.Context, string) (string, error)) error {
	flags := flag.NewFlagSet("backend", flag.ContinueOnError)
	var cfg Config
	flags.StringVar(&cfg.Root, "root", "", "Embedded dashboard directory")
	flags.StringVar(&cfg.StateDirectory, "state-directory", "", "Settings directory")
	flags.StringVar(&cfg.RuntimeDirectory, "runtime-directory", "", "Process state directory")
	flags.StringVar(&cfg.ServiceLogDirectory, "service-log-directory", "", "Service log directory")
	flags.StringVar(&cfg.LogDirectory, "storage-directory", "", "Log directory")
	flags.StringVar(&cfg.ReadyFile, "ready-file", "", "Server readiness file")
	flags.IntVar(&cfg.Port, "port", 8765, "Local port")
	flags.IntVar(&cfg.IntervalMs, "interval-ms", 1000, "Measurement interval")
	flags.IntVar(&cfg.OwnerPID, "owner-pid", 0, "Client PID")
	flags.Int64Var(&cfg.OwnerStartTicks, "owner-start-ticks", 0, "Client start time in .NET ticks")
	flags.BoolVar(&cfg.Paused, "paused", false, "Start paused")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	for _, path := range []string{cfg.Root, cfg.StateDirectory, cfg.RuntimeDirectory, cfg.ServiceLogDirectory, cfg.LogDirectory, cfg.ReadyFile} {
		if !filepath.IsAbs(path) {
			return errors.New("backend paths must be absolute")
		}
	}

	if cfg.OwnerPID <= 0 || !nativeOwnerAlive(cfg.OwnerPID, cfg.OwnerStartTicks) {
		return errors.New("client owner has already exited")
	}

	cfg.OwnerAlive = nativeOwnerAlive
	cfg.PickFolder = pickFolder

	return Run(ctx, cfg)
}
