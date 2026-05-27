//go:build !windows

package runner

import (
	"os"
	"testing"
)

func TestCheckNetworkActivityOwnProcess(t *testing.T) {
	pid := os.Getpid()
	_ = checkNetworkActivity(pid)
}

func TestCheckNetworkActivityDeadPID(t *testing.T) {
	active := checkNetworkActivity(99999)
	if active {
		t.Error("dead PID should not have network activity")
	}
}

func TestCheckNetworkActivityCaching(t *testing.T) {
	pid := os.Getpid()
	r1 := checkNetworkActivity(pid)
	r2 := checkNetworkActivity(pid)
	if r1 != r2 {
		t.Error("cached result should be consistent")
	}
}
