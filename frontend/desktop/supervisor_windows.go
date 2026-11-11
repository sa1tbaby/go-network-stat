//go:build windows

package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/api/client"
	"github.com/sa1tbaby/go-network-stat/backend/config"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/process"
)

// ClientConfig is portable state; immutable application files live in the payload cache.
type ClientConfig = config.Config

type backendState = client.Status

type Supervisor struct {
	opMu           sync.Mutex
	mu             sync.Mutex
	root           string
	executable     string
	cfg            ClientConfig
	job            *process.Job
	process        syscall.Handle
	pid            uint32
	logFile        *os.File
	logPath        string
	readyFile      string
	done           chan struct{}
	url            string
	started        bool
	stopping       bool
	closed         bool
	lastError      error
	lastState      backendState
	lastStatusTime time.Time
	api            *client.Client
}

func NewSupervisor(root, executable string, cfg ClientConfig) (*Supervisor, error) {
	paths := map[string]string{
		"application files": root,
		"Go server":         executable,
		"log folder":        cfg.LogDirectory,
		"settings folder":   cfg.StateDirectory,
	}
	for label, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s: an absolute path is required", label)
		}
	}

	if cfg.IntervalMs != 250 {
		cfg.IntervalMs = 1000
	}

	if cfg.Port < 1024 || cfg.Port > 65515 {
		cfg.Port = 8765
	}

	if _, err := os.Stat(executable); err != nil {
		return nil, fmt.Errorf("Embedded server not found: %w", err)
	}

	for _, name := range []string{"dashboard.html", "official-relays.json", "cod-test-targets.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			return nil, fmt.Errorf("Dashboard component %s not found: %w", name, err)
		}
	}

	for _, directory := range []string{cfg.StateDirectory, cfg.RuntimeDirectory, cfg.ServiceLogDirectory} {
		if !filepath.IsAbs(directory) {
			return nil, errors.New("service directories must be absolute")
		}

		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
	}

	return &Supervisor{
		root:       root,
		executable: executable,
		cfg:        cfg,
		done:       make(chan struct{}),
		lastState: backendState{
			LogDirectory: cfg.LogDirectory,
			IntervalMs:   cfg.IntervalMs,
		},
		api: client.New(),
	}, nil
}

func (s *Supervisor) Start(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.started || s.closed {
		s.mu.Unlock()

		return errors.New("This client has already started the server")
	}

	s.mu.Unlock()
	startTicks, err := process.CurrentStartTicks()
	if err != nil {
		return err
	}

	ready, err := os.CreateTemp(s.cfg.RuntimeDirectory, ".backend-ready-*.json")
	if err != nil {
		return err
	}

	s.readyFile = ready.Name()
	ready.Close()
	// Empty unique readiness file cannot be mistaken for another process's endpoint.
	s.logPath = filepath.Join(s.cfg.ServiceLogDirectory, "client-backend.log")
	s.logFile, err = os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Remove(s.readyFile)

		return err
	}

	fmt.Fprintf(s.logFile, "\n[%s] Starting client PID %d\n", time.Now().Format(time.RFC3339), os.Getpid())
	job, err := process.NewJob()
	if err != nil {
		s.logFile.Close()
		os.Remove(s.readyFile)

		return err
	}

	arguments := s.backendArguments(startTicks)
	processHandle, pid, err := job.Start(s.executable, arguments, s.root, s.logFile)
	if err != nil {
		job.Close()
		s.logFile.Close()
		os.Remove(s.readyFile)

		return err
	}

	s.mu.Lock()
	s.job, s.process, s.pid, s.started = job, processHandle, pid, true
	s.mu.Unlock()
	go s.watchProcess(processHandle)

	return s.waitForBackend(ctx, pid)
}

func (s *Supervisor) backendArguments(startTicks uint64) []string {
	arguments := []string{
		"--backend",
		"--root", s.root,
		"--storage-directory", s.cfg.LogDirectory,
		"--state-directory", s.cfg.StateDirectory,
		"--runtime-directory", s.cfg.RuntimeDirectory,
		"--service-log-directory", s.cfg.ServiceLogDirectory,
		"--interval-ms", strconv.Itoa(s.cfg.IntervalMs),
		"--port", strconv.Itoa(s.cfg.Port),
		"--owner-pid", strconv.Itoa(os.Getpid()),
		"--owner-start-ticks", strconv.FormatUint(startTicks, 10),
		"--ready-file", s.readyFile,
	}
	if s.cfg.StartPaused {
		arguments = append(arguments, "--paused")
	}

	return arguments
}

func (s *Supervisor) waitForBackend(ctx context.Context, pid uint32) error {
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.shutdownLocked()

			return ctx.Err()
		case <-s.done:
			s.shutdownLocked()

			return fmt.Errorf("The collector exited during startup. Details: %s", s.logPath)
		case <-deadline.C:
			s.shutdownLocked()

			return fmt.Errorf("The server did not respond within 45 seconds. Details: %s", s.logPath)
		case <-ticker.C:
			address, snapshot, ready := s.readBackendReady(pid)
			if !ready {
				continue
			}

			s.mu.Lock()
			s.url, s.lastState, s.lastStatusTime = address, snapshot, time.Now()
			s.mu.Unlock()
			os.Remove(s.readyFile)

			return nil
		}
	}
}

func (s *Supervisor) readBackendReady(pid uint32) (string, backendState, bool) {
	contents, err := os.ReadFile(s.readyFile)
	if err != nil || len(contents) > 32768 {
		return "", backendState{}, false
	}

	var fields struct {
		AppID string `json:"appId"`
		PID   uint32 `json:"pid"`
		URL   string `json:"url"`
	}
	if json.Unmarshal(contents, &fields) != nil ||
		fields.AppID != "codex-internet-stability" ||
		fields.PID != pid ||
		!client.ValidBackendURL(fields.URL) {
		return "", backendState{}, false
	}

	snapshot, err := s.api.Status(fields.URL)
	if err != nil {
		return "", backendState{}, false
	}

	return fields.URL, snapshot, true
}

func (s *Supervisor) watchProcess(processHandle syscall.Handle) {
	syscall.WaitForSingleObject(processHandle, 0xffffffff)
	var exitCode uint32
	syscall.GetExitCodeProcess(processHandle, &exitCode)
	s.mu.Lock()
	if !s.stopping {
		s.lastError = fmt.Errorf("The server stopped (code %d). Restart the app. Log: %s", exitCode, s.logPath)
	}

	job := s.job
	s.job = nil
	s.mu.Unlock()
	// A crashed server must not leave independent collectors running.
	if job != nil {
		job.Close()
	}

	close(s.done)
}

func (s *Supervisor) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.url
}

func (s *Supervisor) CurrentDirectory() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.lastState.LogDirectory
}

func (s *Supervisor) SnapshotConfig() ClientConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.cfg
	cfg.LogDirectory = s.lastState.LogDirectory
	cfg.IntervalMs = s.lastState.IntervalMs
	cfg.StartPaused = !s.lastState.Running

	return cfg
}

func (s *Supervisor) RefreshStatus() error {
	s.mu.Lock()
	address, stopping, previousError := s.url, s.stopping, s.lastError
	s.mu.Unlock()
	if previousError != nil {
		return previousError
	}

	if address == "" || stopping {
		return errors.New("The server has not started yet")
	}

	state, err := s.api.Status(address)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.lastState, s.lastStatusTime = state, time.Now()
	s.mu.Unlock()

	return nil
}

// Status is safe to poll from the native UI. Network I/O is only done by refresh.
func (s *Supervisor) Status() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.closed {
		return "Stopping the server and collectors…", s.lastState.LogDirectory
	}

	if s.lastError != nil {
		return s.lastError.Error(), s.lastState.LogDirectory
	}

	if s.url == "" {
		return "Preparing the local server…", s.lastState.LogDirectory
	}

	if s.lastState.Running {
		return "Server running · measuring", s.lastState.LogDirectory
	}

	return "Server running · measurements paused", s.lastState.LogDirectory
}

func (s *Supervisor) SetLogDirectory(path string) error {
	s.mu.Lock()
	address, stopping := s.url, s.stopping
	s.mu.Unlock()
	if address == "" || stopping {
		return errors.New("The server has not started yet or is shutting down")
	}

	if err := s.api.SelectLogDirectory(address, path); err != nil {
		return err
	}

	return s.RefreshStatus()
}

func (s *Supervisor) Shutdown() error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	return s.shutdownLocked()
}

func (s *Supervisor) shutdownLocked() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()

		return nil
	}

	s.stopping = true
	address, started, processHandle := s.url, s.started, s.process
	s.mu.Unlock()
	if started {
		if address != "" {
			_ = s.api.Shutdown(address)
		}

		select {
		case <-s.done:
		case <-time.After(12 * time.Second):
			s.mu.Lock()
			job := s.job
			s.mu.Unlock()
			if job != nil {
				job.Terminate(0)
			} else if processHandle != 0 {
				syscall.TerminateProcess(processHandle, 0)
			}

			select {
			case <-s.done:
			case <-time.After(3 * time.Second):
				return errors.New("Windows did not confirm that the collector stopped")
			}
		}
	}

	s.mu.Lock()
	job := s.job
	s.job = nil
	s.closed = true
	s.process = 0
	s.mu.Unlock()
	if job != nil {
		job.Close()
	}

	if processHandle != 0 {
		syscall.CloseHandle(processHandle)
	}

	if s.logFile != nil {
		s.logFile.Close()
	}

	if s.readyFile != "" {
		os.Remove(s.readyFile)
	}

	return nil
}
