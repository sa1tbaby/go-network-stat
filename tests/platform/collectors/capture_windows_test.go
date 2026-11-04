//go:build windows && amd64

package collectors_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func TestCaptureCurrentProcessIdentityUsesDotNetTicks(t *testing.T) {
	pid, ticks, err := collectors.CurrentProcessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if pid != os.Getpid() || !collectors.ProcessIdentityAlive(pid, ticks) || collectors.ProcessIdentityAlive(pid, ticks+1) {
		t.Fatal("process identity mismatch")
	}

	started := time.Unix(0, (ticks-621355968000000000)*100)
	if age := time.Since(started); age < 0 || age > 10*time.Minute {
		t.Fatalf("invalid process start time %s", started)
	}
}

func TestNativeCaptureCancellationPrecedesAdminAndETW(t *testing.T) {
	dir := t.TempDir()
	pid, ticks, err := collectors.CurrentProcessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	request := collectors.CaptureRequest{
		AppID:           "codex-cod-relay-request",
		Game:            "cod",
		RunID:           strings.Repeat("a", 32),
		GamePID:         pid,
		GameStartTicks:  ticks,
		OwnerPID:        pid,
		OwnerStartTicks: ticks,
	}
	if err = collectors.WriteCaptureRequest(dir, request); err != nil {
		t.Fatal(err)
	}

	if err = collectors.CancelCapture(dir, request.RunID); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"--directory",
		dir,
		"--run-id",
		request.RunID,
		"--game",
		"cod",
		"--game-pid",
		strconv.Itoa(pid),
		"--game-start-ticks",
		strconv.FormatInt(ticks, 10),
	}
	if err = collectors.RunCaptureWorker(args); err != nil {
		t.Fatal(err)
	}

	report, err := collectors.ReadCaptureReport(dir, request)
	if err != nil {
		t.Fatal(err)
	}

	if report.State != "cancelled" || report.CaptureStartMs != 0 || report.PayloadPIDUsed {
		t.Fatalf("capture started unexpectedly: %#v", report)
	}

	if _, err = os.Stat(filepath.Join(dir, "kernel-network.etl")); !os.IsNotExist(err) {
		t.Fatal("canceled helper created an ETL")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = collectors.RunElevatedCapture(ctx, dir, request, 30, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller reached the elevated helper: %v", err)
	}
}
