package system

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// CertDir is where issued certificates live, one subdir per domain.
func CertDir(dataDir, domain string) string {
	return filepath.Join(dataDir, "certs", domain)
}

// IssueLetsEncrypt obtains a certificate with certbot in webroot mode (the
// vhost template always serves /.well-known/acme-challenge from the docroot).
// Returns cert/key paths on success.
func IssueLetsEncrypt(dataDir, domain, docroot, email string) (certPath, keyPath string, err error) {
	if !have("certbot") {
		return "", "", fmt.Errorf("certbot is not installed (apt install certbot)")
	}
	args := []string{
		"certonly", "--webroot", "-w", docroot,
		"-d", domain, "-d", "www." + domain,
		"--non-interactive", "--agree-tos", "--keep-until-expiring",
	}
	if email != "" {
		args = append(args, "-m", email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	if _, err := run("certbot", args...); err != nil {
		return "", "", err
	}
	live := filepath.Join("/etc/letsencrypt/live", domain)
	certPath = filepath.Join(live, "fullchain.pem")
	keyPath = filepath.Join(live, "privkey.pem")
	if _, err := os.Stat(certPath); err != nil {
		return "", "", fmt.Errorf("certbot reported success but %s is missing", certPath)
	}
	return certPath, keyPath, nil
}

// IssueSelfSigned generates a 1-year self-signed certificate, used as a
// fallback and for the panel's own HTTPS endpoint.
func IssueSelfSigned(dataDir, domain string) (certPath, keyPath string, notAfter time.Time, err error) {
	dir := CertDir(dataDir, domain)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	notAfter = time.Now().AddDate(1, 0, 0)
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: domain, Organization: []string{"RePanel self-signed"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{domain, "www." + domain},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	defer certOut.Close()
	if err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return
	}
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	defer keyOut.Close()
	err = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return
}

// CertExpiry parses NotAfter from a PEM certificate file.
func CertExpiry(certPath string) (time.Time, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM data in %s", certPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

// RenewCertificates runs certbot renew; safe to call from a daily ticker.
func RenewCertificates() error {
	if !have("certbot") {
		return nil
	}
	_, err := run("certbot", "renew", "--quiet")
	if err == nil {
		ReloadService("nginx")
	}
	return err
}
