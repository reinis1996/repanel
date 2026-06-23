package system

import (
	"os"
	"path/filepath"
)

// DirSizeMB returns the total size of all regular files under dir in MB.
// A missing directory counts as zero; unreadable entries are skipped.
func DirSizeMB(dir string) float64 {
	var total int64
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return float64(total) / (1 << 20)
}
