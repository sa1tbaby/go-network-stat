package contract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sa1tbaby/go-network-stat/frontend"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func testManager(t *testing.T, adapter bool) (*testsupport.Backend, *testsupport.FakeCollector) {
	b := testsupport.NewBackend(t, adapter)

	return b, b.Fake
}

func apiRequest(t *testing.T, b *testsupport.Backend, method, path string, status int) map[string]any {
	return testsupport.Request(t, b.Handler(), method, path, status)
}

func TestFrozenPanelContractAndAssets(t *testing.T) {
	m, _ := testManager(t, true)
	contract := testsupport.LoadContract(t)
	shapes := contract.Objects
	responses := []struct {
		path  string
		shape string
	}{
		{path: "/api/status", shape: "status"},
		{path: "/api/logs", shape: "logs"},
		{path: "/api/diagnostics", shape: "diagnostics"},
		{path: "/api/cod/relay/status", shape: "relay"},
		{path: "/api/cs2/relay/status", shape: "relay"},
		{path: "/api/route/status?adapterId=Ethernet&address=1.1.1.1", shape: "route"},
	}
	for _, item := range responses {
		body := apiRequest(t, m, http.MethodGet, item.path, 200)
		testsupport.AssertShape(t, shapes, item.shape, body)
		switch item.shape {
		case "status":
			for _, a := range body["adapters"].([]any) {
				testsupport.AssertShape(t, shapes, "adapter", a.(map[string]any))
			}

			for _, target := range body["targets"].([]any) {
				target := target.(map[string]any)
				testsupport.AssertShape(t, shapes, "target", target)
				for _, window := range []string{"all", "last60", "last300"} {
					metrics := target[window].(map[string]any)
					testsupport.AssertShape(t, shapes, "metrics", metrics)
					if metrics["lossPct"] != nil || metrics["avgMs"] != nil || metrics["p95Ms"] != nil || metrics["maxMs"] != nil || metrics["jitterMs"] != nil {
						t.Error("empty statistics must use null values")
					}
				}
			}
		case "logs":
			testsupport.AssertShape(t, shapes, "selection", body["selection"].(map[string]any))
		}
	}

	assets := []struct {
		path string
		file string
	}{
		{path: "/", file: "dashboard.html"},
		{path: "/dashboard.html", file: "dashboard.html"},
		{path: "/api/official-relays", file: "official-relays.json"},
		{path: "/api/cod/test-targets", file: "cod-test-targets.json"},
	}
	for _, item := range assets {
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", item.path, nil))
		data, err := os.ReadFile(filepath.Join(m.Config.Root, item.file))
		if err != nil {
			t.Fatal(err)
		}

		if rec.Code != 200 || rec.Body.String() != string(data) {
			t.Fatalf("served asset %s differs from source", item.file)
		}
	}

	apiRequest(t, m, "POST", "/api/target?host=203.0.113.10&port=443", 200)
	body := apiRequest(t, m, "GET", "/api/status", 200)
	custom := body["customTargets"].([]any)
	if len(custom) != 1 {
		t.Fatal(custom)
	}

	testsupport.AssertShape(t, shapes, "customTarget", custom[0].(map[string]any))
	for _, target := range body["targets"].([]any) {
		target := target.(map[string]any)
		if target["address"] == "203.0.113.10" {
			testsupport.AssertShape(t, shapes, "customTarget", target["details"].(map[string]any))
		}
	}
}

func TestEnglishPanelBytesAndFrozenSchemaCatalog(t *testing.T) {
	c := testsupport.LoadContract(t)
	if len(c.Routes) != 27 || len(c.Objects) != 34 {
		t.Fatalf("frozen catalog changed: %d routes, %d shapes", len(c.Routes), len(c.Objects))
	}

	data, err := frontend.Files.ReadFile("dashboard.html")
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != c.DashboardSHA {
		t.Fatal("English panel checksum differs from the asset contract")
	}

	if c.DashboardLanguage != "en" || !strings.Contains(string(data), `lang="en"`) || !strings.Contains(string(data), "Connection Stability") {
		t.Fatal("panel does not declare the English interface")
	}

	if c.LegacyDashboardSHA != "3f44c91ffef1805c7f6587a21b586c50310b0b9e46da7f00a40d0e491c123df7" {
		t.Fatal("legacy panel checksum was not retained")
	}

	disk, err := os.ReadFile(filepath.Join(testsupport.Root(), "frontend", "dashboard.html"))
	if err != nil || string(disk) != string(data) {
		t.Fatal("embedded frontend differs from source", err)
	}
}
