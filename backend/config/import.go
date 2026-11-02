// Package config owns persistent application settings and legacy layout imports.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func importConfig(destination, source string) error {
	if filepath.Clean(source) == filepath.Clean(destination) {
		return nil
	}

	for _, name := range []string{"settings.json", "log-settings.json", "custom-targets.json"} {
		target := filepath.Join(destination, name)
		if _, err := os.Lstat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}

		path := filepath.Join(source, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			continue
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if !json.Valid(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})) {
			return fmt.Errorf("legacy %s contains invalid JSON", path)
		}
		// Exclusive creation keeps concurrent imports from replacing preferences.
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if os.IsExist(err) {
			continue
		}

		if err != nil {
			return err
		}

		_, writeErr := f.Write(raw)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}

		if closeErr != nil {
			return closeErr
		}
		// Load compares browser and desktop preference timestamps. Preserve their
		// original ordering rather than the order in which migration copied them.
		if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
	}

	return nil
}
