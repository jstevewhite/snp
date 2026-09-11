#!/usr/bin/env bash
# snp — cron-able backup (spec §7).
#
# usage: backup.sh DEST_DIR [retention_days]
#
#   DEST_DIR        where backups go (created if missing)
#   retention_days  delete snp-*.db files older than this many days
#                   (default 7; the RETENTION_DAYS env var or the second
#                   argument override it)
#
# Writes DEST_DIR/snp-YYYYmmdd-HHMMSS.db — a consistent VACUUM INTO copy
# that the snp binary verifies with an integrity check before the script
# finishes — then copies the encryption key alongside as snp.key
# (overwriting), and prunes old backups.
#
# The key file is required to read sensitive snippets from a backup: keep
# it as safe as the database. Copying backups offsite (rsync/rclone) is
# the operator's job.
#
# Quiet on success (cron-friendly); errors go to stderr with exit 1.
set -euo pipefail

SNP_BIN="${SNP_BIN:-/usr/local/bin/snp}"

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: backup.sh DEST_DIR [retention_days]" >&2
  exit 2
fi

DEST_DIR=$1
RETENTION_DAYS=${2:-${RETENTION_DAYS:-7}}

case $RETENTION_DAYS in
  '' | *[!0-9]*)
    echo "backup.sh: retention_days must be a non-negative integer, got '$RETENTION_DAYS'" >&2
    exit 2
    ;;
esac

mkdir -p -- "$DEST_DIR"

ts=$(date +%Y%m%d-%H%M%S)
"$SNP_BIN" backup "$DEST_DIR/snp-$ts.db"

# The key file, resolved from config so a non-default state_dir works.
key_file=$("$SNP_BIN" key show-path)
cp -f -- "$key_file" "$DEST_DIR/snp.key"
chmod 0600 "$DEST_DIR/snp.key"

find "$DEST_DIR" -type f -name 'snp-*.db' -mtime +"$RETENTION_DAYS" -delete
