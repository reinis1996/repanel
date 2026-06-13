// RePanel — open-source hosting control panel for Debian/Ubuntu.
package main

import (
	"flag"
	"io/fs"
	"log"
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
	mux := http.NewServeMux()
	srv.Routes(mux)
	mux.Handle("/", spaHandler())

	// Background housekeeping: session pruning + daily certificate renewal.
	go srv.CollectTraffic() // seed traffic stats at startup, then hourly below
	go func() {
		ticker := time.NewTicker(time.Hour)
		var lastRenew time.Time
		for range ticker.C {
			srv.Auth.PruneSessions()
			srv.MaybeRunScheduledBackups()
			srv.CollectTraffic()
			if time.Since(lastRenew) > 24*time.Hour {
				lastRenew = time.Now()
				if err := system.RenewCertificates(); err != nil {
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

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if certFile != "" && keyFile != "" {
		log.Fatal(httpSrv.ListenAndServeTLS(certFile, keyFile))
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

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
