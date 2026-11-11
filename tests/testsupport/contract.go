package testsupport

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type Contract struct {
	DashboardSHA       string `json:"dashboardSha256"`
	LegacyDashboardSHA string `json:"legacyDashboardSha256"`
	DashboardLanguage  string `json:"dashboardLanguage"`
	Routes             []struct {
		Method   string
		Path     string
		Response string
	}
	Objects map[string]map[string]string `json:"objects"`
}

func LoadContract(t *testing.T) Contract {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(Root(), "docs", "panel-contract.json"))
	if err != nil {
		t.Fatal(err)
	}

	var c Contract
	if err = json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}

	return c
}

func AssertShape(t *testing.T, shapes map[string]map[string]string, name string, value map[string]any) {
	t.Helper()
	shape, ok := shapes[name]
	if !ok {
		t.Fatalf("unknown frozen shape %s", name)
	}

	for key, kind := range shape {
		item, exists := value[key]
		if !exists {
			t.Errorf("%s.%s is absent", name, key)
			continue
		}

		valid := false
		for _, alternative := range strings.Split(kind, "|") {
			switch alternative {
			case "null":
				valid = valid || item == nil
			case "string":
				_, yes := item.(string)
				valid = valid || yes
			case "date":
				s, yes := item.(string)
				_, err := time.Parse(time.RFC3339Nano, s)
				valid = valid || yes && err == nil
			case "boolean":
				_, yes := item.(bool)
				valid = valid || yes
			case "number", "integer":
				number, yes := item.(float64)
				valid = valid || yes && (alternative == "number" || number == math.Trunc(number))
			case "array":
				_, yes := item.([]any)
				valid = valid || yes
			case "object":
				_, yes := item.(map[string]any)
				valid = valid || yes
			default:
				if object, yes := item.(map[string]any); yes {
					AssertShape(t, shapes, alternative, object)
					valid = true
				}
			}
		}

		if !valid {
			t.Errorf("%s.%s=%#v does not match %s", name, key, item, kind)
		}
	}
}
