// RePanel — open-source hosting control panel for Debian/Ubuntu.
package main

import (
	"crypto/tls"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/api"
	"github.com/reinis1996/repanel/internal/config"
	"github.com/reinis1996/repanel/internal/database"
	"github.com/reinis1996/repanel/internal/system"
	"github.com/reinis1996/repanel/web"
)

var version = "0.1.0"

func main() {
	// certbot invokes us as its DNS-01 auth/cleanup hook (see IssueLetsEncryptDNS).
	if len(os.Args) > 1 && os.Args[1] == "acme-hook" {
		runACMEHook(os.Args[2:])
		return
	}
	// Migration restore, run on a fresh host with the panel stopped.
	if len(os.Args) > 1 && os.Args[1] == "restore-config" {
		runRestoreConfig(os.Args[2:])
		return
	}

	configPath := flag.String("config", "/etc/repanel/repanel.conf", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := database.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	srv := api.New(cfg, db, version)
	srv.ConfigPath = *configPath // needed to build certbot DNS-01 hook commands
	// First-run hardening: open the stack's standard ports and enable ufw. Runs
	// once (guarded internally), in the background so it never delays startup.
	go func() {
		srv.SeedFirewall()        // open default stack ports + enable ufw (once)
		srv.EnsureNodeIsolation() // restrict Node app loopback ports to nginx/panel
	}()
	go srv.SyncAntiSpam()     // apply per-domain spam settings (no-op if rspamd absent)
	go srv.SyncMailDelivery() // wire up LMTP delivery + sieve/quota (no-op if sieve absent)
	mux := http.NewServeMux()
	srv.Routes(mux)
	mux.Handle("/", spaHandler())

	// Background housekeeping: session pruning + daily certificate renewal.
	go srv.CollectTraffic()  // seed traffic stats at startup, then hourly below
	go srv.CollectWebStats() // seed web statistics at startup, then hourly below
	go srv.CollectMetrics()  // seed the resource graph, then every 5 minutes below
	go func() {
		t := time.NewTicker(5 * time.Minute)
		for range t.C {
			srv.CollectMetrics()
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Hour)
		var lastRenew time.Time
		for range ticker.C {
			srv.Auth.PruneSessions()
			srv.PruneAudit()
			srv.CheckAlerts()
			srv.MaybeRunScheduledBackups()
			srv.CollectTraffic()
			srv.CollectWebStats()
			srv.EnforceBandwidthLimits()
			srv.SyncCloudflare()
			if time.Since(lastRenew) > 24*time.Hour {
				lastRenew = time.Now()
				ws := system.NewWebServer(cfg.WebServer, cfg.NginxDir, cfg.ApacheDir, cfg.ApachePort)
				if err := system.RenewCertificates(ws); err != nil {
					log.Printf("certificate renewal: %v", err)
				}
			}
		}
	}()

	handler := securityHeaders(mux)
	log.Printf("RePanel %s listening on %s", version, cfg.ListenAddr)

	certFile, keyFile := cfg.TLSCert, cfg.TLSKey
	if certFile == "" || keyFile == "" {
		// Self-provision a certificate for the panel UI on first start.
		cp := filepath.Join(cfg.DataDir, "panel-cert.pem")
		kp := filepath.Join(cfg.DataDir, "panel-key.pem")
		if _, err := os.Stat(cp); os.IsNotExist(err) {
			host, _ := os.Hostname()
			if host == "" {
				host = "repanel.local"
			}
			if c, k, _, err := system.IssueSelfSigned(cfg.DataDir, "panel-self"); err == nil {
				os.Rename(c, cp)
				os.Rename(k, kp)
				log.Printf("generated self-signed certificate for the panel UI (%s)", host)
			}
		}
		if _, err := os.Stat(cp); err == nil {
			certFile, keyFile = cp, kp
		}
	}

	// Install an nginx default catch-all so the bare server IP (or any host that
	// matches no domain/function) is dropped instead of being served whichever
	// tenant vhost nginx would otherwise pick as the default.
	defWS := system.NewWebServer(cfg.WebServer, cfg.NginxDir, cfg.ApacheDir, cfg.ApachePort)
	if err := defWS.WriteDefaultVhost(certFile, keyFile); err != nil {
		log.Printf("default vhost: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Suppress the benign per-connection "TLS handshake error" noise from
		// scanners / clients that reject the self-signed cert (real cert errors
		// surface at load time below).
		ErrorLog: log.New(tlsErrorFilter{os.Stderr}, "", log.LstdFlags),
	}
	if certFile != "" && keyFile != "" {
		if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
			log.Fatalf("load panel certificate: %v", err)
		}
		ln, err := net.Listen("tcp", cfg.ListenAddr)
		if err != nil {
			log.Fatalf("listen on %s: %v", cfg.ListenAddr, err)
		}
		// Serve the certificate through a reloader so a renewed Let's Encrypt cert
		// is picked up without a restart. Peek each connection: serve TLS as usual,
		// but answer a plain-HTTP request to this port with a redirect to https
		// instead of a failed handshake.
		reloader := system.NewCertReloader(certFile, keyFile)
		tlsLn := tls.NewListener(&httpsOnlyListener{Listener: ln}, &tls.Config{GetCertificate: reloader.GetCertificate})
		log.Fatal(httpSrv.Serve(tlsLn))
	}
	log.Println("WARNING: serving over plain HTTP — set TLS_CERT/TLS_KEY in repanel.conf")
	log.Fatal(httpSrv.ListenAndServe())
}

// spaHandler serves the embedded frontend, falling back to index.html for
// client-side routes.
func spaHandler() http.Handler {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatalf("embedded frontend missing: %v", err)
	}
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := dist.Open(p); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}

// runACMEHook is the certbot DNS-01 auth/cleanup hook. certbot sets CERTBOT_DOMAIN
// and CERTBOT_VALIDATION in the environment; we add or remove the _acme-challenge
// TXT record in the domain's managed BIND zone.
func runACMEHook(args []string) {
	fs := flag.NewFlagSet("acme-hook", flag.ExitOnError)
	action := fs.String("action", "auth", "auth | cleanup")
	configPath := fs.String("config", "/etc/repanel/repanel.conf", "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("acme-hook: load config: %v", err)
	}
	domain := os.Getenv("CERTBOT_DOMAIN")
	validation := os.Getenv("CERTBOT_VALIDATION")
	if domain == "" {
		log.Fatal("acme-hook: CERTBOT_DOMAIN not set")
	}
	if *action == "cleanup" {
		err = system.ACMEHookCleanup(cfg.BindDir, domain, validation)
	} else {
		err = system.ACMEHookAuth(cfg.BindDir, domain, validation)
	}
	if err != nil {
		log.Fatalf("acme-hook %s: %v", *action, err)
	}
}

// runRestoreConfig restores a server/migration backup onto this host. The panel
// must be stopped so the database file can be replaced safely.
func runRestoreConfig(args []string) {
	fs := flag.NewFlagSet("restore-config", flag.ExitOnError)
	archive := fs.String("archive", "", "path to the server backup archive")
	configPath := fs.String("config", "/etc/repanel/repanel.conf", "path to config file")
	fs.Parse(args)
	if *archive == "" {
		log.Fatal("restore-config: -archive is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("restore-config: load config: %v", err)
	}
	dbPath := filepath.Join(cfg.DataDir, "repanel.db")
	confDir := filepath.Dir(*configPath)
	certsDir := filepath.Join(cfg.DataDir, "certs")
	if err := system.RestoreServerBackup(*archive, dbPath, confDir, certsDir); err != nil {
		log.Fatalf("restore-config: %v", err)
	}
	log.Printf("restore-config: restored database, %s and certificates; start the panel to regenerate all service config", confDir)
}

func securityHeaders(next http.Handler) http.Handler {
	// The SPA ships its own JS/CSS from the same origin; Tailwind injects inline
	// styles, so style-src needs 'unsafe-inline'. Scripts stay strictly 'self'.
	const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
		"script-src 'self'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", csp)
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
