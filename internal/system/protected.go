package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Password-protected directories (HTTP Basic auth via .htpasswd). The panel
// stores the per-directory credential file under a server-managed directory and
// injects an nginx location / Apache <Directory> block referencing it into the
// domain's generated vhost. nginx and modern Apache both accept bcrypt ($2y$)
// htpasswd hashes, so one file serves either front server.

// htpasswdDir is where the panel keeps generated .htpasswd files, one per
// protected directory, named by domain and directory id.
const htpasswdDir = "/etc/repanel/htpasswd"

// ProtectedSpec describes one protected directory for vhost rendering.
type ProtectedSpec struct {
	ID       int64
	Path     string // URL path, normalized, e.g. /admin
	Realm    string
	DocRoot  string // domain document root, for the Apache <Directory> filesystem path
	Disabled bool   // true when the directory has no users yet (skip injecting auth)
}

// HtpasswdPath returns the credential file path for a domain's protected dir.
func HtpasswdPath(domain string, dirID int64) string {
	return filepath.Join(htpasswdDir, domain, fmt.Sprintf("%d.htpasswd", dirID))
}

// NormalizeProtectedPath cleans a user-supplied URL path to a leading-slash,
// no-trailing-slash form. Returns "" for anything unsafe or for the site root:
// protecting "/" would emit `location ^~ /`, which collides with the vhost's own
// `location /` and makes nginx -t fail, so a real subdirectory is required.
func NormalizeProtectedPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || strings.ContainsAny(p, " \t\r\n\"'{};") || strings.Contains(p, "..") {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "" // the bare site root is not allowed (see above)
	}
	return p
}

// WriteHtpasswd writes (or rewrites) a protected directory's credential file
// from username→bcrypt-hash pairs. With no entries it removes the file.
func WriteHtpasswd(domain string, dirID int64, hashes map[string]string) error {
	path := HtpasswdPath(domain, dirID)
	if len(hashes) == 0 {
		os.Remove(path)
		return nil
	}
	domainDir := filepath.Dir(path)
	if err := os.MkdirAll(domainDir, 0o750); err != nil {
		return err
	}
	var b strings.Builder
	for user, hash := range hashes {
		fmt.Fprintf(&b, "%s:%s\n", user, hash)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o640); err != nil {
		return err
	}
	// The web server workers (www-data on Debian/Ubuntu) read these files when
	// challenging Basic auth, so they must be group-readable by www-data and the
	// path traversable. Without this nginx/Apache can't open the file and every
	// request to the protected directory fails with 403. Best-effort: harmless on
	// a dev box that has no www-data group.
	_, _ = run("chgrp", "-R", "www-data", htpasswdDir)
	os.Chmod(htpasswdDir, 0o750)
	os.Chmod(domainDir, 0o750)
	os.Chmod(path, 0o640)
	// Allow traversal of /etc/repanel itself (o+x only — not listing) so the web
	// user can reach the htpasswd subtree.
	os.Chmod(filepath.Dir(htpasswdDir), 0o751)
	return nil
}

// RemoveDomainHtpasswd deletes all credential files for a domain (on delete).
func RemoveDomainHtpasswd(domain string) {
	os.RemoveAll(filepath.Join(htpasswdDir, domain))
}

// HashHtpasswd returns a bcrypt hash suitable for nginx/Apache htpasswd files.
func HashHtpasswd(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// RenderProtectedNginx renders nginx location blocks enabling basic auth for the
// given directories. Disabled (user-less) directories are skipped so nginx never
// references a missing auth file.
//
// phpSock, when set, is the domain's PHP-FPM socket: the panel serves PHP itself
// (nginx-only stack), so the prefix is matched with ^~ — which stops nginx from
// falling through to the global `location ~ \.php$` regex that would otherwise
// run PHP under the directory WITHOUT auth — and a nested PHP location re-applies
// the auth and fastcgi. When phpSock is "" (PHP is handled by Apache, or the
// site has no PHP) a plain prefix location is enough, since Apache's <Directory>
// auth covers anything Apache executes.
func RenderProtectedNginx(domain, phpSock string, dirs []ProtectedSpec) string {
	var b strings.Builder
	for _, d := range dirs {
		if d.Disabled {
			continue
		}
		realm := strings.ReplaceAll(d.Realm, `"`, ``)
		if realm == "" {
			realm = "Restricted"
		}
		file := HtpasswdPath(domain, d.ID)
		loc := d.Path
		if loc != "/" {
			loc += "/"
		}
		prefix := "location "
		if phpSock != "" {
			prefix = "location ^~ "
		}
		fmt.Fprintf(&b, "\n    %s%s {\n", prefix, loc)
		fmt.Fprintf(&b, "        auth_basic \"%s\";\n", realm)
		fmt.Fprintf(&b, "        auth_basic_user_file %s;\n", file)
		if phpSock != "" {
			fmt.Fprintf(&b, "        location ~ \\.php$ {\n")
			fmt.Fprintf(&b, "            auth_basic \"%s\";\n", realm)
			fmt.Fprintf(&b, "            auth_basic_user_file %s;\n", file)
			b.WriteString("            try_files $uri =404;\n")
			b.WriteString("            include fastcgi_params;\n")
			b.WriteString("            fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
			fmt.Fprintf(&b, "            fastcgi_pass unix:%s;\n", phpSock)
			b.WriteString("        }\n")
		}
		b.WriteString("        try_files $uri $uri/ /index.php?$query_string;\n")
		b.WriteString("    }")
	}
	return b.String()
}

// RenderProtectedApache renders Apache <Directory> blocks enabling basic auth.
func RenderProtectedApache(domain string, dirs []ProtectedSpec) string {
	var b strings.Builder
	for _, d := range dirs {
		if d.Disabled || d.DocRoot == "" {
			continue
		}
		realm := d.Realm
		if realm == "" {
			realm = "Restricted"
		}
		fsPath := filepath.ToSlash(filepath.Join(d.DocRoot, strings.TrimPrefix(d.Path, "/")))
		fmt.Fprintf(&b, "\n    <Directory %s>\n", fsPath)
		b.WriteString("        AuthType Basic\n")
		fmt.Fprintf(&b, "        AuthName \"%s\"\n", strings.ReplaceAll(realm, `"`, ``))
		fmt.Fprintf(&b, "        AuthUserFile %s\n", HtpasswdPath(domain, d.ID))
		b.WriteString("        Require valid-user\n")
		b.WriteString("    </Directory>")
	}
	return b.String()
}
