//go:build windows

package task

import "os"

func killProcessGroup(pid int) {
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
}
