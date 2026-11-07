package diagnostics_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/diagnostics"
)

type host struct {
	root    string
	adapter collectors.Adapter
	mu      sync.Mutex
	events  []string
}

func (h *host) Adapter(id string) (collectors.Adapter, bool) {
	return h.adapter, id == h.adapter.ID
}

func (h *host) LogDirectory() string {
	return h.root
}

func (h *host) StateDirectory() string {
	return h.root
}

func (h *host) Event(kind, _ string) {
	h.mu.Lock()
	h.events = append(h.events, kind)
	h.mu.Unlock()
}

func (h *host) AddDetectedTarget(string, contracts.DetectedTarget, string) error {
	return nil
}

type collector struct {
	adapter      collectors.Adapter
	adapterError error
}

func (c *collector) Adapters(context.Context) ([]collectors.Adapter, error) {
	return []collectors.Adapter{c.adapter}, c.adapterError
}

func (c *collector) ICMP(_ context.Context, r collectors.ProbeRequest) collectors.ProbeResult {
	ms := 3.0

	return collectors.ProbeResult{
		OK:       true,
		Received: true,
		RTTMs:    &ms,
		Status:   "Success",
		Bytes:    r.PayloadBytes,
	}
}

func (c *collector) TCP(ctx context.Context, r collectors.ProbeRequest, _ int) collectors.ProbeResult {
	return c.ICMP(ctx, r)
}

func (c *collector) DNS(context.Context, collectors.ProbeRequest) collectors.ProbeResult {
	ms := 4.0

	return collectors.ProbeResult{
		OK:     true,
		RTTMs:  &ms,
		Status: "Success_DNS_RCODE_0",
		Bytes:  29,
	}
}

func (c *collector) Radio(context.Context, collectors.Adapter) (collectors.RadioSample, error) {
	return collectors.RadioSample{StatisticsAvailable: true, Phys: []collectors.PHYStats{{TxFrames: 100, RetryFrames: 10}}}, nil
}

type streamCollector struct {
	collector
}

func (c *streamCollector) StreamDNS(_ context.Context, r collectors.ProbeRequest, _ int, emit func(collectors.DNSObservation)) error {
	now := time.Now()
	ms := 3.0
	if r.OnPending != nil {
		r.OnPending(1)
	}

	emit(collectors.DNSObservation{
		Sent: now,
		At:   now.Add(3 * time.Millisecond),
		Result: collectors.ProbeResult{
			OK:     true,
			RTTMs:  &ms,
			Status: "Success_DNS_RCODE_0",
		},
	})
	emit(collectors.DNSObservation{
		Sent:   now,
		At:     now.Add(1200 * time.Millisecond),
		Result: collectors.ProbeResult{Status: "TimedOut"},
	})
	emit(collectors.DNSObservation{
		At:     now.Add(1500 * time.Millisecond),
		Late:   true,
		Result: collectors.ProbeResult{Status: "LateReplyAfterTimeout"},
	})
	emit(collectors.DNSObservation{
		Sent:   now,
		At:     now,
		Result: collectors.ProbeResult{Status: "ToolError:fixture", ToolError: true},
	})
	if r.OnPending != nil {
		r.OnPending(0)
	}

	return nil
}

func fixture(t *testing.T, duration time.Duration) (*diagnostics.Service, *host) {
	t.Helper()
	a := collectors.Adapter{
		ID:                "Ethernet",
		Name:              "Ethernet",
		Kind:              "ethernet",
		IPv4:              "192.168.1.2",
		Gateway:           "192.168.1.1",
		Index:             4,
		Available:         true,
		ICMPSafe:          true,
		State:             "Up",
		SpeedMbps:         1000,
		CountersAvailable: true,
	}
	h := &host{root: t.TempDir(), adapter: a}
	s := diagnostics.New(context.Background(), h, &collector{adapter: a}, diagnostics.Options{Duration: duration})
	t.Cleanup(s.Close)

	return s, h
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	return result
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var value map[string]any
	if err = json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}

	return value
}

func writeJSON(t *testing.T, path string, value any, bom bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	if bom {
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	}

	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func start(t *testing.T, s *diagnostics.Service, id string) string {
	t.Helper()
	result, err := s.Start(id)
	if err != nil {
		t.Fatal(err)
	}

	return object(t, result)["runId"].(string)
}

func waitRun(t *testing.T, s *diagnostics.Service, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := object(t, s.Status())
		for _, value := range status["runs"].([]any) {
			run := value.(map[string]any)
			if run["id"] == id && run["running"] == false {
				return run
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("diagnostic run did not finish")

	return nil
}
