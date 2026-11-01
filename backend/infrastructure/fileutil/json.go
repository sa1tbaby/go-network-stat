package fileutil

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteJSON atomically publishes a complete JSON document in an existing directory.
func WriteJSON(path string, value any) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".json-")
	if err != nil {
		return err
	}

	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(content); err != nil {
		file.Close()

		return err
	}

	if err = file.Close(); err != nil {
		return err
	}

	return Replace(name, path)
}
