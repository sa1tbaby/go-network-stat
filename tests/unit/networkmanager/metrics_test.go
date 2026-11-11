package networkmanager_test

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager"
	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
	"github.com/sa1tbaby/go-network-stat/tests/testsupport"
)

func TestMetricLossJitterAndPercentileSemantics(t *testing.T) {
	base := t.TempDir()
	logs := filepath.Join(base, "statistics")
	testsupport.Write(t, filepath.Join(logs, "probes.csv"), storage.ProbeHeader+"\n")
	file, err := os.OpenFile(filepath.Join(logs, "probes.csv"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}

	writer := csv.NewWriter(file)
	now := time.Now()
	rows := []struct {
		ok   bool
		tool bool
		rtt  string
	}{
		{
			ok:   true,
			tool: false,
			rtt:  "10",
		},
		{
			ok:   true,
			tool: false,
			rtt:  "30",
		},
		{
			ok:   false,
			tool: true,
			rtt:  "",
		},
		{
			ok:   false,
			tool: false,
			rtt:  "",
		},
		{
			ok:   true,
			tool: false,
			rtt:  "100.4",
		},
		{
			ok:   true,
			tool: false,
			rtt:  "200",
		},
	}
	for i, row := range rows {
		at := now.Add(time.Duration(i-10) * time.Millisecond)
		_ = writer.Write([]string{
			at.Format(time.RFC3339Nano),
			strconv.FormatInt(at.UnixMilli(), 10),
			"Old adapter",
			"192.0.2.10",
			"7",
			"icmp",
			"203.0.113.10",
			"0",
			strconv.FormatBool(row.ok),
			"fixture",
			row.rtt,
			strconv.FormatBool(row.tool),
		})
	}

	_ = writer.Write([]string{
		now.Format(time.RFC3339Nano),
		strconv.FormatInt(now.UnixMilli(), 10),
		"Old adapter",
		"192.0.2.10",
		"7",
		"icmp",
		"203.0.113.11",
		"0",
		"true",
		"Success",
		"10000.9",
		"false",
	})
	writer.Flush()
	file.Close()
	m := newManager(t, networkmanager.Config{
		StateDirectory: filepath.Join(base, "config"),
		LogDirectory:   logs,
		Paused:         true,
	}, &testsupport.FakeCollector{})
	snapshot := testsupport.JSON(t, m.Snapshot())
	values := snapshot["targets"].([]any)
	first := values[0].(map[string]any)
	for _, window := range []string{"all", "last60", "last300"} {
		metrics := first[window].(map[string]any)
		if metrics["sent"] != float64(5) || metrics["received"] != float64(4) || metrics["lost"] != float64(1) || metrics["errors"] != float64(1) || metrics["lossPct"] != float64(20) {
			t.Fatal("tool error/pending loss denominator changed", metrics)
		}

		if math.Abs(metrics["jitterMs"].(float64)-59.8) > .000001 {
			t.Fatal("failure must break adjacent-success jitter", metrics)
		}

		if metrics["above20Ms"] != float64(3) || metrics["above100Ms"] != float64(2) || metrics["above200Ms"] != float64(0) {
			t.Fatal("strict threshold semantics changed", metrics)
		}
	}

	second := values[1].(map[string]any)
	if second["all"].(map[string]any)["p95Ms"] != float64(10000) || second["last60"].(map[string]any)["p95Ms"] != float64(10000.9) {
		t.Fatal("session histogram/window exact nearest-rank semantics changed", second)
	}
}
