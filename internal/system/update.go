package system

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Self-update: query the project's GitHub releases (with a token for the private
// repo), then download and atomically replace the panel and CLI binaries before
// restarting the service. Asset names match the release workflow:
//   repanel-linux-<arch>, repctl-linux-<arch> (+ .sha256 each)

// DefaultUpdateRepo is the owner/repo used when no update_repo setting is set.
const DefaultUpdateRepo = "reinis1996/repanel"

// panelServiceName is the systemd unit the installer creates.
const panelServiceName = "repanel"

// ReleaseAsset is one downloadable artifact of a GitHub release.
type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // API asset URL (works for private repos with a token)
}

// Release is the subset of the GitHub release API we use.
type Release struct {
	TagName string         `json:"tag_name"`
	Assets  []ReleaseAsset `json:"assets"`
}

func githubGet(url, token, accept string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// The Go client drops the Authorization header on a cross-host redirect
	// (GitHub → the pre-signed S3 asset URL), so the token never leaks to S3.
	return (&http.Client{Timeout: 10 * time.Minute}).Do(req)
}

// LatestRelease returns the newest published release of repo. token is required
// for a private repository.
func LatestRelease(repo, token string) (Release, error) {
	resp, err := githubGet("https://api.github.com/repos/"+repo+"/releases/latest", token, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub API: %s", githubError(resp))
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("parse release: %w", err)
	}
	return rel, nil
}

// githubError extracts a concise message from a non-200 GitHub response.
func githubError(resp *http.Response) string {
	var body struct {
		Message string `json:"message"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body)
	if body.Message != "" {
		return resp.Status + " — " + body.Message
	}
	return resp.Status
}

func findAsset(rel Release, name string) (ReleaseAsset, bool) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return ReleaseAsset{}, false
}

func downloadAsset(a ReleaseAsset, token string) ([]byte, error) {
	resp, err := githubGet(a.URL, token, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", a.Name, githubError(resp))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// ApplyUpdate downloads the release binaries for this host's architecture,
// verifies their checksums, replaces the running panel and CLI binaries and
// returns — the caller restarts the service. Linux only.
func ApplyUpdate(repo, token, currentVersion string) error {
	if !Linux() {
		return fmt.Errorf("self-update is only supported on Linux")
	}
	rel, err := LatestRelease(repo, token)
	if err != nil {
		return err
	}
	if !UpdateAvailable(currentVersion, rel.TagName) {
		return fmt.Errorf("already on the latest version (%s)", currentVersion)
	}
	panelBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate panel binary: %w", err)
	}
	suffix := runtime.GOARCH

	if err := replaceFromAsset(rel, "repanel-linux-"+suffix, panelBin, token); err != nil {
		return err
	}
	// The CLI is best-effort: don't fail the whole update if it's missing.
	cliBin := filepath.Join(filepath.Dir(panelBin), "repctl")
	_ = replaceFromAsset(rel, "repctl-linux-"+suffix, cliBin, token)
	return nil
}

// replaceFromAsset downloads assetName (verifying assetName.sha256 when present)
// and atomically replaces dest.
func replaceFromAsset(rel Release, assetName, dest, token string) error {
	asset, ok := findAsset(rel, assetName)
	if !ok {
		return fmt.Errorf("release has no asset %q", assetName)
	}
	data, err := downloadAsset(asset, token)
	if err != nil {
		return err
	}
	if sum, ok := findAsset(rel, assetName+".sha256"); ok {
		raw, err := downloadAsset(sum, token)
		if err != nil {
			return err
		}
		want := strings.Fields(string(raw))
		got := sha256.Sum256(data)
		if len(want) == 0 || !strings.EqualFold(want[0], hex.EncodeToString(got[:])) {
			return fmt.Errorf("checksum mismatch for %s", assetName)
		}
	}
	tmp := dest + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil { // atomic on the same filesystem
		os.Remove(tmp)
		return fmt.Errorf("install %s: %w", dest, err)
	}
	return nil
}

// RestartPanel asks systemd to restart the panel without blocking, so the caller
// (the panel itself) can return before it is signalled.
func RestartPanel() {
	if have("systemctl") {
		run("systemctl", "restart", "--no-block", panelServiceName)
	}
}

// UpdateAvailable reports whether latest is a newer version than current.
func UpdateAvailable(current, latest string) bool {
	if strings.TrimSpace(latest) == "" {
		return false
	}
	return semverLess(current, latest)
}

func semverLess(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i] // drop pre-release / build metadata
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
