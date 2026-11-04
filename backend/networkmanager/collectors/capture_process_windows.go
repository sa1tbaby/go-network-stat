//go:build windows

package collectors

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type captureProcessEntry struct {
	Size          uint32
	Usage         uint32
	PID           uint32
	DefaultHeapID uintptr
	ModuleID      uint32
	Threads       uint32
	ParentPID     uint32
	Priority      int32
	Flags         uint32
	ExeFile       [260]uint16
}

type captureMemoryCounters struct {
	Size             uint32
	PageFaults       uint32
	PeakWorkingSet   uintptr
	WorkingSet       uintptr
	PeakPagedPool    uintptr
	PagedPool        uintptr
	PeakNonPagedPool uintptr
	NonPagedPool     uintptr
	PageFile         uintptr
	PeakPageFile     uintptr
}

func ProcessIdentity(pid int) (int64, string, error) {
	if pid <= 0 {
		return 0, "", errors.New("Invalid process ID")
	}

	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return 0, "", err
	}

	defer syscall.CloseHandle(handle)
	var created, exited, kernel, user syscall.Filetime
	ok, _, callErr := captureGetTimes.Call(uintptr(handle), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return 0, "", fmt.Errorf("GetProcessTimes: %w", callErr)
	}

	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	ok, _, callErr = captureImageName.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return 0, "", fmt.Errorf("QueryFullProcessImageName: %w", callErr)
	}

	var exit uint32
	if err = syscall.GetExitCodeProcess(handle, &exit); err != nil || exit != 259 {
		return 0, "", errors.New("Process has exited")
	}

	return int64(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime)) + 504911232000000000, syscall.UTF16ToString(buf[:size]), nil
}

func ProcessIdentityAlive(pid int, ticks int64) bool {
	current, _, err := ProcessIdentity(pid)

	return err == nil && current == ticks
}

func CurrentProcessIdentity() (int, int64, error) {
	pid := os.Getpid()
	ticks, _, err := ProcessIdentity(pid)

	return pid, ticks, err
}

func FindGameProcess(game string) (int, int64, error) {
	if game != "cod" && game != "cs2" {
		return 0, 0, errors.New("Unsupported game.")
	}

	snapshot, _, err := captureSnapshot.Call(2, 0)
	if snapshot == ^uintptr(0) {
		return 0, 0, fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}

	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := captureProcessEntry{Size: uint32(unsafe.Sizeof(captureProcessEntry{}))}
	ok, _, _ := captureProcessFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	pid := 0
	var ticks int64
	var largest uintptr
	for ok != 0 {
		if strings.EqualFold(syscall.UTF16ToString(entry.ExeFile[:]), game+".exe") {
			started, name, e := ProcessIdentity(int(entry.PID))
			if e == nil && strings.EqualFold(filepath.Base(name), game+".exe") {
				var working uintptr
				h, e := syscall.OpenProcess(0x0410, false, entry.PID)
				if e == nil {
					info := captureMemoryCounters{Size: uint32(unsafe.Sizeof(captureMemoryCounters{}))}
					read, _, _ := captureMemory.Call(uintptr(h), uintptr(unsafe.Pointer(&info)), uintptr(info.Size))
					if read != 0 {
						working = info.WorkingSet
					}

					syscall.CloseHandle(h)
				}

				if pid == 0 || working > largest {
					pid, ticks, largest = int(entry.PID), started, working
				}
			}
		}

		ok, _, _ = captureProcessNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}

	if pid == 0 {
		return 0, 0, fmt.Errorf("Process %s.exe was not found. Launch the game and retry detection during a match.", game)
	}

	return pid, ticks, nil
}
