//go:build !windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestKillProcessGroupReapsGrandchildren(t *testing.T) {
	// Parent shell spawns a grandchild (sleep) that writes PID to file, then exits.
	// Grandchild inherits the process group from Setpgid.
	// After parent exits, killProcessGroup should kill the grandchild.

	pidFile, err := os.CreateTemp("", "baton-reap-gc-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	pidPath := pidFile.Name()
	pidFile.Close()
	defer os.Remove(pidPath)

	cmd := exec.Command("sh", "-c", fmt.Sprintf(
		"sleep 300 </dev/null >/dev/null 2>&1 & echo $! > %s; exit 0", pidPath))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Run(); err != nil {
		t.Fatalf("cmd failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("reading pid file: %v", err)
	}
	grandchildPID, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatalf("parsing grandchild PID: %v (data: %q)", err, string(pidData))
	}

	// Verify grandchild is alive
	if err := syscall.Kill(grandchildPID, 0); err != nil {
		t.Skipf("grandchild already dead: %v", err)
	}

	// Kill process group (what our fix does after cmd.Wait)
	killProcessGroup(cmd)
	time.Sleep(100 * time.Millisecond)

	// Verify grandchild is dead
	if err := syscall.Kill(grandchildPID, 0); err == nil {
		_ = syscall.Kill(grandchildPID, syscall.SIGKILL)
		t.Fatalf("grandchild %d still alive after killProcessGroup", grandchildPID)
	}
}

func TestKillProcessGroupNilSafe(t *testing.T) {
	// Should not panic
	killProcessGroup(nil)

	cmd := &exec.Cmd{}
	killProcessGroup(cmd)
}

func TestKillProcessGroupAfterWait(t *testing.T) {
	// Simulate the full runner lifecycle:
	// 1. Start process with Setpgid
	// 2. Process spawns grandchild
	// 3. cmd.Wait() returns (parent exits)
	// 4. killProcessGroup called (our fix)
	// 5. Grandchild should be dead

	// Write a temp script that spawns a background process
	tmpFile, err := os.CreateTemp("", "baton-reap-test-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	pidFile, err := os.CreateTemp("", "baton-reap-pid-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	pidPath := pidFile.Name()
	pidFile.Close()
	defer os.Remove(pidPath)

	script := fmt.Sprintf("sleep 300 </dev/null >/dev/null 2>&1 & echo $! > %s; exit 0", pidPath)
	if _, err := tmpFile.WriteString(script); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cmd := exec.Command("sh", tmpFile.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait for parent to finish (like runner does)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("cmd.Wait: %v", err)
	}

	// Read grandchild PID
	time.Sleep(50 * time.Millisecond)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("reading pid file: %v", err)
	}
	grandchildPID, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatalf("parsing grandchild PID: %v (data: %q)", err, string(pidData))
	}

	// Verify grandchild still alive after parent exited
	if err := syscall.Kill(grandchildPID, 0); err != nil {
		t.Skipf("grandchild already dead before killProcessGroup (OS reaped fast): %v", err)
	}

	// This is what our fix does — kill process group after cmd.Wait()
	killProcessGroup(cmd)

	time.Sleep(100 * time.Millisecond)

	// Grandchild should be dead
	if err := syscall.Kill(grandchildPID, 0); err == nil {
		_ = syscall.Kill(grandchildPID, syscall.SIGKILL)
		t.Fatalf("grandchild %d still alive after killProcessGroup post-Wait", grandchildPID)
	}
}
