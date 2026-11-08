package relay

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

// CaptureEngine is the platform boundary for process identity and elevated ETW
// execution. Request and result files remain verified by the service.
type CaptureEngine interface {
	FindGameProcess(string) (int, int64, error)
	CurrentProcessIdentity() (int, int64, error)
	Run(context.Context, string, collectors.CaptureRequest, int, func(int)) error
}

type nativeEngine struct{}

func (nativeEngine) FindGameProcess(game string) (int, int64, error) {
	return collectors.FindGameProcess(game)
}

func (nativeEngine) CurrentProcessIdentity() (int, int64, error) {
	return collectors.CurrentProcessIdentity()
}

func (nativeEngine) Run(ctx context.Context, dir string, request collectors.CaptureRequest, seconds int, started func(int)) error {
	return collectors.RunElevatedCapture(ctx, dir, request, seconds, started)
}

func (s *Service) run(run *relayRun) {
	defer s.wg.Done()
	err := s.engine.Run(s.ctx, run.Directory, run.request, run.DurationSeconds, func(pid int) {
		s.mu.Lock()
		run.HelperPID = pid
		run.State = "preparing"
		run.Message = "Preparing a separate Windows ETW session"
		run.Updated = time.Now().Format(time.RFC3339Nano)
		_ = writeJSON(filepath.Join(run.Directory, "panel-result.json"), run)
		s.mu.Unlock()
	})
	data, readErr := collectors.ReadCaptureReport(run.Directory, run.request)
	if readErr == nil {
		s.mu.Lock()
		run.State = data.State
		run.Message = data.Message
		run.Analysis = data.Analysis
		if data.Error != nil {
			run.Error = *data.Error
			if data.State == "error" {
				err = nil
			}
		}

		s.mu.Unlock()
		if data.State == "detected" && data.Analysis != nil && data.Analysis.State == "detected" && data.Analysis.Selected != nil && s.ctx.Err() == nil {
			p := data.Analysis.Selected
			if !p.Eligible || !collectors.PublicIPv4(p.RemoteIP) || p.RemotePort < 1 || p.RemotePort > 65535 {
				err = errors.New("The capture did not confirm a usable IPv4 UDP peer.")
			} else {
				target := contracts.DetectedTarget{
					Address:    p.RemoteIP,
					SourceIP:   p.LocalIP,
					UDPPort:    p.RemotePort,
					LocalPort:  p.LocalPort,
					InEvents:   p.InEvents,
					OutEvents:  p.OutEvents,
					ObservedAt: time.Now().Format(time.RFC3339Nano),
					Label:      relayGameTitle(run.Game) + " · detected UDP peer",
				}
				err = s.host.AddDetectedTarget(run.Game, target, filepath.Join(run.Directory, "capture.json"))
				if err == nil {
					s.mu.Lock()
					run.AddedAddress = p.RemoteIP
					run.State = "complete"
					run.Message = fmt.Sprintf("Added %s:%d · UDP. The cards check ICMP and the route; ETW does not calculate game UDP loss percentage.", p.RemoteIP, p.RemotePort)
					s.mu.Unlock()
				}
			}
		} else if data.State != "error" {
			err = nil
		}
	} else if err == nil {
		err = fmt.Errorf("The collector exited without a verifiable result: %w", readErr)
	}

	s.mu.Lock()
	if s.ctx.Err() != nil {
		run.State = "cancelled"
		run.Message = "The client is shutting down. Capture stopped."
	} else if err != nil {
		run.State = "error"
		run.Error = err.Error()
		run.Message = err.Error()
		if errors.Is(err, syscall.Errno(1223)) {
			run.State = "cancelled"
			run.Message = "The administrator request was canceled. Capture did not start and no address was added."
		}
	}

	run.Running = false
	run.Completed = time.Now().Format(time.RFC3339Nano)
	run.Updated = time.Now().Format(time.RFC3339Nano)
	_ = writeJSON(filepath.Join(run.Directory, "panel-result.json"), run)
	message := run.Message
	s.mu.Unlock()
	s.host.Event(run.Game+"_relay_finished", message)
}
