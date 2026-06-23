package api

import (
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestDBPrefix(t *testing.T) {
	cases := []struct {
		role models.Role
		user string
		want string
	}{
		{models.RoleAdmin, "admin", ""},                                                  // admin is the server owner: no prefix
		{models.RoleUser, "alice", "alice_"},                                             // normal account
		{models.RoleReseller, "Bob-Co", "bob_co_"},                                       // sanitized + lowercased
		{models.RoleUser, "a_very_long_username_exceeding_the_cap", "a_very_long_user_"}, // 16-char cap
		{models.RoleUser, "***", ""},                                                     // nothing usable -> no prefix
	}
	for _, c := range cases {
		got := dbPrefix(&models.User{Role: c.role, Username: c.user})
		if got != c.want {
			t.Errorf("dbPrefix(%s, %q) = %q, want %q", c.role, c.user, got, c.want)
		}
	}
}

func TestApplyDBPrefix(t *testing.T) {
	if got := applyDBPrefix("alice_", "shop"); got != "alice_shop" {
		t.Errorf("expected alice_shop, got %q", got)
	}
	if got := applyDBPrefix("alice_", "alice_shop"); got != "alice_shop" {
		t.Errorf("prefix already present must not double: got %q", got)
	}
	if got := applyDBPrefix("", "shop"); got != "shop" {
		t.Errorf("empty prefix must pass through: got %q", got)
	}
}
