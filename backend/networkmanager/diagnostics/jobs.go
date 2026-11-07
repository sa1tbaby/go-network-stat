package diagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

type diagnosisJob struct {
	mu            sync.Mutex
	id            string
	dir           string
	started       string
	adapter       collectors.Adapter
	start         time.Time
	cancel        context.CancelFunc
	done          chan struct{}
	running       bool
	stopRequested bool
	err           string
	targets       []*diagnosisTarget
	network       map[string]any
	extra         map[string]any
	maxLag        float64
}

func (s *Service) Start(adapterID string) (any, error) {
	adapter, ok := s.host.Adapter(adapterID)
	if !ok || !adapter.Available || !adapter.ICMPSafe || adapter.IPv4 == "" || adapter.Gateway == "" {
		return nil, errors.New("Select a connected interface with safe IPv4 binding and a gateway.")
	}

	root := s.host.LogDirectory()
	recovered := diagnosisRecoveredJobs(root)
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()

		return nil, errors.New("The collector is shutting down.")
	}

	active := 0
	for _, job := range s.jobs {
		job.mu.Lock()
		if job.running {
			active++
		}

		job.mu.Unlock()
	}

	for dir := range recovered {
		if s.jobs[dir] == nil {
			active++
		}
	}

	if active >= 4 {
		s.mu.Unlock()

		return nil, errors.New("Four independent tests are already running.")
	}

	start := time.Now()
	id := start.Format("20060102-150405") + "-" + diagnosisRandomID()[:8]
	dir := filepath.Join(root, "diagnosis-runs", id)
	if err := diagnosisOrdinaryPath(filepath.Join(root, "diagnosis-runs")); err != nil {
		s.mu.Unlock()

		return nil, err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		s.mu.Unlock()

		return nil, err
	}

	ctx, cancel := context.WithCancel(s.ctx)
	job := &diagnosisJob{
		id:      id,
		dir:     dir,
		started: start.Format(time.RFC3339Nano),
		adapter: adapter,
		start:   start,
		cancel:  cancel,
		done:    make(chan struct{}),
		running: true,
	}
	for i, address := range []string{adapter.Gateway, "1.1.1.1", "8.8.8.8"} {
		scope := "internet_control"
		prefix := []string{"gateway", "cloudflare", "google"}[i]
		pps := 4
		if i == 0 {
			scope = "local_gateway"
			pps = 10
		}

		job.targets = append(job.targets, &diagnosisTarget{
			ID:       prefix + "-icmp",
			Address:  address,
			Protocol: "icmp",
			Scope:    scope,
			PPS:      pps,
		})
	}

	for i, address := range []string{adapter.Gateway, "1.1.1.1", "8.8.8.8"} {
		scope := "internet_control"
		if i == 0 {
			scope = "local_gateway"
		}

		job.targets = append(job.targets, &diagnosisTarget{
			ID:       []string{"gateway", "cloudflare", "google"}[i] + "-udp-dns",
			Address:  address,
			Protocol: "udp_dns",
			Scope:    scope,
			PPS:      1,
		})
	}

	for path, data := range map[string]any{
		filepath.Join(dir, "run.json"): map[string]any{
			"appId":       "codex-network-diagnosis-run",
			"id":          id,
			"adapterName": adapter.Name,
			"kind":        adapter.Kind,
			"started":     job.started,
		},
		filepath.Join(root, "diagnosis-index.json"): map[string]any{"appId": "codex-network-diagnosis-index", "activeRunId": id},
	} {
		if err := diagnosisWriteJSON(path, data); err != nil {
			cancel()
			s.mu.Unlock()

			return nil, err
		}
	}

	if err := job.saveRuntime(); err != nil {
		cancel()
		s.mu.Unlock()

		return nil, err
	}

	s.jobs[dir] = job
	s.mu.Unlock()
	go s.run(ctx, job)
	s.host.Event("diagnosis_start", "Two-minute diagnostics: "+adapter.Name+"; frequent ICMP, UDP DNS, and adapter counters")

	return map[string]any{
		"ok":            true,
		"runId":         id,
		"activeCount":   active + 1,
		"maxConcurrent": 4,
	}, nil
}

func (s *Service) Stop(id string) (any, error) {
	if !diagnosisSafeID(id) {
		return nil, errors.New("Specify the runId of the test.")
	}

	dir := filepath.Join(s.host.LogDirectory(), "diagnosis-runs", id)
	s.mu.Lock()
	job := s.jobs[dir]
	s.mu.Unlock()
	if job == nil {
		if _, err := diagnosisManifest(dir, id); err != nil {
			return nil, err
		}

		if diagnosisRecoveredAlive(dir) {
			if err := diagnosisWriteStop(dir); err != nil {
				return nil, err
			}
		}

		return map[string]any{"ok": true, "runId": id}, nil
	}

	job.mu.Lock()
	job.stopRequested = true
	err := job.saveRuntime()
	job.mu.Unlock()
	if err != nil {
		return nil, err
	}

	if err := diagnosisWriteStop(job.dir); err != nil {
		return nil, err
	}

	job.cancel()
	s.host.Event("diagnosis_stop", "Requested stop for test "+id)

	return map[string]any{"ok": true, "runId": id}, nil
}
