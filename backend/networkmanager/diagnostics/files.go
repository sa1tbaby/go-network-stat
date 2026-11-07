package diagnostics

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

func nullableDiagnosisError(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func diagnosisOrdinaryPath(path string) error {
	path = filepath.Clean(path)
	for {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("The log path is a link: %s", path)
		}

		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}

		path = parent
	}
}

func diagnosisWriteJSON(path string, value any) error {
	if err := diagnosisOrdinaryPath(path); err != nil {
		return err
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".diagnosis-*.tmp")
	if err != nil {
		return err
	}

	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()

		return err
	}

	if err = file.Sync(); err != nil {
		file.Close()

		return err
	}

	if err = file.Close(); err != nil {
		return err
	}

	return fileutil.Replace(name, path)
}

func diagnosisReadJSON(path string) map[string]any {
	if diagnosisOrdinaryPath(path) != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var value map[string]any
	if json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &value) != nil {
		return nil
	}

	return value
}

func diagnosisManifest(dir, id string) (map[string]any, error) {
	manifest := diagnosisReadJSON(filepath.Join(dir, "run.json"))
	if manifest["appId"] != "codex-network-diagnosis-run" || manifest["id"] != id {
		return nil, errors.New("Test not found in the current log folder.")
	}

	return manifest, nil
}

func diagnosisString(m map[string]any, key string) string {
	value, _ := m[key].(string)

	return value
}
