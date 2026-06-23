package system

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

var (
	cpuMu        sync.Mutex
	lastCPUTotal uint64
	lastCPUIdle  uint64
)

// Info gathers live host metrics from /proc and statfs. On non-Linux dev
// machines most fields stay zero.
func Info(version string) models.SystemInfo {
	info := models.SystemInfo{PanelVersion: version, CPUCount: numCPU()}
	info.Hostname, _ = os.Hostname()
	if !Linux() {
		info.OS = "development (" + osName() + ")"
		return info
	}
	info.OS = readOSRelease()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		info.Kernel = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f, _, ok := strings.Cut(strings.TrimSpace(string(b)), " "); ok {
			if v, err := strconv.ParseFloat(f, 64); err == nil {
				info.Uptime = int64(v)
			}
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(b))
		if len(parts) >= 3 {
			info.LoadAvg = strings.Join(parts[:3], " ")
		}
	}
	info.MemTotalMB, info.MemUsedMB = memInfo()
	info.CPUUsage = cpuUsage()
	info.DiskTotalGB, info.DiskUsedGB = diskUsage("/")
	return info
}

func numCPU() int {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\nprocessor") + 1
}

func osName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "local"
}

func readOSRelease() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return "Linux"
}

func memInfo() (totalMB, usedMB int64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	var total, available int64
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(fields[1], 10, 64) // kB
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	return total / 1024, (total - available) / 1024
}

// cpuUsage computes utilisation between successive calls using /proc/stat.
func cpuUsage() float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line, _, _ := strings.Cut(string(b), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}
	var total, idle uint64
	for i, f := range fields[1:] {
		v, _ := strconv.ParseUint(f, 10, 64)
		total += v
		if i == 3 || i == 4 { // idle + iowait
			idle += v
		}
	}
	cpuMu.Lock()
	defer cpuMu.Unlock()
	dTotal := total - lastCPUTotal
	dIdle := idle - lastCPUIdle
	lastCPUTotal, lastCPUIdle = total, idle
	if dTotal == 0 {
		return 0
	}
	return float64(dTotal-dIdle) / float64(dTotal) * 100
}

// Uptime helper for formatting on the client side is not needed; raw seconds
// are reported. Keep a small helper for logs.
func FormatUptime(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	return d.String()
}
