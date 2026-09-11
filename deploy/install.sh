#!/usr/bin/env bash
# snp — one-command install on a systemd Linux host (spec §7).
#
# usage: install.sh [-n hostname] [-o owner] [-b binary]
#
#   -n hostname   tailnet node name            (default: snp)
#   -o owner      Tailscale login allowed in   (required)
#   -b binary     path to the snp binary       (default: <repo>/bin/snp)
#
# What it does:
#   * creates the system user/group `snp` (home /var/lib/snp)
#   * installs the binary to /usr/local/bin/snp
#   * writes the starter config to /var/lib/snp/.config/snp/config.toml
#     (an existing config is kept, never overwritten)
#   * writes /var/lib/snp/.config/snp/authkey when TS_AUTHKEY is set in
#     the environment; the file is kept after the first join so a state
#     wipe can re-join the tailnet without re-issuing steps
#   * installs /etc/systemd/system/snp.service and enables it
#   * installs deploy/backup.sh to /usr/local/sbin/snp-backup plus a
#     daily cron entry (/etc/cron.d/snp-backup, 03:17) into
#     /var/lib/snp/backups
#
# The auth key (admin panel, or `tailscale authkeys`) is only needed for
# the first join; afterwards the node state under
# /var/lib/snp/.local/share/snp/tsnet is sufficient.
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

HOSTNAME_=snp
OWNER=""
BIN="${SCRIPT_DIR}/../bin/snp"

usage() {
  cat <<'EOF'
usage: install.sh [-n hostname] [-o owner] [-b binary]

  -n hostname   tailnet node name (default: snp)
  -o owner      Tailscale login allowed in (required)
  -b binary     path to the snp binary (default: <repo>/bin/snp)
EOF
}

while getopts ":n:o:b:h" opt; do
  case $opt in
    n) HOSTNAME_=$OPTARG ;;
    o) OWNER=$OPTARG ;;
    b) BIN=$OPTARG ;;
    h) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done

if [ -z "$OWNER" ]; then
  echo "install.sh: -o owner is required (your Tailscale login)" >&2
  usage >&2
  exit 2
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh: must run as root (try sudo)" >&2
  exit 1
fi

# The binary must exist and actually run on this machine (catches a
# cross-build for the wrong architecture before the service flaps).
if [ ! -x "$BIN" ]; then
  echo "install.sh: binary not found or not executable: $BIN (run 'make build' first)" >&2
  exit 1
fi
if ! "$BIN" help >/dev/null 2>&1; then
  echo "install.sh: $BIN does not run on this machine (wrong architecture?)" >&2
  exit 1
fi

# 1. The snp user (idempotent).
if ! id -u snp >/dev/null 2>&1; then
  useradd -r -m -U -d /var/lib/snp -s /bin/false snp
fi

# 2. The binary.
install -m 0755 -o root -g root "$BIN" /usr/local/bin/snp

# 3. The starter config (never overwrites an existing one).
install -d -m 0700 -o snp -g snp /var/lib/snp/.config/snp
CFG=/var/lib/snp/.config/snp/config.toml
if [ -e "$CFG" ]; then
  echo "install.sh: keeping existing $CFG"
else
  cat > "$CFG" <<EOF
hostname  = "$HOSTNAME_"
owner     = "$OWNER"
log_level = "info"
EOF
  chown snp:snp "$CFG"
  chmod 0600 "$CFG"
fi

# 4. The auth key (first join only; the file is kept afterwards).
AUTHKEY_FILE=/var/lib/snp/.config/snp/authkey
if [ -n "${TS_AUTHKEY:-}" ]; then
  printf 'TS_AUTHKEY=%s\n' "$TS_AUTHKEY" > "$AUTHKEY_FILE"
  chown snp:snp "$AUTHKEY_FILE"
  chmod 0600 "$AUTHKEY_FILE"
else
  echo "install.sh: TS_AUTHKEY not set; the first start will need it."
  echo "  Write $AUTHKEY_FILE containing one line"
  echo "    TS_AUTHKEY=tskey-..."
  echo "  then: systemctl restart snp"
fi

# 5. The systemd unit.
install -m 0644 "$SCRIPT_DIR/snp.service" /etc/systemd/system/snp.service

# 6. The backup script and a daily cron entry.
install -m 0755 -o root -g root "$SCRIPT_DIR/backup.sh" /usr/local/sbin/snp-backup
install -d -m 0700 -o snp -g snp /var/lib/snp/backups
cat > /etc/cron.d/snp-backup <<'EOF'
# Daily snp backup at 03:17, 7-day retention (deploy/backup.sh).
17 3 * * * snp /usr/local/sbin/snp-backup /var/lib/snp/backups
EOF
chmod 0644 /etc/cron.d/snp-backup

# 7. Enable and start.
systemctl daemon-reload
systemctl enable --now snp

echo
echo "snp installed."
echo "  status:  systemctl status snp"
echo "  logs:    journalctl -u snp -f"
echo "  access:  https://$HOSTNAME_.<your-tailnet>.ts.net   (DNS name, not a 100.x IP)"
