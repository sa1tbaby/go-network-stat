package api_test

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAPICommandsOriginAndValidation(t *testing.T) {
	m, _ := testManager(t, true)
	commands := []string{
		"/api/start",
		"/api/stop",
		"/api/mark",
		"/api/interval?ms=250",
		"/api/interval?ms=1000",
	}
	for _, path := range commands {
		if apiRequest(t, m, "POST", path, 200)["ok"] != true {
			t.Fatal(path)
		}
	}

	invalidCommands := []string{
		"/api/target?host=http://bad",
		"/api/target?host=203.0.113.10&port=65536",
		"/api/target?host=1.1.1.1",
		"/api/target/remove?address=missing",
		"/api/route/start?adapterId=missing&address=1.1.1.1",
		"/api/diagnostics/start?adapterId=missing",
		"/api/diagnostics/stop?runId=../../bad",
		"/api/diagnostics/delete?runId=../../bad",
		"/api/logs/reset?directory=" + url.QueryEscape(t.TempDir()),
		"/api/logs/select?path=relative",
		"/api/logs/pick",
	}
	for _, path := range invalidCommands {
		body := apiRequest(t, m, "POST", path, 400)
		if msg, ok := body["error"].(string); !ok || msg == "" {
			t.Fatalf("missing JSON command error at %s", path)
		}
	}

	for _, origin := range []string{"https://attacker.example", "http://127.0.0.1:8766"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/mark", nil)
		req.Header.Set("Origin", origin)
		m.Handler().ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("foreign Origin allowed: %s", origin)
		}
	}

	for _, origin := range []string{"http://127.0.0.1:8765", "http://localhost:8765"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/mark", nil)
		req.Header.Set("Origin", origin)
		m.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatal(origin, rec.Code)
		}
	}

	unknownRoutes := []struct {
		method string
		path   string
	}{
		{method: "GET", path: "/api/start"},
		{method: "POST", path: "/api/status"},
		{method: "GET", path: "/unknown"},
		{method: "POST", path: "/api/interval?ms=500"},
	}
	for _, item := range unknownRoutes {
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, httptest.NewRequest(item.method, item.path, nil))
		if rec.Code != 404 {
			t.Errorf("unknown route %s %s status=%d", item.method, item.path, rec.Code)
		}
	}
}
