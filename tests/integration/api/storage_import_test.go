package api_test

import (
	"encoding/csv"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/routes"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func TestLegacyStorageImportSwitchAndSafeReset(t *testing.T) {
	m, _ := testManager(t, false)
	dir := t.TempDir()
	now := time.Now().UnixMilli()
	writeFixture(t, filepath.Join(dir, "probes.csv"), "\ufeff"+storage.ProbeHeader+"\n")
	f, err := os.OpenFile(filepath.Join(dir, "probes.csv"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}

	w := csv.NewWriter(f)
	started := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	for _, row := range [][]string{
		{started, strconvTest(now - 1000), "Old Wi-Fi", "192.0.2.11", "12", "icmp", "203.0.113.77", "0", "True", "Success", "10.5", "False"},
		{started, strconvTest(now - 500), "Old Wi-Fi", "192.0.2.11", "12", "icmp", "203.0.113.77", "0", "False", "TimedOut", "", "False"},
		{"damaged"},
	} {
		if err = w.Write(row); err != nil {
			t.Fatal(err)
		}
	}

	w.Flush()
	f.Close()
	writeFixture(t, filepath.Join(dir, "events.jsonl"), "{\"time\":\""+started+"\",\"kind\":\"user_mark\",\"message\":\"old session\"}\n{broken}\n")
	unrelated := filepath.Join(dir, "keep.txt")
	writeFixture(t, unrelated, "user file")
	routeFile := filepath.Join(dir, "routes", "old.json")
	if err = storage.PublishJSON(routeFile, &routes.Job{
		AppID:     "codex-internet-stability-route",
		AdapterID: "Old Wi-Fi",
		Address:   "203.0.113.77",
		LogFile:   routeFile,
		State:     "running",
		Hops:      []*routes.Hop{},
	}); err != nil {
		os.MkdirAll(filepath.Dir(routeFile), 0700)
		if err = storage.PublishJSON(routeFile, &routes.Job{
			AppID:     "codex-internet-stability-route",
			AdapterID: "Old Wi-Fi",
			Address:   "203.0.113.77",
			LogFile:   routeFile,
			State:     "running",
			Hops:      []*routes.Hop{},
		}); err != nil {
			t.Fatal(err)
		}
	}

	apiRequest(t, m, "POST", "/api/logs/select?path="+url.QueryEscape(dir), 200)
	body := apiRequest(t, m, "GET", "/api/status", 200)
	if body["started"] != started {
		t.Fatal("session start not restored", body["started"])
	}

	targets := body["targets"].([]any)
	if len(targets) != 1 {
		t.Fatal(targets)
	}

	stats := targets[0].(map[string]any)["all"].(map[string]any)
	if stats["sent"] != float64(2) || stats["lost"] != float64(1) || stats["lossPct"] != float64(50) {
		t.Fatal("legacy CSV metrics not restored", stats)
	}

	logs := apiRequest(t, m, "GET", "/api/logs", 200)
	if !strings.Contains(logs["warning"].(string), "2") {
		t.Fatal("damaged records not surfaced", logs)
	}

	route := apiRequest(t, m, "GET", "/api/route/status?adapterId=Old+Wi-Fi&address=203.0.113.77", 200)
	if route["state"] != "error" {
		t.Fatal("unfinished route not recovered", route)
	}

	apiRequest(t, m, "POST", "/api/target?host=203.0.113.88&port=", 200)
	apiRequest(t, m, "POST", "/api/logs/reset?directory="+url.QueryEscape(dir), 200)
	if got, err := os.ReadFile(unrelated); err != nil || string(got) != "user file" {
		t.Fatal("reset removed unrelated file", err)
	}

	if _, err = os.Stat(routeFile); !os.IsNotExist(err) {
		t.Fatal("owned route was not cleared", err)
	}

	if got, err := os.ReadFile(filepath.Join(dir, "probes.csv")); err != nil || string(got) != storage.ProbeHeader+"\n" {
		t.Fatal("probe reset changed legacy CSV header", err, string(got))
	}

	body = apiRequest(t, m, "GET", "/api/status", 200)
	if len(body["targets"].([]any)) != 0 || len(body["customTargets"].([]any)) != 1 {
		t.Fatal("reset must clear history but preserve custom targets", body)
	}
}
