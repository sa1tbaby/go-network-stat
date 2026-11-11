//go:build windows

package desktop_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/api/assets"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/process"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/frontend/desktop"
)

func backendIntegrationPayload(t *testing.T) (source, executable string) {
	t.Helper()
	if os.Getenv("STABILITY_TEST_BUNDLED") != "1" && os.Getenv("STABILITY_TEST_EXE") == "" {
		t.Skip("set STABILITY_TEST_BUNDLED=1 or STABILITY_TEST_EXE for isolated native Go backend integration")
	}

	payload, err := assets.Prepare(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}

	executable = os.Getenv("STABILITY_TEST_EXE")
	if executable == "" {
		executable = filepath.Join(t.TempDir(), "InternetStability.exe")
		build := exec.Command("go", "build", "-o", executable, "../../../cmd/client")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build native backend: %v\n%s", err, output)
		}
	}

	executable, err = filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}

	return payload.Root, executable
}

// All settings/logs belong to TempDir. The normal monitor is paused; no game,
// administrator elevation or packet capture is requested by these tests.
func TestBackendHostLifecycle(t *testing.T) {
	source, executable := backendIntegrationPayload(t)
	fixture := t.TempDir()
	cfg := desktop.ClientConfig{
		RuntimeDirectory:    filepath.Join(fixture, "runtime"),
		ServiceLogDirectory: filepath.Join(fixture, "service-logs"),
		StateDirectory:      filepath.Join(fixture, "Settings"),
		LogDirectory:        filepath.Join(fixture, "Logs — current"),
		IntervalMs:          1000,
		StartPaused:         true,
		Port:                18765,
	}
	supervisor, err := desktop.NewSupervisor(source, executable, cfg)
	if err != nil {
		t.Fatal(err)
	}

	defer supervisor.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := supervisor.Start(ctx); err != nil {
		log, _ := os.ReadFile(filepath.Join(cfg.ServiceLogDirectory, "client-backend.log"))
		t.Fatalf("start: %v\n%s", err, log)
	}

	response, err := http.Get(supervisor.URL())
	if err != nil {
		t.Fatal(err)
	}

	page, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(page), "Connection Stability") {
		t.Fatalf("dashboard: HTTP %d", response.StatusCode)
	}

	if !supervisor.SnapshotConfig().StartPaused {
		t.Fatal("startup pause was not retained")
	}

	secondStore := filepath.Join(fixture, "Logs — secondary")
	if err := supervisor.SetLogDirectory(secondStore); err != nil {
		t.Fatal(err)
	}

	if supervisor.CurrentDirectory() != secondStore {
		t.Fatal("directory change did not reach supervisor")
	}

	var state struct {
		Adapters []struct {
			ID        string `json:"id"`
			Available bool   `json:"available"`
			ICMPSafe  bool   `json:"icmpSafe"`
		} `json:"adapters"`
	}
	get := func(path string, target any) {
		t.Helper()
		r, e := http.Get(supervisor.URL() + path)
		if e != nil {
			t.Fatal(e)
		}

		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("GET %s: HTTP %d", path, r.StatusCode)
		}

		if e := json.NewDecoder(r.Body).Decode(target); e != nil {
			t.Fatal(e)
		}
	}
	post := func(path string, target any) {
		t.Helper()
		r, e := http.Post(supervisor.URL()+path, "application/json", nil)
		if e != nil {
			t.Fatal(e)
		}

		defer r.Body.Close()
		if r.StatusCode != 200 {
			b, _ := io.ReadAll(r.Body)
			t.Fatalf("POST %s: HTTP %d %s", path, r.StatusCode, b)
		}

		if e := json.NewDecoder(r.Body).Decode(target); e != nil {
			t.Fatal(e)
		}
	}
	get("api/status", &state)
	adapterID := ""
	for _, a := range state.Adapters {
		if a.Available && a.ICMPSafe {
			adapterID = a.ID
			break
		}
	}

	if adapterID != "" {
		ids := []string{}
		for i := 0; i < 2; i++ {
			var started struct {
				RunID string `json:"runId"`
			}
			post("api/diagnostics/start?adapterId="+url.QueryEscape(adapterID), &started)
			if started.RunID == "" {
				t.Fatal("missing runId")
			}

			ids = append(ids, started.RunID)
		}

		if ids[0] == ids[1] {
			t.Fatal("diagnostics did not allocate independent runs")
		}

		if err := supervisor.SetLogDirectory(filepath.Join(fixture, "Should not switch")); err == nil {
			t.Fatal("storage switch during diagnosis accepted")
		}

		var stopped map[string]any
		post("api/diagnostics/stop?runId="+url.QueryEscape(ids[0]), &stopped)
		var diagnostics struct {
			Runs []struct {
				ID      string `json:"id"`
				Running bool   `json:"running"`
			} `json:"runs"`
		}
		get("api/diagnostics", &diagnostics)
		secondRunning := false
		for _, run := range diagnostics.Runs {
			if run.ID == ids[1] && run.Running {
				secondRunning = true
			}
		}

		if !secondRunning {
			t.Fatal("stopping one run stopped another")
		}
	}

	endpoint := supervisor.URL()
	if err := supervisor.Shutdown(); err != nil {
		t.Fatal(err)
	}

	if _, err := http.Get(endpoint + "api/status"); err == nil {
		t.Fatal("backend still answers after shutdown")
	}

	if _, err := os.Stat(filepath.Join(secondStore, "log-store.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(source, "log-settings.json")); !os.IsNotExist(err) {
		t.Fatal("immutable assets were used for settings")
	}
}

// Owner/cancellation validation runs before any elevation check, game lookup or
// ETW command. These fixtures never open a trace session or a UAC dialog.
func TestCaptureOwnerCancellationBeforeETW(t *testing.T) {
	ticks, err := process.CurrentStartTicks()
	if err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []string{"dead-owner", "reused-owner-pid", "cancel-file"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			runID := "00000000000000000000000000000001"
			ownerPID, ownerTicks := os.Getpid(), ticks
			if scenario == "dead-owner" {
				ownerPID = 2147483647
			}

			if scenario == "reused-owner-pid" {
				ownerTicks++
			}

			request := map[string]any{
				"appId":           "codex-cod-relay-request",
				"game":            "cod",
				"runId":           runID,
				"gamePid":         os.Getpid(),
				"gameStartTicks":  ticks,
				"ownerPid":        ownerPID,
				"ownerStartTicks": ownerTicks,
			}
			data, _ := json.Marshal(request)
			if err := os.WriteFile(filepath.Join(directory, "request.json"), data, 0600); err != nil {
				t.Fatal(err)
			}

			if scenario == "cancel-file" {
				if err := os.WriteFile(filepath.Join(directory, "cancel.json"), []byte(`{"appId":"codex-cod-relay-cancel"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}

			_ = collectors.RunCaptureWorker([]string{
				"--directory",
				directory,
				"--run-id",
				runID,
				"--game",
				"cod",
				"--game-pid",
				strconv.Itoa(os.Getpid()),
				"--game-start-ticks",
				strconv.FormatUint(ticks, 10),
				"--duration-seconds",
				"20",
			})
			data, err = os.ReadFile(filepath.Join(directory, "capture.json"))
			if err != nil {
				t.Fatal(err)
			}

			var result struct {
				State          string `json:"state"`
				Session        string `json:"session"`
				CaptureStartMs *int64 `json:"captureStartMs"`
			}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}

			if result.State != "cancelled" || (result.CaptureStartMs != nil && *result.CaptureStartMs != 0) {
				t.Fatalf("capture was not cancelled before ETW: %s", data)
			}
		})
	}
}
