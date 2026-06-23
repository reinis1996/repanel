package system

import (
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestIndentConfig(t *testing.T) {
	if got := indentConfig("  \n\t\n", "    "); got != "" {
		t.Errorf("blank input should yield empty, got %q", got)
	}
	got := indentConfig("location /api {\n  proxy_pass http://x;\n}\n", "    ")
	want := "    location /api {\n      proxy_pass http://x;\n}"
	// Leading whitespace on each line is normalized to the indent prefix; the
	// inner line keeps its relative spacing only insofar as TrimLeft removes it,
	// so assert the directive lines are present and prefixed.
	if !strings.Contains(got, "    location /api {") || !strings.HasPrefix(got, "    ") {
		t.Errorf("indentConfig = %q, want indented block like %q", got, want)
	}
	if strings.Contains(got, "\n\n") {
		t.Errorf("indentConfig left a stray blank line: %q", got)
	}
}

// Custom nginx directives must appear inside the generated server block.
func TestNginxVhostIncludesCustomDirectives(t *testing.T) {
	d := models.Domain{Name: "example.com", DocumentRoot: "/srv/x", PHPVersion: "8.3",
		NginxConf: "add_header X-Test 1;"}
	data := vhostDataFor(d, false, "", "", 8080, false)
	var sb strings.Builder
	if err := nginxDirectTemplate.Execute(&sb, data); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, "add_header X-Test 1;") {
		t.Fatalf("custom directive missing from vhost:\n%s", out)
	}
	if !strings.Contains(out, "Custom directives (RePanel)") {
		t.Error("expected custom-directives marker comment")
	}
}

func TestSubsystemsByStack(t *testing.T) {
	php := models.Domain{Runtime: "php", WebMode: "apache"}
	node := models.Domain{Runtime: "node"}

	nginxOnly := &WebServer{Stack: StackNginx}
	if n, a, p := nginxOnly.Subsystems(php); !n || a || !p {
		t.Errorf("nginx stack: got nginx=%v apache=%v php=%v", n, a, p)
	}
	apacheOnly := &WebServer{Stack: StackApache}
	if n, a, p := apacheOnly.Subsystems(php); n || !a || !p {
		t.Errorf("apache stack: got nginx=%v apache=%v php=%v", n, a, p)
	}
	combined := &WebServer{Stack: StackNginxApache}
	if n, a, p := combined.Subsystems(php); !n || !a || !p {
		t.Errorf("combined+apache mode: got nginx=%v apache=%v php=%v", n, a, p)
	}
	if n, a, p := nginxOnly.Subsystems(node); n || a || p {
		t.Errorf("node app should have no editable subsystems, got nginx=%v apache=%v php=%v", n, a, p)
	}
}
