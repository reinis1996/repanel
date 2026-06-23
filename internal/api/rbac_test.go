package api

import (
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestHasModule(t *testing.T) {
	admin := &models.User{Role: models.RoleAdmin}
	if !admin.HasModule(models.ModuleMail) {
		t.Error("admins must have every module")
	}
	u := &models.User{Role: models.RoleUser, Permissions: []string{models.ModuleDNS}}
	if !u.HasModule(models.ModuleDNS) {
		t.Error("user should have a granted module")
	}
	if u.HasModule(models.ModuleMail) {
		t.Error("user must not have an ungranted module")
	}
}

// A reseller may not grant beyond their own modules; admins (nil allowed) cap nothing.
func TestCapPermissions(t *testing.T) {
	got := capPermissions([]string{"dns", "mail", "ftp"}, []string{"dns", "ftp"})
	if len(got) != 2 || got[0] != "dns" || got[1] != "ftp" {
		t.Errorf("cap = %v, want [dns ftp]", got)
	}
	if uncapped := capPermissions([]string{"dns"}, nil); len(uncapped) != 1 || uncapped[0] != "dns" {
		t.Errorf("nil allowed should not cap, got %v", uncapped)
	}
}
