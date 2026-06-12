# RePanel

**RePanel** is a free, open-source hosting control panel for Debian and Ubuntu servers — an alternative to Plesk and DirectAdmin. One small Go binary with an embedded web UI manages the entire hosting stack: websites, DNS, mail, databases, FTP, SSL, cron and the firewall.

![status](https://img.shields.io/badge/status-alpha-orange) ![license](https://img.shields.io/badge/license-MIT-blue) ![go](https://img.shields.io/badge/go-1.26-00ADD8)

## Features

- **Websites & Domains** — per-domain nginx vhosts with isolated PHP-FPM pools (each site runs as its own system user), selectable PHP version, suspend/unsuspend
- **DNS** — authoritative zones served by BIND with a full record editor (A, AAAA, CNAME, MX, TXT, NS, SRV, CAA) and sane default zone templates
- **Mail** — virtual mailboxes and aliases on Postfix + Dovecot (IMAP/POP3/SMTP auth), per-mailbox quotas
- **Databases** — MariaDB databases with a dedicated user per database, live size reporting
- **File Manager** — browse, upload, download, edit, rename and delete inside a jailed web space
- **FTP** — ProFTPD accounts jailed to a chosen directory
- **SSL/TLS** — one-click Let's Encrypt (with automatic nightly renewal) or self-signed certificates
- **Scheduled tasks** — cron jobs that run as the customer's system user
- **Users & resellers** — admin / reseller / user roles; resellers manage their own customers
- **Services & firewall** — systemd service control and ufw management from the UI
- **Security** — bcrypt passwords, HttpOnly+SameSite session cookies, HTTPS out of the box, per-site open_basedir, path-jailed file operations

## Installation

On a **fresh** Debian 12+ / Ubuntu 22.04+ server, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/main/scripts/install.sh | sh
```

Then open `https://<server-ip>:8443` and create the administrator account. The installer sets up nginx, PHP-FPM, MariaDB, BIND, Postfix, Dovecot, ProFTPD, certbot, ufw and fail2ban, and wires them all to the panel.

> The panel serves its UI over HTTPS with a self-signed certificate by default; point `TLS_CERT` / `TLS_KEY` in `/etc/repanel/repanel.conf` at a real certificate to remove the browser warning.

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
| `BIND_DIR` | `/etc/bind` | BIND config root |
| `MAIL_DIR` | `/etc/repanel/mail` | generated postfix/dovecot maps |
| `TLS_CERT` / `TLS_KEY` | *(self-signed)* | panel UI certificate |
| `SESSION_HOURS` | `24` | session lifetime |

## Roadmap

- [ ] Backups (scheduled, per-account, restore from UI)
- [ ] Usage statistics & per-account resource quotas (disk/traffic)
- [ ] Webmail (Roundcube one-click install)
- [ ] One-click apps (WordPress installer)
- [ ] DKIM/DMARC management & rspamd integration
- [ ] Apache as an alternative web server
- [ ] PostgreSQL support
- [ ] Multi-server / slave DNS
- [ ] API tokens & CLI client
- [ ] Localization

## Contributing

Issues and pull requests are welcome. Run `go vet ./...` and `npm run build` before submitting.

## License

[MIT](LICENSE) — free for everyone, forever.
