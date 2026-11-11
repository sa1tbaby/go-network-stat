package networkmanager_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/targets"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func TestCatalogAdditionPreservesObservedUDPAndExistingTCPSettings(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}

	registry, err := targets.Open(config)
	if err != nil {
		t.Fatal(err)
	}

	address := "84.17.57.129"
	if err = registry.Put(targets.Target{
		Address: address,
		Host:    "original.fixture",
		Port:    443,
		AddedAt: time.Now().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}

	m := newManager(t, networkmanager.Config{
		StateDirectory: config,
		LogDirectory:   filepath.Join(base, "statistics"),
		Paused:         true,
	}, &testsupport.FakeCollector{Interfaces: []collectors.Adapter{testsupport.Adapter()}})
	if _, err = m.AddControlTarget(address); err != nil {
		t.Fatal(err)
	}

	if err = m.AddDetectedTarget("cod", contracts.DetectedTarget{
		Address:   address,
		SourceIP:  "192.0.2.10",
		UDPPort:   3074,
		LocalPort: 32000,
		InEvents:  123,
		OutEvents: 456,
	}, "capture.json"); err != nil {
		t.Fatal(err)
	}

	if _, err = m.AddControlTarget(address); err != nil {
		t.Fatal(err)
	}

	value := testsupport.JSON(t, m.Snapshot())["customTargets"].([]any)[0].(map[string]any)
	if value["origin"] != "cod_etw" || value["host"] != "original.fixture" || value["port"] != float64(443) || value["udpPort"] != float64(3074) || value["inEvents"] != float64(123) {
		t.Fatal("catalog erased configured target/UDP evidence", value)
	}
}
