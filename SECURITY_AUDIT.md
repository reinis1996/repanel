# RePanel — Security Audit Report

**Date:** 2026-06-14
**Scope:** Entire repository (Go backend, embedded React UI, installer, CI/CD, system integrations)
**Assessed against attacker model:** network access + a *normal* (non-admin) panel account + arbitrary HTTP requests + full source knowledge
**Auditor role:** AppSec / pentest / multi-tenant isolation review

> ⚠️ This is a self-assessment of code that is **not yet committed/deployed**. Several findings are exploitable today and should block any production / multi-tenant deployment until fixed. Nothing here has been validated on a live Linux host — severities are based on code review.

---

## 0. Remediation Status — updated 2026-06-14 (branch `security-hardening`)

✅ Fixed · 🟡 Partial · ⚪ Accepted (by design / documented)

| ID | Severity | Status | Where fixed |
|----|----------|--------|-------------|
| F-01 | Critical | ✅ Fixed | `unixuser.go` (`CreateUnixUser`, `managedAccount` uid≥1000 guard on set-password/delete); `ftp.go` namespaces FTP accounts as `<sysUser>-<label>` |
| F-02 | Critical | ✅ Fixed | `files.go` `ResolveJailed` now resolves symlinks and re-verifies containment (residual TOCTOU noted) |
| F-03 | Critical | ✅ Fixed | `databases.go` reserved-name + cross-tenant ownership checks; `mysql.go`/`postgres.go` refuse to adopt/re-password existing users/roles |
| F-04 | High | ✅ Fixed | `unixuser.go` `SysUserName(userID)` → unique `rpu<id>`; all callers updated |
| F-05 | High | ✅ Fixed | Same symlink-safe `ResolveJailed` as F-02 + `maxRestoreBytes` cap |
| F-06 | High | ✅ Fixed | `mail.go` `validMailLocalPart` (no `..`, no `/`, no traversal) on mailbox + alias |
| F-07 | Medium | 🟡 Partial | `domains.go` cross-tenant sub/parent overlap check added; **external domain-ownership verification still recommended** |
| F-08 | Medium | ✅ Fixed | `ratelimit.go` per-IP login limiter wired into `handleLogin` |
| F-09 | Medium | ✅ Fixed | `main.go` `securityHeaders` adds CSP + HSTS |
| F-10 | Medium | ✅ Fixed | `backup.go` `maxRestoreBytes` total cap + `io.LimitReader` |
| F-11 | Medium | ⚪ Accepted | DB admin via socket/sudo is by-design; documented, escaping verified. Least-priv admin = long-term |
| F-12 | Low | ✅ Fixed | `cron.go` schedule rejects `\n`/`\r`/`\t`; literal-space field regex |
| F-13 | Low | ✅ Fixed | `bind.go` `formatTXT` always escapes/quotes (no passthrough) |
| F-14 | Low | ✅ Fixed | `wordpress.go` admin password via STDIN (`--prompt=admin_password`) |
| F-15 | Medium | ✅ Fixed | `server.go` `fail` logs full error + returns generic message with ref id |
| F-16 | Low | ✅ Fixed | `auth_handlers.go` setup serialized by `setupMu` |
| F-17 | Low | ✅ Fixed | Sessions revoked on self password change (keep current) and admin reset (all) |
| F-18 | Medium | ✅ Fixed | `scope` column + `RequestReadOnly` enforcement (read-only tokens → GET/HEAD/OPTIONS only); UI selector |
| F-19 | Low | 🟡 Partial | README download-verification added; **Action SHA-pinning recommended in workflow comment, not yet applied** |

**Net result:** all Critical/High findings fixed; all Medium except F-07 (partial) and F-11 (accepted) fixed; all Low except F-19 (partial) fixed. Regression tests added in `internal/system/security_fixes_test.go` and `internal/api/security_fixes_test.go`. `go build`/`vet`/`test` and `npm run build` all pass.

Residual items before a clean production sign-off: external domain-ownership proof (F-07), Action SHA-pinning (F-19), and the long-term hardening in §6 (drop-root file I/O to fully close F-02 TOCTOU, MFA, least-privilege DB admin).

---

## 1. Executive Summary

RePanel is a single-binary, root-running hosting control panel. The code is generally clean: **SQL is parameterized everywhere** (no app-level SQLi), passwords use **bcrypt**, sessions use **HttpOnly + SameSite=Strict + Secure** cookies, API tokens are **SHA-256 hashed at rest**, most resources are tenant-scoped via `scopeWhere`, and tar extraction / DNS / cron generators have *some* injection guards.

However, because **the panel runs as root and brokers shared OS resources (unix users, MySQL/Postgres superuser, the filesystem) on behalf of untrusted tenants**, several places trust user input that crosses a tenant or privilege boundary. The result is **multiple critical privilege-escalation and cross-tenant paths** that let a normal customer become **host root** or read/modify **another customer's files, databases, and mail**.

The highest-impact issues:

| # | Title | Severity |
|---|-------|----------|
| F-01 | FTP account creation resets the password of **any existing system user (incl. `root`)** | **Critical** |
| F-02 | File manager **symlink traversal → arbitrary file read/write as root** | **Critical** |
| F-03 | **Cross-tenant database takeover** via shared DB user / role password reset | **Critical / High** |
| F-04 | **System-user name collision** breaks per-tenant file/PHP/FTP isolation | **High** |
| F-05 | Backup **restore symlink/path traversal** → arbitrary file write as root | **High** |
| F-06 | Mail local-part **path traversal** → `mkdir`/maildir manipulation as root | **High** |
| F-07 | **No domain-ownership validation** (domain/zone/vhost claim & takeover) | **Medium** |
| F-08 | **No rate limiting / lockout** on `/api/login` | **Medium** |

**Production Readiness Score: 38 / 100** (multi-tenant deployment). The architecture is sound; the gating issues are concentrated in OS-resource brokering and the filesystem jail and are fixable without redesign.

---

## 2. Risk Matrix

```
 Impact ▲
        │
CRITICAL│  F-03            F-01  F-02
        │
   HIGH │  F-07      F-05  F-04  F-06
        │
 MEDIUM │  F-15 F-18 F-08  F-09
        │
    LOW │  F-12 F-13 F-16  F-10 F-11 F-14 F-17 F-19
        └───────────────────────────────────────────▶
            Low        Medium        High   Likelihood
```

| ID | Finding | Severity | CVSS (est.) | Likelihood |
|----|---------|----------|-------------|------------|
| F-01 | FTP create resets arbitrary unix user password | Critical | 9.8 | High |
| F-02 | File-manager symlink traversal (root R/W) | Critical | 9.1 | High |
| F-03 | Cross-tenant DB takeover (shared user/role) | Critical | 8.8 | Medium |
| F-04 | sysUser name collision → isolation break | High | 8.1 | Medium |
| F-05 | Backup restore symlink/path traversal | High | 7.5 | Medium |
| F-06 | Mail local-part path traversal (root mkdir) | High | 7.1 | Medium |
| F-07 | No domain ownership validation | Medium | 6.5 | High |
| F-08 | No login rate limiting / lockout | Medium | 6.5 | High |
| F-09 | Missing CSP / HSTS headers | Medium | 5.3 | High |
| F-10 | Restore/extract: no decompression-bomb limit | Medium | 5.3 | Medium |
| F-11 | DB/PHP install ops run as root via socket/sudo | Medium | 5.0 | Low |
| F-12 | Cron schedule regex allows newline (`\s`) | Low | 4.0 | Low |
| F-13 | TXT record leading-quote pass-through | Low | 3.5 | Low |
| F-14 | WP admin password on `wp` CLI arg (ps leak) | Low | 3.7 | Medium |
| F-15 | Internal error strings returned to client | Medium | 4.3 | High |
| F-16 | Setup race (double admin) | Low | 3.1 | Low |
| F-17 | Sessions not revoked on password change | Low | 3.5 | Low |
| F-18 | API tokens have no scopes (full role power) | Medium | 4.0 | Medium |
| F-19 | CI uses floating action tags; `curl|sh` install | Low | 3.5 | Low |

---

## 3. Detailed Findings

### F-01 — FTP account creation resets the password of any existing system user (incl. `root`) — CRITICAL (CVSS ~9.8)

**Files:** `internal/api/ftp.go:37-84` (esp. `:73`, `:78`), `internal/system/unixuser.go:27-42` (`:34-36`), `internal/system/unixuser.go:45-59`

**Technical explanation.** `handleFTPCreate` validates the requested FTP username only against `validFTPUser = ^[a-z][a-z0-9_-]{2,30}$` — which matches `root`, `www-data`, `postgres`, `bind`, `vmail`, and **another tenant's `sysUser`**. It then calls:

```go
system.EnsureUnixUser(username, home)   // ftp.go:73
system.SetUnixPassword(username, req.Password) // ftp.go:78
```

`EnsureUnixUser` **treats a pre-existing account as success and returns nil** (`unixuser.go:34-36`), so no new account is created. `SetUnixPassword` then runs `chpasswd` for that name. The panel runs as **root**, so this sets the password of an arbitrary, already-existing OS account.

**Exploitation scenario.**
1. Attacker (normal user) `POST /api/ftp {"username":"root","password":"Pwned12345","directory":""}`.
2. `EnsureUnixUser("root", …)` → `id -u root` succeeds → returns nil.
3. `SetUnixPassword("root","Pwned12345")` → `chpasswd` sets **root's password**.
4. Attacker `ssh root@host` (or `su`) → full host compromise. Variants: target `postgres`/`www-data` to pivot, or target another tenant's `sysUser` (e.g. `alice`) then SFTP in as that tenant. `DELETE /api/ftp/{id}` similarly runs `userdel` on the chosen name → delete `www-data`/another tenant.

**Impact.** Customer → root; full multi-tenant compromise; host takeover.

**Fix.** Never reuse/modify pre-existing accounts. Namespace FTP users under the owning tenant, reject reserved/system names, and only `chpasswd` accounts the panel created and owns.

```go
// unixuser.go — make creation explicit; refuse to touch foreign accounts.
func CreateUnixUser(name, home string) error {
    if !validSysName.MatchString(name) { return fmt.Errorf("invalid system username %q", name) }
    if _, err := run("id", "-u", name); err == nil {
        return fmt.Errorf("account %q already exists", name) // DO NOT silently succeed
    }
    _, err := run("useradd", "--create-home", "--home-dir", home, "--shell", "/usr/sbin/nologin", name)
    return err
}
```
```go
// ftp.go — namespace + reserved-name guard, and only manage rows we created.
ftpName := sysUser + "-" + username           // tenant-scoped, collision-free
if isReservedSystemUser(ftpName) { /* 400 */ }
// On password change / delete, verify the unix user belongs to this panel user
// (e.g. uid >= 1000 AND homedir under WebRoot/<sysUser>) before chpasswd/userdel.
```

---

### F-02 — File-manager symlink traversal → arbitrary file read/write/delete as root — CRITICAL (CVSS ~9.1)

**Files:** `internal/system/files.go:28-36` (`ResolveJailed`) and all callers (`files.go:70-176`), invoked from `internal/api/files_handlers.go`.

**Technical explanation.** `ResolveJailed` does a **purely lexical** check (`filepath.Clean` + prefix). It does **not** resolve symlinks. All file ops (`os.ReadFile`, `os.WriteFile`, `os.OpenFile`, `os.RemoveAll`, `os.Rename`) run **as root** and **follow symlinks**. A tenant who can place a symlink inside their own web space (trivially, via FTP/SFTP, or via `MkdirJailed`+upload, or via the file manager itself) can make a path that is lexically "inside the jail" resolve to anywhere on the host.

**Exploitation scenario.**
1. Tenant creates symlink `~/public_html/x → /` (or `/etc`, or `/var/www/<other-tenant>`), e.g. over SFTP.
2. `GET /api/files/content?path=x/etc/shadow` → `ResolveJailed` accepts (`jail/x/etc/shadow` is lexically under jail) → `os.ReadFile` follows the symlink → **reads `/etc/shadow` as root**.
3. `POST /api/files/content {"path":"x/etc/cron.d/root","content":"* * * * * root chmod u+s /bin/bash"}` → **arbitrary write as root** → root shell. Or read/overwrite another tenant's files.

**Impact.** Arbitrary file read/write/delete as root; full host + cross-tenant compromise.

**Fix.** Resolve symlinks safely and confine to the jail. Best: drop privileges to the tenant's uid for file ops, or use `openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS)`. Minimum viable in pure Go: `EvalSymlinks` the resolved path (and its parent for creates) and re-check containment; open final components with `O_NOFOLLOW`.

```go
func ResolveJailed(jail, rel string) (string, error) {
    jail = filepath.Clean(jail)
    p := filepath.Clean(filepath.Join(jail, filepath.FromSlash(rel)))
    // Resolve symlinks on the existing portion and re-verify containment.
    real := p
    for {
        if r, err := filepath.EvalSymlinks(real); err == nil { real = r; break }
        parent := filepath.Dir(real)
        if parent == real { break }
        real = parent
    }
    if real != jail && !strings.HasPrefix(real, jail+string(filepath.Separator)) {
        return "", fmt.Errorf("path escapes home directory")
    }
    return p, nil // and open with O_NOFOLLOW on the final element
}
```
The same root-cause applies to **F-05** (restore).

---

### F-03 — Cross-tenant database takeover via shared DB user / role — CRITICAL→HIGH (CVSS ~8.8)

**Files:** `internal/api/databases.go:62-114` (`handleDBCreate`), `:154-178` (`handleDBPassword`); `internal/system/mysql.go:39-74`; `internal/system/postgres.go:38-84`.

**Technical explanation.** A tenant fully controls the DB **username** (`validDBInput = [A-Za-z0-9_]{1,48}`). Only `db_entries.name` is globally unique; **`db_user` is not**, and there is no check that a requested DB user isn't already owned by another tenant. MySQL/Postgres users are a **global, shared namespace**, and the panel performs privileged operations on them:

- **Postgres** (`postgres.go:49`): `CreatePostgresDatabase` runs `ALTER ROLE "<user>" WITH LOGIN PASSWORD '<new>'` **even when the role already exists** → directly resets a victim role's password.
- **MySQL** (`mysql.go:67-74`): `SetDatabasePassword` runs `ALTER USER '<user>'@'localhost' IDENTIFIED BY '<new>'` on a user the caller named, and `CreateDatabase` `GRANT ALL ... TO '<user>'` attaches an existing user to the attacker's DB.

**Exploitation scenario (Postgres).**
1. Victim has role `appdb` owning DB `appdb`.
2. Attacker `POST /api/databases {"name":"atk","user":"appdb","password":"known","engine":"postgres"}`.
3. `CreatePostgresDatabase` → `ALTER ROLE "appdb" WITH LOGIN PASSWORD 'known'` resets the victim's role password.
4. Attacker connects as `appdb/known` (peer/local) → reads/writes **victim's database**.

**Exploitation scenario (MySQL).** Attacker creates a DB with `user` = victim's existing MySQL user → `GRANT ALL ON atk.* TO victimuser`; then `POST /api/databases/{atk}/password` → `ALTER USER victimuser IDENTIFIED BY 'known'` (scoped only to the attacker's own `db_entry`, but the *user* is shared) → attacker logs in as `victimuser`, which retains grants on the victim's databases.

**Impact.** Cross-tenant database read/write; credential reset of other tenants' DB accounts.

**Fix.** Make DB users tenant-namespaced and refuse foreign/shared users.
- Derive `db_user` as `<sysUser>_<name>` (or store ownership and enforce it).
- Before `CREATE/ALTER/GRANT`, verify the target user/role isn't already associated with a *different* panel user.
- Postgres: only `CREATE ROLE` if absent; never `ALTER ... PASSWORD` on a pre-existing role you don't own.
- Add a `UNIQUE` ownership mapping for DB users in `db_entries` and validate on every `handleDBPassword`/`handleDBDelete`.

---

### F-04 — System-user name collision breaks tenant isolation — HIGH (CVSS ~8.1)

**File:** `internal/system/unixuser.go:16-23` (`SysUserName`).

**Technical explanation.** Panel usernames allow `.` (`validUsername = ^[A-Za-z][A-Za-z0-9_.-]{2,31}$`), but `SysUserName` lowercases and **strips** everything outside `[a-z0-9_-]`, then falls back to a shared literal `rp-user` for anything that no longer matches `validSysName`. The mapping is **not injective**:

- `john.doe` and `johndoe` → both `johndoe`.
- `a..`, `a.` and other reductions < 2 valid chars → **all become `rp-user`**.

Two distinct panel users that map to the same `sysUser` **share** the home dir, the file-manager jail (`jailFor`), the PHP-FPM pool, and FTP home — i.e. full cross-tenant file access and code execution context.

**Exploitation scenario.** Attacker registers (or a reseller creates) `john.doe` after victim `johndoe` exists; both resolve to `/var/www/johndoe`. Attacker's file manager and PHP pool now operate on the victim's web space.

**Impact.** Cross-tenant file/code/data access; isolation collapse.

**Fix.** Make `sysUser` a **stable, unique, non-derived** identifier — e.g. `u<userID>` or store an explicit `users.sys_user` column allocated at creation with a uniqueness check; never silently collapse to a shared default.

```go
func (s *Server) sysUserForPanelUser(userID int64) (string, error) {
    name := fmt.Sprintf("rpu%d", userID) // unique by construction
    ...
}
```

---

### F-05 — Backup restore symlink / path traversal → arbitrary file write as root — HIGH (CVSS ~7.5)

**Files:** `internal/system/backup.go:164-231` (`RestoreBackup`), `internal/api/backups.go:302-341`.

**Technical explanation.** Restore extraction is jailed only via the lexical `ResolveJailed` (F-02) and writes as root following symlinks. A tenant who controls their web space can plant a symlinked directory; a restore that writes a file "under" that path escapes the jail. Restore also has **no extracted-size cap** (see F-10).

**Exploitation.** Create real dir `web/d/x` → back up → replace `web/d` with symlink `→ /etc/cron.d` → restore → file written to `/etc/cron.d/x` as root.

**Fix.** Same as F-02 (symlink-safe resolution / `O_NOFOLLOW` / drop privileges). Restore should also reject non-regular/non-dir entries (already does) and cap output size.

---

### F-06 — Mail local-part path traversal → arbitrary directory creation as root + map manipulation — HIGH (CVSS ~7.1)

**Files:** `internal/api/mail.go:118-123`, `internal/system/mail.go:104-120` (`EnsureMaildir`), `internal/system/mail.go:42-49`.

**Technical explanation.** The mailbox address validator rejects `" \t\n:"` in the local part but **allows `/` and `.`**. `EnsureMaildir` builds `filepath.Join("/var/mail/vhosts", domain, user)` and `MkdirAll(…, 0o770)` **as root**, and the same address is written verbatim into the postfix `virtual_mailbox_maps` path.

**Exploitation.** `POST /api/mail/boxes {"address":"../../../../tmp/x@my-owned-domain.com", …}` → `filepath.Join` resolves to `/tmp/x` → root-owned dir created anywhere; the postfix map gets a traversing maildir path, allowing delivery outside the vhost base and corruption of the shared map files.

**Impact.** Arbitrary directory creation as root; mail-routing manipulation; potential cross-mailbox interference; map file corruption (mail DoS).

**Fix.** Strictly validate the local part (e.g. `^[a-z0-9._%+-]{1,64}$`, no leading `.`/`..`), and additionally confine `EnsureMaildir` paths with a jail check.

```go
var validLocalPart = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._%+-]{0,62}[a-z0-9])?$`)
if !validLocalPart.MatchString(local) || strings.Contains(local, "..") { /* 400 */ }
```

---

### F-07 — No domain ownership validation — MEDIUM (CVSS ~6.5)

**File:** `internal/api/domains.go:68-129` (`handleDomainCreate`).

**Technical explanation.** Any user can create **any** domain name (only format + global uniqueness checked). The panel then generates an nginx/Apache vhost (`server_name`) and a BIND zone and will attempt Let's Encrypt for it. There's no proof-of-control and no first-come protection between tenants for legitimately shared infrastructure.

**Exploitation / impact.** A tenant can: claim a domain another tenant legitimately uses (DoS / pre-emption, since `name` is globally unique); stand up a vhost for any domain that resolves to the host (phishing/hijack of traffic mis-pointed at the server); reserve `mail.<victim>` / arbitrary FQDNs. Combined with serving content, this enables traffic hijacking for any domain whose DNS points here.

**Fix.** Require domain-ownership verification (DNS TXT challenge or registrar/HTTP token) before activating vhosts/zones for non-admin users, or restrict domain creation to admin/reseller approval.

---

### F-08 — No rate limiting / account lockout on login — MEDIUM (CVSS ~6.5)

**Files:** `internal/auth/auth.go:43-64`, `internal/api/auth_handlers.go:17-38`.

**Technical explanation.** `Login` does constant-time-ish work (good — burns bcrypt even for unknown users) but there is **no per-IP/per-account rate limit, throttle, or lockout**. fail2ban is installed by `install.sh` but is configured only for SSH by default, not the panel. The panel is internet-facing on `:8443`.

**Exploitation.** Unlimited online password guessing against `/api/login` (and `Authorization: Bearer` token guessing, though tokens are high-entropy).

**Fix.** Add per-IP + per-account throttling with exponential backoff/temporary lockout; optionally ship a fail2ban filter for the panel log. Consider CAPTCHA after N failures and MFA for admins (currently **no MFA** exists).

---

### F-09 — Missing CSP and HSTS security headers — MEDIUM (CVSS ~5.3)

**File:** `cmd/repanel/main.go:124-131` (`securityHeaders`).

**Technical explanation.** Sets `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy` (good), but **no `Content-Security-Policy`** and **no `Strict-Transport-Security`**. React mitigates most XSS (no `dangerouslySetInnerHTML` found), but defense-in-depth is missing and there's no HSTS to enforce TLS.

**Fix.**
```go
w.Header().Set("Content-Security-Policy",
  "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
if r.TLS != nil {
  w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
}
```

---

### F-10 — Restore/extract: no decompression-bomb or size limit — MEDIUM (CVSS ~5.3)

**Files:** `internal/system/backup.go:220` (`io.Copy` on restore), `internal/system/wordpress.go:118`.

**Restore/extract copy unbounded data.** A crafted/oversized archive (or a gzip bomb) can exhaust disk/inode and DoS the host. (WordPress core is trusted, but restore archives, while server-generated today, become attacker-influenced if upload-restore is ever added.)

**Fix.** Use `io.CopyN` with a per-file and per-archive cap; count total bytes and abort past a threshold; enforce quota during restore.

---

### F-11 — DB and PHP provisioning run as root (socket / sudo) — MEDIUM (CVSS ~5.0, design)

**Files:** `internal/system/mysql.go:21`, `internal/system/postgres.go:33`, `internal/system/php.go:43-110` (apt as root).

By design MariaDB is administered as root via the unix socket and Postgres via `sudo -u postgres`. This is conventional, but it means **any** SQL/identifier-injection or argument-injection in these paths is a full-DB or full-host compromise. Current escaping (`quoteIdent`, `escapeSQLString`, `pgEscape`, `validDBName`) is correct, and `InstallPHP` package names are constrained to a hardcoded `knownPHPVersions` set — but the blast radius warrants defense-in-depth: dedicated least-privilege admin accounts, allowlisted statements, and never building SQL by string concat for any future field.

---

### F-12 — Cron schedule regex allows whitespace incl. newline — LOW (CVSS ~4.0)

**File:** `internal/system/cron.go:15` (`cronScheduleRe`).

`(\S+\s+){4}\S+` uses `\s`, which matches `\n`. The command field is newline-stripped (`cron.go:45`, good) and the 5-token cap prevents injecting a 6th `root` user field, so **root cron injection is not achievable**, but a newline in the *schedule* can still emit malformed lines and corrupt `/etc/cron.d/repanel` (DoS of all tenants' cron). **Fix:** validate exactly 5 space-separated fields with `[^\s]` and reject `\n`/`\r`; or split on single spaces and validate each field.

---

### F-13 — TXT record leading-quote pass-through — LOW (CVSS ~3.5)

**File:** `internal/system/bind.go:102-105` (`formatTXT`).

A TXT value beginning with `"` is emitted verbatim. Record validation forbids newlines, so this is confined to the tenant's own zone (no cross-tenant), but malformed quoting can break the tenant's own zone load. **Fix:** always escape/quote; don't trust a leading quote.

---

### F-14 — WordPress admin password passed as CLI argument — LOW (CVSS ~3.7)

**File:** `internal/system/wordpress.go:194-201`.

`--admin_password=<pass>` is visible in `/proc/<pid>/cmdline` to other local users (e.g. other tenants' shells) for the lifetime of the `wp` process. No shell injection (exec args). **Fix:** pass via `--prompt` + stdin, or an env/`WP_CLI` config file, not argv.

---

### F-15 — Internal error strings returned to clients — MEDIUM (CVSS ~4.3)

**File:** `internal/api/server.go:178-186` (`fail`).

`fail` returns `err.Error()` (up to 300 chars) to the client, leaking absolute paths, SQL errors, `mysql`/`psql`/`useradd` stderr, etc., aiding reconnaissance. **Fix:** log full error server-side; return a generic message + correlation id to the client.

---

### F-16 — First-run setup race (double admin) — LOW (CVSS ~3.1)

**File:** `internal/api/auth_handlers.go:91-122`. `adminExists()` check and insert aren't atomic; two concurrent `POST /api/setup` could both pass. Window is tiny (pre-provisioning). **Fix:** wrap in a transaction / unique guard, or a one-shot setup token.

---

### F-17 — Sessions not revoked on password change — LOW (CVSS ~3.5)

**File:** `internal/api/auth_handlers.go:52-75`. Changing a password (self or by admin via `handleUserUpdate`) does not delete the user's other sessions (only suspension does, `users.go:181-183`). **Fix:** `DELETE FROM sessions WHERE user_id = ?` (optionally keeping the current one) on any password change.

---

### F-18 — API tokens have no scopes — MEDIUM (CVSS ~4.0)

**Files:** `internal/api/tokens.go`, `internal/auth/auth.go:100-121`. A token inherits the issuer's **full role** (an admin token = full admin). No per-token scoping, IP binding, or last-secret rotation. **Fix:** add scopes/capabilities and optional IP allowlists; surface token power clearly in the UI.

---

### F-19 — Supply-chain / deployment hardening — LOW (CVSS ~3.5)

**Files:** `.github/workflows/release.yml`, `scripts/install.sh`.

- GitHub Actions are pinned to **floating major tags** (`actions/checkout@v4`, `softprops/action-gh-release@v2`) — mutable; pin to commit SHAs. Workflow `permissions: contents: write` is appropriately scoped.
- `install.sh` is delivered via `curl … | sh` (bootstrap trust). Releases publish a `.sha256` but the README one-liner doesn't verify it. The Sury key is fetched over HTTPS into `trusted.gpg.d` at install time (F-11 area) — acceptable but unverified-by-fingerprint.
- **Insecure default:** panel serves a **self-signed** cert by default (documented), and falls back to **plain HTTP** with only a log warning if no cert is set (`main.go:92-93`) — sessions then transit unencrypted.

---

## 4. Quick Wins (< 1 hour each)

- **F-01:** Reserved-name denylist + `useradd` that refuses pre-existing accounts; namespace FTP users as `<sysUser>-<name>`. *(highest ROI)*
- **F-06:** Add `validLocalPart` regex for mailbox local parts.
- **F-09:** Add CSP + HSTS in `securityHeaders`.
- **F-12:** Tighten `cronScheduleRe` to reject newlines.
- **F-13:** Always escape TXT values.
- **F-15:** Stop returning `err.Error()` to clients.
- **F-17:** Invalidate sessions on password change.
- **F-19:** Pin GitHub Actions to SHAs; document `sha256` verification in README.

## 5. High-Priority Fixes (this sprint)

- **F-02 / F-05:** Symlink-safe jail (`O_NOFOLLOW` / `EvalSymlinks` recheck / `openat2 RESOLVE_BENEATH`) for **all** file-manager and restore operations; ideally drop to the tenant uid for filesystem I/O.
- **F-01:** Full FTP-account rework (namespacing, ownership checks on password/delete).
- **F-03:** Tenant-namespaced DB users/roles; never `ALTER ... PASSWORD`/`GRANT` a role you don't own; ownership column + checks.
- **F-04:** Replace derived `SysUserName` with a unique, stored, non-collapsing identifier (`rpu<id>`).
- **F-08:** Login rate limiting + lockout (+ panel fail2ban filter).

## 6. Long-Term Improvements

- **Drop root where possible:** run file/PHP/FTP operations under the tenant uid via a small privileged helper with a narrow, audited interface; reduce the panel's root surface.
- **MFA** (TOTP) for admin/reseller accounts.
- **Domain ownership verification** (F-07) and per-tenant resource quotas/limits to curb abuse and resource exhaustion.
- **Least-privilege DB admin** accounts instead of root socket / `sudo postgres`.
- **Audit log** of privileged actions (user/DB/FTP/file/cron changes) with actor + IP.
- **Per-token scopes** (F-18) and short-lived sessions with refresh.
- **Container/namespace isolation** (per-tenant cgroups/namespaces or PHP chroot) for stronger breakout resistance.
- **CSP nonces** + Subresource Integrity for the embedded UI.

---

## 7. Production Readiness Score

**38 / 100** for multi-tenant production.

Rationale: solid foundations (parameterized SQL, bcrypt, good cookie flags, token hashing, broad tenant scoping) are undermined by **three critical OS-resource-brokering flaws (F-01/F-02/F-03)** that yield root or cross-tenant access from a normal account, plus high-severity isolation gaps (F-04/F-05/F-06). After the High-Priority fixes land and are verified on Linux, a re-score in the **75-85** range is realistic.

---

## 8. Security Scorecard by Category

| Category | Score | Notes |
|---|---|---|
| Authentication | 7/10 | bcrypt, good cookies, timing-equalized login; **no rate limit, no MFA** (F-08) |
| Authorization / multi-tenant | 3/10 | Good `scopeWhere` scoping, but F-01/F-03/F-04 break isolation |
| API security (IDOR/mass-assign) | 7/10 | Ownership checks present & parameterized; mass-assignment avoided via explicit structs |
| Injection (SQL/cmd/template) | 6/10 | No app SQLi, no shell exec; identifier/escape hygiene good; F-06/F-12 input gaps |
| File security / path traversal | 2/10 | Lexical-only jail + root I/O → F-02/F-05 |
| Web security (XSS/CSRF/CORS) | 7/10 | React escaping, SameSite=Strict; **no CSP/HSTS** (F-09) |
| Cryptography / secrets | 8/10 | bcrypt, SHA-256 token hashing, CSPRNG salts/tokens; WP pass via argv (F-14) |
| Data exposure | 6/10 | Dashboard host data now admin-gated; error leakage (F-15) |
| Infra / deployment | 6/10 | Scoped CI perms + checksums; floating action tags; self-signed/HTTP-fallback default (F-19) |
| Dependencies | 8/10 | Small, mainstream set (modernc sqlite, x/crypto); keep patched |
| Business logic / abuse | 4/10 | No domain ownership (F-07), no per-tenant rate/resource limits |

---

### Appendix — Positive controls observed
Parameterized SQL throughout; bcrypt password hashing with user-enumeration timing defense (`auth.go:47`); HttpOnly + SameSite=Strict + Secure session cookies (`auth_handlers.go:28-36`); API tokens stored as SHA-256 with `rpat_` prefix; consistent tenant scoping via `scopeWhere`; tar-traversal guard in WordPress extract (`wordpress.go:102`); DNS record newline rejection (`dns.go:128`); cron command newline stripping (`cron.go:45`); firewall/ufw args validated (`firewall.go`); dashboard host metrics now admin-only.
