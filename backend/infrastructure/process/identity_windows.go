//go:build windows

// Package process contains Windows process ownership and lifetime primitives.
package process

import (
	"syscall"
	"unsafe"
)

func CurrentStartTicks() (uint64, error) {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, err
	}

	var creation, exit, kernel, user syscall.Filetime
	ok, _, callErr := supervisorGetProcessTimes.Call(uintptr(process), uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return 0, supervisorAPIError("GetProcessTimes", callErr)
	}
	// FILETIME and DateTime.Ticks both use 100 ns units, with epochs 1601 and 0001.
	return (uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)) + 504911232000000000, nil
}

func OwnerAlive(pid int, expectedTicks int64) bool {
	handle, err := syscall.OpenProcess(0x1000|0x100000, false, uint32(pid))
	if err != nil {
		return false
	}

	defer syscall.CloseHandle(handle)
	if result, err := syscall.WaitForSingleObject(handle, 0); err != nil || result != syscall.WAIT_TIMEOUT {
		return false
	}

	var creation, exit, kernel, user syscall.Filetime
	ok, _, _ := supervisorGetProcessTimes.Call(uintptr(handle), uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	actual := int64((uint64(creation.HighDateTime)<<32)|uint64(creation.LowDateTime)) + 504911232000000000

	return ok != 0 && actual == expectedTicks
}
