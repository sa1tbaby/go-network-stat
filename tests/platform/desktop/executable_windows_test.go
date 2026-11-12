//go:build windows

package desktop_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/sa1tbaby/go-network-stat/backend/config"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
)

// This opt-in test starts the final portable EXE, rather than calling its Go
// supervisor methods. Every setting and log belongs to a temporary fixture.
// No GUI, browser or elevated capture is opened; the ordinary monitor is paused.
func TestPackagedExecutableLifecycle(t *testing.T) {
	if os.Getenv("STABILITY_EXE_INTEGRATION") != "1" {
		t.Skip("set STABILITY_EXE_INTEGRATION=1 to test the final portable EXE")
	}

	executable, err := filepath.Abs(filepath.Join("..", "..", "..", "dist", "InternetStability.exe"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(executable); err != nil {
		t.Fatal(err)
	}

	t.Run("graceful_exit", func(t *testing.T) {
		fixture := startExecutableFixture(t, executable, "15s")
		backend, backendPID, image := fixture.openBackend(t)
		defer syscall.CloseHandle(backend)
		response, err := fixture.http.Get(fixture.endpoint)
		if err != nil {
			t.Fatal(err)
		}

		page, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(string(page), "Connection Stability") {
			t.Fatalf("panel: HTTP %d, bytes %d", response.StatusCode, len(page))
		}

		select {
		case err := <-fixture.done:
			fixture.waited = true
			if err != nil {
				t.Fatalf("graceful client exit: %v", err)
			}
		case <-time.After(25 * time.Second):
			t.Fatal("client did not exit after its 15 second timer")
		}

		assertFixtureProcessStopped(t, backend, backendPID)
		fixture.assertPortClosed(t)
		t.Logf("EXE_EVIDENCE graceful clientPID=%d backendPID=%d image=%q http=200 clientExited=true backendExited=true portClosed=true", fixture.command.Process.Pid, backendPID, image)
	})
	t.Run("crash_kills_active_collectors", func(t *testing.T) {
		fixture := startExecutableFixture(t, executable, "45s")
		backend, backendPID, image := fixture.openBackend(t)
		defer syscall.CloseHandle(backend)
		var status struct {
			Adapters []struct {
				ID        string `json:"id"`
				Available bool   `json:"available"`
				ICMPSafe  bool   `json:"icmpSafe"`
			} `json:"adapters"`
		}
		if err := fixture.getJSON("api/status", &status); err != nil {
			t.Fatal(err)
		}

		adapterID := ""
		for _, adapter := range status.Adapters {
			if adapter.Available && adapter.ICMPSafe {
				adapterID = adapter.ID
				break
			}
		}

		if adapterID == "" {
			t.Fatal("no available source-bound adapter for isolated collector test")
		}

		response, err := fixture.http.Post(fixture.endpoint+"api/diagnostics/start?adapterId="+url.QueryEscape(adapterID), "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}

		var started struct {
			RunID string `json:"runId"`
			Error string `json:"error"`
		}
		err = json.NewDecoder(response.Body).Decode(&started)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || started.RunID == "" {
			t.Fatalf("diagnosis: %+v HTTP %d error %v", started, response.StatusCode, err)
		}

		var diagnostics struct {
			Runs []struct {
				ID       string `json:"id"`
				Running  bool   `json:"running"`
				Network  any    `json:"network"`
				Radio    any    `json:"radio"`
				Ethernet any    `json:"ethernet"`
			} `json:"runs"`
		}
		deadline := time.Now().Add(8 * time.Second)
		observed := false
		for time.Now().Before(deadline) {
			if err := fixture.getJSON("api/diagnostics", &diagnostics); err != nil {
				t.Fatal(err)
			}

			for _, run := range diagnostics.Runs {
				if run.ID == started.RunID && run.Running && run.Network != nil && (run.Radio != nil || run.Ethernet != nil) {
					observed = true
				}
			}

			if observed {
				break
			}

			time.Sleep(100 * time.Millisecond)
		}

		if !observed {
			t.Fatal("native network and line collectors did not publish live state")
		}

		if err := fixture.command.Process.Kill(); err != nil {
			t.Fatal(err)
		}

		select {
		case <-fixture.done:
			fixture.waited = true
		case <-time.After(5 * time.Second):
			t.Fatal("killed client did not terminate")
		}

		assertFixtureProcessStopped(t, backend, backendPID)
		fixture.assertPortClosed(t)
		t.Logf("EXE_EVIDENCE crash clientPID=%d backendPID=%d image=%q backendExited=true nativeCollectorsStopped=true portClosed=true", fixture.command.Process.Pid, backendPID, image)
	})
}

type executableFixture struct {
	command   *exec.Cmd
	done      chan error
	waited    bool
	directory string
	logs      string
	endpoint  string
	http      *http.Client
}

func startExecutableFixture(t *testing.T, executable, duration string) *executableFixture {
	t.Helper()
	if connection, err := net.DialTimeout("tcp", "127.0.0.1:18865", 300*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("isolated integration port 18865 is already occupied")
	}

	directory := t.TempDir()
	layout, err := data.New(directory)
	if err != nil {
		t.Fatal(err)
	}

	if err = layout.Create(); err != nil {
		t.Fatal(err)
	}

	logs := filepath.Join(directory, "Logs — current")
	config, _ := json.Marshal(config.Config{
		LogDirectory: logs,
		IntervalMs:   1000,
		StartPaused:  true,
		Port:         18865,
	})
	if err := os.WriteFile(filepath.Join(directory, "config", "settings.json"), config, 0600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	command := exec.CommandContext(ctx, executable, "--headless", "--paused", "--data-dir", directory, "--port", "18865", "--exit-after", duration)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	fixture := &executableFixture{
		command:   command,
		done:      make(chan error, 1),
		directory: directory,
		logs:      logs,
		endpoint:  "http://127.0.0.1:18865/",
		http:      &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}},
	}
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}

	go func() {
		fixture.done <- command.Wait()
	}()
	t.Cleanup(func() {
		cancel()
		if !fixture.waited {
			_ = command.Process.Kill()
			select {
			case <-fixture.done:
			case <-time.After(5 * time.Second):
			}
		}
	})
	deadline := time.Now().Add(13 * time.Second)
	for time.Now().Before(deadline) {
		var status struct {
			AppID        string `json:"appId"`
			Running      bool   `json:"running"`
			LogDirectory string `json:"logDir"`
		}
		if fixture.getJSON("api/status", &status) == nil && status.AppID == "codex-internet-stability" {
			if status.Running || status.LogDirectory != logs {
				t.Fatalf("fixture escaped paused isolated configuration: %+v", status)
			}

			return fixture
		}

		select {
		case err := <-fixture.done:
			fixture.waited = true
			output, _ := os.ReadFile(filepath.Join(directory, "logs", "client-backend.log"))
			t.Fatalf("client exited before readiness: %v\n%s", err, output)
		default:
		}

		time.Sleep(150 * time.Millisecond)
	}

	output, _ := os.ReadFile(filepath.Join(directory, "logs", "client-backend.log"))
	t.Fatalf("portable EXE was not ready within 13s\n%s", output)

	return nil
}

func (f *executableFixture) getJSON(path string, target any) error {
	response, err := f.http.Get(f.endpoint + path)
	if err != nil {
		return err
	}

	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(target)
}

func (f *executableFixture) openBackend(t *testing.T) (syscall.Handle, uint32, string) {
	t.Helper()
	var state struct {
		PID uint32 `json:"pid"`
	}
	data, err := os.ReadFile(filepath.Join(f.directory, "runtime", "run-state.json"))
	if err != nil || json.Unmarshal(data, &state) != nil || state.PID == 0 {
		t.Fatalf("backend run-state: pid %d error %v", state.PID, err)
	}

	handle, err := syscall.OpenProcess(0x00100000|0x1000, false, state.PID)
	if err != nil {
		t.Fatal(err)
	}

	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, callErr := syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW").Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		syscall.CloseHandle(handle)
		t.Fatal(callErr)
	}

	image := syscall.UTF16ToString(buffer[:size])
	if !strings.EqualFold(filepath.Clean(image), filepath.Clean(f.command.Path)) {
		syscall.CloseHandle(handle)
		t.Fatalf("backend did not run the native Go executable: %s", image)
	}

	return handle, state.PID, image
}

func assertFixtureProcessStopped(t *testing.T, handle syscall.Handle, pid uint32) {
	t.Helper()
	if value, err := syscall.WaitForSingleObject(handle, 5000); err != nil || value != syscall.WAIT_OBJECT_0 {
		t.Fatalf("owned PID %d survived client exit: wait %d error %v", pid, value, err)
	}
}

func (f *executableFixture) assertPortClosed(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", "127.0.0.1:18865", 300*time.Millisecond)
		if err != nil {
			return
		}

		connection.Close()
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("isolated server port remained open after client exit")
}
