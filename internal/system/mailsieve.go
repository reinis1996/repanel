package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// Per-mailbox Sieve scripts implement the autoresponder (vacation) and filter
// rules. The panel writes the active script directly into the mailbox's mail home
// (~/.dovecot.sieve); Dovecot compiles and runs it on LMTP delivery once mail
// features are enabled (see EnsureMailDelivery).

// BuildSieveScript renders a mailbox's filters and autoresponder into a single
// Sieve script. It returns "" when there is nothing to apply.
func BuildSieveScript(filters []models.MailFilter, ar models.MailAutoresponder) string {
	var rules, extReq strings.Builder
	req := map[string]bool{}

	for _, f := range filters {
		test := sieveTest(f)
		if test == "" {
			continue
		}
		action, ext := sieveAction(f)
		if action == "" {
			continue
		}
		for _, e := range ext {
			req[e] = true
		}
		fmt.Fprintf(&rules, "if %s {\n%s\n    stop;\n}\n", test, action)
	}

	if ar.Enabled && strings.TrimSpace(ar.Message) != "" {
		req["vacation"] = true
		subj := ar.Subject
		if subj == "" {
			subj = "Out of office"
		}
		fmt.Fprintf(&rules, "vacation :days 1 :subject \"%s\" \"%s\";\n",
			sieveString(subj), sieveString(ar.Message))
	}

	if rules.Len() == 0 {
		return ""
	}
	if len(req) > 0 {
		exts := make([]string, 0, len(req))
		for e := range req {
			exts = append(exts, `"`+e+`"`)
		}
		// Deterministic order keeps regenerated scripts stable.
		for i := 0; i < len(exts); i++ {
			for j := i + 1; j < len(exts); j++ {
				if exts[j] < exts[i] {
					exts[i], exts[j] = exts[j], exts[i]
				}
			}
		}
		fmt.Fprintf(&extReq, "require [%s];\n", strings.Join(exts, ", "))
	}
	return "# Managed by RePanel — do not edit, regenerated from panel state.\n" + extReq.String() + "\n" + rules.String()
}

// sieveTest renders the condition of a filter, or "" if it is malformed.
func sieveTest(f models.MailFilter) string {
	val := sieveString(f.Value)
	match := ":contains"
	if f.Op == "is" {
		match = ":is"
	}
	switch f.Field {
	case "from", "to", "subject":
		header := strings.Title(f.Field) //nolint:staticcheck // ASCII header name
		return fmt.Sprintf("header %s \"%s\" \"%s\"", match, header, val)
	case "any":
		return fmt.Sprintf("header %s [\"From\", \"To\", \"Cc\", \"Subject\"] \"%s\"", match, val)
	}
	return ""
}

// sieveAction renders a filter's action and the Sieve extensions it requires.
func sieveAction(f models.MailFilter) (string, []string) {
	switch f.Action {
	case "fileinto":
		if f.Arg == "" {
			return "", nil
		}
		return fmt.Sprintf("    fileinto \"%s\";", sieveString(f.Arg)), []string{"fileinto"}
	case "forward":
		if f.Arg == "" {
			return "", nil
		}
		return fmt.Sprintf("    redirect \"%s\";", sieveString(f.Arg)), nil
	case "discard":
		return "    discard;", nil
	case "keep":
		return "    keep;", nil
	}
	return "", nil
}

// sieveString escapes a value for a Sieve quoted string.
func sieveString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// mailHome returns a mailbox's Dovecot mail home directory.
func mailHome(address string) (string, bool) {
	user, domain, ok := strings.Cut(address, "@")
	if !ok {
		return "", false
	}
	return filepath.Join("/var/mail/vhosts", domain, user), true
}

// WriteMailboxSieve writes (or removes when script is empty) a mailbox's active
// Sieve script and hands it to the vmail user. Compiling is left to Dovecot.
func WriteMailboxSieve(address, script string) error {
	if !Linux() {
		return nil
	}
	home, ok := mailHome(address)
	if !ok {
		return fmt.Errorf("invalid address %q", address)
	}
	if err := os.MkdirAll(home, 0o770); err != nil {
		return err
	}
	active := filepath.Join(home, ".dovecot.sieve")
	if strings.TrimSpace(script) == "" {
		os.Remove(active)
		os.Remove(active + ".bin") // stale compiled script
		return nil
	}
	if err := os.WriteFile(active, []byte(script), 0o640); err != nil {
		return err
	}
	os.Remove(active + ".bin") // force recompile on next delivery
	if _, err := run("id", "-u", "vmail"); err == nil {
		run("chown", "vmail:vmail", active)
	}
	return nil
}
