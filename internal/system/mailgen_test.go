package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestBuildSieveScript(t *testing.T) {
	filters := []models.MailFilter{
		{Field: "subject", Op: "contains", Value: "invoice", Action: "fileinto", Arg: "Invoices"},
		{Field: "from", Op: "is", Value: "spam@x.com", Action: "discard"},
		{Field: "to", Op: "contains", Value: "sales@", Action: "forward", Arg: "team@x.com"},
	}
	ar := models.MailAutoresponder{Enabled: true, Subject: "Away", Message: `Out "now"`}
	script := BuildSieveScript(filters, ar)

	for _, want := range []string{
		`require [`, `"fileinto"`, `"vacation"`,
		`header :contains "Subject" "invoice"`,
		`fileinto "Invoices";`,
		`header :is "From" "spam@x.com"`,
		`discard;`,
		`redirect "team@x.com";`,
		`vacation :days 1 :subject "Away"`,
		`Out \"now\"`, // quote escaped
	} {
		if !strings.Contains(script, want) {
			t.Errorf("sieve script missing %q\n---\n%s", want, script)
		}
	}

	// Nothing configured → empty script.
	if BuildSieveScript(nil, models.MailAutoresponder{}) != "" {
		t.Error("expected empty script when there are no rules")
	}
}

func TestRebuildMailMapsFeatures(t *testing.T) {
	dir := t.TempDir()
	boxes := []models.Mailbox{
		{Address: "joe@a.com", PasswordHash: "HASH", QuotaMB: 500},
		{Address: "unl@a.com", PasswordHash: "HASH2", QuotaMB: 0},
	}
	aliases := []models.MailAlias{
		{Source: "fwd@a.com", Destination: "ext@x.com", KeepCopy: true},
		{Source: "@a.com", Destination: "catchall@a.com"}, // catch-all
	}
	lists := []models.MailList{
		{Address: "team@a.com", Members: []string{"joe@a.com", "ext@x.com"}},
	}
	if err := RebuildMailMaps(dir, []string{"a.com"}, boxes, aliases, lists); err != nil {
		t.Fatal(err)
	}

	passwd, _ := os.ReadFile(filepath.Join(dir, "passwd"))
	if !strings.Contains(string(passwd), "joe@a.com:{SHA512-CRYPT}HASH::::::userdb_quota_rule=*:storage=500M") {
		t.Errorf("quota not written into passwd:\n%s", passwd)
	}
	if !strings.Contains(string(passwd), "unl@a.com:{SHA512-CRYPT}HASH2\n") {
		t.Errorf("unlimited mailbox should have no quota field:\n%s", passwd)
	}

	va, _ := os.ReadFile(filepath.Join(dir, "virtual_aliases"))
	s := string(va)
	if !strings.Contains(s, "fwd@a.com ext@x.com, fwd@a.com") {
		t.Errorf("keep-copy forwarder missing self-target:\n%s", s)
	}
	if !strings.Contains(s, "@a.com catchall@a.com") {
		t.Errorf("catch-all missing:\n%s", s)
	}
	if !strings.Contains(s, "team@a.com joe@a.com, ext@x.com") {
		t.Errorf("distribution list missing:\n%s", s)
	}
}

func TestDovecotDeliveryConf(t *testing.T) {
	c23 := dovecotDeliveryConf("/etc/repanel/mail", 23)
	for _, want := range []string{"protocol lmtp", "sieve", "quota = maildir", "private/dovecot-lmtp", "/etc/repanel/mail/passwd"} {
		if !strings.Contains(c23, want) {
			t.Errorf("2.3 config missing %q", want)
		}
	}
	c24 := dovecotDeliveryConf("/etc/repanel/mail", 24)
	for _, want := range []string{"mail_path", "service lmtp", "sieve"} {
		if !strings.Contains(c24, want) {
			t.Errorf("2.4 config missing %q", want)
		}
	}
}
