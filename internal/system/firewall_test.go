package system

import "testing"

// DefaultPanelPorts must always open SSH and the panel port, emit only valid
// port/proto pairs, and skip an invalid panel port.
func TestDefaultPanelPorts(t *testing.T) {
	ports := DefaultPanelPorts("8443")
	var hasSSH, hasPanel bool
	for _, p := range ports {
		if !validPort.MatchString(p.Port) {
			t.Errorf("invalid port %q", p.Port)
		}
		if p.Proto != "tcp" && p.Proto != "udp" {
			t.Errorf("invalid proto %q for %q", p.Proto, p.Port)
		}
		switch p.Port {
		case "22":
			hasSSH = true
		case "8443":
			hasPanel = true
		}
	}
	if !hasSSH {
		t.Error("SSH must always be opened")
	}
	if !hasPanel {
		t.Error("the panel port must be opened")
	}
	for _, p := range DefaultPanelPorts("not-a-port") {
		if p.Note == "RePanel" {
			t.Error("an invalid panel port must be skipped")
		}
	}
}
