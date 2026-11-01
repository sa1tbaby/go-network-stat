// Package data defines the single writable tree shared by application services.
package data

import (
	"os"
	"path/filepath"
	"strings"
)

// LegacyDirectories are read-only import locations. Measurement history stays
// where it was written and remains selectable by the existing storage API.
func LegacyDirectories(executable string) []string {
	dir := filepath.Dir(executable)
	dirs := []string{filepath.Join(dir, "InternetStability-data"), filepath.Join(dir, "internet-stability")}
	if strings.EqualFold(filepath.Base(dir), "dist") {
		parent := filepath.Dir(dir)
		dirs = append(dirs, filepath.Join(parent, "InternetStability-data"), filepath.Join(parent, "internet-stability"))
	}

	if base, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(base, "InternetStability"))
	}

	return dirs
}
