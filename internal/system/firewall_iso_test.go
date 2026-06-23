package system

import (
	"strings"
	"testing"
)

const sampleBeforeRules = `*filter
:ufw-before-input - [0:0]
:ufw-before-output - [0:0]
:ufw-before-forward - [0:0]

# allow all on loopback
-A ufw-before-input -i lo -j ACCEPT
-A ufw-before-output -o lo -j ACCEPT

# quickly process packets for which we already have a connection
-A ufw-before-output -m state --state RELATED,ESTABLISHED -j ACCEPT
COMMIT
`

func TestInsertIsolation(t *testing.T) {
	out, changed, err := insertIsolation(sampleBeforeRules, "ufw-before-output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected the file to change")
	}
	// The managed block must appear, and BEFORE the loopback-accept line so it
	// isn't short-circuited by the blanket loopback ACCEPT.
	if !strings.Contains(out, isoBegin) || !strings.Contains(out, "--dport 30000:39999") {
		t.Fatalf("managed block missing:\n%s", out)
	}
	bIdx := strings.Index(out, isoBegin)
	loopIdx := strings.Index(out, "-A ufw-before-output -o lo -j ACCEPT")
	if bIdx < 0 || loopIdx < 0 || bIdx > loopIdx {
		t.Errorf("isolation block must come before the loopback-accept line (block=%d loop=%d)", bIdx, loopIdx)
	}
	// REJECT must be the last of our three rules.
	if !strings.Contains(out, "-j REJECT --reject-with tcp-reset") {
		t.Error("missing REJECT rule")
	}

	// Idempotent: applying again yields identical content and no change.
	out2, changed2, err := insertIsolation(out, "ufw-before-output")
	if err != nil {
		t.Fatalf("unexpected error on second pass: %v", err)
	}
	if changed2 || out2 != out {
		t.Error("insertIsolation is not idempotent")
	}

	// Exactly one managed block (no duplication).
	if n := strings.Count(out, isoBegin); n != 1 {
		t.Errorf("expected exactly one managed block, got %d", n)
	}
}

func TestInsertIsolationNoMarker(t *testing.T) {
	// A file without the expected loopback rule must be left untouched (fail closed).
	if _, _, err := insertIsolation("*filter\nCOMMIT\n", "ufw-before-output"); err == nil {
		t.Error("expected an error when the loopback rule is absent")
	}
}
