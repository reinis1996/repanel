//go:build !linux

package system

func diskUsage(path string) (totalGB, usedGB float64) { return 0, 0 }
