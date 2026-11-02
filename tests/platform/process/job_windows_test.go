//go:build windows

package process_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/process"
)

func TestOwnerStartTicksMatchProcessTime(t *testing.T) {
	ticks, err := process.CurrentStartTicks()
	if err != nil {
		t.Fatal(err)
	}
	// DateTime UTC ticks converted to Unix must describe this test process startup.
	start := time.Unix(0, int64(ticks-621355968000000000)*100)
	if elapsed := time.Since(start); elapsed < 0 || elapsed > 5*time.Minute {
		t.Fatalf("invalid DateTime tick conversion: %v (%v ago)", ticks, elapsed)
	}
}

func TestJobClosureKillsWorkerAndDescendants(t *testing.T) {
	job, err := process.NewJob()
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if job != nil {
			job.Close()
		}
	}()
	workingDirectory := t.TempDir()
	pidsPath := filepath.Join(workingDirectory, "owned-pids.txt")
	t.Setenv("STABILITY_JOB_HELPER", "parent")
	t.Setenv("STABILITY_JOB_PIDS", pidsPath)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	log, err := os.Create(filepath.Join(workingDirectory, "helper.log"))
	if err != nil {
		t.Fatal(err)
	}

	defer log.Close()
	worker, _, err := job.Start(executable, []string{"-test.run=^TestSupervisorJobChildHelper$"}, workingDirectory, log)
	if err != nil {
		t.Fatal(err)
	}

	defer syscall.CloseHandle(worker)
	var pids []uint32
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		content, _ := os.ReadFile(pidsPath)
		lines := strings.Fields(string(content))
		if len(lines) == 3 {
			for _, line := range lines {
				pid, parseErr := strconv.ParseUint(line, 10, 32)
				if parseErr != nil {
					t.Fatal(parseErr)
				}

				pids = append(pids, uint32(pid))
			}

			break
		}

		time.Sleep(30 * time.Millisecond)
	}

	if len(pids) != 3 {
		content, _ := os.ReadFile(filepath.Join(workingDirectory, "helper.log"))
		t.Fatalf("expected worker, child and grandchild; pids=%v; output=%s", pids, content)
	}

	var processes []syscall.Handle
	for _, pid := range pids {
		process, err := syscall.OpenProcess(0x00100000, false, pid) // SYNCHRONIZE
		if err != nil {
			t.Fatalf("open helper PID %d: %v", pid, err)
		}

		processes = append(processes, process)
		defer syscall.CloseHandle(process)
		if status, _ := syscall.WaitForSingleObject(process, 0); status != syscall.WAIT_TIMEOUT {
			t.Fatalf("PID %d exited before job closure", pid)
		}
	}

	if err := job.Close(); err != nil {
		t.Fatal(err)
	}

	job = nil
	for index, process := range processes {
		status, err := syscall.WaitForSingleObject(process, 5000)
		if err != nil || status != syscall.WAIT_OBJECT_0 {
			t.Fatalf("PID %d survived job closure: status %d error %v", pids[index], status, err)
		}
	}
}

// This helper intentionally has no cleanup: Windows job containment must reap it.
func TestSupervisorJobChildHelper(t *testing.T) {
	role := os.Getenv("STABILITY_JOB_HELPER")
	if role == "" {
		return
	}

	file, err := os.OpenFile(os.Getenv("STABILITY_JOB_PIDS"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}

	file.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	file.Close()
	next := ""
	if role == "parent" {
		next = "child"
	} else if role == "child" {
		next = "grandchild"
	}

	if next != "" {
		executable, _ := os.Executable()
		command := exec.Command(executable, "-test.run=^TestSupervisorJobChildHelper$")
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		for _, variable := range os.Environ() {
			if !strings.HasPrefix(variable, "STABILITY_JOB_HELPER=") {
				command.Env = append(command.Env, variable)
			}
		}

		command.Env = append(command.Env, "STABILITY_JOB_HELPER="+next)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}

	for {
		time.Sleep(time.Hour)
	}
}
