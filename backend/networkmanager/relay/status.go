package relay

import (
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func relayGameTitle(game string) string {
	if game == "cs2" {
		return "CS2"
	}

	return "CoD"
}

func (s *Service) Status(game string) any {
	s.mu.Lock()
	run := s.runs[game]
	if run == nil {
		s.mu.Unlock()

		return map[string]any{
			"game":            game,
			"state":           "idle",
			"running":         false,
			"message":         "Launch " + relayGameTitle(game) + " and click the button. Join a match for more reliable flow detection.",
			"durationSeconds": 20,
		}
	}

	copy := *run
	s.mu.Unlock()
	stage, message := copy.State, copy.Message
	if copy.Running {
		if capture, err := collectors.ReadCaptureReport(copy.Directory, copy.request); err == nil {
			stage, message = capture.State, capture.Message
			if stage == "detected" {
				stage = "adding"
				message = "UDP peer found; adding test cards…"
			}
		}
	}

	return map[string]any{
		"game":            copy.Game,
		"state":           stage,
		"running":         copy.Running,
		"message":         message,
		"runId":           copy.RunID,
		"started":         copy.Started,
		"updated":         copy.Updated,
		"completed":       copy.Completed,
		"gamePid":         copy.GamePID,
		"helperPid":       copy.HelperPID,
		"durationSeconds": copy.DurationSeconds,
		"error":           copy.Error,
		"addedAddress":    copy.AddedAddress,
		"analysis":        copy.Analysis,
		"logDirectory":    copy.Directory,
	}
}
