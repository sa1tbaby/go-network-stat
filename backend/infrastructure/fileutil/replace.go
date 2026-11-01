// Package fileutil contains the filesystem operations shared by the client,
// network manager and elevated helper.
package fileutil

import (
	"os"
	"time"
)

// Replace publishes a completed temporary file in one rename. On Windows a
// short-lived reader (including antivirus) can deny replacement; retry that
// specific condition without ever deleting/truncating the previous snapshot.
// Source and destination must be on the same filesystem.
func Replace(source, destination string) error {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		err := os.Rename(source, destination)
		if err == nil || !retryReplacement(err) || !time.Now().Before(deadline) {
			return err
		}

		time.Sleep(10 * time.Millisecond)
	}
}
