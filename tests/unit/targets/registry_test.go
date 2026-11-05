package targets_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/targets"
)

func TestRegistryPersistsLimitsAndReturnsCopies(t *testing.T) {
	dir := t.TempDir()
	r, err := targets.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 8; i++ {
		if err = r.Put(targets.Target{
			Address: fmt.Sprintf("203.0.113.%d", i+1),
			Host:    "host.fixture",
			AddedAt: time.Now().Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err = r.Put(targets.Target{Address: "203.0.113.9"}); err == nil {
		t.Fatal("target limit ignored")
	}

	values := r.All()
	values[0].Host = "modified"
	fresh, _ := r.Get(values[0].Address)
	if fresh.Host != "host.fixture" {
		t.Fatal("registry leaked mutable state")
	}

	loaded, err := targets.Open(dir)
	if err != nil || len(loaded.All()) != 8 {
		t.Fatal("persisted targets not restored", err)
	}

	if err = r.Delete(values[0].Address); err != nil {
		t.Fatal(err)
	}

	if _, exists := r.Get(values[0].Address); exists {
		t.Fatal("deleted target remains")
	}
}
