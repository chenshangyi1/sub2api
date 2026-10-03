#!/bin/sh
set -eu

mode="${1:-panel}"
if [ "$#" -gt 0 ]; then
  shift
fi

case "$mode" in
  panel)
    exec /usr/local/bin/upstream-ops "$@"
    ;;
  worker)
    cd /app/worker
    exec node dist/monitor.mjs "$@"
    ;;
  migrate)
    exec /usr/local/bin/upstream-ops-migrate "$@"
    ;;
  *)
    exec /usr/local/bin/upstream-ops "$mode" "$@"
    ;;
esac
