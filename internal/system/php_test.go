package system

import "testing"

func TestValidPHPVersion(t *testing.T) {
	for _, v := range []string{"8.3", "8.4", "7.4"} {
		if !validPHPVersion(v) {
			t.Errorf("%q should be a valid PHP version", v)
		}
	}
	for _, v := range []string{"", "8", "8.4.1", "9.9", "8.3; rm -rf", "../etc"} {
		if validPHPVersion(v) {
			t.Errorf("%q should be rejected", v)
		}
	}
}

// managedServices must surface a php<ver>-fpm unit for each installed version
// rather than a hardcoded one.
func TestManagedServicesIncludesInstalledPHP(t *testing.T) {
	want := map[string]bool{}
	for _, v := range PHPVersions() {
		want["php"+v+"-fpm"] = true
	}
	got := map[string]bool{}
	for _, s := range managedServices() {
		got[s.Name] = true
	}
	for unit := range want {
		if !got[unit] {
			t.Errorf("managed services missing %q", unit)
		}
	}
	if got["php8.3-fpm"] && !want["php8.3-fpm"] {
		t.Errorf("php8.3-fpm should not be hardcoded when it is not installed")
	}
}
