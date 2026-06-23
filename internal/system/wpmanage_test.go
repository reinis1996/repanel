package system

import (
	"github.com/reinis1996/repanel/internal/models"
	"testing"
)

// These exercise the input guards that must reject crafted values before any
// WP-CLI process is spawned, so they're deterministic without wp installed.

func TestWPUserCreateValidation(t *testing.T) {
	cases := []struct {
		name                     string
		login, email, role, pass string
	}{
		{"bad login", "a;rm -rf", "a@b.co", "editor", "password1"},
		{"bad email", "alice", "not-an-email", "editor", "password1"},
		{"bad role", "alice", "a@b.co", "Super-Admin!", "password1"},
		{"short password", "alice", "a@b.co", "editor", "short"},
	}
	for _, c := range cases {
		if err := WPUserCreate("rpu1", "/d", c.login, c.email, c.role, c.pass); err == nil {
			t.Errorf("%s: expected rejection", c.name)
		}
	}
}

func TestWPAutoUpdateValidation(t *testing.T) {
	if err := WPAutoUpdate("rpu1", "/d", "widget", "akismet", true); err == nil {
		t.Error("invalid kind must be rejected")
	}
	if err := WPAutoUpdate("rpu1", "/d", "plugin", "../evil", true); err == nil {
		t.Error("invalid slug must be rejected")
	}
}

func TestWPSearchReplaceValidation(t *testing.T) {
	if _, err := WPSearchReplace("rpu1", "/d", "-x", "new", true); err == nil {
		t.Error("term starting with '-' must be rejected")
	}
	if _, err := WPSearchReplace("rpu1", "/d", "old\nvalue", "new", true); err == nil {
		t.Error("multiline term must be rejected")
	}
}

func TestWPSetConfigMemoryValidation(t *testing.T) {
	if err := WPSetConfig("rpu1", "/d", models.WPConfig{MemoryLimit: "lots"}); err == nil {
		t.Error("malformed memory limit must be rejected")
	}
}

func TestWPUserSetRoleValidation(t *testing.T) {
	if err := WPUserSetRole("rpu1", "/d", 0, "editor"); err == nil {
		t.Error("invalid user id must be rejected")
	}
	if err := WPUserSetRole("rpu1", "/d", 5, "Bad Role"); err == nil {
		t.Error("invalid role must be rejected")
	}
}

func TestCoerceStr(t *testing.T) {
	cases := map[any]string{
		"hi":       "hi",
		float64(7): "7",
		true:       "true",
		nil:        "",
	}
	for in, want := range cases {
		if got := coerceStr(in); got != want {
			t.Errorf("coerceStr(%v) = %q, want %q", in, got, want)
		}
	}
}
