package contract_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/app"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func TestAllFrozenRoutesDispatchToPublicDomainServices(t *testing.T) {
	c := testsupport.LoadContract(t)
	b := testsupport.NewBackendWithConfig(t, true, func(cfg *app.Config) {
		cfg.PickFolder = func(context.Context, string) (string, error) {
			return "", nil
		}
	})
	covered := map[string]bool{}
	runID := ""
	wait := func(label string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if ready() {
				return
			}

			time.Sleep(time.Millisecond)
		}

		t.Fatal("timed out: " + label)
	}
	for _, route := range c.Routes {
		key := route.Method + " " + route.Path
		if route.Path == "/api/shutdown" {
			continue
		}

		path := route.Path
		if route.Method == "GET" {
			if path == "/api/route/status" {
				path += "?adapterId=Ethernet&address=1.1.1.1"
			}

			rec := httptest.NewRecorder()
			b.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
			if rec.Code != 200 {
				t.Fatalf("%s: %d %s", key, rec.Code, rec.Body.String())
			}

			if !strings.HasPrefix(route.Response, "asset ") {
				body := testsupport.JSON(t, mustDecode(t, rec.Body.Bytes()))
				testsupport.AssertShape(t, c.Objects, route.Response, body)
			}

			covered[key] = true
			continue
		}

		switch route.Path {
		case "/api/interval":
			path += "?ms=250"
		case "/api/target":
			path += "?host=203.0.113.10&port=443"
		case "/api/target/remove":
			path += "?address=203.0.113.10"
		case "/api/cod/test-target/add":
			path += "?address=84.17.57.129"
		case "/api/route/start":
			path += "?adapterId=Ethernet&address=1.1.1.1"
		case "/api/logs/select":
			path += "?path=" + url.QueryEscape(t.TempDir())
		case "/api/logs/reset":
			path += "?directory=" + url.QueryEscape(b.App.Manager().LogDirectory())
		case "/api/diagnostics/start":
			path += "?adapterId=Ethernet"
		case "/api/diagnostics/stop", "/api/diagnostics/delete":
			path += "?runId=" + url.QueryEscape(runID)
		}

		body := apiRequest(t, b, "POST", path, 200)
		testsupport.AssertShape(t, c.Objects, route.Response, body)
		covered[key] = true
		switch route.Path {
		case "/api/route/start":
			wait("route completion", func() bool {
				return testsupport.JSON(t, b.App.Manager().RouteStatus("Ethernet", "1.1.1.1"))["state"] != "running"
			})
			status := testsupport.JSON(t, b.App.Manager().RouteStatus("Ethernet", "1.1.1.1"))
			testsupport.AssertShape(t, c.Objects, "routeJob", status)
			for _, hop := range status["hops"].([]any) {
				testsupport.AssertShape(t, c.Objects, "routeHop", hop.(map[string]any))
			}
		case "/api/logs/pick":
			wait("picker cancellation", func() bool {
				return apiRequest(t, b, "GET", "/api/logs", 200)["choosing"] == false
			})
		case "/api/diagnostics/start":
			runID = body["runId"].(string)
		case "/api/diagnostics/stop":
			wait("diagnostic stop", func() bool {
				return apiRequest(t, b, "GET", "/api/diagnostics", 200)["running"] == false
			})
		}
	}

	body := apiRequest(t, b, "POST", "/api/shutdown", 200)
	testsupport.AssertShape(t, c.Objects, "ok", body)
	covered["POST /api/shutdown"] = true
	if len(covered) != 27 {
		t.Fatalf("covered %d frozen routes; expected 27", len(covered))
	}
}

func mustDecode(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}

	return value
}
