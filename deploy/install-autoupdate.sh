#!/usr/bin/env bash
# snp — run the server from this checkout as a systemd user unit and
# redeploy automatically whenever origin/main moves.
#
# usage: deploy/install-autoupdate.sh [-n hostname] [-U health-url] [-i interval]
#
#   -n hostname   tailnet node name passed to `snp serve` (default: snp)
#   -U url        health URL for deploy/update.sh; default derived at
#                 deploy time as https://<hostname>.<MagicDNS suffix>/api/me
#   -i interval   how often to check origin/main (systemd time span,
#                 default: 5min)
#
# What it does:
#   * renders deploy/user/{snp.service,snp-update.service,snp-update.timer}
#     into ~/.config/systemd/user/ with this checkout's path baked in
#   * rewrites a bare-key deploy/.authkey into the TS_AUTHKEY=... form
#     the unit's EnvironmentFile needs (the key itself is unchanged)
#   * stops an instance started by the old nohup-style script if .pid
#     names a live process
#   * enables and starts snp.service and snp-update.timer
#
# Requires bin/snp already built (`make build`) and lingering enabled for
# this user (`sudo loginctl enable-linger $USER`) so the units outlive
# the login session. Re-run after changing options; it is idempotent.
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO=$(cd -- "$SCRIPT_DIR/.." && pwd)

HOSTNAME_=snp
HEALTH_URL=""
INTERVAL=5min

usage() {
  sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'
}

while getopts 'n:U:i:h' opt; do
  case $opt in
    n) HOSTNAME_=$OPTARG ;;
    U) HEALTH_URL=$OPTARG ;;
    i) INTERVAL=$OPTARG ;;
    h) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done

die() { printf 'install-autoupdate.sh: %s\n' "$*" >&2; exit 1; }

[[ -x $REPO/bin/snp ]] || die "no $REPO/bin/snp; run make build first"
command -v systemctl >/dev/null || die "systemctl not found; this needs a systemd host"

if [[ $(loginctl show-user "$USER" -p Linger --value 2>/dev/null) != yes ]]; then
  echo "warning: lingering is off for $USER, so the units stop with your login session." >&2
  echo "         Fix with: sudo loginctl enable-linger $USER" >&2
fi

# --- auth key file → EnvironmentFile form ------------------------------
authkey=$REPO/deploy/.authkey
if [[ -f $authkey ]] && ! grep -q '^TS_AUTHKEY=' "$authkey"; then
  key=$(tr -d '[:space:]' <"$authkey")
  printf 'TS_AUTHKEY=%s\n' "$key" >"$authkey"
  chmod 600 "$authkey"
  echo "rewrote deploy/.authkey as TS_AUTHKEY=... for the unit's EnvironmentFile"
fi

# --- render the units --------------------------------------------------
unit_dir=$HOME/.config/systemd/user
mkdir -p "$unit_dir"
health_arg=""
[[ -n $HEALTH_URL ]] && health_arg="-U $HEALTH_URL"

render() {  # render SRC DEST
  sed -e "s|@REPO@|$REPO|g" \
      -e "s|@HOSTNAME@|$HOSTNAME_|g" \
      -e "s|@HEALTH_ARG@|$health_arg|g" \
      -e "s|@INTERVAL@|$INTERVAL|g" \
      "$1" >"$2"
  echo "installed $2"
}
render "$SCRIPT_DIR/user/snp.service"        "$unit_dir/snp.service"
render "$SCRIPT_DIR/user/snp-update.service" "$unit_dir/snp-update.service"
render "$SCRIPT_DIR/user/snp-update.timer"   "$unit_dir/snp-update.timer"

# --- retire an instance from the old nohup-style update.sh ------------
if [[ -f $REPO/.pid ]]; then
  pid=$(cat "$REPO/.pid")
  if kill -0 "$pid" 2>/dev/null; then
    echo "stopping the nohup-started snp (pid $pid) so the unit can take over"
    kill "$pid"
    for _ in {1..120}; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.5
    done
    kill -0 "$pid" 2>/dev/null && kill -9 "$pid"
  fi
  rm -f "$REPO/.pid"
fi

# --- enable -----------------------------------------------------------
systemctl --user daemon-reload
systemctl --user enable --now snp.service
systemctl --user enable --now snp-update.timer

cat <<MSG

snp.service and snp-update.timer are enabled for $USER.

  systemctl --user status snp            # the server
  journalctl --user -u snp -f            # its log
  systemctl --user list-timers snp-update.timer
  journalctl --user -u snp-update        # deploy history
  systemctl --user --failed              # a rolled-back deploy shows here

Deploy by hand:  $REPO/deploy/update.sh -n $HOSTNAME_ ${health_arg}
MSG
