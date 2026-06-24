# RePanel

**RePanel** is a free, open-source hosting control panel for Debian and Ubuntu servers — an alternative to Plesk and DirectAdmin. One small Go binary with an embedded web UI manages the entire hosting stack: websites, DNS, mail, databases, FTP, SSL, cron and the firewall.

![status](https://img.shields.io/badge/status-alpha-orange) ![license](https://img.shields.io/badge/license-MIT-blue) ![go](https://img.shields.io/badge/go-1.26-00ADD8)

## Features

- **Websites & Domains** — per-domain vhosts with isolated PHP-FPM pools (each site runs as its own system user), selectable PHP version, suspend/unsuspend, and a choice of web server — nginx, Apache, or nginx in front of Apache — per website
- **Node.js apps** — switch any domain's runtime from PHP to Node.js: pick an installed Node version, set the startup file and environment variables, and the panel reverse-proxies the site to the app, runs it under the account's own system user, and gives you npm-install / start / stop / restart controls. A built-in Node version manager installs official Node releases side-by-side (mirroring the PHP version manager)
- **DNS** — authoritative zones served by BIND with a full record editor (A, AAAA, CNAME, MX, TXT, NS, SRV, CAA) and sane default zone templates; optional secondary DNS servers receive zone transfers (AXFR) and change notifications automatically; **DNSSEC** inline-signing per zone (BIND `dnssec-policy`, with the DS records to publish at your registrar surfaced in the UI); and optional **Cloudflare sync** — bind a zone to Cloudflare and push RePanel's records up on every change (or pull Cloudflare's records down) automatically
- **Mail** — virtual mailboxes and aliases on Postfix + Dovecot (IMAP/POP3/SMTP auth), one-click DKIM signing (OpenDKIM) with DMARC/SPF records published automatically into managed zones, and optional **spam filtering + antivirus** (rspamd + ClamAV) with a per-domain on/off toggle. Delivery runs through Dovecot LMTP, enabling:
  - **Enforced per-mailbox quotas** (Dovecot quota plugin)
  - **Forwarders** with optional keep-a-copy, multi-destination targets, and **catch-all** addresses
  - **Distribution lists** — a list address that fans out to its members
  - **Autoresponders / vacation** messages and per-mailbox **Sieve filters** (move/forward/discard rules), managed from the panel
  - **Outbound smarthost relay** — send through an external SMTP provider (SendGrid, Mailgun, …) with SASL auth
  - **IMAP migration** — import existing mailboxes from another server with imapsync, as tracked background jobs
- **Webmail** — opt-in Roundcube webmail served at `webmail.<domain>`, enabled per domain from the Mail page (with the `webmail` DNS record published into managed zones)
- **Databases** — MariaDB and (optional) PostgreSQL databases with a dedicated user per database, live size reporting, and an optional **web database admin** (Adminer) served at `dbadmin.<panel-host>` and linked per database
- **File Manager** — browse, upload, download, edit, rename and delete inside a jailed web space
- **FTP** — ProFTPD accounts jailed to a chosen directory
- **SSL/TLS** — one-click Let's Encrypt (HTTP-01) or self-signed certificates with automatic nightly renewal, plus **wildcard certificates** via DNS-01 (the `_acme-challenge` record is published automatically into the domain's RePanel-hosted BIND zone, and `certbot renew` reuses the same hook), **custom certificate upload** (paste your own cert + key), **assigning a certificate to mail (Postfix/Dovecot), FTP (ProFTPD) or the panel itself**, and a **dedicated control-panel certificate** flow — secure the panel's own HTTPS port for its hostname directly from the UI (Let's Encrypt, custom upload, or self-signed); renewed certificates are picked up without a restart
- **One-click apps** — install popular web apps into any domain — WordPress, Drupal, Nextcloud, Matomo and Grav: RePanel downloads the app, provisions a dedicated database when one is needed, and finishes setup. WordPress is fully automatic (writes `wp-config.php` and, with WP-CLI, creates the admin account); the others are completed in their browser installer with the database credentials the panel created
- **WordPress Workbench** — manage installed sites via WP-CLI (as the site's own system user): update core/plugins/themes individually or all at once, toggle per-item auto-updates, manage WP users with one-click admin login and password resets, run maintenance ops (cache/transient/permalink flush, WP-cron, core checksum verification, salt regeneration), and handle the database & config (search-replace for migrations, optimize, SQL export, `wp-config` constants)
- **Web Application Firewall** — per-domain ModSecurity with the OWASP Core Rule Set, installed on demand for the active web server. Owners enable it per site and choose blocking or detection-only mode; admins can add custom rules and exclusions. Config is regenerated from panel state (surviving SSL/PHP/suspend rebuilds) and validated before reload, with automatic rollback if rejected
- **Per-site config editor** — admins can add custom directives per domain for nginx (`server {}`), Apache (`<VirtualHost>`) and the PHP-FPM pool. Overrides are stored in the panel database and merged into the generated config on every rebuild (so they survive SSL/PHP/suspend regeneration), validated with `nginx -t` / `apachectl -t` / `php-fpm -t` before reload, and rolled back automatically if rejected. Includes a read-only view of the fully rendered config (and the domain's effective mail map entries)
- **Scheduled tasks** — cron jobs that run as the customer's system user
- **Backups** — on-demand and nightly per-account archives (web files + mail + database dumps in plain tar.gz), with download and retention. **Offsite destinations** via rclone (S3, Backblaze B2, SFTP, FTP, plus Dropbox/Google Drive and others by pasting an rclone remote) — completed backups upload automatically with per-remote retention. **Selective restore** — whole archive, individual components (web / a specific database / a specific mailbox domain), or a single web file. **Server / migration backup** — download the panel database + `/etc/repanel` + certificates and restore on a fresh host with `repanel restore-config` to migrate the whole server
- **Usage & quotas** — live disk usage per account (web/mail/databases) with optional disk quotas enforced on uploads, mailboxes and databases; **monthly bandwidth quotas** that auto-suspend an account's sites when exceeded and restore them when usage drops (e.g. at the next month); per-account **count limits** (max domains / mailboxes / databases); and live **resource caps** via systemd cgroups (CPU, memory and process limits applied to the account's Node apps and shell sessions)
- **Traffic accounting** — per-account and per-domain bandwidth, tallied incrementally from the nginx access logs with a daily history chart
- **Web statistics** — AWStats-style per-site analytics (unique visitors, pageviews, hits, bandwidth, top pages, top referrers and status-code breakdown) parsed natively from each domain's access log every hour — no Perl, cron jobs or extra packages, with a daily chart and 90-day history
- **Users & resellers** — admin / reseller / user roles; resellers manage their own customers; per-account **module grants** (gate which feature areas — DNS, Mail, Databases, …— each account can reach); and reusable **hosting plans** that bundle resource limits and grantable modules into a named template you assign on account create/edit (with per-account overrides still available)
- **Services & firewall** — systemd service control and ufw management from the UI, plus one-click installation of additional PHP-FPM versions (from the distro's multi-version PHP repo) so each site can run a different PHP, plus **OS package updates** — list upgradable packages (flagging security updates) and apply them as a background job with live apt output
- **Web terminal** — admin-only browser shell (xterm.js over a WebSocket to a PTY) on the host, with every session recorded in the audit log
- **Panel self-update** — admins see the current vs latest GitHub release and apply the update (binaries replaced and the service restarted) from the UI
- **Monitoring & alerting** — historical CPU / memory / disk graphs (sampled every 5 minutes) and a daily-traffic chart on the dashboard; per-service status and recent journal logs to diagnose why something is down; a tail viewer for the host's key logs (nginx, mail, fail2ban, auth, syslog); and email/webhook **alerts** for disk-full, a service going down, certificate expiry and backup failures (de-duplicated — sent only when a condition newly fires)
- **API tokens** — personal access tokens for the REST API (`Authorization: Bearer …`) with optional expiry, scoped to the issuing account's role
- **Security** — bcrypt passwords, HttpOnly+SameSite session cookies, HTTPS out of the box, per-site open_basedir, path-jailed file operations. Plus:
  - **Two-factor authentication** (TOTP) for panel logins, with QR enrollment and one-time recovery codes
  - **Login as customer** — admins (and resellers, for their own customers) can impersonate an account, with a clear banner and one-click return
  - **fail2ban management** — view jails and banned IPs, ban/unban addresses, and maintain a never-ban whitelist from the UI
  - **Audit log** — every authenticated mutation plus login and impersonation events, searchable and retained for 180 days
  - **Per-account SSH access** — opt-in shell login with managed `authorized_keys`, off by default

## Installation

On a **fresh** Debian 12+ / Ubuntu 22.04+ server, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh | sh
```

Then open `https://<server-ip>:8443` and create the administrator account. The installer sets up nginx, PHP-FPM, MariaDB, BIND, Postfix, Dovecot, ProFTPD, certbot, ufw and fail2ban, and wires them all to the panel. It also installs the `repctl` command-line client.

> The panel serves its UI over HTTPS with a self-signed certificate by default; point `TLS_CERT` / `TLS_KEY` in `/etc/repanel/repanel.conf` at a real certificate to remove the browser warning. Until then sessions on that host transit a self-signed channel — use a real certificate before exposing the panel publicly.

> **Verifying the download.** Each release publishes a `repanel-linux-<arch>.sha256` next to the binary. To audit before running, fetch and check it rather than piping the installer straight into a shell:
> ```sh
> ARCH=amd64
> base=https://github.com/reinis1996/repanel/releases/latest/download
> curl -fsSLO "$base/repanel-linux-$ARCH" -O "$base/repanel-linux-$ARCH.sha256"
> sha256sum -c "repanel-linux-$ARCH.sha256"
> ```

### Installer options

The installer is configured with environment variables passed on the same line.
They all have safe defaults — the bare command above installs the standard nginx
stack — and they combine freely:

```sh
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh \
  | WEB_SERVER=nginx-apache WITH_POSTGRES=1 WITH_WEBMAIL=1 WITH_ANTISPAM=1 sh
```

| Variable | Default | Values | What it does |
|---|---|---|---|
| `WEB_SERVER` | `nginx` | `nginx` · `apache` · `nginx-apache` | Web server stack. `nginx` serves every site directly; `apache` makes Apache own `:80`/`:443`; `nginx-apache` puts nginx in front and lets each site pick nginx-only, Apache-only or nginx+Apache from the panel. |
| `APACHE_PORT` | `8080` | port number | Loopback port Apache listens on as the backend in the `nginx-apache` stack (ignored otherwise). |
| `WITH_POSTGRES` | `0` | `0` · `1` | Also install PostgreSQL alongside MariaDB, so customers can create Postgres databases. The panel auto-detects it, so you can `apt install postgresql` later instead. |
| `WITH_WEBMAIL` | `0` | `0` · `1` | Install Roundcube webmail (SQLite store) served at `webmail.<domain>` for opted-in domains. Auto-detected, so `apt install roundcube` later also works. |
| `WITH_ANTISPAM` | `0` | `0` · `1` | Install rspamd (spam filtering) + ClamAV (virus scanning) and wire them into Postfix; spam filtering is then toggled per domain on the Mail page. Can also be installed later from the panel. |

The sections below walk through each option with full examples. The panel listens
on `:8443`; paths and other runtime settings (which are *not* installer flags) are
configured afterwards in `/etc/repanel/repanel.conf` — see [Configuration](#configuration).

### Choosing a web server

By default RePanel installs **nginx** and serves every site from it. To use
**Apache** instead, or to run **nginx in front of Apache** (so each website can
be set to nginx-only, Apache-only, or nginx+Apache from the panel), set
`WEB_SERVER` when installing:

```sh
# Apache only
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh | WEB_SERVER=apache sh

# nginx fronting Apache — per-site choice in the panel (Apache backend on :8080)
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh | WEB_SERVER=nginx-apache sh
```

In the `nginx-apache` stack the **Web** column on the Websites page lets you
switch each domain between *nginx* (served directly via PHP-FPM), *Apache*
(nginx reverse-proxies everything to Apache) and *nginx → Apache* (nginx serves
static files and proxies PHP to Apache). PHP always runs in the site's isolated
FPM pool, so behaviour is identical whichever server is in front.

To install PostgreSQL alongside MariaDB, run the installer with `WITH_POSTGRES=1`
(or `apt install postgresql` later — the panel detects it automatically):

```sh
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh | WITH_POSTGRES=1 sh
```

To install Roundcube webmail, run the installer with `WITH_WEBMAIL=1` (or
`apt install roundcube` later — the panel detects it automatically and serves it
at `webmail.<domain>` for opted-in domains):

```sh
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh | WITH_WEBMAIL=1 sh
```

To install spam filtering and antivirus, run the installer with `WITH_ANTISPAM=1`
(rspamd + ClamAV); spam filtering is then toggled per domain from the Mail page:

```sh
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh | WITH_ANTISPAM=1 sh
```

(Flags combine, e.g. `WITH_POSTGRES=1 WITH_WEBMAIL=1 WITH_ANTISPAM=1`.) Node.js
versions are installed on demand from the panel's Node version manager, and
Adminer is installed on demand when an admin enables the web database admin.

## Building from source

Requires Go ≥ 1.26 and Node ≥ 20.

```sh
git clone https://github.com/reinis1996/repanel
cd repanel
make build          # builds web UI + linux binary into ./dist/
```

For development:

```sh
# terminal 1 — API (uses ./dev.conf so nothing touches system paths)
printf 'LISTEN=127.0.0.1:8099\nDATA_DIR=./.devdata\n' > dev.conf
go run ./cmd/repanel -config dev.conf

# terminal 2 — UI with hot reload, proxies /api to :8099
cd web && npm install && npm run dev
```

## Architecture

```
cmd/repanel/        entry point, HTTP server, SPA serving
internal/api/       REST API handlers (one file per module)
internal/auth/      bcrypt + cookie sessions
internal/database/  embedded SQLite (panel state mirror)
internal/system/    host integration: nginx, BIND, Postfix/Dovecot,
                    MariaDB, ProFTPD, certbot, cron, ufw, systemd, /proc
web/                React + TypeScript + Tailwind UI (embedded in the binary)
scripts/            installer + systemd unit
```

Design principles:

- **The database is a mirror, not the source of truth the services read.** RePanel stores desired state in SQLite and *generates* native config (nginx vhosts, BIND zone files, postfix maps, cron.d entries) from it. Any file can be rebuilt at any time, and the panel never edits files it didn't create.
- **Graceful degradation.** Every integration detects whether its service is installed; missing components simply show as "not installed" instead of breaking the panel.
- **Customer isolation.** Each panel user gets a locked system account; their sites' PHP pools, cron jobs and FTP logins run under it, with `open_basedir` and path jails on top.

## Configuration

`/etc/repanel/repanel.conf` (KEY=VALUE):

| Key | Default | Purpose |
|---|---|---|
| `LISTEN` | `:8443` | panel listen address |
| `DATA_DIR` | `/var/lib/repanel` | SQLite db, issued certificates |
| `WEB_ROOT` | `/var/www` | base directory for customer sites |
| `NGINX_DIR` | `/etc/nginx` | nginx config root |
| `APACHE_DIR` | `/etc/apache2` | Apache config root |
| `WEB_SERVER` | `nginx` | web server stack: `nginx`, `apache` or `nginx-apache` |
| `APACHE_PORT` | `8080` | Apache backend port (nginx-apache stack) |
| `BIND_DIR` | `/etc/bind` | BIND config root |
| `MAIL_DIR` | `/etc/repanel/mail` | generated postfix/dovecot maps |
| `TLS_CERT` / `TLS_KEY` | *(self-signed)* | panel UI certificate |
| `SESSION_HOURS` | `24` | session lifetime |

## Roadmap

- [x] Backups (scheduled, per-account, restore from UI)
- [x] Disk usage statistics & per-account disk quotas
- [x] Traffic accounting per account
- [x] Webmail (Roundcube, served at webmail.&lt;domain&gt;)
- [x] One-click apps (WordPress installer)
- [x] DKIM/DMARC management (OpenDKIM signing + DNS records)
- [x] Apache as an alternative web server (per-site nginx / Apache / nginx+Apache)
- [x] PostgreSQL support (alongside MariaDB)
- [x] Secondary / slave DNS (zone transfers + NOTIFY to secondary nameservers)
- [x] DNSSEC (BIND inline-signing per zone)
- [x] Cloudflare DNS sync (push/pull a zone to Cloudflare)
- [x] Node.js apps (per-domain Node runtime + version manager)
- [x] More one-click apps (Drupal, Nextcloud, Matomo, Grav)
- [x] Spam filtering & antivirus (rspamd + ClamAV)
- [x] Web database admin (Adminer)
- [x] Hosting plans, per-account resource/bandwidth quotas & cgroup limits
- [x] Panel self-update + OS package updates
- [x] API tokens (personal access tokens for the REST API)
- [x] CLI client (`repctl`)
- [ ] Localization

## Command-line client

`repctl` is a small companion binary that drives the same REST API from a
terminal or scripts, authenticating with a personal API token (create one under
**API Tokens** in the panel).

```sh
# point it at your panel once (token from the API Tokens page)
repctl --url https://panel.example.com:8443 --token rpat_… login

# then:
repctl whoami
repctl domains list
repctl domains create example.com --php 8.3 --dns
repctl backups list
repctl api GET /api/dashboard          # raw access to any endpoint
```

Configuration is taken from flags, then the `REPANEL_URL` / `REPANEL_TOKEN`
environment variables, then the saved config file. Pass `--insecure` for panels
using the default self-signed certificate, and `--json` for machine-readable
output.

## Contributing

Issues and pull requests are welcome. Run `go vet ./...` and `npm run build` before submitting.

## License

[MIT](LICENSE) — free for everyone, forever.
