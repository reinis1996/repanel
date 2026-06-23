package system

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// SampleResources returns current CPU, memory and disk utilisation as
// percentages, for the historical metrics graphs. CPU is computed as the delta
// since the previous sample, so it keeps its own /proc/stat state independent of
// the dashboard's live Info() reading.
var (
	sampMu        sync.Mutex
	sampLastTotal uint64
	sampLastIdle  uint64
)

func SampleResources() (cpu, mem, disk float64) {
	cpu = sampleCPU()
	if total, used := memInfo(); total > 0 {
		mem = float64(used) / float64(total) * 100
	}
	if total, used := diskUsage("/"); total > 0 {
		disk = float64(used) / float64(total) * 100
	}
	return cpu, mem, disk
}

func sampleCPU() float64 {
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
	sampMu.Lock()
	defer sampMu.Unlock()
	first := sampLastTotal == 0
	dTotal := total - sampLastTotal
	dIdle := idle - sampLastIdle
	sampLastTotal, sampLastIdle = total, idle
	if first || dTotal == 0 {
		return 0 // no prior baseline to diff against yet
	}
	return float64(dTotal-dIdle) / float64(dTotal) * 100
}
