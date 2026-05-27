//go:build !windows

package runner

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	netCacheMu     sync.Mutex
	netCacheResult map[int]netCacheEntry
)

type netCacheEntry struct {
	active  bool
	checked time.Time
}

func init() {
	netCacheResult = make(map[int]netCacheEntry)
}

func checkNetworkActivity(pid int) bool {
	netCacheMu.Lock()
	defer netCacheMu.Unlock()

	if entry, ok := netCacheResult[pid]; ok && time.Since(entry.checked) < 30*time.Second {
		return entry.active
	}

	active := probeNetwork(pid)
	netCacheResult[pid] = netCacheEntry{active: active, checked: time.Now()}
	return active
}

func probeNetwork(pid int) bool {
	out, err := exec.Command("lsof", "-p", fmt.Sprintf("%d", pid), "-i", "TCP", "-a", "-sTCP:ESTABLISHED").
		CombinedOutput()
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return len(lines) > 1
}
