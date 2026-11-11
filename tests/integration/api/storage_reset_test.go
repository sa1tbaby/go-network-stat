package api_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func TestStorageRejectsForeignFilesAndSymlinks(t *testing.T) {
	m, _ := testManager(t, false)
	old := m.App.Manager().LogDirectory()
	for _, file := range []string{"probes.csv", "adapters.csv", "status.json", "events.jsonl"} {
		dir := t.TempDir()
		writeFixture(t, filepath.Join(dir, file), "foreign contents\n")
		apiRequest(t, m, "POST", "/api/logs/select?path="+url.QueryEscape(dir), 400)
		if m.App.Manager().LogDirectory() != old {
			t.Fatal("invalid switch changed active store")
		}

		b, _ := os.ReadFile(filepath.Join(dir, file))
		if string(b) != "foreign contents\n" {
			t.Fatal("foreign file modified")
		}
	}

	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "linked")); err != nil {
		t.Skip("symlink creation unavailable", err)
	}

	apiRequest(t, m, "POST", "/api/logs/select?path="+url.QueryEscape(filepath.Join(base, "linked", "logs")), 400)
	if _, err := os.Stat(filepath.Join(outside, "logs")); !os.IsNotExist(err) {
		t.Fatal("switch traversed symlink", err)
	}
}

func TestResetPreservesForeignFilesInsideOwnedDiagnosisRun(t *testing.T) {
	m, _ := testManager(t, false)
	dir := m.App.Manager().LogDirectory()
	run := filepath.Join(dir, "diagnosis-runs", "20261107-120000-abcd1234")
	writeFixture(t, filepath.Join(run, "run.json"), `{"appId":"codex-network-diagnosis-run","id":"20261107-120000-abcd1234"}`)
	foreign := []string{
		"runtime.json",
		"diagnostics-live.json",
		"radio-live.json",
		"ethernet-live.json",
		"diagnostics-stop.flag",
		"diagnostics/notes/probes.csv",
		"diagnostics/notes/summary.json",
		"radio/radio.csv",
		"radio/summary.json",
	}
	for _, name := range foreign {
		writeFixture(t, filepath.Join(run, filepath.FromSlash(name)), "foreign user data")
	}

	owned := filepath.Join(run, "diagnostics", "owned")
	writeFixture(t, filepath.Join(owned, "summary.json"), `{"appId":"codex-single-pc-diagnostics"}`)
	writeFixture(t, filepath.Join(owned, "probes.csv"), "own probe log")
	writeFixture(t, filepath.Join(owned, "keep.txt"), "foreign note")
	apiRequest(t, m, "POST", "/api/logs/reset?directory="+url.QueryEscape(dir), 200)
	for _, name := range foreign {
		data, err := os.ReadFile(filepath.Join(run, filepath.FromSlash(name)))
		if err != nil || string(data) != "foreign user data" {
			t.Errorf("foreign same-name file removed: %s (%v)", name, err)
		}
	}

	for _, path := range []string{filepath.Join(run, "run.json"), filepath.Join(owned, "summary.json"), filepath.Join(owned, "probes.csv")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("owned file remains: %s (%v)", path, err)
		}
	}

	if _, err := os.Stat(filepath.Join(owned, "keep.txt")); err != nil {
		t.Fatal("unrecognized note removed", err)
	}
}

func TestResetRejectsSymlinkInKnownDiagnosisDataBeforeDeletingFiles(t *testing.T) {
	m, _ := testManager(t, false)
	dir := m.App.Manager().LogDirectory()
	run := filepath.Join(dir, "diagnosis-runs", "20261107-120000-abcd1234")
	manifest := filepath.Join(run, "run.json")
	writeFixture(t, manifest, `{"appId":"codex-network-diagnosis-run","id":"20261107-120000-abcd1234"}`)
	outside := t.TempDir()
	writeFixture(t, filepath.Join(outside, "summary.json"), `{"appId":"codex-single-pc-diagnostics"}`)
	writeFixture(t, filepath.Join(outside, "probes.csv"), "external data")
	if err := os.Symlink(outside, filepath.Join(run, "diagnostics")); err != nil {
		t.Skip("symlink creation unavailable", err)
	}

	apiRequest(t, m, "POST", "/api/logs/reset?directory="+url.QueryEscape(dir), 400)
	if _, err := os.Stat(manifest); err != nil {
		t.Fatal("reset partially deleted files before ownership preflight completed", err)
	}

	if data, err := os.ReadFile(filepath.Join(outside, "probes.csv")); err != nil || string(data) != "external data" {
		t.Fatal("external symlink target modified", err)
	}
}

func TestStorageCommandsRejectActiveDiagnosticsAndRoutes(t *testing.T) {
	b, _ := testManager(t, true)
	dir := b.App.Manager().LogDirectory()
	start := apiRequest(t, b, "POST", "/api/diagnostics/start?adapterId=Ethernet", 200)
	apiRequest(t, b, "POST", "/api/logs/reset?directory="+url.QueryEscape(dir), 400)
	apiRequest(t, b, "POST", "/api/diagnostics/stop?runId="+url.QueryEscape(start["runId"].(string)), 200)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if apiRequest(t, b, "GET", "/api/diagnostics", 200)["running"] == false {
			break
		}

		time.Sleep(time.Millisecond)
	}

	release := make(chan struct{})
	b.Fake.Probe = func(collectors.ProbeRequest) collectors.ProbeResult {
		<-release

		return collectors.ProbeResult{ToolError: true, Status: "FixtureToolError"}
	}
	apiRequest(t, b, "POST", "/api/route/start?adapterId=Ethernet&address=1.1.1.1", 200)
	apiRequest(t, b, "POST", "/api/logs/reset?directory="+url.QueryEscape(dir), 400)
	close(release)
}
