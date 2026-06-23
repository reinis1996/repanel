package system

import "testing"

func TestValidWPSlug(t *testing.T) {
	for _, s := range []string{"woocommerce", "akismet", "twentytwentyfour", "wp-super-cache", "a", "hello-dolly"} {
		if !validWPSlug.MatchString(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"--activate", "../etc", "WooCommerce", "-x", "", ".hidden", "a b", "slug;rm"} {
		if validWPSlug.MatchString(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

// Action whitelists and slug validation must reject bad input before any WP-CLI
// call (so these are deterministic without wp installed).
func TestWPActionValidation(t *testing.T) {
	if err := WPPluginAction("rpu1", "/d", "akismet", "bogus"); err == nil {
		t.Error("unknown plugin action must be rejected")
	}
	if err := WPPluginAction("rpu1", "/d", "--evil", "update"); err == nil {
		t.Error("invalid plugin slug must be rejected")
	}
	if err := WPThemeAction("rpu1", "/d", "twentytwentyfour", "deactivate"); err == nil {
		t.Error("themes have no deactivate action")
	}
}
