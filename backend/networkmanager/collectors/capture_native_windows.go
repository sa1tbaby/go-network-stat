//go:build windows

package collectors

import (
	"syscall"
)

var (
	captureKernel          = syscall.NewLazyDLL("kernel32.dll")
	captureGetTimes        = captureKernel.NewProc("GetProcessTimes")
	captureSnapshot        = captureKernel.NewProc("CreateToolhelp32Snapshot")
	captureProcessFirst    = captureKernel.NewProc("Process32FirstW")
	captureProcessNext     = captureKernel.NewProc("Process32NextW")
	captureImageName       = captureKernel.NewProc("QueryFullProcessImageNameW")
	captureSystemDirectory = captureKernel.NewProc("GetSystemDirectoryW")
	captureMemory          = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	captureShell           = syscall.NewLazyDLL("shell32.dll")
	captureIsAdmin         = captureShell.NewProc("IsUserAnAdmin")
	captureShellExecute    = captureShell.NewProc("ShellExecuteExW")
	captureEvent           = syscall.NewLazyDLL("wevtapi.dll")
	captureEvtQuery        = captureEvent.NewProc("EvtQuery")
	captureEvtNext         = captureEvent.NewProc("EvtNext")
	captureEvtRender       = captureEvent.NewProc("EvtRender")
	captureEvtClose        = captureEvent.NewProc("EvtClose")
)
