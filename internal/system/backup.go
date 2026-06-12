package system

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Backups are plain .tar.gz archives with this layout:
//
//	manifest.json          panel metadata (domains, records, mailboxes...)
//	web/<...>              the account's web space
//	mail/<domain>/<...>    maildirs of the account's domains
//	databases/<name>.sql   mysqldump per database
//
// The format is deliberately tool-friendly: an admin can always extract a
// backup by hand with standard tar/mysql commands.

// BackupSource describes a directory to include in the archive.
type BackupSource struct {
	Prefix string // archive path prefix, e.g. "web" or "mail/example.com"
	Dir    string // filesystem directory to walk
}

// CreateBackupArchive writes a complete backup archive to dest.
func CreateBackupArchive(dest string, manifest []byte, sources []BackupSource, dbNames []string) (err error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(dest) // don't leave half-written archives around
		}
	}()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	// manifest
	if err = writeTarBytes(tw, "manifest.json", manifest); err != nil {
		return err
	}

	// directory trees
	for _, src := range sources {
		if err = addDirToTar(tw, src.Prefix, src.Dir); err != nil {
			return fmt.Errorf("archive %s: %w", src.Dir, err)
		}
	}

	// database dumps
	for _, name := range dbNames {
		if err = addDatabaseDump(tw, name); err != nil {
			return fmt.Errorf("dump database %s: %w", name, err)
		}
	}

	if err = tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeTarBytes(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: int64(len(data))}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// addDirToTar walks dir, storing regular files and directories under prefix.
// A missing dir is skipped silently (e.g. account without mail).
func addDirToTar(tw *tar.Writer, prefix, dir string) error {
	dir = filepath.Clean(dir)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return nil
		}
		name := prefix + "/" + filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return nil
		}
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: info.ModTime()})
		case info.Mode().IsRegular():
			hdr := &tar.Header{Name: name, Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime()}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			src, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer src.Close()
			// CopyN guards against the file growing while we stream it.
			_, err = io.CopyN(tw, src, info.Size())
			return err
		default:
			return nil // sockets/symlinks etc. are not archived
		}
	})
}

// addDatabaseDump streams `mysqldump <name>` into databases/<name>.sql via a
// temp file (tar headers need the size upfront).
func addDatabaseDump(tw *tar.Writer, name string) error {
	if !have("mysqldump") {
		return fmt.Errorf("mysqldump not installed")
	}
	if !validDBName.MatchString(name) {
		return fmt.Errorf("invalid database name")
	}
	tmp, err := os.CreateTemp("", "repanel-dump-*.sql")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	cmd := exec.Command("mysqldump", "--single-transaction", "--quick", "--routines", name)
	cmd.Stdout = tmp
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: "databases/" + name + ".sql", Mode: 0o640, Size: size}); err != nil {
		return err
	}
	_, err = io.Copy(tw, tmp)
	return err
}

// RestoreBackup extracts an archive: entries under each prefix in dirTargets
// are restored to the mapped directory, and databases/<name>.sql files whose
// name appears in allowedDBs are imported through the mysql client.
func RestoreBackup(archive string, dirTargets map[string]string, allowedDBs map[string]bool, sysUser string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(hdr.Name)

		// database import
		if rest, ok := strings.CutPrefix(name, "databases/"); ok && strings.HasSuffix(rest, ".sql") {
			dbName := strings.TrimSuffix(rest, ".sql")
			if !allowedDBs[dbName] || !validDBName.MatchString(dbName) {
				continue
			}
			if err := importDatabaseDump(dbName, tr); err != nil {
				return fmt.Errorf("restore database %s: %w", dbName, err)
			}
			continue
		}

		// file extraction
		for prefix, target := range dirTargets {
			rest, ok := strings.CutPrefix(name, prefix+"/")
			if !ok || rest == "" {
				continue
			}
			dest, err := ResolveJailed(target, rest)
			if err != nil {
				return fmt.Errorf("unsafe path %q in archive", name)
			}
			switch hdr.Typeflag {
			case tar.TypeDir:
				os.MkdirAll(dest, 0o755)
			case tar.TypeReg:
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
					return err
				}
				out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
				if err != nil {
					return err
				}
				if _, err := io.Copy(out, tr); err != nil {
					out.Close()
					return err
				}
				out.Close()
				chownToUser(dest, sysUser)
			}
			break
		}
	}
	return nil
}

func importDatabaseDump(name string, src io.Reader) error {
	if !have("mysql") {
		return fmt.Errorf("mysql client not installed")
	}
	cmd := exec.Command("mysql", name)
	cmd.Stdin = src
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
