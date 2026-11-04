//go:build !windows

package collectors

import (
	"context"
	"errors"
)

var errNativeCapturePlatform = errors.New("ETW capture is only available on Windows")

func ProcessIdentity(int) (int64, string, error) {
	return 0, "", errNativeCapturePlatform
}

func ProcessIdentityAlive(int, int64) bool {
	return false
}

func CurrentProcessIdentity() (int, int64, error) {
	return 0, 0, errNativeCapturePlatform
}

func FindGameProcess(string) (int, int64, error) {
	return 0, 0, errNativeCapturePlatform
}

func RunElevatedCapture(context.Context, string, CaptureRequest, int, func(int)) error {
	return errNativeCapturePlatform
}

func runNativeCapture(context.Context, string, CaptureRequest, *CaptureReport, func() error) error {
	return errNativeCapturePlatform
}
