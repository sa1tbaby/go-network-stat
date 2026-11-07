package diagnostics

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

func (job *diagnosisJob) saveRuntime() error {
	// Native workers are goroutines in the backend process; no independent PID
	// can be recovered after that process exits. Preserve the old manifest shape.
	return diagnosisWriteJSON(filepath.Join(job.dir, "runtime.json"), map[string]any{
		"appId":          "codex-diagnosis-runtime",
		"id":             job.id,
		"started":        job.started,
		"error":          nullableDiagnosisError(job.err),
		"stopRequested":  job.stopRequested,
		"executionModel": "go-goroutines",
		"ownerPid":       os.Getpid(),
		"children":       []any{},
		"running":        job.running,
	})
}

func diagnosisWriteStop(dir string) error {
	path := filepath.Join(dir, "diagnostics-stop.flag")
	if err := diagnosisOrdinaryPath(path); err != nil {
		return err
	}

	return os.WriteFile(path, []byte("stop"), 0600)
}

func diagnosisRandomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}

	return hex.EncodeToString(b)
}

func diagnosisSafeID(id string) bool {
	if id == "" || len(id) > 100 {
		return false
	}

	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}

	return true
}
