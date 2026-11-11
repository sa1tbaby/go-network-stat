//go:build windows

package desktop

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sa1tbaby/go-network-stat/backend/api/assets"
	"github.com/sa1tbaby/go-network-stat/backend/config"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

type clientApp struct {
	mu         sync.Mutex
	opMu       sync.Mutex
	cfg        ClientConfig
	layout     data.Layout
	payload    assets.Payload
	supervisor *Supervisor
	status     string
	ctx        context.Context
	cancel     context.CancelFunc
}

type clientOptions struct {
	dataDirectory string
	port          int
	headless      bool
	paused        bool
	intervalMs    int
	exitAfter     time.Duration
}

func Run() {
	options := readClientOptions()

	release, already, err := AcquireSingleInstance()
	if err != nil {
		ShowError(err)

		return
	}

	if already {
		ShowExistingWindow()

		return
	}

	defer release()

	layout, cfg, err := loadClientSettings(options)
	if err != nil {
		ShowError(err)

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	app := &clientApp{
		cfg:    cfg,
		layout: layout,
		status: "Preparing embedded components…",
		ctx:    ctx,
		cancel: cancel,
	}
	if err := config.Save(layout, cfg); err != nil {
		ShowError(err)

		return
	}

	var startDone sync.WaitGroup
	startDone.Add(1)
	go func() {
		defer startDone.Done()
		app.start()
	}()

	stopSignal := make(chan os.Signal, 1)
	signal.Notify(stopSignal, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopSignal)
	go func() {
		select {
		case <-stopSignal:
			cancel()
		case <-ctx.Done():
		}
	}()
	if options.exitAfter > 0 {
		time.AfterFunc(options.exitAfter, cancel)
	}

	go app.refreshLoop()

	if options.headless {
		<-ctx.Done()
		startDone.Wait()
		app.shutdown()

		return
	}

	go func() {
		<-ctx.Done()
		requestClientWindowExit()
	}()

	err = RunWindow(UIActions{
		InitialDirectory: cfg.LogDirectory,
		OnBrowse:         app.browseDirectory,
		OnOpenPanel:      app.openDashboard,
		OnOpenLogs:       app.openLogs,
		OnExit: func() {
			cancel()
			startDone.Wait()
			app.shutdown()
		},
		OnStatus: app.view,
	})
	cancel()
	startDone.Wait()
	app.shutdown()
	if err != nil {
		ShowError(err)
	}
}

func readClientOptions() clientOptions {
	dataFlag := flag.String("data-dir", "", "Application data root directory")
	portFlag := flag.Int("port", 0, "Local dashboard port")
	headless := flag.Bool("headless", false, "Run without a window (for lifecycle checks)")
	paused := flag.Bool("paused", false, "Start with measurements paused")
	intervalFlag := flag.Int("interval-ms", 0, "Regular measurement interval: 1000 or 250 ms")
	exitAfter := flag.Duration("exit-after", 0, "Exit after the specified duration")
	flag.Parse()

	return clientOptions{
		dataDirectory: *dataFlag,
		port:          *portFlag,
		headless:      *headless,
		paused:        *paused,
		intervalMs:    *intervalFlag,
		exitAfter:     *exitAfter,
	}
}

func loadClientSettings(options clientOptions) (data.Layout, ClientConfig, error) {
	layout, err := data.Resolve(options.dataDirectory)
	if err != nil {
		return data.Layout{}, ClientConfig{}, err
	}

	var legacy []string
	if options.dataDirectory == "" {
		executable, _ := os.Executable()
		legacy = data.LegacyDirectories(executable)
	}

	cfg, err := config.Load(layout, legacy)
	if err != nil {
		return data.Layout{}, ClientConfig{}, err
	}

	if options.port != 0 {
		cfg.Port = options.port
	}

	if options.paused {
		cfg.StartPaused = true
	}

	if options.intervalMs == 250 || options.intervalMs == 1000 {
		cfg.IntervalMs = options.intervalMs
	}

	return layout, cfg, nil
}

func (a *clientApp) browseDirectory(owner uintptr) (string, error) {
	_, current := a.view()
	selected, err := PickFolder(owner, current)
	if err != nil || selected == "" {
		return "", err
	}

	return selected, a.changeDirectory(selected)
}

func (a *clientApp) openDashboard() error {
	a.mu.Lock()
	supervisor := a.supervisor
	a.mu.Unlock()
	if supervisor == nil || supervisor.URL() == "" {
		return errors.New("The dashboard is still starting. Wait for the ready message.")
	}

	return ShellOpen(supervisor.URL())
}

func (a *clientApp) openLogs() error {
	_, directory := a.view()

	return ShellOpen(directory)
}

// Uses the same graceful exit path as the tray menu, keeping the window alive
// until all of this client's workers have stopped.
func requestClientWindowExit() {
	class, _ := syscall.UTF16PtrFromString("InternetStabilityClientWindow")
	user32 := syscall.NewLazyDLL("user32.dll")
	window, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(class)), 0)
	if window != 0 {
		user32.NewProc("PostMessageW").Call(window, 0x111, 1104, 0)
	}
}

func (a *clientApp) start() {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	payload, err := assets.Prepare(a.layout.Cache)
	if err == nil {
		a.payload = payload
		err = a.startBackendLocked()
	}

	if err != nil && a.ctx.Err() == nil {
		a.mu.Lock()
		a.status = "Could not start the dashboard: " + err.Error()
		a.mu.Unlock()
	}
}

func (a *clientApp) startBackendLocked() error {
	if err := a.ctx.Err(); err != nil {
		return err
	}

	a.mu.Lock()
	cfg := a.cfg
	a.status = "Starting the server and collectors…"
	a.mu.Unlock()
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	s, err := NewSupervisor(a.payload.Root, executable, cfg)
	if err != nil {
		return err
	}

	if err = s.Start(a.ctx); err != nil {
		s.Shutdown()

		return err
	}

	a.mu.Lock()
	a.supervisor = s
	a.status = "Dashboard ready · " + s.URL()
	a.mu.Unlock()
	a.writeClientState(s.URL())

	return nil
}

func (a *clientApp) refreshLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.refreshStatus()
		}
	}
}

func (a *clientApp) refreshStatus() {
	a.mu.Lock()
	supervisor := a.supervisor
	a.mu.Unlock()
	if supervisor == nil {
		return
	}

	err := supervisor.RefreshStatus()
	a.opMu.Lock()
	cfg := supervisor.SnapshotConfig()
	a.mu.Lock()
	changed := a.cfg.LogDirectory != cfg.LogDirectory ||
		a.cfg.IntervalMs != cfg.IntervalMs ||
		a.cfg.StartPaused != cfg.StartPaused
	if err != nil {
		a.status = "Server unavailable: " + err.Error()
	} else {
		a.cfg = cfg
		status := "Measuring"
		if cfg.StartPaused {
			status = "Measurements paused"
		}

		a.status = status + " · " + supervisor.URL()
	}

	a.mu.Unlock()
	if changed && err == nil {
		_ = config.Save(a.layout, cfg)
	}

	a.opMu.Unlock()
}

func (a *clientApp) view() (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.status, a.cfg.LogDirectory
}

func (a *clientApp) changeDirectory(path string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	s := a.supervisor
	a.mu.Unlock()
	if s != nil {
		if err := s.SetLogDirectory(path); err != nil {
			return err
		}

		cfg := s.SnapshotConfig()
		a.mu.Lock()
		a.cfg = cfg
		a.mu.Unlock()
	} else {
		a.mu.Lock()
		a.cfg.LogDirectory = path
		a.mu.Unlock()
		if a.payload.Root == "" {
			return errors.New("Embedded components are not ready. Restart the app.")
		}

		if err := a.startBackendLocked(); err != nil {
			return err
		}
	}

	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	return config.Save(a.layout, cfg)
}

func (a *clientApp) shutdown() {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	s := a.supervisor
	a.supervisor = nil
	a.status = "Stopping the server and collectors…"
	a.mu.Unlock()
	if s == nil {
		return
	}

	_ = s.RefreshStatus()
	cfg := s.SnapshotConfig()
	_ = config.Save(a.layout, cfg)
	err := s.Shutdown()
	a.writeClientState("")
	if err != nil {
		_ = os.WriteFile(filepath.Join(a.layout.Logs, "shutdown-error.txt"), []byte(err.Error()), 0600)
	}
}

func (a *clientApp) writeClientState(url string) {
	fields := map[string]any{
		"appId":   "internet-stability-go-client",
		"pid":     os.Getpid(),
		"url":     url,
		"updated": time.Now().Format(time.RFC3339),
	}
	_ = fileutil.WriteJSON(filepath.Join(a.layout.Runtime, "client-state.json"), fields)
}

func ShowError(err error) {
	message, _ := syscall.UTF16PtrFromString(err.Error())
	title, _ := syscall.UTF16PtrFromString("Connection Stability")
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
