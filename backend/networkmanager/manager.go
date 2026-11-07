// Package networkmanager owns adapters, scheduled monitoring and measurement state.
// HTTP and process lifecycle live in backend/api and backend/app respectively.
package networkmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/routes"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/targets"
)

const appID = "codex-internet-stability"

// Config contains domain settings and ports; it has no HTTP listener or process identity.
// StateChanged must not call back into Manager: it runs while the state lock is held.
type Config struct {
	Root           string
	StateDirectory string
	LogDirectory   string
	IntervalMs     int
	Paused         bool
	PickFolder     func(context.Context, string) (string, error)
	StateChanged   func(State) error
	RouteOptions   routes.Options
}

type Manager struct {
	mu                 sync.Mutex
	opMu               sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	cfg                Config
	collector          collectors.Collector
	adapters           map[string]collectors.Adapter
	targets            map[string]*target
	custom             *targets.Registry
	events             []EventRow
	routeEngine        *routes.Engine
	logDir             string
	started            string
	interval           int
	measuring          bool
	stopping           bool
	startedMonitor     bool
	selectionState     string
	selectionDirectory string
	selectionError     string
	storageWarning     string
	choosing           bool
	writers            *storage.Store
	activityGuard      func() bool
	wg                 sync.WaitGroup
	closeOnce          sync.Once
	closeErr           error
}

func New(ctx context.Context, cfg Config, collector collectors.Collector) (*Manager, error) {
	if collector == nil {
		return nil, errors.New("network collector is required")
	}

	if cfg.Root == "" {
		cfg.Root = "."
	}

	var err error
	cfg.Root, err = filepath.Abs(cfg.Root)
	if err != nil {
		return nil, err
	}

	if cfg.StateDirectory == "" {
		return nil, errors.New("configuration directory is required")
	}

	cfg.StateDirectory, err = storage.FullPath(cfg.StateDirectory)
	if err != nil {
		return nil, err
	}

	if err = storage.NoLink(cfg.StateDirectory); err != nil {
		return nil, err
	}

	if err = os.MkdirAll(cfg.StateDirectory, 0700); err != nil {
		return nil, err
	}

	if cfg.IntervalMs != 250 {
		cfg.IntervalMs = 1000
	}

	registry, err := targets.Open(cfg.StateDirectory)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{
		ctx:            ctx,
		cancel:         cancel,
		cfg:            cfg,
		collector:      collector,
		adapters:       map[string]collectors.Adapter{},
		targets:        map[string]*target{},
		custom:         registry,
		events:         []EventRow{},
		interval:       cfg.IntervalMs,
		measuring:      !cfg.Paused,
		started:        iso(),
		selectionState: "idle",
	}
	m.routeEngine = routes.New(ctx, m, collector, cfg.RouteOptions)
	if err = m.initializeStorage(); err != nil {
		cancel()
		m.routeEngine.Close()

		return nil, err
	}

	m.refresh()

	return m, nil
}

// SetActivityGuard binds external diagnostic/capture activity at composition time.
func (m *Manager) SetActivityGuard(guard func() bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activityGuard = guard
}

func (m *Manager) Start() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	if m.stopping || m.ctx.Err() != nil {
		m.mu.Unlock()

		return errors.New("The collector is shutting down.")
	}

	if m.startedMonitor {
		m.mu.Unlock()

		return nil
	}

	m.startedMonitor = true
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		m.monitor()
	}()

	return nil
}

func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.opMu.Lock()
		m.mu.Lock()
		m.stopping = true
		m.mu.Unlock()
		m.cancel()
		m.opMu.Unlock()
		m.routeEngine.Close()
		m.wg.Wait()
		m.Event("stop", "Monitoring stopped")
		m.mu.Lock()
		defer m.mu.Unlock()
		if err := m.saveSnapshotLocked(); err != nil {
			m.closeErr = err
		}

		if m.writers != nil {
			if err := m.writers.Close(); err != nil && m.closeErr == nil {
				m.closeErr = err
			}
		}
	})

	return m.closeErr
}
