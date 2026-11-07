package diagnostics

import (
	"context"
	"sync"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

// Service owns independent, source-bound diagnostic runs and their saved history.
type Service struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	host       contracts.ServiceHost
	collector  collectors.Collector
	jobs       map[string]*diagnosisJob
	probeCache map[string]diagnosisComparisonCache
	closed     bool
	duration   time.Duration
}

// Options controls the duration of an independent diagnostic run. A zero
// duration selects the normal two-minute measurement.
type Options struct {
	Duration time.Duration
}

// New creates a diagnostics service whose workers follow the application context.
func New(ctx context.Context, host contracts.ServiceHost, collector collectors.Collector, options Options) *Service {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(ctx)
	duration := options.Duration
	if duration <= 0 {
		duration = 120 * time.Second
	}

	return &Service{
		ctx:       ctx,
		cancel:    cancel,
		host:      host,
		collector: collector,
		jobs:      map[string]*diagnosisJob{},
		duration:  duration,
	}
}

var _ contracts.DiagnosisService = (*Service)(nil)

func (s *Service) Active() bool {
	s.mu.Lock()
	for _, j := range s.jobs {
		j.mu.Lock()
		running := j.running
		j.mu.Unlock()
		if running {
			s.mu.Unlock()

			return true
		}
	}

	s.mu.Unlock()

	return len(diagnosisRecoveredJobs(s.host.LogDirectory())) > 0
}

func (s *Service) Close() {
	s.cancel()
	for dir := range diagnosisRecoveredJobs(s.host.LogDirectory()) {
		_ = diagnosisWriteStop(dir)
	}

	s.mu.Lock()
	s.closed = true
	jobs := make([]*diagnosisJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}

	s.mu.Unlock()
	for _, j := range jobs {
		j.mu.Lock()
		if j.running {
			j.stopRequested = true
		}

		j.mu.Unlock()
		j.cancel()
	}

	for _, j := range jobs {
		<-j.done
	}
}
