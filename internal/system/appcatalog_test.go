package system

import "testing"

func TestStripPath(t *testing.T) {
	cases := []struct {
		in    string
		strip int
		want  string
	}{
		{"wordpress/wp-admin/index.php", 1, "wp-admin/index.php"},
		{"drupal-10.3.1/core/install.php", 1, "core/install.php"},
		{"./grav/index.php", 1, "index.php"},
		{"matomo/", 1, ""},    // the stripped dir itself
		{"readme.txt", 1, ""}, // top-level file gets dropped when stripping 1
		{"a/b/c", 2, "c"},
	}
	for _, c := range cases {
		if got := stripPath(c.in, c.strip); got != c.want {
			t.Errorf("stripPath(%q, %d) = %q, want %q", c.in, c.strip, got, c.want)
		}
	}
}

func TestSafeTargetRejectsEscape(t *testing.T) {
	dest := "/var/www/user/example.com/public_html"
	if _, ok := safeTarget(dest, "wp-admin/x.php"); !ok {
		t.Error("a normal path should be allowed")
	}
	for _, bad := range []string{"../../../etc/passwd", "..", "../evil"} {
		if _, ok := safeTarget(dest, bad); ok {
			t.Errorf("traversal path %q should be rejected", bad)
		}
	}
}

func TestFindCatalogApp(t *testing.T) {
	if _, ok := FindCatalogApp("wordpress"); !ok {
		t.Error("wordpress should be in the catalog")
	}
	if a, ok := FindCatalogApp("grav"); !ok || a.NeedsDB || a.Config != "none" {
		t.Errorf("grav should be a no-DB, no-config app: %+v", a)
	}
	if _, ok := FindCatalogApp("nope"); ok {
		t.Error("unknown app should not be found")
	}
}
