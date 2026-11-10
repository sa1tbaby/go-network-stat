package client_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/api/client"
)

func localServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("loopback sockets unavailable: %v", err)
		}

		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	return server
}

func TestVerifiedClientStatusDirectoryPauseAndShutdown(t *testing.T) {
	var mu sync.Mutex
	calls := []string{}
	directory := `C:\Logs — folder with spaces`
	server := localServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status":
			json.NewEncoder(w).Encode(map[string]any{
				"appId":         "codex-internet-stability",
				"serverRunning": true,
				"running":       true,
				"logDir":        directory,
				"intervalMs":    250,
			})
		case "/api/logs/select":
			if r.URL.Query().Get("path") != directory {
				t.Errorf("folder query corrupted: %s", r.URL.RawQuery)
			}

			json.NewEncoder(w).Encode(map[string]any{"ok": true, "directory": directory})
		case "/api/stop", "/api/shutdown":
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	api := client.New()
	state, err := api.Status(server.URL)
	if err != nil || state.LogDirectory != directory || state.IntervalMs != 250 || !state.Running {
		t.Fatal(state, err)
	}

	if err = api.SelectLogDirectory(server.URL, directory); err != nil {
		t.Fatal(err)
	}

	if err = api.Pause(server.URL); err != nil {
		t.Fatal(err)
	}

	if err = api.Shutdown(server.URL); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if strings.Join(calls, "|") != "GET /api/status|POST /api/logs/select|GET /api/status|POST /api/stop|POST /api/shutdown" {
		t.Fatal("unexpected API transport flow", calls)
	}
}

func TestPauseDiscoverySkipsUntrustedOrStaleState(t *testing.T) {
	var mu sync.Mutex
	stopped := 0
	server := localServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/status" {
			json.NewEncoder(w).Encode(map[string]any{
				"appId":         "codex-internet-stability",
				"serverRunning": true,
				"logDir":        "fixture",
			})

			return
		}

		if r.URL.Path == "/api/stop" && r.Method == "POST" {
			mu.Lock()
			stopped++
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})

			return
		}

		http.NotFound(w, r)
	}))
	dir := t.TempDir()
	write := func(name string, state map[string]any) string {
		path := filepath.Join(dir, name)
		data, _ := json.Marshal(state)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}

		return path
	}
	foreign := write("foreign.json", map[string]any{"appId": "unrelated", "url": server.URL})
	remote := write("remote.json", map[string]any{"appId": "codex-internet-stability", "url": "http://example.com:8765/"})
	valid := write("valid.json", map[string]any{"appId": "codex-internet-stability", "url": server.URL})
	api := client.New()
	if err := api.PauseFromStateFiles([]string{filepath.Join(dir, "missing.json"), foreign, remote, valid}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if stopped != 1 {
		t.Fatal("pause sent to unverified endpoint", stopped)
	}
}

func TestClientRejectsWrongApplicationAndPreservesServerErrors(t *testing.T) {
	server := localServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			json.NewEncoder(w).Encode(map[string]any{
				"appId":         "wrong-application",
				"serverRunning": true,
				"logDir":        "fixture",
			})

			return
		}

		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"error": "Wait for the check to finish"})
	}))
	api := client.New()
	if _, err := api.Status(server.URL); err == nil {
		t.Fatal("unrelated server accepted")
	}

	if err := api.SelectLogDirectory(server.URL, "fixture"); err == nil || !strings.Contains(err.Error(), "Wait for the check to finish") {
		t.Fatal("domain error lost", err)
	}

	if _, err := api.Status("http://example.com:8765/"); err == nil {
		t.Fatal("external endpoint accepted")
	}
}
