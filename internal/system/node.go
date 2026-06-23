package system

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Node.js version management. Versions are installed side-by-side as the official
// prebuilt binaries under /opt/repanel/node/<major>/, so each domain's Node app
// can pick its own version without apt conflicts (mirrors php.go for PHP).

const nodeRoot = "/opt/repanel/node"

// knownNodeVersions are the LTS majors the panel offers to install.
var knownNodeVersions = []string{"18", "20", "22", "24"}

// KnownNodeVersions returns the installable Node majors.
func KnownNodeVersions() []string {
	out := make([]string, len(knownNodeVersions))
	copy(out, knownNodeVersions)
	return out
}

func validNodeMajor(v string) bool {
	for _, k := range knownNodeVersions {
		if k == v {
			return true
		}
	}
	return false
}

// nodeArch maps the Go arch to the Node.js distribution arch token.
func nodeArch() (string, bool) {
	switch runtime.GOARCH {
	case "amd64":
		return "x64", true
	case "arm64":
		return "arm64", true
	case "arm":
		return "armv7l", true
	}
	return "", false
}

// InstalledNodeVersions returns the panel-managed Node majors present on disk.
func InstalledNodeVersions() []string {
	out := []string{}
	entries, err := os.ReadDir(nodeRoot)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(nodeRoot, e.Name(), "bin", "node")); err == nil {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// NodeBinary returns the node executable for a major version, falling back to a
// system-wide node of that major when no panel-managed copy exists.
func NodeBinary(version string) (string, bool) {
	if p := filepath.Join(nodeRoot, version, "bin", "node"); fileExists(p) {
		return p, true
	}
	if bin, ok := detectNode()[version]; ok {
		return bin, true
	}
	return "", false
}

// NodeNpmBinary returns the npm executable matching a Node version.
func NodeNpmBinary(version string) (string, bool) {
	if p := filepath.Join(nodeRoot, version, "bin", "npm"); fileExists(p) {
		return p, true
	}
	if p, err := exec.LookPath("npm"); err == nil {
		return p, true
	}
	return "", false
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// InstallNode downloads the latest release of a Node major and installs it under
// /opt/repanel/node/<major>/. Long-running; callers run it in the background.
func InstallNode(version string) error {
	if !validNodeMajor(version) {
		return fmt.Errorf("unsupported Node version %q", version)
	}
	arch, ok := nodeArch()
	if !ok {
		return fmt.Errorf("unsupported architecture %s", runtime.GOARCH)
	}
	full, err := latestNodeRelease(version)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://nodejs.org/dist/v%s/node-v%s-linux-%s.tar.gz", full, full, arch)
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(url)
	if err != nil {
		return fmt.Errorf("download Node: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Node: unexpected status %s", resp.Status)
	}
	dest := filepath.Join(nodeRoot, version)
	tmp := dest + ".tmp"
	os.RemoveAll(tmp)
	if err := extractNodeTarball(resp.Body, tmp); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if !fileExists(filepath.Join(tmp, "bin", "node")) {
		os.RemoveAll(tmp)
		return fmt.Errorf("downloaded archive did not contain bin/node")
	}
	os.RemoveAll(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// latestNodeRelease resolves the newest full version (e.g. "20.11.1") of a major
// from the Node.js release index (which is ordered newest-first).
func latestNodeRelease(major string) (string, error) {
	resp, err := (&http.Client{Timeout: time.Minute}).Get("https://nodejs.org/dist/index.json")
	if err != nil {
		return "", fmt.Errorf("fetch Node release index: %w", err)
	}
	defer resp.Body.Close()
	var rels []struct {
		Version string `json:"version"` // "v20.11.1"
	}
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return "", fmt.Errorf("parse Node release index: %w", err)
	}
	prefix := "v" + major + "."
	for _, r := range rels {
		if strings.HasPrefix(r.Version, prefix) {
			return strings.TrimPrefix(r.Version, "v"), nil
		}
	}
	return "", fmt.Errorf("no Node %s release found", major)
}

// extractNodeTarball unpacks a Node .tar.gz into dest, stripping the leading
// "node-v…/" directory and refusing any entry that would escape dest.
func extractNodeTarball(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(dest)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		i := strings.IndexByte(hdr.Name, '/')
		if i < 0 {
			continue // the top-level dir entry itself
		}
		name := hdr.Name[i+1:]
		if name == "" {
			continue
		}
		target := filepath.Join(dest, name)
		if rel, err := filepath.Rel(cleanDest, target); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // bounded by Node release size
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0o755)
			os.Remove(target)
			os.Symlink(hdr.Linkname, target) // bin/npm, bin/npx, … are symlinks
		}
	}
}
