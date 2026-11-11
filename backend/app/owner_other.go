//go:build !windows

package app

func nativeOwnerAlive(int, int64) bool {
	return false
}
