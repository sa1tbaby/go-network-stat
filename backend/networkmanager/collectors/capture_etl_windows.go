//go:build windows

package collectors

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// EvtQuery handles must be used on the creating OS thread. ETL is read only in
// forward order, as required by the native Windows Event Log API.
func readCaptureETL(path string, gamePID int, check func() error) ([]UDPEvent, int, int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, 0, 0, err
	}

	query, _, callErr := captureEvtQuery.Call(0, uintptr(unsafe.Pointer(name)), 0, 0x2|0x100)
	if query == 0 {
		return nil, 0, 0, fmt.Errorf("EvtQuery ETL: %w", callErr)
	}

	defer captureEvtClose.Call(query)
	rows := []UDPEvent{}
	total, ipv6 := 0, 0
	for {
		if err := check(); err != nil {
			return nil, total, ipv6, err
		}

		var handles [32]uintptr
		var returned uint32
		ok, _, nextErr := captureEvtNext.Call(query, uintptr(len(handles)), uintptr(unsafe.Pointer(&handles[0])), 1000, 0, uintptr(unsafe.Pointer(&returned)))
		if ok == 0 {
			if errors.Is(nextErr, syscall.Errno(259)) {
				break
			}

			return nil, total, ipv6, fmt.Errorf("EvtNext: %w", nextErr)
		}

		var parseErr error
		for i := uint32(0); i < returned; i++ {
			handle := handles[i]
			if parseErr == nil {
				total++
				data, err := renderCaptureEvent(handle)
				if err == nil {
					var event *UDPEvent
					var isIPv6 bool
					event, isIPv6, err = ParseKernelNetworkXML(data, gamePID)
					if isIPv6 {
						ipv6++
					}

					if event != nil {
						rows = append(rows, *event)
					}
				}

				if err != nil {
					parseErr = err
				}
			}

			captureEvtClose.Call(handle)
		}

		if parseErr != nil {
			return nil, total, ipv6, parseErr
		}
	}

	return rows, total, ipv6, nil
}

func renderCaptureEvent(handle uintptr) ([]byte, error) {
	var used, count uint32
	ok, _, err := captureEvtRender.Call(0, handle, 1, 0, 0, uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&count)))
	if ok == 0 && !errors.Is(err, syscall.Errno(122)) {
		return nil, fmt.Errorf("EvtRender size: %w", err)
	}

	if used == 0 || used > 1024*1024 {
		return nil, errors.New("Invalid ETW XML event size")
	}

	buf := make([]uint16, (int(used)+1)/2)
	ok, _, err = captureEvtRender.Call(0, handle, 1, uintptr(len(buf)*2), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&count)))
	if ok == 0 {
		return nil, fmt.Errorf("EvtRender XML: %w", err)
	}

	return []byte(syscall.UTF16ToString(buf)), nil
}
