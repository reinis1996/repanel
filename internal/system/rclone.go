package system

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// Offsite backups via rclone, which speaks S3, Backblaze B2, SFTP, FTP, Dropbox,
// Google Drive and dozens more behind one interface. A destination is just a set
// of rclone parameters (built and stored by the API); here we render them into a
// throwaway config file and drive `rclone` to upload, test and prune.

const rcloneTimeout = 2 * time.Hour // uploads can be large/slow

// HaveRclone reports whether rclone is installed.
func HaveRclone() bool { return have("rclone") }

// InstallRclone installs rclone from the distro repositories.
func InstallRclone() error {
	if !Linux() || !have("apt-get") {
		return fmt.Errorf("rclone can only be installed on a Debian/Ubuntu host")
	}
	_, err := apt("install", "-y", "-q", "rclone")
	return err
}

// RcloneObscure returns rclone's obscured form of a password (required in the
// config for sftp/ftp). Falls back to the plaintext if rclone is unavailable.
func RcloneObscure(plain string) (string, error) {
	if plain == "" || !have("rclone") {
		return plain, nil
	}
	out, err := runStdin(plain, "rclone", "obscure", "-")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// writeRcloneConfig renders a single "[repanel]" remote from config into a
// root-only temp file and returns its path. The "raw" key, if present, is written
// verbatim (the paste-your-own-rclone-config escape hatch); otherwise every other
// key becomes a `key = value` line.
func writeRcloneConfig(config map[string]string) (string, error) {
	var b strings.Builder
	b.WriteString("[repanel]\n")
	if raw, ok := config["raw"]; ok && strings.TrimSpace(raw) != "" {
		b.WriteString(strings.TrimSpace(raw) + "\n")
	} else {
		// Deterministic key order keeps the file stable and testable.
		keys := make([]string, 0, len(config))
		for k := range config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if config[k] != "" {
				fmt.Fprintf(&b, "%s = %s\n", k, config[k])
			}
		}
	}
	f, err := os.CreateTemp("", "repanel-rclone-*.conf")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	os.Chmod(f.Name(), 0o600)
	return f.Name(), nil
}

// rcloneRun executes rclone with a generated config and a timeout.
func rcloneRun(config map[string]string, args ...string) (string, error) {
	if !have("rclone") {
		return "", fmt.Errorf("rclone is not installed on this server")
	}
	conf, err := writeRcloneConfig(config)
	if err != nil {
		return "", err
	}
	defer os.Remove(conf)
	full := append([]string{"--config", conf}, args...)
	out, err := runOpts(rcloneTimeout, nil, "rclone", full...)
	if err != nil {
		return out, fmt.Errorf("%s", firstLine(strings.TrimSpace(out)))
	}
	return out, nil
}

// RcloneUpload copies localFile to remotePath/remoteName on the destination.
func RcloneUpload(config map[string]string, remotePath, remoteName, localFile string) error {
	target := "repanel:" + path.Join(remotePath, remoteName)
	_, err := rcloneRun(config, "copyto", localFile, target)
	return err
}

// RcloneTest verifies a destination by creating its base path (a cheap,
// idempotent reachability check).
func RcloneTest(config map[string]string, remotePath string) error {
	_, err := rcloneRun(config, "mkdir", "repanel:"+remotePath)
	return err
}

// RclonePrune keeps the newest `keep` .tar.gz archives under remoteDir, deleting
// the rest. Backup filenames are timestamped, so lexical sort is chronological.
func RclonePrune(config map[string]string, remoteDir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	out, err := rcloneRun(config, "lsf", "repanel:"+remoteDir)
	if err != nil {
		return err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if strings.HasSuffix(name, ".tar.gz") {
			files = append(files, name)
		}
	}
	if len(files) <= keep {
		return nil
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	for _, name := range files[keep:] {
		rcloneRun(config, "deletefile", "repanel:"+path.Join(remoteDir, name))
	}
	return nil
}
