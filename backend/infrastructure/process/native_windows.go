//go:build windows

// Package process contains Windows process ownership and lifetime primitives.
package process

import (
	"fmt"
	"syscall"
)

var (
	supervisorKernel            = syscall.NewLazyDLL("kernel32.dll")
	supervisorCreateJob         = supervisorKernel.NewProc("CreateJobObjectW")
	supervisorSetJobInformation = supervisorKernel.NewProc("SetInformationJobObject")
	supervisorAssignProcess     = supervisorKernel.NewProc("AssignProcessToJobObject")
	supervisorTerminateJob      = supervisorKernel.NewProc("TerminateJobObject")
	supervisorResumeThread      = supervisorKernel.NewProc("ResumeThread")
	supervisorGetProcessTimes   = supervisorKernel.NewProc("GetProcessTimes")
)

const (
	supervisorJobKillOnClose = 0x00002000
	supervisorInfinite       = 0xffffffff
	supervisorCreateSuspend  = 0x00000004
	supervisorCreateNoWindow = 0x08000000
)

type supervisorJobBasicLimits struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type supervisorIOCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type supervisorJobExtendedLimits struct {
	BasicLimitInformation supervisorJobBasicLimits
	IOInfo                supervisorIOCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

func supervisorAPIError(operation string, callErr error) error {
	if callErr == nil || callErr == syscall.Errno(0) {
		return fmt.Errorf("%s: Windows API returned an error", operation)
	}

	return fmt.Errorf("%s: %w", operation, callErr)
}
