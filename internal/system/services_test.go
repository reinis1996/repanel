package system

import "testing"

func TestCleanPkgVersion(t *testing.T) {
	cases := map[string]string{
		"1:2.4.62-1~deb12u1": "2.4.62", // epoch + revision stripped
		"1.22.1-9+deb12u1":   "1.22.1",
		"15+248":             "15", // postgresql meta -> major
		"8.3.6-1":            "8.3.6",
		"2.3.19":             "2.3.19", // already clean
		"":                   "",
	}
	for in, want := range cases {
		if got := cleanPkgVersion(in); got != want {
			t.Errorf("cleanPkgVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServicePackage(t *testing.T) {
	cases := map[string]string{
		"nginx":       "nginx",
		"ssh":         "openssh-server",
		"mariadb":     "mariadb-server",
		"php8.3-fpm":  "php8.3-fpm", // dynamic PHP units map to themselves
		"php8.1-fpm":  "php8.1-fpm",
		"repanel":     "", // not a distro package
		"nonexistent": "",
	}
	for unit, want := range cases {
		if got := servicePackage(unit); got != want {
			t.Errorf("servicePackage(%q) = %q, want %q", unit, got, want)
		}
	}
}
