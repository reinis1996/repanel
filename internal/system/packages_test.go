package system

import "testing"

func TestParseAptUpgrade(t *testing.T) {
	out := `Reading package lists...
Building dependency tree...
The following packages will be upgraded:
  base-files libssl1.1 nginx-common
3 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
Inst base-files [11.1+deb11u7] (11.1+deb11u8 Debian:11.8/oldstable [amd64])
Inst libssl1.1 [1.1.1n-0+deb11u4] (1.1.1n-0+deb11u5 Debian:11.8/oldstable, Debian-Security:11/oldstable-security [amd64])
Inst tzdata (2024a-0ubuntu0.22.04 Ubuntu:22.04/jammy-security [all])
Conf base-files (11.1+deb11u8 Debian:11.8/oldstable [amd64])`

	ups := parseAptUpgrade(out)
	if len(ups) != 3 {
		t.Fatalf("got %d updates, want 3: %+v", len(ups), ups)
	}

	byName := map[string]int{}
	for i, u := range ups {
		byName[u.Name] = i
	}

	// Plain upgrade: parsed, not security.
	bf := ups[byName["base-files"]]
	if bf.CurrentVersion != "11.1+deb11u7" || bf.NewVersion != "11.1+deb11u8" || bf.Security {
		t.Errorf("base-files parsed wrong: %+v", bf)
	}
	// Security via "Debian-Security" origin.
	if !ups[byName["libssl1.1"]].Security {
		t.Errorf("libssl1.1 should be flagged security: %+v", ups[byName["libssl1.1"]])
	}
	// Security via "jammy-security" origin, and no installed bracket.
	tz := ups[byName["tzdata"]]
	if tz.CurrentVersion != "" || tz.NewVersion != "2024a-0ubuntu0.22.04" || !tz.Security {
		t.Errorf("tzdata parsed wrong: %+v", tz)
	}
}
