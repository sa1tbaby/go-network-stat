package diagnostics

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/collectors"
)

func diagnosisRecoveredAlive(dir string) bool {
	if diagnosisOrdinaryPath(dir) != nil {
		return false
	}

	path := filepath.Join(dir, "runtime.json")
	if diagnosisOrdinaryPath(path) != nil {
		return false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var runtime struct {
		AppID    string `json:"appId"`
		ID       string `json:"id"`
		Children []struct {
			PID      int   `json:"pid"`
			Ticks    int64 `json:"startTimeUtcTicks"`
			ExitCode *int  `json:"exitCode"`
		} `json:"children"`
	}
	if json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &runtime) != nil || runtime.AppID != "codex-diagnosis-runtime" || runtime.ID != filepath.Base(dir) {
		return false
	}

	for _, child := range runtime.Children {
		if child.ExitCode == nil && child.PID > 0 && child.Ticks > 0 && collectors.ProcessIdentityAlive(child.PID, child.Ticks) {
			return true
		}
	}

	return false
}

func diagnosisRecoveredJobs(root string) map[string]bool {
	result := map[string]bool{}
	parent := filepath.Join(root, "diagnosis-runs")
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if entry.IsDir() && diagnosisSafeID(entry.Name()) {
			dir := filepath.Join(parent, entry.Name())
			if _, err := diagnosisManifest(dir, entry.Name()); err == nil && diagnosisRecoveredAlive(dir) {
				result[dir] = true
			}
		}
	}

	return result
}
