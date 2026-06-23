package system

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Server / migration backup: a single archive of the panel's own state — the
// SQLite database (the source of truth), /etc/repanel (config, mail maps, DKIM
// keys) and the issued certificates. Restoring it on a fresh host and starting
// the panel regenerates every native config file from the database, so it doubles
// as a whole-server migration.
//
// Layout:
//	repanel.db            consistent snapshot of the panel database
//	etc-repanel/<...>     /etc/repanel tree
//	certs/<...>           issued certificates (DataDir/certs)

// CreateServerBackup writes the migration archive to dest. dbSnapshot is a
// consistent copy of the database (the caller makes it with VACUUM INTO); confDir
// is /etc/repanel and certsDir the issued-certificate directory.
func CreateServerBackup(dest, dbSnapshot, confDir, certsDir string) (err error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(dest)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	if err = addFileToTar(tw, "repanel.db", dbSnapshot); err != nil {
		return fmt.Errorf("archive database: %w", err)
	}
	if err = addDirToTar(tw, "etc-repanel", confDir); err != nil {
		return fmt.Errorf("archive config: %w", err)
	}
	if err = addDirToTar(tw, "certs", certsDir); err != nil {
		return fmt.Errorf("archive certs: %w", err)
	}
	if err = tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// addFileToTar streams a single regular file into the archive under name.
func addFileToTar(tw *tar.Writer, name, srcPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, src, info.Size())
	return err
}

// RestoreServerBackup unpacks a migration archive: the database to dbPath, the
// config tree to confDir and certificates to certsDir. It must run with the panel
// stopped (see the `restore-config` subcommand).
func RestoreServerBackup(archive, dbPath, confDir, certsDir string) error {
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
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(hdr.Name)
		var dest string
		switch {
		case name == "repanel.db":
			dest = dbPath
		case name == "etc-repanel" || hasPrefix(name, "etc-repanel/"):
			dest = filepath.Join(confDir, trimFirst(name, "etc-repanel"))
		case hasPrefix(name, "certs/"):
			dest = filepath.Join(certsDir, trimFirst(name, "certs"))
		default:
			continue
		}
		// Confine extraction to the intended roots.
		if rel, err := filepath.Rel(restoreRoot(dest, dbPath, confDir, certsDir), dest); err != nil || hasPrefix(rel, "..") {
			return fmt.Errorf("unsafe path in archive: %s", name)
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
			_, err = io.Copy(out, io.LimitReader(tr, maxRestoreBytes))
			out.Close()
			if err != nil {
				return err
			}
		}
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func trimFirst(name, prefix string) string {
	return filepath.FromSlash(name[len(prefix):])
}

// restoreRoot returns the destination root a path belongs to, for the traversal check.
func restoreRoot(dest, dbPath, confDir, certsDir string) string {
	switch {
	case dest == dbPath:
		return filepath.Dir(dbPath)
	case hasPrefix(dest, confDir):
		return confDir
	default:
		return certsDir
	}
}
