#!/bin/sh
# Makes the benchmark's data once (docs/benchmark.md): a server of its own on
# a fresh data folder, the synthetic data (setup.mjs, sized by its variables),
# then waits until the server has worked through the visibility list's jobs
# the data made, and stops it. Later levels run on copies of this stopped data.
#
#   LEAFEN_BIN=./bench-run/leafen BENCH_DIR=./bench-run sh bench/data.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
bin=${LEAFEN_BIN:?give LEAFEN_BIN, the leafen server built from this source}
dir=${BENCH_DIR:-bench-run}
port=${BENCH_PORT:-18995}
[ "$port" = 49163 ] && { echo "49163 is the port a production Leafen listens on; use another" >&2; exit 2; }
command -v sqlite3 >/dev/null || { echo "sqlite3 is needed to see the server's jobs" >&2; exit 2; }
mkdir -p "$dir"
[ -e "$dir/base" ] && { echo "$dir/base is there already; move it away to make the data again" >&2; exit 2; }
curl -s -m 2 "http://127.0.0.1:$port/api/leafen/v1/health" >/dev/null && { echo "port $port is busy" >&2; exit 2; }
mkdir -p "$dir/base/data"
LEAFEN_SETUP_OPEN_MODE=print LEAFEN_RATE_LIMIT_PROFILE=test LEAFEN_MEMORY_LIMIT=${LEAFEN_MEMORY_LIMIT:-1536m} \
  "$bin" --dir "$dir/base/data" serve --http "127.0.0.1:$port" > "$dir/base/server.log" 2>&1 &
server=$!
echo "$server" > "$dir/base/server.pid"
trap 'kill "$server" 2>/dev/null || true' EXIT
BENCH_DIR="$dir/base" BENCH_PORT=$port node "$here/setup.mjs"
# The visibility list's jobs: each member put in a workspace is one.
while :; do
  left=$(sqlite3 -readonly "$dir/base/data/data.db" "select count(*) from leafen_bootstrap_visibility_projection_jobs;" 2>/dev/null | tail -1)
  echo "$(date '+%H:%M:%S') jobs left: ${left:-?}"
  [ "$left" = 0 ] && break
  sleep 30
done
kill "$server"
wait "$server" 2>/dev/null || true
trap - EXIT
rm -f "$dir/base/server.pid"
echo "data made in $dir/base"
