//go:build windows

package fileutil

import (
	"errors"
	"syscall"
)

func retryReplacement(err error) bool {
	return errors.Is(err, syscall.Errno(5)) || errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33))
}
