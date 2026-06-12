#!/bin/sh
# RePanel installer for a fresh Debian 12+/Ubuntu 22.04+ server.
#
#   curl -fsSL https://raw.githubusercontent.com/reinis1996/repanel/main/scripts/install.sh | sh
#
# Installs the full hosting stack (nginx, PHP-FPM, MariaDB, BIND, Postfix,
# Dovecot, ProFTPD, certbot, ufw, fail2ban), wires it to RePanel and starts
# the panel on https://<server>:8443.
set -eu

REPO="repanel/repanel"
PANEL_PORT=8443
CONF_DIR=/etc/repanel
DATA_DIR=/var/lib/repanel
BIN=/usr/local/bin/repanel

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
  nginx php-fpm php-mysql php-curl php-gd php-mbstring php-xml php-zip \
  mariadb-server bind9 bind9utils \
  postfix postfix-pcre dovecot-imapd dovecot-pop3d dovecot-lmtpd \
  proftpd-basic certbot ufw fail2ban curl ca-certificates >/dev/null

PHP_VER="$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;' 2>/dev/null || echo 8.2)"
say "Detected PHP $PHP_VER"

# ---- panel binary ----------------------------------------------------------
if [ -x ./repanel ]; then
  say "Using local repanel binary"
  install -m 755 ./repanel "$BIN"
elif [ -f ./go.mod ] && command -v go >/dev/null 2>&1; then
  say "Building repanel from source"
  (cd web && command -v npm >/dev/null 2>&1 && npm install --silent && npm run build --silent) || true
  go build -o "$BIN" ./cmd/repanel
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
BIND_DIR=/etc/bind
MAIL_DIR=$CONF_DIR/mail
SESSION_HOURS=24
EOF

# ---- nginx -----------------------------------------------------------------
say "Configuring nginx"
mkdir -p /etc/nginx/repanel.d
cat > /etc/nginx/conf.d/zz-repanel.conf <<'EOF'
# Managed by RePanel — loads all per-domain vhosts.
include /etc/nginx/repanel.d/*.conf;
EOF
nginx -t >/dev/null && systemctl reload nginx

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

cat > /etc/dovecot/conf.d/99-repanel.conf <<EOF
# Managed by RePanel
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
# dovecot needs read access to the passwd file
chgrp dovecot "$CONF_DIR/mail/passwd" 2>/dev/null || true
systemctl restart dovecot postfix

# ---- ProFTPD ---------------------------------------------------------------
say "Configuring ProFTPD"
mkdir -p /etc/proftpd/conf.d
cat > /etc/proftpd/conf.d/repanel.conf <<'EOF'
# Managed by RePanel
DefaultRoot ~
RequireValidShell off
EOF
systemctl restart proftpd || true

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
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q 'Status: active'; then
  say "Opening panel port in ufw"
  ufw allow "$PANEL_PORT/tcp" >/dev/null || true
fi

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
say "Done!"
printf '\n  RePanel is running.\n  Open \033[1mhttps://%s:%s\033[0m and create the admin account.\n' "${IP:-<server-ip>}" "$PANEL_PORT"
printf '  (The panel uses a self-signed certificate — your browser will warn once.)\n\n'
