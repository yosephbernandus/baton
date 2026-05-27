//go:build !windows

package task

import "syscall"

func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func ProcessGroupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return false
	}
	return pgid == pid
}
