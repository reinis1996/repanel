package system

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	return IssueLetsEncryptHosts(docroot, email, domain, "www."+domain)
}

// IssueLetsEncryptHosts obtains a certificate covering one or more hostnames in
// webroot mode. The first host names the certificate (its live directory). Used
// for domains (apex + www) and for function URLs (a single host, no www).
func IssueLetsEncryptHosts(docroot, email string, hosts ...string) (certPath, keyPath string, err error) {
	if !have("certbot") {
		return "", "", fmt.Errorf("certbot is not installed (apt install certbot)")
	}
	if len(hosts) == 0 {
		return "", "", fmt.Errorf("no hostnames given")
	}
	args := []string{"certonly", "--webroot", "-w", docroot}
	for _, h := range hosts {
		args = append(args, "-d", h)
	}
	args = append(args, "--non-interactive", "--agree-tos", "--keep-until-expiring")
	if email != "" {
		args = append(args, "-m", email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	if _, err := run("certbot", args...); err != nil {
		return "", "", err
	}
	live := filepath.Join("/etc/letsencrypt/live", hosts[0])
	certPath = filepath.Join(live, "fullchain.pem")
	keyPath = filepath.Join(live, "privkey.pem")
	if _, err := os.Stat(certPath); err != nil {
		return "", "", fmt.Errorf("certbot reported success but %s is missing", certPath)
	}
	return certPath, keyPath, nil
}

// IssueLetsEncryptDNS obtains a wildcard certificate (*.domain + domain) using
// the DNS-01 challenge, automated through the panel's own BIND zone. certbot runs
// in manual mode and calls this binary back as the auth/cleanup hook; the hook
// commands are persisted by certbot so `certbot renew` reuses them. selfBin is the
// panel executable and configPath its config file (so the hook can find BindDir).
func IssueLetsEncryptDNS(selfBin, configPath, email, domain string) (certPath, keyPath string, err error) {
	if !have("certbot") {
		return "", "", fmt.Errorf("certbot is not installed (apt install certbot)")
	}
	// certbot runs each hook through a shell, so the whole command is one argv
	// element; single-quote the paths for that shell.
	auth := fmt.Sprintf("'%s' acme-hook -action auth -config '%s'", selfBin, configPath)
	cleanup := fmt.Sprintf("'%s' acme-hook -action cleanup -config '%s'", selfBin, configPath)
	args := []string{
		"certonly", "--manual", "--preferred-challenges", "dns",
		"--manual-auth-hook", auth, "--manual-cleanup-hook", cleanup,
		"--cert-name", domain, "-d", "*." + domain, "-d", domain,
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

// SaveCustomCert validates a user-supplied certificate/key pair (they must parse
// and match), writes them under the domain's cert directory, and returns the
// stored paths plus the certificate's expiry.
func SaveCustomCert(dataDir, domain, certPEM, keyPEM string) (certPath, keyPath string, notAfter time.Time, err error) {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("certificate and key are invalid or do not match: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("parse certificate: %w", err)
	}
	dir := CertDir(dataDir, domain)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certPath, []byte(ensureTrailingNewline(certPEM)), 0o644); err != nil {
		return
	}
	if err = os.WriteFile(keyPath, []byte(ensureTrailingNewline(keyPEM)), 0o600); err != nil {
		return
	}
	return certPath, keyPath, leaf.NotAfter, nil
}

func ensureTrailingNewline(s string) string {
	if !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

// ApplyMailCert points Postfix (SMTP) and Dovecot (IMAP/POP3) at a certificate
// and reloads them.
func ApplyMailCert(cert, key string) error {
	if have("postconf") {
		run("postconf", "-e", "smtpd_tls_cert_file = "+cert)
		run("postconf", "-e", "smtpd_tls_key_file = "+key)
		run("postconf", "-e", "smtpd_tls_security_level = may")
		ReloadService("postfix")
	}
	// Dovecot: a panel-owned snippet wins over the distro default (loaded later).
	conf := fmt.Sprintf("# Managed by RePanel — mail TLS certificate.\nssl = yes\nssl_cert = <%s\nssl_key = <%s\n", cert, key)
	if err := os.WriteFile("/etc/dovecot/conf.d/99-repanel-ssl.conf", []byte(conf), 0o644); err != nil {
		return err
	}
	return ReloadService("dovecot")
}

// ApplyFTPCert configures ProFTPD's TLS module to use a certificate and reloads it.
func ApplyFTPCert(cert, key string) error {
	conf := fmt.Sprintf(`# Managed by RePanel — FTP TLS certificate.
<IfModule mod_tls.c>
  TLSEngine on
  TLSProtocol TLSv1.2 TLSv1.3
  TLSRSACertificateFile %s
  TLSRSACertificateKeyFile %s
  TLSOptions NoSessionReuseRequired
</IfModule>
`, cert, key)
	if err := os.MkdirAll("/etc/proftpd/conf.d", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/proftpd/conf.d/repanel-tls.conf", []byte(conf), 0o644); err != nil {
		return err
	}
	return ReloadService("proftpd")
}

// ApplyPanelCert points the panel's own HTTPS listener at a certificate by setting
// TLS_CERT/TLS_KEY in the config file. The caller restarts the panel to apply it.
func ApplyPanelCert(configPath, cert, key string) error {
	return setConfigValues(configPath, map[string]string{"TLS_CERT": cert, "TLS_KEY": key})
}

// PanelACMEWebroot is the directory the control panel's own ACME HTTP-01
// challenge is served from, so Let's Encrypt can validate the panel hostname
// even when it isn't a customer website.
func PanelACMEWebroot() string { return "/usr/share/repanel/acme" }

// WritePanelACMEVhost writes (host=="" removes) a tiny nginx vhost that answers
// the ACME HTTP-01 challenge for the panel hostname on port 80, then reloads
// nginx. It persists so `certbot renew` keeps working. nginx fronts only.
func (ws *WebServer) WritePanelACMEVhost(host string) error {
	confPath := filepath.Join(nginxConfDir(ws.NginxDir), "panel-acme.conf")
	if host == "" {
		os.Remove(confPath)
		return reloadNginx()
	}
	if err := os.MkdirAll(PanelACMEWebroot(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(nginxConfDir(ws.NginxDir), 0o755); err != nil {
		return err
	}
	conf := fmt.Sprintf(`# Managed by RePanel — Let's Encrypt HTTP-01 for the control panel.
server {
    listen 80;
    listen [::]:80;
    server_name %s;
    location /.well-known/acme-challenge/ { root %s; }
    location / { return 404; }
}
`, host, PanelACMEWebroot())
	if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
		return err
	}
	return reloadNginx()
}

// CertReloader serves the panel's TLS certificate, reloading it from disk when
// the file changes — so a renewed Let's Encrypt certificate is picked up without
// restarting the panel. Safe for concurrent use.
type CertReloader struct {
	certPath, keyPath string
	mu                sync.RWMutex
	cached            *tls.Certificate
	modTime           time.Time
}

// NewCertReloader returns a reloader for the given certificate/key paths.
func NewCertReloader(certPath, keyPath string) *CertReloader {
	return &CertReloader{certPath: certPath, keyPath: keyPath}
}

// GetCertificate is a tls.Config.GetCertificate callback: it returns the cached
// certificate, reloading from disk whenever the file's modification time changes.
func (c *CertReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	fi, statErr := os.Stat(c.certPath)
	if statErr == nil {
		c.mu.RLock()
		cached, mod := c.cached, c.modTime
		c.mu.RUnlock()
		if cached != nil && fi.ModTime().Equal(mod) {
			return cached, nil
		}
	}
	cert, err := tls.LoadX509KeyPair(c.certPath, c.keyPath)
	if err != nil {
		c.mu.RLock()
		defer c.mu.RUnlock()
		if c.cached != nil {
			return c.cached, nil // keep serving the last good certificate
		}
		return nil, err
	}
	c.mu.Lock()
	c.cached = &cert
	if fi != nil {
		c.modTime = fi.ModTime()
	}
	c.mu.Unlock()
	return &cert, nil
}

// setConfigValues updates (or appends) KEY=VALUE lines in a repanel.conf-style
// file, leaving everything else untouched.
func setConfigValues(path string, kv map[string]string) error {
	existing, _ := os.ReadFile(path)
	seen := map[string]bool{}
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(string(existing)))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if key, _, ok := strings.Cut(trimmed, "="); ok && !strings.HasPrefix(trimmed, "#") {
			if v, found := kv[strings.TrimSpace(key)]; found {
				fmt.Fprintf(&out, "%s=%s\n", strings.TrimSpace(key), v)
				seen[strings.TrimSpace(key)] = true
				continue
			}
		}
		out.WriteString(line + "\n")
	}
	for k, v := range kv {
		if !seen[k] {
			fmt.Fprintf(&out, "%s=%s\n", k, v)
		}
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
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

// RenewCertificates runs certbot renew; safe to call from a daily ticker. On
// success it reloads whichever web servers the stack runs so renewed
// certificates take effect.
func RenewCertificates(ws *WebServer) error {
	if !have("certbot") {
		return nil
	}
	_, err := run("certbot", "renew", "--quiet")
	if err == nil && ws != nil {
		ws.reloadAll()
		// Services may use a renewed certificate too; reloading is harmless when
		// they don't. Best-effort.
		ReloadService("postfix")
		ReloadService("dovecot")
		ReloadService("proftpd")
	}
	return err
}
