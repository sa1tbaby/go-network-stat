package collectors

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func RunCaptureWorker(args []string) error {
	fields, err := parseCaptureOptions(args)
	if err != nil {
		return err
	}

	request, err := loadCaptureRequest(fields)
	if err != nil {
		return err
	}

	report := &CaptureReport{
		AppID:           "codex-cod-relay-capture",
		Game:            fields.game,
		RunID:           fields.runID,
		GamePID:         fields.processID,
		GameStartTicks:  fields.startTicks,
		State:           "preparing",
		Message:         "Preparing Windows ETW capture",
		Started:         time.Now().Format(time.RFC3339Nano),
		DurationSeconds: fields.duration,
		Session:         "CodexRelay-" + (fields.runID)[:16],
		ETL:             filepath.Join(fields.directory, "kernel-network.etl"),
	}
	save := func() error {
		report.Updated = time.Now().Format(time.RFC3339Nano)

		return captureWriteJSON(filepath.Join(fields.directory, "capture.json"), report)
	}
	if err = save(); err != nil {
		return err
	}

	err = runNativeCapture(context.Background(), fields.directory, request, report, save)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			report.State = "cancelled"
			report.Message = err.Error()
		} else {
			report.State = "error"
			message := err.Error()
			report.Error = &message
			report.Message = message
		}
	}

	report.Completed = time.Now().Format(time.RFC3339Nano)
	if saveErr := save(); saveErr != nil {
		return saveErr
	}

	if report.State == "cancelled" {
		return nil
	}

	return err
}

func checkCaptureOwner(dir string, r CaptureRequest) error {
	if _, err := os.Stat(filepath.Join(dir, "cancel.json")); err == nil {
		return fmt.Errorf("The client canceled capture: %w", context.Canceled)
	}

	if !ProcessIdentityAlive(r.OwnerPID, r.OwnerStartTicks) {
		return fmt.Errorf("The client exited. Capture stopped: %w", context.Canceled)
	}

	return nil
}
