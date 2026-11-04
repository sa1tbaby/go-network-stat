//go:build windows

package collectors

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func runNativeCapture(ctx context.Context, dir string, request CaptureRequest, report *CaptureReport, save func() error) (result error) {
	if err := checkCaptureOwner(dir, request); err != nil {
		return err
	}

	admin, _, _ := captureIsAdmin.Call()
	if admin == 0 {
		return errors.New("Windows ETW requires administrator privileges.")
	}

	checkGame := func() error {
		ticks, name, err := ProcessIdentity(request.GamePID)
		if err != nil || ticks != request.GameStartTicks || !strings.EqualFold(filepath.Base(name), request.Game+".exe") {
			return errors.New("The game exited or restarted; retry detection.")
		}

		return nil
	}
	if err := checkGame(); err != nil {
		return err
	}

	if err := captureOrdinaryPath(report.ETL); err != nil {
		return err
	}

	if _, err := os.Lstat(report.ETL); err == nil {
		return errors.New("Capture ETL file already exists")
	} else if !os.IsNotExist(err) {
		return err
	}

	buffer := make([]uint16, 32768)
	n, _, callErr := captureSystemDirectory.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || n >= uintptr(len(buffer)) {
		return fmt.Errorf("GetSystemDirectory: %w", callErr)
	}

	logman := filepath.Join(syscall.UTF16ToString(buffer[:n]), "logman.exe")
	command := func(args ...string) error {
		cmdCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cmdCtx, logman, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("logman: %w: %s", err, strings.TrimSpace(string(output)))
		}

		return nil
	}
	owned := false
	defer func() {
		if owned {
			if err := command("stop", report.Session, "-ets"); err != nil {
				if result == nil {
					result = err
				} else {
					result = fmt.Errorf("%w; cleanup: %v", result, err)
				}
			}
		}
	}()
	if err := command("start", report.Session, "-ets", "-p", "Microsoft-Windows-Kernel-Network", "0x30", "5", "-o", report.ETL, "-f", "bincirc", "-max", "16"); err != nil {
		return err
	}

	owned = true
	report.CaptureStartMs = time.Now().UnixMilli()
	report.State = "capturing"
	report.Message = "Observing UDP traffic for " + request.Game + ".exe, " + strconv.Itoa(report.DurationSeconds) + " seconds"
	if err := save(); err != nil {
		return err
	}

	deadline := time.NewTimer(time.Duration(report.DurationSeconds) * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	loop := true
	for loop {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			loop = false
		case <-ticker.C:
			if err := checkCaptureOwner(dir, request); err != nil {
				return err
			}

			if err := checkGame(); err != nil {
				return err
			}
		}
	}

	report.CaptureEndMs = time.Now().UnixMilli()
	if err := command("stop", report.Session, "-ets"); err != nil {
		return err
	}

	owned = false
	report.State = "analyzing"
	report.Message = "Checking the duration, directions, and dominance of UDP flows"
	if err := save(); err != nil {
		return err
	}

	if err := checkCaptureOwner(dir, request); err != nil {
		return err
	}

	if err := checkGame(); err != nil {
		return err
	}

	events, total, ipv6, err := readCaptureETL(report.ETL, request.GamePID, func() error {
		return checkCaptureOwner(dir, request)
	})
	if err != nil {
		return err
	}

	analysis := ClassifyUDP(events, request.GamePID, report.CaptureStartMs, report.CaptureEndMs, request.Game)
	if ipv6 > 0 && analysis.GameUDPEvents == 0 {
		analysis.Message = "Only IPv6 UDP traffic was detected. This dashboard supports IPv4 for cards and route tracing; no address was added."
	}

	report.PayloadPIDUsed = true
	report.Analysis = &analysis
	report.TotalETWEvents = total
	report.IPv6GameUDPEvents = ipv6
	report.State = analysis.State
	report.Message = analysis.Message

	return nil
}
