package api

import "testing"

func TestWPDBName(t *testing.T) {
	cases := map[string]string{
		"example.com":      "wp_example_com",
		"blog.example.com": "wp_blog_example_com",
		"My-Site.NET":      "wp_my_site_net",
	}
	for in, want := range cases {
		if got := wpDBName(in); got != want {
			t.Errorf("wpDBName(%q) = %q, want %q", in, got, want)
		}
	}
	// Must always satisfy the MariaDB identifier rules used on create.
	for _, in := range []string{"example.com", "a-very-long-domain-name-that-keeps-going-and-going.example.com"} {
		if got := wpDBName(in); !validDBInput.MatchString(got) {
			t.Errorf("wpDBName(%q) = %q is not a valid identifier", in, got)
		}
	}
}
