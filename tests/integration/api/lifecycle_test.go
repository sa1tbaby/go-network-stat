package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/app"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func TestRunLoopbackPortFallbackReadyAndOwnerLifecycle(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("loopback sockets unavailable in this sandbox: %v", err)
		}

		t.Fatal(err)
	}

	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	base := t.TempDir()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	ready := filepath.Join(base, "state", "ready.json")
	var ownerAlive atomic.Bool
	ownerAlive.Store(true)
	var ownerChecked atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx, app.Config{
			Root:             filepath.Join(root, "frontend"),
			StateDirectory:   filepath.Join(base, "state"),
			RuntimeDirectory: filepath.Join(base, "runtime"),
			LogDirectory:     filepath.Join(base, "logs"),
			ReadyFile:        ready,
			Port:             port,
			Paused:           true,
			OwnerPID:         321,
			OwnerStartTicks:  123456789,
			OwnerAlive: func(pid int, ticks int64) bool {
				if pid == 321 && ticks == 123456789 {
					ownerChecked.Store(true)
				}

				return ownerAlive.Load()
			},
		})
	}()
	var state map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, readErr := os.ReadFile(ready); readErr == nil && json.Unmarshal(data, &state) == nil {
			break
		}

		select {
		case err := <-done:
			t.Fatal("server exited before ready", err)
		default:
		}

		time.Sleep(5 * time.Millisecond)
	}

	if state == nil {
		t.Fatal("ready state was not created")
	}

	endpoint, ok := state["url"].(string)
	if !ok {
		t.Fatal(state)
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}

	chosen, err := strconv.Atoi(parsed.Port())
	if err != nil || parsed.Hostname() != "127.0.0.1" || chosen <= port || chosen >= port+20 {
		t.Fatal("listener did not use loopback fallback range", endpoint)
	}

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(endpoint + "api/status")
	if err != nil {
		t.Fatal(err)
	}

	var status map[string]any
	err = json.NewDecoder(response.Body).Decode(&status)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || status["serverRunning"] != true || status["running"] != false {
		t.Fatal("ready endpoint unavailable or paused state lost", status, err)
	}

	if state["appId"] != storage.AppID || state["logDir"] != filepath.Join(base, "logs") {
		t.Fatal("run-state contract changed", state)
	}

	ownerAlive.Store(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("backend outlived owning process")
	}

	if !ownerChecked.Load() {
		t.Fatal("process identity was not checked with PID and start ticks")
	}

	data, err := os.ReadFile(filepath.Join(base, "logs", "status.json"))
	if err != nil {
		t.Fatal(err)
	}

	if json.Unmarshal(data, &status) != nil || status["serverRunning"] != false {
		t.Fatal("shutdown snapshot missing", string(data))
	}
}
