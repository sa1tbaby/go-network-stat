//go:build windows

// Package process contains Windows process ownership and lifetime primitives.
package process

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// Start assigns a suspended process before any child code can execute.
// All ordinary descendants inherit this job, including collector subprocesses.
func (job *Job) Start(executable string, arguments []string, directory string, log *os.File) (syscall.Handle, uint32, error) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.handle == 0 {
		return 0, 0, fmt.Errorf("process job is closed")
	}

	application, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return 0, 0, err
	}

	var pieces []string
	for _, argument := range append([]string{executable}, arguments...) {
		pieces = append(pieces, syscall.EscapeArg(argument))
	}

	commandLine, err := syscall.UTF16PtrFromString(strings.Join(pieces, " "))
	if err != nil {
		return 0, 0, err
	}

	workingDirectory, err := syscall.UTF16PtrFromString(directory)
	if err != nil {
		return 0, 0, err
	}

	null, err := os.Open(os.DevNull)
	if err != nil {
		return 0, 0, err
	}

	defer null.Close()
	if err = syscall.SetHandleInformation(syscall.Handle(log.Fd()), 1, 1); err != nil {
		return 0, 0, err
	}

	defer syscall.SetHandleInformation(syscall.Handle(log.Fd()), 1, 0)
	if err = syscall.SetHandleInformation(syscall.Handle(null.Fd()), 1, 1); err != nil {
		return 0, 0, err
	}

	defer syscall.SetHandleInformation(syscall.Handle(null.Fd()), 1, 0)
	startup := syscall.StartupInfo{
		Cb:        uint32(unsafe.Sizeof(syscall.StartupInfo{})),
		Flags:     syscall.STARTF_USESTDHANDLES,
		StdInput:  syscall.Handle(null.Fd()),
		StdOutput: syscall.Handle(log.Fd()),
		StdErr:    syscall.Handle(log.Fd()),
	}
	var process syscall.ProcessInformation
	err = syscall.CreateProcess(application, commandLine, nil, nil, true, supervisorCreateSuspend|supervisorCreateNoWindow, nil, workingDirectory, &startup, &process)
	if err != nil {
		return 0, 0, fmt.Errorf("Starting collector: %w", err)
	}

	fail := func(err error) (syscall.Handle, uint32, error) {
		syscall.TerminateProcess(process.Process, 1)
		syscall.WaitForSingleObject(process.Process, 3000)
		syscall.CloseHandle(process.Thread)
		syscall.CloseHandle(process.Process)

		return 0, 0, err
	}
	ok, _, callErr := supervisorAssignProcess.Call(uintptr(job.handle), uintptr(process.Process))
	if ok == 0 {
		return fail(supervisorAPIError("Failed to attach the collector to the client process", callErr))
	}

	previousCount, _, callErr := supervisorResumeThread.Call(uintptr(process.Thread))
	if previousCount == 0xffffffff {
		return fail(supervisorAPIError("ResumeThread", callErr))
	}

	syscall.CloseHandle(process.Thread)

	return process.Process, process.ProcessId, nil
}
