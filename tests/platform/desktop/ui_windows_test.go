//go:build windows

package desktop_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/sa1tbaby/go-network-stat/frontend/desktop"
)

func TestBackendFolderPickerClosesOnOwnerCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	directory := t.TempDir()
	go func() {
		_, err := desktop.PickFolderForBackend(ctx, directory)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("picker did not report owner cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native folder dialog prevented backend shutdown")
	}
}

func TestNativeWindowLifecycle(t *testing.T) {
	// This test creates its own window with fake callbacks and no server/processes.
	// Do not send input to a user's running instance of the application.
	if existing, _, _ := uiUser32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(uiString(uiClassName))), 0); existing != 0 {
		t.Skip("A real client window is already open")
	}

	var pathMu sync.Mutex
	directory := `C:\InternetStability-test-initial`
	var panelClicks, logClicks atomic.Int32
	exitStarted := make(chan struct{})
	exitRelease := make(chan struct{})
	var releaseOnce sync.Once
	windowDone := make(chan error, 1)
	go func() {
		windowDone <- desktop.RunWindow(desktop.UIActions{
			InitialDirectory: directory,
			OnBrowse: func(owner uintptr) (string, error) {
				pathMu.Lock()
				defer pathMu.Unlock()
				directory = `C:\InternetStability-test-selected`

				return directory, nil
			},
			OnOpenPanel: func() error {
				panelClicks.Add(1)

				return nil
			},
			OnOpenLogs: func() error {
				logClicks.Add(1)

				return nil
			},
			OnStatus: func() (string, string) {
				pathMu.Lock()
				defer pathMu.Unlock()

				return "Dashboard ready", directory
			},
			OnExit: func() {
				close(exitStarted)
				<-exitRelease
			},
		})
	}()
	var hwnd uintptr
	wait := func(label string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if predicate() {
				return
			}

			time.Sleep(10 * time.Millisecond)
		}

		t.Fatalf("timed out: %s", label)
	}
	wait("own native window", func() bool {
		found, _, _ := uiUser32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(uiString(uiClassName))), 0)
		if found == 0 {
			return false
		}

		var pid uint32
		uiUser32.NewProc("GetWindowThreadProcessId").Call(found, uintptr(unsafe.Pointer(&pid)))
		if pid != uint32(os.Getpid()) {
			return false
		}

		hwnd = found

		return true
	})
	defer func() {
		releaseOnce.Do(func() {
			close(exitRelease)
		})
		uiUser32.NewProc("PostMessageW").Call(hwnd, 0x111, idTrayExit, 0)
	}()
	visible := func() bool {
		value, _, _ := uiUser32.NewProc("IsWindowVisible").Call(hwnd)

		return value != 0
	}
	alive := func() bool {
		value, _, _ := uiUser32.NewProc("IsWindow").Call(hwnd)

		return value != 0
	}
	command := func(id uintptr) {
		uiUser32.NewProc("SendMessageW").Call(hwnd, 0x111, id, 0)
	}
	wait("visible window", visible)
	command(idBrowse)
	edit, _, _ := uiUser32.NewProc("GetDlgItem").Call(hwnd, 1000)
	var editText [1024]uint16
	uiUser32.NewProc("GetWindowTextW").Call(edit, uintptr(unsafe.Pointer(&editText[0])), uintptr(len(editText)))
	if text := stringFromUI(editText[:]); !strings.HasSuffix(text, "test-selected") {
		t.Fatalf("selected folder not displayed: %q", text)
	}

	command(idPanel)
	command(idTrayLogs)
	wait("panel and log callbacks", func() bool {
		return panelClicks.Load() == 1 && logClicks.Load() == 1
	})
	uiUser32.NewProc("ShowWindow").Call(hwnd, 6)
	wait("minimize hides taskbar window", func() bool {
		return !visible()
	})
	if !desktop.ShowClientWindow() {
		t.Fatal("second-instance activation did not find own window")
	}

	wait("restore from tray", visible)
	uiUser32.NewProc("SendMessageW").Call(hwnd, 0x10, 0, 0)
	wait("close button hides window", func() bool {
		return !visible()
	})
	select {
	case <-exitStarted:
		t.Fatal("close button stopped the client")
	default:
	}

	command(idTrayExit)
	select {
	case <-exitStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("tray exit did not start controlled shutdown")
	}

	if !alive() {
		t.Fatal("window destroyed before server shutdown completed")
	}

	releaseOnce.Do(func() {
		close(exitRelease)
	})
	select {
	case err := <-windowDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native message loop did not exit after cleanup")
	}

	if alive() {
		t.Fatal("window still exists after controlled exit")
	}
}

func stringFromUI(value []uint16) string {
	// Avoid making the smoke test depend on client/server configuration code.
	end := 0
	for end < len(value) && value[end] != 0 {
		end++
	}

	runes := make([]rune, end)
	for index, char := range value[:end] {
		runes[index] = rune(char)
	}

	return string(runes)
}

var uiUser32 = syscall.NewLazyDLL("user32.dll")

const (
	uiClassName = "InternetStabilityClientWindow"
	idBrowse    = 1001
	idPanel     = 1002
	idTrayLogs  = 1103
	idTrayExit  = 1104
)

func uiString(value string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(value)

	return p
}
