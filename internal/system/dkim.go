package system

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DKIM signing is handled by OpenDKIM running as a Postfix milter (wired by the
// installer). The panel is the source of truth: it generates the RSA keys and
// regenerates OpenDKIM's key/signing tables from database state, the same
// mirror-and-rebuild model used for mail maps and DNS zones.

// DKIMSelector is the DKIM selector RePanel publishes keys under.
const DKIMSelector = "repanel"

const (
	openDKIMKeysRoot    = "/etc/opendkim/keys"
	openDKIMKeyTable    = "/etc/opendkim/key.table"
	openDKIMSignTable   = "/etc/opendkim/signing.table"
	openDKIMTrustedFile = "/etc/opendkim/trusted.hosts"
)

// GeneratedDKIMKey is a freshly minted key: the PEM private key to persist and
// the public-key TXT record value to publish in DNS.
type GeneratedDKIMKey struct {
	Selector   string
	PrivatePEM string
	PublicTXT  string // v=DKIM1; k=rsa; p=<base64>
}

// GenerateDKIMKey creates a 2048-bit RSA key and the DNS TXT record advertising
// its public half.
func GenerateDKIMKey() (GeneratedDKIMKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return GeneratedDKIMKey{}, err
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return GeneratedDKIMKey{}, err
	}
	pubB64 := base64.StdEncoding.EncodeToString(pubDER)
	return GeneratedDKIMKey{
		Selector:   DKIMSelector,
		PrivatePEM: string(privPEM),
		PublicTXT:  "v=DKIM1; k=rsa; p=" + pubB64,
	}, nil
}

// DKIMDomain pairs a domain with the private key OpenDKIM should sign it with.
type DKIMDomain struct {
	Domain     string
	Selector   string
	PrivatePEM string
}

// RebuildDKIM rewrites OpenDKIM's per-domain key files and its KeyTable and
// SigningTable from the full set of enabled domains, then reloads the service.
// It is a no-op off Linux (and when OpenDKIM is not installed), so the panel
// still tracks keys for DNS during development.
func RebuildDKIM(domains []DKIMDomain) error {
	if !Linux() || !have("opendkim") {
		return nil
	}
	if err := os.MkdirAll(openDKIMKeysRoot, 0o750); err != nil {
		return err
	}
	var keyTable, signTable strings.Builder
	for _, d := range domains {
		if !validDomainNameSys(d.Domain) {
			continue
		}
		dir := filepath.Join(openDKIMKeysRoot, d.Domain)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		keyPath := filepath.Join(dir, d.Selector+".private")
		if err := os.WriteFile(keyPath, []byte(d.PrivatePEM), 0o640); err != nil {
			return err
		}
		fmt.Fprintf(&keyTable, "%s._domainkey.%s %s:%s:%s\n", d.Selector, d.Domain, d.Domain, d.Selector, keyPath)
		fmt.Fprintf(&signTable, "*@%s %s._domainkey.%s\n", d.Domain, d.Selector, d.Domain)
	}
	if err := os.WriteFile(openDKIMKeyTable, []byte(keyTable.String()), 0o640); err != nil {
		return err
	}
	if err := os.WriteFile(openDKIMSignTable, []byte(signTable.String()), 0o640); err != nil {
		return err
	}
	// OpenDKIM must be able to read the keys it signs with.
	run("chown", "-R", "opendkim:opendkim", openDKIMKeysRoot, openDKIMKeyTable, openDKIMSignTable)
	return ReloadService("opendkim")
}

// validDomainNameSys is a conservative check used before writing a domain into
// OpenDKIM paths/tables.
func validDomainNameSys(name string) bool {
	if name == "" || len(name) > 253 || strings.ContainsAny(name, "/ \t\n\\") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return false
		}
	}
	return strings.Contains(name, ".")
}
