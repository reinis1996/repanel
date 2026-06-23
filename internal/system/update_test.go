package system

import "testing"

func TestUpdateAvailable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"1.2.3", "v1.2.4", true},
		{"1.2.3", "v1.3.0", true},
		{"1.2.3", "v2.0.0", true},
		{"1.2.3", "v1.2.3", false},
		{"1.2.3", "v1.2.2", false},
		{"0.1.0", "v0.1.0", false},
		{"1.2.3", "", false}, // unknown latest → no update
		// Pre-release/build metadata is ignored (release tags are plain X.Y.Z).
		{"1.0.0-rc1", "v1.0.0", false},
		{"1.0.0", "v1.0.1-beta", true},
	}
	for _, c := range cases {
		if got := UpdateAvailable(c.current, c.latest); got != c.want {
			t.Errorf("UpdateAvailable(%q,%q)=%v want %v", c.current, c.latest, got, c.want)
		}
	}
}
