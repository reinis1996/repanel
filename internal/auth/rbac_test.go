package auth

import "testing"

// JoinPermissions drops unknown modules and de-duplicates; SplitPermissions
// parses the stored CSV back.
func TestPermissionsRoundTrip(t *testing.T) {
	csv := JoinPermissions([]string{"dns", "mail", "bogus", "dns"})
	if csv != "dns,mail" {
		t.Fatalf("JoinPermissions = %q, want %q", csv, "dns,mail")
	}
	got := SplitPermissions(csv)
	if len(got) != 2 || got[0] != "dns" || got[1] != "mail" {
		t.Errorf("SplitPermissions = %v", got)
	}
	if len(SplitPermissions("")) != 0 {
		t.Error("empty CSV should yield no permissions")
	}
}
