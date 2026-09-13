#!/usr/bin/env bash
# snp — timer target: deploy when origin/main has moved.
#
# usage: deploy/autoupdate.sh [update.sh options]
#
# Fetches origin/main and hands off to deploy/update.sh when it differs
# from HEAD. Skips, without touching anything, when the checkout is not
# on main (exit 0) or when origin/main is the commit recorded in
# deploy/.last-failed by a rolled-back deploy (exit 1, so the oneshot
# unit stays failed and `systemctl --user --failed` shows it until
# origin/main moves on).
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

FAILED_MARK=deploy/.last-failed

branch=$(git symbolic-ref --short -q HEAD || true)
if [[ $branch != main ]]; then
  echo "autoupdate: checkout is on '${branch:-detached HEAD}', not main; skipping"
  exit 0
fi

git fetch -q origin main
head=$(git rev-parse HEAD)
remote=$(git rev-parse origin/main)
if [[ $head == "$remote" ]]; then
  exit 0
fi
if ! git merge-base --is-ancestor "$head" "$remote"; then
  echo "autoupdate: local main ${head:0:7} is ahead of or diverged from origin/main ${remote:0:7}; skipping"
  exit 0
fi

if [[ -f $FAILED_MARK && $(head -n 1 "$FAILED_MARK") == "$remote" ]]; then
  echo "autoupdate: origin/main ${remote:0:7} failed its last deploy (see $FAILED_MARK); skipping until it moves" >&2
  exit 1
fi

echo "autoupdate: origin/main moved ${head:0:7} → ${remote:0:7}; deploying"
exec deploy/update.sh "$@"
