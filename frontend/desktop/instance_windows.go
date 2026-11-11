//go:build windows

package desktop

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

var (
	supervisorKernel      = syscall.NewLazyDLL("kernel32.dll")
	supervisorCreateMutex = supervisorKernel.NewProc("CreateMutexW")
	supervisorFindWindow  = syscall.NewLazyDLL("user32.dll").NewProc("FindWindowW")
	supervisorPostMessage = syscall.NewLazyDLL("user32.dll").NewProc("PostMessageW")
)

func AcquireSingleInstance() (release func(), already bool, err error) {
	name, _ := syscall.UTF16PtrFromString("Local\\InternetStabilityPortableClient")
	handle, _, callErr := supervisorCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, false, fmt.Errorf("CreateMutexW: %w", callErr)
	}

	var once sync.Once
	release = func() {
		once.Do(func() {
			syscall.CloseHandle(syscall.Handle(handle))
		})
	}

	return release, callErr == syscall.ERROR_ALREADY_EXISTS, nil
}

func ShowExistingWindow() {
	className, _ := syscall.UTF16PtrFromString("InternetStabilityClientWindow")
	window, _, _ := supervisorFindWindow.Call(uintptr(unsafe.Pointer(className)), 0)
	if window != 0 {
		supervisorPostMessage.Call(window, 0x8001, 0, 0)
	}
}
