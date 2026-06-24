#!/bin/sh
# RePanel installer for a fresh Debian 12+/Ubuntu 22.04+ server.
#
#   curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/master/scripts/install.sh | sh
#
# Installs the full hosting stack (nginx, PHP-FPM, MariaDB, BIND, Postfix,
# Dovecot, ProFTPD, certbot, ufw, fail2ban), wires it to RePanel and starts
# the panel on https://<server>:8443.
set -eu

REPO="reinis1996/repanel"
PANEL_PORT=8443
CONF_DIR=/etc/repanel
DATA_DIR=/var/lib/repanel
BIN=/usr/local/bin/repanel
CLI_BIN=/usr/local/bin/repctl

# Web server stack: nginx (default), apache, or nginx-apache (nginx fronts
# :80/:443 and reverse-proxies to Apache on APACHE_PORT). In the nginx-apache
# stack each website chooses nginx-only, Apache-only or nginx+Apache from the
# panel.
WEB_SERVER="${WEB_SERVER:-nginx}"
APACHE_PORT="${APACHE_PORT:-8080}"
case "$WEB_SERVER" in
  nginx|apache|nginx-apache) ;;
  *) printf 'ERROR: WEB_SERVER must be nginx, apache or nginx-apache (got %s)\n' "$WEB_SERVER" >&2; exit 1 ;;
esac

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || fail "run this installer as root (sudo sh install.sh)"

. /etc/os-release 2>/dev/null || fail "cannot detect distribution"
case "${ID:-}" in
  debian|ubuntu) ;;
  *) fail "unsupported distribution '$ID' — RePanel supports Debian and Ubuntu" ;;
esac

export DEBIAN_FRONTEND=noninteractive

say "Installing packages (this can take a few minutes)..."
apt-get update -qq
apt-get install -y -qq \
  php-fpm php-cli php-mysql php-curl php-gd php-mbstring php-xml php-zip \
  mariadb-server bind9 bind9utils \
  postfix postfix-pcre dovecot-imapd dovecot-pop3d dovecot-lmtpd dovecot-sieve \
  opendkim opendkim-tools \
  proftpd-basic certbot ufw fail2ban curl ca-certificates >/dev/null

# Web server packages depend on the chosen stack.
case "$WEB_SERVER" in
  nginx)        WEB_PKGS="nginx" ;;
  apache)       WEB_PKGS="apache2" ;;
  nginx-apache) WEB_PKGS="nginx apache2" ;;
esac
say "Installing web server: $WEB_SERVER"
apt-get install -y -qq $WEB_PKGS >/dev/null

PHP_VER="$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;' 2>/dev/null || echo 8.2)"
say "Detected PHP $PHP_VER"

# ---- WP-CLI (completes one-click WordPress installs) -----------------------
if ! command -v wp >/dev/null 2>&1; then
  say "Installing WP-CLI"
  if curl -fsSL -o /usr/local/bin/wp https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar; then
    chmod 755 /usr/local/bin/wp
  else
    rm -f /usr/local/bin/wp
    say "WARNING: WP-CLI download failed — WordPress installs will need the browser setup wizard"
  fi
fi

# ---- optional PostgreSQL ---------------------------------------------------
# RePanel manages PostgreSQL databases when a server is present; it is not part
# of the default stack. Set WITH_POSTGRES=1 to install it here, or install
# `postgresql` yourself later — the panel detects it automatically.
if [ "${WITH_POSTGRES:-0}" = 1 ]; then
  say "Installing PostgreSQL (WITH_POSTGRES=1)"
  apt-get install -y -qq postgresql php-pgsql >/dev/null
  systemctl enable --now postgresql >/dev/null 2>&1 || true
fi

# ---- panel binary + CLI ----------------------------------------------------
if [ -x ./repanel ]; then
  say "Using local repanel binary"
  install -m 755 ./repanel "$BIN"
  [ -x ./repctl ] && install -m 755 ./repctl "$CLI_BIN"
elif [ -f ./go.mod ] && command -v go >/dev/null 2>&1; then
  say "Building repanel from source"
  (cd web && command -v npm >/dev/null 2>&1 && npm install --silent && npm run build --silent) || true
  go build -o "$BIN" ./cmd/repanel
  go build -o "$CLI_BIN" ./cmd/repctl || true
else
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64)  ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    *) fail "unsupported architecture $ARCH" ;;
  esac
  URL="https://github.com/$REPO/releases/latest/download/repanel-linux-$ARCH"
  say "Downloading $URL"
  curl -fsSL -o "$BIN" "$URL" || fail "download failed — build from source instead (see README)"
  chmod 755 "$BIN"
  # The CLI is optional; don't fail the install if it isn't published yet.
  if curl -fsSL -o "$CLI_BIN" "https://github.com/$REPO/releases/latest/download/repctl-linux-$ARCH"; then
    chmod 755 "$CLI_BIN"
  else
    rm -f "$CLI_BIN"
  fi
fi

# ---- directories & config --------------------------------------------------
say "Writing panel configuration"
mkdir -p "$CONF_DIR" "$CONF_DIR/mail" "$DATA_DIR" /var/www
[ -f "$CONF_DIR/repanel.conf" ] || cat > "$CONF_DIR/repanel.conf" <<EOF
# RePanel configuration
LISTEN=:$PANEL_PORT
DATA_DIR=$DATA_DIR
WEB_ROOT=/var/www
NGINX_DIR=/etc/nginx
APACHE_DIR=/etc/apache2
WEB_SERVER=$WEB_SERVER
APACHE_PORT=$APACHE_PORT
BIND_DIR=/etc/bind
MAIL_DIR=$CONF_DIR/mail
SESSION_HOURS=24
EOF

# ---- web server (nginx / apache, per WEB_SERVER) ---------------------------
# nginx is the front for the nginx and nginx-apache stacks; Apache fronts the
# apache stack and runs as a plain-HTTP backend in the nginx-apache stack. The
# panel writes per-domain vhosts into <server>/repanel.d and reloads from there.
configure_nginx() {
  say "Configuring nginx"
  mkdir -p /etc/nginx/repanel.d
  cat > /etc/nginx/conf.d/zz-repanel.conf <<'EOF'
# Managed by RePanel — loads all per-domain vhosts.
include /etc/nginx/repanel.d/*.conf;
EOF
  nginx -t >/dev/null 2>&1 && systemctl enable --now nginx >/dev/null 2>&1
  systemctl reload nginx 2>/dev/null || true
}

configure_apache() {
  say "Configuring Apache"
  mkdir -p /etc/apache2/repanel.d
  # Load per-domain vhosts.
  grep -q 'repanel.d' /etc/apache2/apache2.conf 2>/dev/null || \
    echo 'IncludeOptional /etc/apache2/repanel.d/*.conf' >> /etc/apache2/apache2.conf
  # PHP runs through the per-domain FPM pools via mod_proxy_fcgi.
  a2enmod proxy proxy_fcgi rewrite setenvif headers remoteip >/dev/null 2>&1 || true
  # The stock catch-all site would shadow our vhosts.
  a2dissite 000-default default-ssl >/dev/null 2>&1 || true
}

case "$WEB_SERVER" in
  nginx)
    configure_nginx
    ;;
  apache)
    configure_apache
    a2enmod ssl >/dev/null 2>&1 || true
    # Apache owns :80/:443 directly; make sure nothing else is bound there.
    systemctl disable --now nginx >/dev/null 2>&1 || true
    apache2ctl configtest >/dev/null 2>&1 && systemctl enable --now apache2 >/dev/null 2>&1
    systemctl reload apache2 2>/dev/null || true
    ;;
  nginx-apache)
    configure_nginx
    configure_apache
    # Apache listens only on the loopback backend port; nginx terminates TLS.
    cat > /etc/apache2/ports.conf <<EOF
# Managed by RePanel — Apache runs as a backend behind nginx.
Listen 127.0.0.1:$APACHE_PORT
EOF
    apache2ctl configtest >/dev/null 2>&1 && systemctl enable --now apache2 >/dev/null 2>&1
    systemctl reload apache2 2>/dev/null || true
    ;;
esac

# ---- BIND ------------------------------------------------------------------
say "Configuring BIND"
mkdir -p /etc/bind/repanel-zones
touch /etc/bind/named.conf.repanel
grep -q 'named.conf.repanel' /etc/bind/named.conf.local 2>/dev/null || \
  echo 'include "/etc/bind/named.conf.repanel";' >> /etc/bind/named.conf.local
chown -R bind:bind /etc/bind/repanel-zones
named-checkconf && systemctl reload bind9 || true

# ---- mail (postfix + dovecot, virtual mailboxes) ---------------------------
say "Configuring Postfix and Dovecot"
if ! id vmail >/dev/null 2>&1; then
  groupadd -g 5000 vmail
  useradd -u 5000 -g 5000 -d /var/mail/vhosts -s /usr/sbin/nologin vmail
fi
mkdir -p /var/mail/vhosts
chown -R vmail:vmail /var/mail/vhosts

touch "$CONF_DIR/mail/virtual_domains" "$CONF_DIR/mail/virtual_mailboxes" \
      "$CONF_DIR/mail/virtual_aliases" "$CONF_DIR/mail/passwd"
chmod 640 "$CONF_DIR/mail/passwd"
postmap "$CONF_DIR/mail/virtual_domains" "$CONF_DIR/mail/virtual_mailboxes" \
        "$CONF_DIR/mail/virtual_aliases"

postconf -e \
  "virtual_mailbox_domains = hash:$CONF_DIR/mail/virtual_domains" \
  "virtual_mailbox_maps = hash:$CONF_DIR/mail/virtual_mailboxes" \
  "virtual_alias_maps = hash:$CONF_DIR/mail/virtual_aliases" \
  "virtual_mailbox_base = /var/mail/vhosts" \
  "virtual_minimum_uid = 100" \
  "virtual_uid_maps = static:5000" \
  "virtual_gid_maps = static:5000" \
  "smtpd_sasl_type = dovecot" \
  "smtpd_sasl_path = private/auth" \
  "smtpd_sasl_auth_enable = yes" \
  "smtpd_recipient_restrictions = permit_mynetworks permit_sasl_authenticated reject_unauth_destination"

# Dovecot 2.4 (Debian 13+, Ubuntu 25.10+) renamed mail_location and changed
# the passdb/userdb syntax; write config matching the installed version.
DOVECOT_VER="$(dovecot --version 2>/dev/null | awk '{print $1}')"
case "$DOVECOT_VER" in
2.4*|2.5*|3.*)
  cat > /etc/dovecot/conf.d/99-repanel.conf <<EOF
# Managed by RePanel (Dovecot >= 2.4 syntax)
mail_driver = maildir
mail_path = /var/mail/vhosts/%{user | domain}/%{user | username}
# Store INBOX in the maildir; the stock 2.4 default (mail_inbox_path =
# /var/mail/%{user}) points at a path vmail cannot write, breaking INBOX.
mail_inbox_path =
mail_uid = vmail
mail_gid = vmail
first_valid_uid = 5000
last_valid_uid = 5000

passdb passwd-file {
  passwd_file_path = $CONF_DIR/mail/passwd
  default_password_scheme = SHA512-CRYPT
}

userdb static {
  fields {
    uid = vmail
    gid = vmail
    home = /var/mail/vhosts/%{user | domain}/%{user | username}
  }
}

service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
EOF
  ;;
*)
  cat > /etc/dovecot/conf.d/99-repanel.conf <<EOF
# Managed by RePanel (Dovecot 2.3 syntax)
mail_location = maildir:/var/mail/vhosts/%d/%n
mail_uid = vmail
mail_gid = vmail
first_valid_uid = 5000
last_valid_uid = 5000

passdb {
  driver = passwd-file
  args = scheme=SHA512-CRYPT username_format=%u $CONF_DIR/mail/passwd
}
userdb {
  driver = static
  args = uid=vmail gid=vmail home=/var/mail/vhosts/%d/%n
}

service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
EOF
  ;;
esac
# dovecot needs read access to the passwd file
chgrp dovecot "$CONF_DIR/mail/passwd" 2>/dev/null || true
# Don't abort the whole installation if mail needs manual attention.
systemctl restart dovecot || say "WARNING: dovecot failed to start — check 'journalctl -u dovecot' after installation"
systemctl restart postfix

# ---- OpenDKIM (DKIM signing, driven by the panel) --------------------------
say "Configuring OpenDKIM"
mkdir -p /etc/opendkim/keys
: > /etc/opendkim/key.table
: > /etc/opendkim/signing.table
cat > /etc/opendkim/trusted.hosts <<'EOF'
127.0.0.1
::1
localhost
EOF
cat > /etc/opendkim.conf <<'EOF'
# Managed by RePanel
Syslog               yes
UMask                002
Mode                 sv
Canonicalization     relaxed/simple
KeyTable             /etc/opendkim/key.table
SigningTable         refile:/etc/opendkim/signing.table
ExternalIgnoreList   /etc/opendkim/trusted.hosts
InternalHosts        /etc/opendkim/trusted.hosts
Socket               inet:8891@localhost
PidFile              /run/opendkim/opendkim.pid
OversignHeaders      From
UserID               opendkim
EOF
chown -R opendkim:opendkim /etc/opendkim
# Wire OpenDKIM into Postfix as a milter (the panel writes the tables/keys).
postconf -e \
  "milter_default_action = accept" \
  "milter_protocol = 6" \
  "smtpd_milters = inet:localhost:8891" \
  "non_smtpd_milters = inet:localhost:8891"
systemctl enable --now opendkim >/dev/null 2>&1 || say "WARNING: opendkim failed to start — check 'journalctl -u opendkim'"
systemctl restart postfix

# ---- ProFTPD ---------------------------------------------------------------
say "Configuring ProFTPD"
mkdir -p /etc/proftpd/conf.d
cat > /etc/proftpd/conf.d/repanel.conf <<'EOF'
# Managed by RePanel
DefaultRoot ~
RequireValidShell off
# Passive port range opened by the panel's firewall rules (must match
# system.FTPPassivePorts).
PassivePorts 49152 49251
EOF
systemctl restart proftpd || true

# ---- optional webmail (Roundcube) ------------------------------------------
# Webmail is not part of the default stack. Set WITH_WEBMAIL=1 to install
# Roundcube here, or `apt install roundcube` yourself later — the panel detects
# it automatically and serves it at webmail.<domain> for opted-in domains.
if [ "${WITH_WEBMAIL:-0}" = 1 ]; then
  say "Installing Roundcube webmail (WITH_WEBMAIL=1)"
  # Preseed dbconfig to a self-contained sqlite store so the install needs no
  # database credentials and stays non-interactive.
  echo "roundcube-core roundcube/dbconfig-install boolean true" | debconf-set-selections
  echo "roundcube-core roundcube/database-type select sqlite3"  | debconf-set-selections
  if apt-get install -y -qq roundcube roundcube-core roundcube-sqlite3 >/dev/null 2>&1; then
    # Point Roundcube at the local Dovecot/Postfix.
    RC_CONF=/etc/roundcube/config.inc.php
    if [ -f "$RC_CONF" ]; then
      sed -i "s#^\$config\['imap_host'\].*#\$config['imap_host'] = 'localhost:143';#" "$RC_CONF" 2>/dev/null || true
      sed -i "s#^\$config\['smtp_host'\].*#\$config['smtp_host'] = 'localhost:587';#" "$RC_CONF" 2>/dev/null || true
      grep -q "imap_host" "$RC_CONF" || echo "\$config['imap_host'] = 'localhost:143';" >> "$RC_CONF"
      grep -q "smtp_host" "$RC_CONF" || echo "\$config['smtp_host'] = 'localhost:587';" >> "$RC_CONF"
    fi
    say "Roundcube installed — enable webmail per domain from the Mail page"
  else
    say "WARNING: Roundcube install failed — webmail will be unavailable"
  fi
fi

# ---- optional anti-spam / anti-virus (rspamd + ClamAV) ---------------------
# Not part of the default stack. Set WITH_ANTISPAM=1 to install rspamd (spam
# filtering) and ClamAV (virus scanning) here, or install them later from the
# panel's Mail page (admin → "Install rspamd + ClamAV"). Either way the panel
# wires them into Postfix and applies per-domain settings.
if [ "${WITH_ANTISPAM:-0}" = 1 ]; then
  say "Installing rspamd + ClamAV (WITH_ANTISPAM=1)"
  if apt-get install -y -qq rspamd redis-server clamav clamav-daemon clamav-freshclam >/dev/null 2>&1; then
    # Add rspamd to Postfix's milter list alongside OpenDKIM (port 8891).
    CUR_MILTERS=$(postconf -h smtpd_milters 2>/dev/null)
    case "$CUR_MILTERS" in
      *11332*) : ;;
      "") postconf -e "smtpd_milters = inet:localhost:11332" "non_smtpd_milters = inet:localhost:11332" ;;
      *)  postconf -e "smtpd_milters = ${CUR_MILTERS}, inet:localhost:11332" \
                     "non_smtpd_milters = ${CUR_MILTERS}, inet:localhost:11332" ;;
    esac
    # Wire ClamAV into rspamd's antivirus module.
    mkdir -p /etc/rspamd/local.d
    cat > /etc/rspamd/local.d/antivirus.conf <<'RSAV'
# Managed by RePanel — scan mail through ClamAV.
clamav {
  type = "clamav";
  servers = "/var/run/clamav/clamd.ctl";
  scan_mime_parts = true;
  symbol = "CLAM_VIRUS";
  action = "reject";
  message = "This message contains a virus and has been rejected.";
}
RSAV
    systemctl enable --now redis-server >/dev/null 2>&1 || true
    systemctl enable --now clamav-freshclam >/dev/null 2>&1 || true
    systemctl enable --now clamav-daemon >/dev/null 2>&1 || true
    systemctl enable --now rspamd >/dev/null 2>&1 || true
    systemctl restart postfix >/dev/null 2>&1 || true
    say "rspamd + ClamAV installed — toggle per domain from the Mail page"
  else
    say "WARNING: rspamd/ClamAV install failed — anti-spam will be unavailable"
  fi
fi

# ---- systemd unit ----------------------------------------------------------
say "Installing systemd service"
cat > /etc/systemd/system/repanel.service <<EOF
[Unit]
Description=RePanel hosting control panel
After=network.target

[Service]
ExecStart=$BIN -config $CONF_DIR/repanel.conf
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now repanel

# ---- firewall --------------------------------------------------------------
# The panel opens the full set of stack ports (web 80/443, mail, DNS, FTP +
# passive range, SSH and the panel port) and enables ufw automatically when you
# create the admin account. Here we only make sure SSH and the panel port are
# reachable in case ufw is already active before setup.
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q 'Status: active'; then
  say "Ensuring SSH and panel port are open in ufw"
  ufw allow OpenSSH >/dev/null 2>&1 || ufw allow 22/tcp >/dev/null 2>&1 || true
  ufw allow "$PANEL_PORT/tcp" >/dev/null 2>&1 || true
fi

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
say "Done!"
printf '\n  RePanel is running.\n  Open \033[1mhttps://%s:%s\033[0m and create the admin account.\n' "${IP:-<server-ip>}" "$PANEL_PORT"
printf '  (The panel uses a self-signed certificate — your browser will warn once.)\n\n'
