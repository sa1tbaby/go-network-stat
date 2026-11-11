//go:build windows

package app

import "github.com/sa1tbaby/go-network-stat/backend/infrastructure/process"

func nativeOwnerAlive(pid int, ticks int64) bool {
	return process.OwnerAlive(pid, ticks)
}
