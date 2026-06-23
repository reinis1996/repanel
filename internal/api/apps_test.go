package api

import "testing"

func TestAppDBName(t *testing.T) {
	cases := []struct{ app, domain, want string }{
		{"wordpress", "example.com", "wordpress_example_com"},
		{"wordpress", "blog.example.com", "wordpress_blog_example_com"},
		{"drupal", "My-Site.NET", "drupal_my_site_net"},
		{"nextcloud", "example.com", "nextcloud_example_com"},
	}
	for _, c := range cases {
		if got := appDBName(c.app, c.domain); got != c.want {
			t.Errorf("appDBName(%q, %q) = %q, want %q", c.app, c.domain, got, c.want)
		}
	}
	// Must always satisfy the MariaDB identifier rules used on create.
	for _, in := range []string{"example.com", "a-very-long-domain-name-that-keeps-going-and-going-forever.example.com"} {
		if got := appDBName("nextcloud", in); !validDBInput.MatchString(got) {
			t.Errorf("appDBName(nextcloud, %q) = %q is not a valid identifier", in, got)
		}
	}
}

func TestDocRootPurgeable(t *testing.T) {
	root := "/var/www"
	ok := []string{
		"/var/www/repuser5/example.com/public_html",
		"/var/www/u/d", // exactly two levels deep
	}
	for _, p := range ok {
		if !docRootPurgeable(root, p) {
			t.Errorf("expected %q to be purgeable", p)
		}
	}
	bad := []string{
		"/var/www",              // the web root itself
		"/var/www/repuser5",     // a user's home — too shallow
		"/etc/passwd",           // outside the web root
		"/var/www/../etc",       // escape attempt
		"/",                     // filesystem root
	}
	for _, p := range bad {
		if docRootPurgeable(root, p) {
			t.Errorf("expected %q to be refused", p)
		}
	}
}
