package system

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// One-click web-application catalog. Each entry knows where to fetch the app and
// how to unpack it; the panel downloads the archive into a domain's document
// root, provisions a database when the app needs one, and (for WordPress) writes
// a ready-to-use config. Other apps are finished in their browser installer with
// the database credentials the panel created.

// CatalogApp describes an installable application.
type CatalogApp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	NeedsDB     bool   `json:"needs_db"`
	// Config is how the app is set up after extraction:
	//   wordpress = panel writes wp-config + runs WP-CLI (fully automatic)
	//   manual    = finish in the browser installer (panel shows the DB creds)
	//   none      = works immediately (e.g. a flat-file CMS)
	Config string `json:"config"`
	URL    string `json:"-"` // download URL (latest stable)
	Format string `json:"-"` // targz | tarbz2 | zip
	Strip  int    `json:"-"` // leading path components to drop from archive entries
}

var appCatalog = []CatalogApp{
	{
		ID: "wordpress", Name: "WordPress", Category: "CMS", NeedsDB: true, Config: "wordpress",
		Description: "The world's most popular CMS and blogging platform.",
		URL:         "https://wordpress.org/latest.tar.gz", Format: "targz", Strip: 1,
	},
	{
		ID: "drupal", Name: "Drupal", Category: "CMS", NeedsDB: true, Config: "manual",
		Description: "Flexible open-source CMS for content-rich, ambitious sites.",
		URL:         "https://www.drupal.org/download-latest/tar.gz", Format: "targz", Strip: 1,
	},
	{
		ID: "nextcloud", Name: "Nextcloud", Category: "Productivity", NeedsDB: true, Config: "manual",
		Description: "Self-hosted file sync, sharing and collaboration suite.",
		URL:         "https://download.nextcloud.com/server/releases/latest.tar.bz2", Format: "tarbz2", Strip: 1,
	},
	{
		ID: "matomo", Name: "Matomo", Category: "Analytics", NeedsDB: true, Config: "manual",
		Description: "Privacy-friendly web analytics — a Google Analytics alternative.",
		URL:         "https://builds.matomo.org/matomo.zip", Format: "zip", Strip: 1,
	},
	{
		ID: "grav", Name: "Grav", Category: "CMS", NeedsDB: false, Config: "none",
		Description: "Fast, modern flat-file CMS — no database required.",
		URL:         "https://getgrav.org/download/core/grav/latest", Format: "zip", Strip: 1,
	},
}

// AppCatalog returns the installable applications.
func AppCatalog() []CatalogApp { return appCatalog }

// FindCatalogApp looks up an app by id.
func FindCatalogApp(id string) (CatalogApp, bool) {
	for _, a := range appCatalog {
		if a.ID == id {
			return a, true
		}
	}
	return CatalogApp{}, false
}

// InstallCatalogApp downloads the app and extracts it into docRoot, handing the
// files to the site's system user. It does not configure DB apps (those are
// finished in the browser installer); WordPress has its own dedicated installer.
func InstallCatalogApp(app CatalogApp, docRoot, sysUser string) error {
	tmp, err := downloadToTemp(app.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", app.Name, err)
	}
	defer os.Remove(tmp)
	if err := extractArchive(tmp, docRoot, app.Format, app.Strip); err != nil {
		return fmt.Errorf("unpack %s: %w", app.Name, err)
	}
	if Linux() && validSysName.MatchString(sysUser) {
		if _, err := run("chown", "-R", sysUser+":"+sysUser, docRoot); err != nil {
			return fmt.Errorf("chown docroot: %w", err)
		}
	}
	return nil
}

// downloadToTemp fetches url to a temp file and returns its path.
func downloadToTemp(url string) (string, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s", resp.Status)
	}
	tmp, err := os.CreateTemp("", "repanel-app-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 700<<20)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	tmp.Close()
	return tmp.Name(), nil
}

// extractArchive unpacks srcPath into dest, dropping `strip` leading path
// components from each entry and refusing any path that escapes dest.
func extractArchive(srcPath, dest, format string, strip int) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	os.Remove(filepath.Join(dest, "index.html")) // drop the default placeholder
	switch format {
	case "zip":
		return extractZip(srcPath, dest, strip)
	case "targz", "tarbz2":
		return extractTar(srcPath, dest, format, strip)
	}
	return fmt.Errorf("unsupported archive format %q", format)
}

// stripPath drops the first `strip` path components; returns "" when nothing
// remains (e.g. a top-level file when stripping the single app directory).
func stripPath(name string, strip int) string {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "./"), "/")
	parts := strings.Split(name, "/")
	if len(parts) <= strip {
		return ""
	}
	return strings.Join(parts[strip:], "/")
}

func safeTarget(dest, name string) (string, bool) {
	target := filepath.Join(dest, name)
	rel, err := filepath.Rel(filepath.Clean(dest), target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return target, true
}

func extractTar(srcPath, dest, format string, strip int) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	var dr io.Reader = f
	if format == "targz" {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		dr = gz
	} else {
		dr = bzip2.NewReader(f)
	}
	tr := tar.NewReader(dr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := stripPath(hdr.Name, strip)
		if name == "" {
			continue
		}
		target, ok := safeTarget(dest, name)
		if !ok {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeExtracted(target, tr); err != nil {
				return err
			}
		}
		// symlinks/hardlinks are skipped for safety
	}
}

func extractZip(srcPath, dest string, strip int) error {
	zr, err := zip.OpenReader(srcPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		name := stripPath(zf.Name, strip)
		if name == "" {
			continue
		}
		target, ok := safeTarget(dest, name)
		if !ok {
			return fmt.Errorf("unsafe path in archive: %s", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeExtracted(target, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeExtracted(target string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	// Bound per-file size to blunt a pathological archive (trusted sources, but
	// cheap insurance).
	_, err = io.Copy(out, io.LimitReader(src, 1<<30))
	return err
}
