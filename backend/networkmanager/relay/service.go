package relay

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

type relayRun struct {
	Game            string                  `json:"game"`
	RunID           string                  `json:"runId"`
	State           string                  `json:"state"`
	Message         string                  `json:"message"`
	Started         string                  `json:"started"`
	Updated         string                  `json:"updated"`
	Completed       any                     `json:"completed"`
	Directory       string                  `json:"directory"`
	Error           any                     `json:"error"`
	AddedAddress    any                     `json:"addedAddress"`
	GamePID         int                     `json:"gamePid"`
	GameStartTicks  int64                   `json:"gameStartTicks"`
	HelperPID       any                     `json:"helperPid"`
	Running         bool                    `json:"running"`
	DurationSeconds int                     `json:"durationSeconds"`
	Analysis        *collectors.UDPAnalysis `json:"analysis"`
	request         collectors.CaptureRequest
}

// Service discovers active game UDP peers through an elevated capture worker.
type Service struct {
	engine CaptureEngine
	mu     sync.Mutex
	host   contracts.ServiceHost
	runs   map[string]*relayRun
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

// New uses the native Windows process/capture implementation.
func New(ctx context.Context, host contracts.ServiceHost) *Service {
	return NewWithEngine(ctx, host, nativeEngine{})
}

// NewWithEngine composes the service with a different capture execution port.
func NewWithEngine(ctx context.Context, host contracts.ServiceHost, engine CaptureEngine) *Service {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(ctx)
	if engine == nil {
		engine = nativeEngine{}
	}

	return &Service{
		host:   host,
		engine: engine,
		runs:   map[string]*relayRun{},
		ctx:    ctx,
		cancel: cancel,
	}
}

var _ contracts.RelayService = (*Service)(nil)

func (s *Service) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.Running {
			return true
		}
	}

	return false
}

func (s *Service) Start(game string) (any, error) {
	if game != "cod" && game != "cs2" {
		return nil, errors.New("Unsupported game.")
	}

	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()

		return nil, errors.New("The client is shutting down.")
	}

	if run := s.runs[game]; run != nil && run.Running {
		result := map[string]any{
			"ok":     true,
			"reused": true,
			"runId":  run.RunID,
		}
		s.mu.Unlock()

		return result, nil
	}

	for _, run := range s.runs {
		if run.Running {
			s.mu.Unlock()

			return nil, errors.New("Wait for detection to finish for the other game.")
		}
	}

	s.mu.Unlock()
	root := s.host.LogDirectory()
	pid, ticks, err := s.engine.FindGameProcess(game)
	if err != nil {
		return nil, err
	}

	ownerPID, ownerTicks, err := s.engine.CurrentProcessIdentity()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()

		return nil, errors.New("The client is shutting down.")
	}

	if run := s.runs[game]; run != nil && run.Running {
		result := map[string]any{
			"ok":     true,
			"reused": true,
			"runId":  run.RunID,
		}
		s.mu.Unlock()

		return result, nil
	}

	for _, run := range s.runs {
		if run.Running {
			s.mu.Unlock()

			return nil, errors.New("Wait for detection to finish for the other game.")
		}
	}

	id := randomID()
	dir := filepath.Join(root, game+"-relay-runs", id)
	started := time.Now().Format(time.RFC3339Nano)
	request := collectors.CaptureRequest{
		AppID:           "codex-cod-relay-request",
		Game:            game,
		RunID:           id,
		GamePID:         pid,
		GameStartTicks:  ticks,
		OwnerPID:        ownerPID,
		OwnerStartTicks: ownerTicks,
		Started:         started,
	}
	if err := collectors.WriteCaptureRequest(dir, request); err != nil {
		s.mu.Unlock()

		return nil, err
	}

	run := &relayRun{
		Game:            game,
		RunID:           id,
		State:           "awaiting_admin",
		Running:         true,
		Started:         started,
		Updated:         started,
		Directory:       dir,
		GamePID:         pid,
		GameStartTicks:  ticks,
		DurationSeconds: 20,
		Message:         "Approve the Windows administrator request for ETW capture. UDP traffic will then be observed for 20 seconds.",
		request:         request,
	}
	s.runs[game] = run
	if err := writeJSON(filepath.Join(dir, "panel-result.json"), run); err != nil {
		delete(s.runs, game)
		s.mu.Unlock()

		return nil, err
	}

	s.wg.Add(1)
	s.mu.Unlock()
	s.host.Event(game+"_relay_start", "Detecting the active UDP peer for "+game+".exe")
	go s.run(run)

	return map[string]any{
		"ok":     true,
		"reused": false,
		"runId":  id,
	}, nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	runs := make([]*relayRun, 0, len(s.runs))
	for _, run := range s.runs {
		if run.Running {
			copy := *run
			runs = append(runs, &copy)
		}
	}

	s.mu.Unlock()
	s.cancel()
	for _, run := range runs {
		_ = collectors.CancelCapture(run.Directory, run.RunID)
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
	}
}
