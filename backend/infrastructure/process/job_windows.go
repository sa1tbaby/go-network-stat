//go:build windows

// Package process contains Windows process ownership and lifetime primitives.
package process

import (
	"sync"
	"syscall"
	"unsafe"
)

// Job owns a Windows process tree; closing it terminates every descendant.
type Job struct {
	mu     sync.Mutex
	handle syscall.Handle
}

func NewJob() (*Job, error) {
	h, _, callErr := supervisorCreateJob.Call(0, 0)
	if h == 0 {
		return nil, supervisorAPIError("CreateJobObjectW", callErr)
	}

	limits := supervisorJobExtendedLimits{}
	limits.BasicLimitInformation.LimitFlags = supervisorJobKillOnClose
	ok, _, callErr := supervisorSetJobInformation.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		syscall.CloseHandle(syscall.Handle(h))

		return nil, supervisorAPIError("SetInformationJobObject", callErr)
	}

	return &Job{handle: syscall.Handle(h)}, nil
}

func (job *Job) Close() error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.handle == 0 {
		return nil
	}

	err := syscall.CloseHandle(job.handle)
	if err == nil {
		job.handle = 0
	}

	return err
}

func (job *Job) Terminate(code uint32) error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.handle == 0 {
		return nil
	}

	ok, _, err := supervisorTerminateJob.Call(uintptr(job.handle), uintptr(code))
	if ok == 0 {
		return supervisorAPIError("TerminateJobObject", err)
	}

	return nil
}
