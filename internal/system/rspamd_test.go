package system

import "testing"

func TestRspamdKey(t *testing.T) {
	cases := map[string]string{
		"example.com":   "example_com",
		"my-site.co.uk": "my_site_co_uk",
		"a.b.c":         "a_b_c",
	}
	for in, want := range cases {
		if got := rspamdKey(in); got != want {
			t.Errorf("rspamdKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRspamdDomainRe(t *testing.T) {
	if got := rspamdDomainRe("example.com"); got != `example\.com` {
		t.Errorf("rspamdDomainRe = %q", got)
	}
	if got := rspamdDomainRe("a.b.c"); got != `a\.b\.c` {
		t.Errorf("rspamdDomainRe = %q", got)
	}
}
