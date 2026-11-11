//go:build windows

package desktop

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

type uiBrowseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	Param       uintptr
	Image       int32
}

// ShellOpen lets Windows choose the default browser or file manager.
func ShellOpen(target string) error {
	result, _, callErr := uiShell32.NewProc("ShellExecuteW").Call(0,
		uintptr(unsafe.Pointer(uiString("open"))), uintptr(unsafe.Pointer(uiString(target))), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("Could not open: %s (code %d, %v)", target, result, callErr)
	}

	return nil
}

// PickFolder uses the native shell folder picker. An empty result is cancellation.
// It must be called on the UI thread, which RunWindow initializes for COM.
func PickFolder(owner uintptr, initial string) (string, error) {
	return PickFolderContext(context.Background(), owner, initial)
}

// PickFolderContext also closes the native dialog when its owning backend exits.
// The caller must lock the OS thread and initialize a COM apartment.
func PickFolderContext(ctx context.Context, owner uintptr, initial string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	displayName := make([]uint16, 32768)
	initialPath := uiString(initial)
	done := make(chan struct{})
	defer close(done)

	callback := syscall.NewCallback(func(hwnd uintptr, message uint32, lparam, data uintptr) uintptr {
		if message != 1 {
			return 0
		}

		if initial != "" {
			uiUser32.NewProc("SendMessageW").Call(hwnd, 0x467, 1, uintptr(unsafe.Pointer(initialPath)))
		}

		go closeFolderOnCancel(ctx, done, hwnd)

		return 0
	})
	fields := uiBrowseInfo{
		Owner:       owner,
		DisplayName: &displayName[0],
		Title:       uiString("Choose a folder for logs and saved checks"),
		Flags:       0x51,
		Callback:    callback,
	}
	folderIDList, _, _ := uiShell32.NewProc("SHBrowseForFolderW").Call(uintptr(unsafe.Pointer(&fields)))
	runtime.KeepAlive(initialPath)
	if err := ctx.Err(); err != nil {
		if folderIDList != 0 {
			uiOle32.NewProc("CoTaskMemFree").Call(folderIDList)
		}

		return "", err
	}

	if folderIDList == 0 {
		return "", nil
	}

	defer uiOle32.NewProc("CoTaskMemFree").Call(folderIDList)

	path := make([]uint16, 32768)
	ok, _, _ := uiShell32.NewProc("SHGetPathFromIDListEx").Call(folderIDList, uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)), 0)
	if ok == 0 {
		return "", errors.New("Choose a regular folder on disk")
	}

	return syscall.UTF16ToString(path), nil
}

func closeFolderOnCancel(ctx context.Context, done <-chan struct{}, hwnd uintptr) {
	select {
	case <-ctx.Done():
		select {
		case <-done:
			return
		default:
			uiUser32.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0) // WM_CLOSE
		}
	case <-done:
	}
}
