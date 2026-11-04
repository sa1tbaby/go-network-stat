package collectors

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

func captureOrdinaryPath(path string) error {
	path = filepath.Clean(path)
	for {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Capture path contains a link")
		}

		next := filepath.Dir(path)
		if next == path {
			return nil
		}

		path = next
	}
}

func captureWriteJSON(path string, data any) error {
	if err := captureOrdinaryPath(path); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".capture-*.tmp")
	if err != nil {
		return err
	}

	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(encoded); err != nil {
		file.Close()

		return err
	}

	if err = file.Sync(); err != nil {
		file.Close()

		return err
	}

	if err = file.Close(); err != nil {
		return err
	}

	return fileutil.Replace(name, path)
}

func WriteCaptureRequest(dir string, request CaptureRequest) error {
	if err := captureOrdinaryPath(dir); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return captureWriteJSON(filepath.Join(dir, "request.json"), request)
}

func CancelCapture(dir, runID string) error {
	return captureWriteJSON(filepath.Join(dir, "cancel.json"), map[string]any{"appId": "codex-cod-relay-cancel", "runId": runID})
}

func ReadCaptureReport(dir string, request CaptureRequest) (*CaptureReport, error) {
	path := filepath.Join(dir, "capture.json")
	if err := captureOrdinaryPath(path); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() > 262144 {
		return nil, errors.New("Capture result exceeds size limit")
	}

	var report CaptureReport
	if err = json.NewDecoder(io.LimitReader(file, 262145)).Decode(&report); err != nil {
		return nil, err
	}

	if report.AppID != "codex-cod-relay-capture" || report.RunID != request.RunID || report.GamePID != request.GamePID || report.GameStartTicks != request.GameStartTicks || report.Game != request.Game {
		return nil, errors.New("Capture result identity mismatch")
	}

	return &report, nil
}
