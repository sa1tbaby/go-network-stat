// Package testsupport supplies black-box fixtures through production service ports.
package testsupport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/app"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/routes"
)

type FakeCollector struct {
	mu         sync.Mutex
	Interfaces []collectors.Adapter
	Requests   []collectors.ProbeRequest
	TCPPorts   []int
	Probe      func(collectors.ProbeRequest) collectors.ProbeResult
}

func (f *FakeCollector) Adapters(context.Context) ([]collectors.Adapter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]collectors.Adapter(nil), f.Interfaces...), nil
}

func (f *FakeCollector) SetAdapters(a []collectors.Adapter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Interfaces = append([]collectors.Adapter(nil), a...)
}

func (f *FakeCollector) Observed() ([]collectors.ProbeRequest, []int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]collectors.ProbeRequest(nil), f.Requests...), append([]int(nil), f.TCPPorts...)
}

func (f *FakeCollector) result(r collectors.ProbeRequest) collectors.ProbeResult {
	if f.Probe != nil {
		return f.Probe(r)
	}

	return collectors.ProbeResult{ToolError: true, Status: "FixtureToolError"}
}

func (f *FakeCollector) ICMP(_ context.Context, r collectors.ProbeRequest) collectors.ProbeResult {
	f.mu.Lock()
	f.Requests = append(f.Requests, r)
	f.mu.Unlock()

	return f.result(r)
}

func (f *FakeCollector) TCP(_ context.Context, r collectors.ProbeRequest, p int) collectors.ProbeResult {
	f.mu.Lock()
	f.Requests = append(f.Requests, r)
	f.TCPPorts = append(f.TCPPorts, p)
	f.mu.Unlock()

	return f.result(r)
}

func (f *FakeCollector) DNS(_ context.Context, r collectors.ProbeRequest) collectors.ProbeResult {
	return f.result(r)
}

func (f *FakeCollector) Radio(context.Context, collectors.Adapter) (collectors.RadioSample, error) {
	return collectors.RadioSample{}, nil
}

func Adapter() collectors.Adapter {
	return collectors.Adapter{
		ID:                "Ethernet",
		Name:              "Ethernet",
		Kind:              "ethernet",
		State:             "Up",
		IPv4:              "192.0.2.10",
		Gateway:           "192.0.2.1",
		Index:             7,
		ICMPSafe:          true,
		Available:         true,
		SpeedMbps:         1000,
		CountersAvailable: true,
	}
}

func Root() string {
	_, file, _, _ := runtime.Caller(0)

	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

type Backend struct {
	App    *app.App
	Fake   *FakeCollector
	Config app.Config
}

func NewBackend(t *testing.T, adapter bool) *Backend {
	return NewBackendWithConfig(t, adapter, nil)
}

func NewBackendWithConfig(t *testing.T, adapter bool, configure func(*app.Config)) *Backend {
	t.Helper()
	fake := &FakeCollector{}
	if adapter {
		fake.Interfaces = []collectors.Adapter{Adapter()}
	}

	base := t.TempDir()
	cfg := app.Config{
		Root:                filepath.Join(Root(), "frontend"),
		StateDirectory:      filepath.Join(base, "config"),
		RuntimeDirectory:    filepath.Join(base, "runtime"),
		LogDirectory:        filepath.Join(base, "statistics"),
		ServiceLogDirectory: filepath.Join(base, "logs"),
		Port:                8765,
		Paused:              true,
		RouteOptions: routes.Options{
			SampleGoal:       1,
			MaxHops:          2,
			SamplingInterval: time.Millisecond,
		},
	}
	if configure != nil {
		configure(&cfg)
	}

	a, err := app.New(context.Background(), cfg, app.Dependencies{Collector: fake, Relay: &Relay{}})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})

	return &Backend{
		App:    a,
		Fake:   fake,
		Config: cfg,
	}
}

func (b *Backend) Handler() http.Handler {
	return b.App.Handler()
}

func Request(t *testing.T, handler http.Handler, method, path string, status int) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	if rec.Code != status {
		t.Fatalf("%s %s status=%d want=%d: %s", method, path, rec.Code, status, rec.Body.String())
	}

	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("response must disable caching")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err, rec.Body.String())
	}

	return body
}

func JSON(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func Write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

type Relay struct {
	mu      sync.Mutex
	started bool
}

func (r *Relay) Status(game string) any {
	r.mu.Lock()
	defer r.mu.Unlock()

	return map[string]any{
		"game":            game,
		"state":           "idle",
		"running":         false,
		"message":         "fixture",
		"durationSeconds": 20,
	}
}

func (r *Relay) Start(string) (any, error) {
	return map[string]any{"ok": true, "runId": "fixture-relay"}, nil
}

func (r *Relay) Active() bool {
	return false
}

func (r *Relay) Close() {}
