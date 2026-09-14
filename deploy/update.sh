#!/usr/bin/env bash
# snp — deploy origin/main to this host, with rollback.
#
# usage: deploy/update.sh [-n hostname] [-U health-url] [-t seconds]
#                         [-s unit] [-k keep] [-f]
#
#   -n hostname   tailnet node name; the health URL defaults to
#                 https://<hostname>.<MagicDNS suffix>/api/me. Default:
#                 the --hostname the installed unit runs with, else snp
#   -U url        health URL to poll instead of the derived one
#   -t seconds    how long to wait for the health check (default: 60)
#   -s unit       systemd user unit that runs the server (default: snp.service)
#   -k keep       pre-deploy database backups to keep (default: 5)
#   -f            deploy even when HEAD already equals origin/main
#                 (rebuild and restart)
#
# Runs from a checkout on `main` with a clean tree; the server is the
# systemd user unit installed by deploy/install-autoupdate.sh, and the
# health check must run from a device logged in as the owner, because
# /api/me answers 200 only to the owner.
#
# Sequence:
#   1. fetch; stop if HEAD == origin/main (unless -f)
#   2. keep the current binary as bin/snp.prev
#   3. fast-forward main and `make build` — the running server is not
#      touched until both have succeeded
#   4. back up the database with the *previous* binary (the new one
#      would migrate the schema on open) into
#      $SNP_STATE_DIR/pre-deploy/ (default ~/.local/share/snp/pre-deploy)
#   5. restart the unit and poll the health URL
#   6. on failure: stop the unit, put the previous binary back, restore
#      the backup if the schema version changed (the failed database is
#      kept alongside as snp.db.failed-<stamp>), reset the checkout to
#      the previous commit, start again, and record the bad commit in
#      deploy/.last-failed so deploy/autoupdate.sh skips it until
#      origin/main moves on
#
# Exit status: 0 deployed (or nothing to do), 1 rolled back, 2 rollback
# itself failed and the server needs manual attention.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

HOSTNAME_=""   # resolved by health_url: -n, else the unit's --hostname, else snp
HEALTH_URL=""
TIMEOUT=60
UNIT=snp.service
KEEP=5
FORCE=0

usage() {
  sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'
}

while getopts 'n:U:t:s:k:fh' opt; do
  case $opt in
    n) HOSTNAME_=$OPTARG ;;
    U) HEALTH_URL=$OPTARG ;;
    t) TIMEOUT=$OPTARG ;;
    s) UNIT=$OPTARG ;;
    k) KEEP=$OPTARG ;;
    f) FORCE=1 ;;
    h) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done

STATE_DIR=${SNP_STATE_DIR:-$HOME/.local/share/snp}
BACKUP_DIR=$STATE_DIR/pre-deploy
DB=$STATE_DIR/snp.db
FAILED_MARK=deploy/.last-failed
STUB=web/dist/index.html

log()  { printf '==> %s\n' "$*"; }
warn() { printf 'update.sh: %s\n' "$*" >&2; }
die()  { warn "$@"; exit 2; }

# The node name the unit actually runs with (its ExecStart --hostname),
# so a hand run without -n checks the right host. A wrong default here
# is not harmless: the probe never reaches the server and a healthy
# deploy gets rolled back.
unit_hostname() {
  systemctl --user show "$UNIT" -p ExecStart --value 2>/dev/null |
    sed -n 's/.*--hostname[= ]\([^ ;}]*\).*/\1/p' | head -n 1
}

health_url() {
  if [[ -n $HEALTH_URL ]]; then
    printf '%s' "$HEALTH_URL"
    return
  fi
  if [[ -z $HOSTNAME_ ]]; then
    HOSTNAME_=$(unit_hostname)
    [[ -n $HOSTNAME_ ]] || HOSTNAME_=snp
  fi
  command -v tailscale >/dev/null ||
    die "no -U and no tailscale CLI to derive the health URL from"
  local suffix
  suffix=$(tailscale status --json 2>/dev/null |
    sed -n 's/.*"MagicDNSSuffix": *"\([^"]*\)".*/\1/p' | head -n 1)
  [[ -n $suffix ]] || die "could not read the MagicDNS suffix from tailscale status"
  printf 'https://%s.%s/api/me' "$HOSTNAME_" "$suffix"
}

# wait_healthy URL — poll until the URL answers 2xx or TIMEOUT elapses.
wait_healthy() {
  local deadline=$((SECONDS + TIMEOUT))
  while (( SECONDS < deadline )); do
    if curl -fsS -m 5 -o /dev/null "$1" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

# schema_version FILE — the store's schema version, or "" when sqlite3
# is not installed (the caller then restores unconditionally).
schema_version() {
  command -v sqlite3 >/dev/null || return 0
  sqlite3 -readonly "$1" \
    'SELECT COALESCE(MAX(version), 0) FROM schema_version' 2>/dev/null || true
}

URL=$(health_url)

# --- 1. sanity + fetch ------------------------------------------------
branch=$(git symbolic-ref --short -q HEAD || true)
[[ $branch == main ]] ||
  die "checkout is on '${branch:-detached HEAD}', not main; refusing to deploy from it"
git checkout -q -- "$STUB"   # every build rewrites the stub; that is not a change
if [[ -n $(git status --porcelain --untracked-files=no) ]]; then
  die "working tree has uncommitted changes; commit or stash them first"
fi
[[ -x bin/snp ]] || die "bin/snp is missing; run make build once first"

# Preflight the health check against the server that is running now. A
# URL that does not answer 200 before the deploy is a wrong URL, not a
# broken deploy, and would only turn into a rollback of a healthy
# server sixty seconds from now. Skipped when the unit is not running,
# since then there is nothing to compare against.
if systemctl --user is-active --quiet "$UNIT"; then
  curl -fsS -m 5 -o /dev/null "$URL" 2>/dev/null ||
    die "health check $URL does not answer 200 against the running server; fix -n/-U before deploying"
else
  warn "$UNIT is not running; skipping the health-check preflight"
fi

prev=$(git rev-parse HEAD)
log "git fetch origin main"
git fetch -q origin main
target=$(git rev-parse origin/main)
if [[ $prev == "$target" ]]; then
  if [[ $FORCE == 0 ]]; then
    log "already at ${prev:0:7}; nothing to deploy (use -f to rebuild anyway)"
    exit 0
  fi
elif ! git merge-base --is-ancestor "$prev" "$target"; then
  die "HEAD ${prev:0:7} is not behind origin/main ${target:0:7} (local commits ahead, or diverged); refusing to deploy"
fi

# --- 2–3. keep the old binary, fast-forward, build --------------------
cp -p bin/snp bin/snp.prev
log "git merge --ff-only ${prev:0:7} → ${target:0:7}"
git merge -q --ff-only origin/main
log "make build"
make build
git checkout -q -- "$STUB"

# --- 4. back up the database with the previous binary -----------------
stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup=$BACKUP_DIR/snp-$stamp-${prev:0:7}.db
mkdir -p "$BACKUP_DIR"
log "backup → $backup"
./bin/snp.prev backup "$backup"
ls -1t "$BACKUP_DIR"/snp-*.db 2>/dev/null | tail -n +"$((KEEP + 1))" | xargs -r rm -f

# --- 5. restart and watch ---------------------------------------------
log "systemctl --user restart $UNIT"
systemctl --user restart "$UNIT"
log "waiting up to ${TIMEOUT}s for $URL"
if wait_healthy "$URL"; then
  rm -f "$FAILED_MARK" bin/snp.prev
  log "deployed $(git rev-parse --short HEAD) ($(./bin/snp version 2>/dev/null || echo 'version unknown')); $URL healthy"
  exit 0
fi

# --- 6. roll back -----------------------------------------------------
warn "$URL not healthy within ${TIMEOUT}s; rolling back ${target:0:7} → ${prev:0:7}"
printf '%s\n%s\n' "$target" "$stamp" >"$FAILED_MARK"
systemctl --user stop "$UNIT" || true
journalctl --user -u "$UNIT" -n 20 --no-pager >&2 || true

mv -f bin/snp bin/snp.failed
mv -f bin/snp.prev bin/snp

if [[ -f $DB ]]; then
  live=$(schema_version "$DB")
  saved=$(schema_version "$backup")
  if [[ -n $live && $live == "$saved" ]]; then
    warn "schema version $live unchanged; keeping the live database"
  else
    warn "restoring $backup (live schema '${live:-?}', backup '${saved:-?}'); failed database kept as $DB.failed-$stamp"
    mv -f "$DB" "$DB.failed-$stamp"
    [[ -f $DB-wal ]] && mv -f "$DB-wal" "$DB.failed-$stamp-wal"
    [[ -f $DB-shm ]] && mv -f "$DB-shm" "$DB.failed-$stamp-shm"
    cp -p "$backup" "$DB"
  fi
fi

git reset -q --keep "$prev" || warn "git reset --keep $prev failed; checkout left at ${target:0:7}"
git checkout -q -- "$STUB"

systemctl --user start "$UNIT"
if wait_healthy "$URL"; then
  warn "rolled back to ${prev:0:7}; ${target:0:7} is recorded in $FAILED_MARK and will be skipped until origin/main moves"
  exit 1
fi
warn "ROLLBACK FAILED: $URL still unhealthy on ${prev:0:7}; the server needs manual attention"
exit 2
