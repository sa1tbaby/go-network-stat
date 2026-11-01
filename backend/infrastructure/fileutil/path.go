package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// NoLinks rejects an existing symlink anywhere in an output path. Missing
// components are allowed so callers can validate before creating directories.
func NoLinks(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path is a symbolic link: %s", path)
		}

		parent := filepath.Dir(path)
		if parent == path || parent == "." {
			return nil
		}

		path = parent
	}
}
