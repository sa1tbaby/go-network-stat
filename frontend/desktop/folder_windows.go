//go:build windows

package desktop

import (
	"context"
	"fmt"
	"runtime"
)

// PickFolderForBackend is the native UI port supplied by the executable composition root.
func PickFolderForBackend(ctx context.Context, current string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Each HTTP request owns its own COM apartment. No UI globals/window loop are
	// shared with the desktop client, which runs in a separate process.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result, _, _ := uiOle32.NewProc("CoInitializeEx").Call(0, 2)
	if int32(result) < 0 {
		return "", fmt.Errorf("CoInitializeEx: 0x%x", result)
	}

	defer uiOle32.NewProc("CoUninitialize").Call()

	return PickFolderContext(ctx, 0, current)
}
