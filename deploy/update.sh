#!/usr/bin/env bash
# snp — pull main, rebuild, and restart the running server.
#
# usage: deploy/update.sh
#
# The tailscale auth key comes from $TS_AUTHKEY, or from the gitignored
# deploy/.authkey file (keep it chmod 600). The previous instance, if
# .pid names a live process, gets SIGTERM and is waited for: tsnet holds
# the state dir open while alive, so starting the new instance too early
# just stalls it on startup. The new pid is written to .pid and the
# server's output is appended to snp.out.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

PID_FILE=.pid
LOG_FILE=snp.out
HOSTNAME_=snip
OWNER=jstevewhite@gmail.com

if [[ -z "${TS_AUTHKEY:-}" ]]; then
  if [[ -f deploy/.authkey ]]; then
    TS_AUTHKEY=$(<deploy/.authkey)
  else
    echo "update.sh: no auth key; set TS_AUTHKEY or create deploy/.authkey" >&2
    exit 1
  fi
fi
export TS_AUTHKEY

echo "==> git pull"
git pull --ff-only

echo "==> make build"
make build

echo "==> stop previous instance"
if [[ -f $PID_FILE ]]; then
  pid=$(cat "$PID_FILE")
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    for _ in {1..120}; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.5
    done
    if kill -0 "$pid" 2>/dev/null; then
      echo "update.sh: pid $pid ignored SIGTERM for 60s, sending SIGKILL" >&2
      kill -9 "$pid"
      sleep 1
    fi
  fi
  rm -f "$PID_FILE"
fi

echo "==> start new instance"
nohup ./bin/snp serve --hostname "$HOSTNAME_" --owner "$OWNER" >>"$LOG_FILE" 2>&1 &
echo $! >"$PID_FILE"

sleep 1
pid=$(cat "$PID_FILE")
if kill -0 "$pid" 2>/dev/null; then
  echo "snp running: pid $pid (see $LOG_FILE)"
else
  echo "update.sh: snp exited immediately; last log lines:" >&2
  tail -n 20 "$LOG_FILE" >&2
  rm -f "$PID_FILE"
  exit 1
fi
