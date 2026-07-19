//go:build !windows

package workerstatus

import "syscall"

func processIsAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}
