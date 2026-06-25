package system

import (
	"path/filepath"
	"testing"
)

func TestResolveDocRoot(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "laravel", "public")

	// Relative and absolute inputs that stay inside resolve to the same path.
	for _, in := range []string{"laravel/public", sub} {
		got, err := ResolveDocRoot(base, in)
		if err != nil {
			t.Fatalf("ResolveDocRoot(%q) error: %v", in, err)
		}
		if got != sub {
			t.Fatalf("ResolveDocRoot(%q) = %q, want %q", in, got, sub)
		}
	}

	// Escapes and empties are rejected.
	for _, in := range []string{"", "   ", "../../etc", filepath.Join(filepath.Dir(base), "other")} {
		if _, err := ResolveDocRoot(base, in); err == nil {
			t.Fatalf("ResolveDocRoot(%q) accepted an escape/empty path", in)
		}
	}
}

func TestWebSpaceBase(t *testing.T) {
	cases := map[string]string{
		"/var/www/rpu1/site.dev/public_html":    "/var/www/rpu1/site.dev",
		"/var/www/rpu1/site.dev/laravel/public": "/var/www/rpu1/site.dev",
		"/srv/elsewhere/public":                 "/srv/elsewhere/public", // no sysUser segment: fall back to docroot
	}
	for docRoot, want := range cases {
		if got := webSpaceBase(docRoot, "rpu1"); got != want {
			t.Fatalf("webSpaceBase(%q) = %q, want %q", docRoot, got, want)
		}
	}
}
