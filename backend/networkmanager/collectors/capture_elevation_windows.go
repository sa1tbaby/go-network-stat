//go:build windows

package collectors

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// shellExecuteInfo follows SHELLEXECUTEINFOW, including pointer-sized padding.
type captureShellExecuteInfo struct {
	Size       uint32
	Mask       uint32
	Window     uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	Instance   uintptr
	IDList     uintptr
	Class      *uint16
	ClassKey   uintptr
	HotKey     uint32
	Icon       uintptr
	Process    uintptr
}

func RunElevatedCapture(ctx context.Context, dir string, request CaptureRequest, duration int, onStarted func(int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := checkCaptureOwner(dir, request); err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	arguments := []string{
		"--capture-worker",
		"--directory",
		dir,
		"--run-id",
		request.RunID,
		"--game",
		request.Game,
		"--game-pid",
		strconv.Itoa(request.GamePID),
		"--game-start-ticks",
		strconv.FormatInt(request.GameStartTicks, 10),
		"--duration-seconds",
		strconv.Itoa(duration),
	}
	for i, arg := range arguments {
		arguments[i] = syscall.EscapeArg(arg)
	}

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(executable)
	params, _ := syscall.UTF16PtrFromString(strings.Join(arguments, " "))
	directory, _ := syscall.UTF16PtrFromString(filepath.Dir(executable))
	info := captureShellExecuteInfo{
		Size:       uint32(unsafe.Sizeof(captureShellExecuteInfo{})),
		Mask:       0x00000040 | 0x00000400,
		Verb:       verb,
		File:       file,
		Parameters: params,
		Directory:  directory,
		Show:       0,
	}
	ok, _, callErr := captureShellExecute.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return fmt.Errorf("Failed to start the ETW collector: %w", callErr)
	}

	if info.Process == 0 {
		return errors.New("Windows did not return the ETW collector process")
	}

	defer syscall.CloseHandle(syscall.Handle(info.Process))
	pid, _, _ := captureKernel.NewProc("GetProcessId").Call(info.Process)
	if onStarted != nil {
		onStarted(int(pid))
	}

	deadline := time.Now().Add(90 * time.Second)
	var cancelled time.Time
	for {
		status, err := syscall.WaitForSingleObject(syscall.Handle(info.Process), 250)
		if err != nil {
			return err
		}

		if status == 0 {
			var code uint32
			if err = syscall.GetExitCodeProcess(syscall.Handle(info.Process), &code); err != nil {
				return err
			}

			if code != 0 {
				return fmt.Errorf("The ETW collector exited with code %d", code)
			}

			return nil
		}

		if ctx.Err() != nil || time.Now().After(deadline) {
			if cancelled.IsZero() {
				_ = CancelCapture(dir, request.RunID)
				cancelled = time.Now()
			}

			if time.Since(cancelled) > 5*time.Second {
				return errors.New("The ETW collector did not exit after the stop request; check the capture log.")
			}
		}
	}
}
