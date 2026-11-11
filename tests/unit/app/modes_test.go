package app_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/app"
)

func TestModeDispatchLeavesDesktopFlagsToCompositionRoot(t *testing.T) {
	for _, args := range [][]string{nil, {"--headless"}, {"--data-dir", "fixture"}, {"--unknown"}} {
		handled, err := app.HandleMode(args, nil)
		if handled || err != nil {
			t.Fatalf("desktop args handled as worker: %v, %v %v", args, handled, err)
		}
	}
}

func TestWorkerValidationRunsBeforeUIOrPrivilegedWork(t *testing.T) {
	cases := [][]string{
		{"--backend"},
		{"--backend", "--root", "relative"},
		{"--backend", "--not-a-flag"},
		{"--capture-worker"},
		{"--udp-test"},
		{"--udp-responder", "-port", "1"},
	}
	for _, args := range cases {
		called := false
		handled, err := app.HandleMode(args, func(context.Context, string) (string, error) {
			called = true

			return "", nil
		})
		if !handled || err == nil || called {
			t.Fatalf("mode validation escaped backend: %v handled=%v error=%v picker=%v", args, handled, err, called)
		}
	}
}

func TestBackendModeRequiresProcessIdentityAfterAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	args := []string{"--backend"}
	for _, name := range []string{"root", "state-directory", "runtime-directory", "service-log-directory", "storage-directory", "ready-file"} {
		args = append(args, "--"+name, filepath.Join(dir, name))
	}

	handled, err := app.HandleMode(args, nil)
	if !handled || err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("backend did not validate owner identity: handled=%v err=%v", handled, err)
	}
}
