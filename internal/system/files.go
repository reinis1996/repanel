package system

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// File manager operations. Every path is resolved inside a jail directory
// (the customer's web root) — attempts to escape with ".." or absolute paths
// are rejected.

type FileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
}

const maxEditableSize = 2 << 20 // 2 MB limit for in-browser editing

// ResolveJailed joins rel onto jail and guarantees the result stays inside.
func ResolveJailed(jail, rel string) (string, error) {
	jail = filepath.Clean(jail)
	p := filepath.Join(jail, filepath.FromSlash(rel))
	p = filepath.Clean(p)
	if p != jail && !strings.HasPrefix(p, jail+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes home directory")
	}
	return p, nil
}

func ListDir(jail, rel string) ([]FileEntry, error) {
	dir, err := ResolveJailed(jail, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			Mode:    info.Mode().Perm().String(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func ReadFileJailed(jail, rel string) ([]byte, error) {
	p, err := ResolveJailed(jail, rel)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxEditableSize {
		return nil, fmt.Errorf("file larger than 2 MB; download it instead")
	}
	return os.ReadFile(p)
}

func WriteFileJailed(jail, rel string, data []byte, sysUser string) error {
	p, err := ResolveJailed(jail, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	chownToUser(p, sysUser)
	return nil
}

func MkdirJailed(jail, rel, sysUser string) error {
	p, err := ResolveJailed(jail, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return err
	}
	chownToUser(p, sysUser)
	return nil
}

func DeleteJailed(jail, rel string) error {
	p, err := ResolveJailed(jail, rel)
	if err != nil {
		return err
	}
	if p == filepath.Clean(jail) {
		return fmt.Errorf("refusing to delete the home directory itself")
	}
	return os.RemoveAll(p)
}

func RenameJailed(jail, fromRel, toRel string) error {
	from, err := ResolveJailed(jail, fromRel)
	if err != nil {
		return err
	}
	to, err := ResolveJailed(jail, toRel)
	if err != nil {
		return err
	}
	return os.Rename(from, to)
}

// OpenFileJailed returns a reader for downloads.
func OpenFileJailed(jail, rel string) (io.ReadCloser, os.FileInfo, error) {
	p, err := ResolveJailed(jail, rel)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if st.IsDir() {
		f.Close()
		return nil, nil, fmt.Errorf("is a directory")
	}
	return f, st, nil
}

// SaveUploadJailed streams an upload into the jail.
func SaveUploadJailed(jail, rel string, src io.Reader, sysUser string) error {
	p, err := ResolveJailed(jail, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	dst, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	chownToUser(p, sysUser)
	return nil
}

func chownToUser(path, sysUser string) {
	if Linux() && sysUser != "" && validSysName.MatchString(sysUser) {
		run("chown", sysUser+":"+sysUser, path)
	}
}
