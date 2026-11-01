//go:build !windows

package fileutil

func retryReplacement(error) bool {
	return false
}
