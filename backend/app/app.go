// Package app constructs the backend services and owns their lifecycle.
package app

import (
	"context"
	"net/http"
	"sync"

	"github.com/sa1tbaby/go-network-stat/backend/api"
	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/diagnostics"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/relay"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/routes"
)

type Config struct {
	Root                string
	StateDirectory      string
	RuntimeDirectory    string
	LogDirectory        string
	ServiceLogDirectory string
	ReadyFile           string
	Port                int
	IntervalMs          int
	OwnerPID            int
	OwnerStartTicks     int64
	Paused              bool
	PickFolder          func(context.Context, string) (string, error)
	OwnerAlive          func(int, int64) bool
	RouteOptions        routes.Options
}

// Dependencies supplies real interchangeable domain ports, including deterministic collectors.
type Dependencies struct {
	Collector collectors.Collector
	Diagnosis contracts.DiagnosisService
	Relay     contracts.RelayService
}

type App struct {
	mu        sync.Mutex
	cfg       Config
	ctx       context.Context
	cancel    context.CancelFunc
	manager   *networkmanager.Manager
	diagnosis contracts.DiagnosisService
	relay     contracts.RelayService
	handler   *api.Handler
	port      int
	closeOnce sync.Once
	closeErr  error
}

func New(ctx context.Context, cfg Config, dependencies Dependencies) (*App, error) {
	if cfg.Port <= 0 {
		cfg.Port = 8765
	}

	if err := prepareDirectories(cfg); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	a := &App{
		ctx:    ctx,
		cancel: cancel,
		cfg:    cfg,
		port:   cfg.Port,
	}
	collector := dependencies.Collector
	if collector == nil {
		collector = collectors.New()
	}

	manager, err := networkmanager.New(ctx, networkmanager.Config{
		Root:           cfg.Root,
		StateDirectory: cfg.StateDirectory,
		LogDirectory:   cfg.LogDirectory,
		IntervalMs:     cfg.IntervalMs,
		Paused:         cfg.Paused,
		PickFolder:     cfg.PickFolder,
		StateChanged:   a.publishState,
		RouteOptions:   cfg.RouteOptions,
	}, collector)
	if err != nil {
		cancel()

		return nil, err
	}

	a.manager = manager
	a.diagnosis = dependencies.Diagnosis
	if a.diagnosis == nil {
		a.diagnosis = diagnostics.New(ctx, manager, collector, diagnostics.Options{})
	}

	a.relay = dependencies.Relay
	if a.relay == nil {
		a.relay = relay.New(ctx, manager)
	}

	manager.SetActivityGuard(func() bool {
		return a.diagnosis.Active() || a.relay.Active()
	})
	a.handler, err = a.newHandler()
	if err != nil {
		a.Close()

		return nil, err
	}

	return a, nil
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) Manager() *networkmanager.Manager {
	return a.manager
}

func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.cancel()
		a.diagnosis.Close()
		a.relay.Close()
		a.closeErr = a.manager.Close()
	})

	return a.closeErr
}

func (a *App) newHandler() (*api.Handler, error) {
	return api.New(api.Config{
		Root:      a.cfg.Root,
		Port:      a.port,
		Manager:   a.manager,
		Diagnosis: a.diagnosis,
		Relay:     a.relay,
		Shutdown:  a.cancel,
	})
}
