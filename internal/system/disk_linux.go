//go:build linux

package system

import "syscall"

func diskUsage(path string) (totalGB, usedGB float64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	free := float64(st.Bavail) * float64(st.Bsize)
	const gb = 1 << 30
	return total / gb, (total - free) / gb
}
