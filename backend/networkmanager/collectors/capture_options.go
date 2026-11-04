package collectors

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type captureOptions struct {
	directory  string
	runID      string
	game       string
	processID  int
	startTicks int64
	duration   int
}

func parseCaptureOptions(args []string) (captureOptions, error) {
	var fields captureOptions
	flags := flag.NewFlagSet("capture-worker", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&fields.directory, "directory", "", "")
	flags.StringVar(&fields.runID, "run-id", "", "")
	flags.StringVar(&fields.game, "game", "cod", "")
	flags.IntVar(&fields.processID, "game-pid", 0, "")
	flags.Int64Var(&fields.startTicks, "game-start-ticks", 0, "")
	flags.IntVar(&fields.duration, "duration-seconds", 20, "")
	if err := flags.Parse(args); err != nil {
		return fields, err
	}

	if flags.NArg() != 0 || !filepath.IsAbs(fields.directory) || len(fields.runID) != 32 || strings.Trim(fields.runID, "0123456789abcdef") != "" || (fields.game != "cod" && fields.game != "cs2") || fields.processID <= 0 || fields.startTicks <= 0 || fields.duration < 15 || fields.duration > 30 {
		return fields, errors.New("Invalid capture worker arguments")
	}

	return fields, nil
}

func loadCaptureRequest(fields captureOptions) (CaptureRequest, error) {
	var request CaptureRequest
	if err := captureOrdinaryPath(fields.directory); err != nil {
		return request, err
	}

	if err := captureOrdinaryPath(filepath.Join(fields.directory, "request.json")); err != nil {
		return request, err
	}

	data, err := os.ReadFile(filepath.Join(fields.directory, "request.json"))
	if err != nil {
		return request, err
	}

	if err = json.Unmarshal(data, &request); err != nil {
		return request, err
	}

	if request.AppID != "codex-cod-relay-request" || request.RunID != fields.runID || request.Game != fields.game || request.GamePID != fields.processID || request.GameStartTicks != fields.startTicks || request.OwnerPID <= 0 || request.OwnerStartTicks <= 0 {
		return request, errors.New("Capture request identity mismatch")
	}

	return request, nil
}
