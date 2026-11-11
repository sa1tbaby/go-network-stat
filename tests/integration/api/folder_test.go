package api_test

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/app"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func TestNativeFolderSelectionIsAsynchronous(t *testing.T) {
	base := t.TempDir()
	selected := t.TempDir()
	release := make(chan struct{})
	cfg := app.Config{
		Root:             filepath.Join(testsupport.Root(), "frontend"),
		StateDirectory:   filepath.Join(base, "config"),
		RuntimeDirectory: filepath.Join(base, "runtime"),
		LogDirectory:     filepath.Join(base, "statistics"),
		Paused:           true,
		PickFolder: func(ctx context.Context, initial string) (string, error) {
			select {
			case <-release:
				return selected, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	application, err := app.New(context.Background(), cfg, app.Dependencies{Collector: &testsupport.FakeCollector{}, Relay: &testsupport.Relay{}})
	if err != nil {
		t.Fatal(err)
	}

	defer application.Close()
	request := func(method, path string, status int) map[string]any {
		return testsupport.Request(t, application.Handler(), method, path, status)
	}
	request("POST", "/api/logs/pick", 200)
	if request("GET", "/api/logs", 200)["choosing"] != true {
		t.Fatal("picker must expose asynchronous state")
	}

	request("POST", "/api/logs/reset?directory="+url.QueryEscape(application.Manager().LogDirectory()), 400)
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state := request("GET", "/api/logs", 200)
		if state["choosing"] == false {
			if state["directory"] != selected || state["selection"].(map[string]any)["state"] != "complete" {
				t.Fatal(state)
			}

			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("picker did not finish")
}
