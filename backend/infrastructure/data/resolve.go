// Package data defines the single writable tree shared by application services.
package data

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultRoot keeps development releases in dist/ next to the project data/.
// A portable executable elsewhere owns a data/ directory beside itself.
func DefaultRoot(executable string) string {
	dir := filepath.Dir(executable)
	if strings.EqualFold(filepath.Base(dir), "dist") {
		dir = filepath.Dir(dir)
	}

	return filepath.Join(dir, "data")
}

// Resolve creates the requested layout or chooses the portable default. When
// that location is read-only, only the default falls back to per-user storage.
func Resolve(explicit string) (Layout, error) {
	if explicit != "" {
		layout, err := New(explicit)
		if err != nil {
			return Layout{}, err
		}

		return layout, layout.Create()
	}

	executable, err := os.Executable()
	if err != nil {
		return Layout{}, err
	}

	root := DefaultRoot(executable)
	// go run removes its temporary executable tree after exit. Keep CLI results
	// in the launch directory so a development run does not erase measurements.
	if filepath.Base(filepath.Dir(executable)) == "exe" {
		for dir := filepath.Dir(executable); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if strings.HasPrefix(filepath.Base(dir), "go-build") {
				workingDirectory, err := os.Getwd()
				if err != nil {
					return Layout{}, err
				}

				root = filepath.Join(workingDirectory, "data")
				break
			}
		}
	}

	layout, err := New(root)
	if err != nil {
		return Layout{}, err
	}

	if err = layout.Create(); err == nil {
		return layout, nil
	}

	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, err = os.UserConfigDir()
		if err != nil {
			return Layout{}, err
		}
	}

	layout, err = New(filepath.Join(base, "InternetStability", "data"))
	if err != nil {
		return Layout{}, err
	}

	return layout, layout.Create()
}
