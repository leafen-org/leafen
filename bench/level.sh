#!/bin/sh
# One level of the benchmark (docs/benchmark.md) on a copy of the stopped data
# data.sh made: a server of its own on the copy, half a minute to settle, the
# load (load.mjs), then the server stops and the copy goes. Results add to
# BENCH_DIR/results.jsonl.
#
#   LEAFEN_BIN=./bench-run/leafen BENCH_DIR=./bench-run sh bench/level.sh 300k 30000
#   (a name, the people at once, and their think time in ms: 100000 is
#   Microsoft's typical user, 60000 its heavy one)
set -eu
here=$(cd "$(dirname "$0")" && pwd)
bin=${LEAFEN_BIN:?give LEAFEN_BIN, the leafen server built from this source}
dir=${BENCH_DIR:-bench-run}
port=${BENCH_PORT:-18995}
name=${1:?give the level a name}
users=${2:?give how many people at once}
think=${3:-100000}
[ "$port" = 49163 ] && { echo "49163 is the port a production Leafen listens on; use another" >&2; exit 2; }
[ -f "$dir/base/state.json" ] || { echo "no data: run data.sh first" >&2; exit 2; }
curl -s -m 2 "http://127.0.0.1:$port/api/leafen/v1/health" >/dev/null && { echo "port $port is busy" >&2; exit 2; }
run="$dir/runs/$name"
rm -rf "${run:?}"
mkdir -p "$run"
# A copy that costs nothing where the disk can clone (APFS, Btrfs, XFS).
cp -c -R "$dir/base/data" "$run/data" 2>/dev/null || cp --reflink=auto -R "$dir/base/data" "$run/data" 2>/dev/null || cp -R "$dir/base/data" "$run/data"
cp "$dir/base/state.json" "$run/state.json"
LEAFEN_SETUP_OPEN_MODE=print LEAFEN_RATE_LIMIT_PROFILE=test LEAFEN_MEMORY_LIMIT=${LEAFEN_MEMORY_LIMIT:-1536m} \
  "$bin" --dir "$run/data" serve --http "127.0.0.1:$port" > "$run/server.log" 2>&1 &
server=$!
trap 'kill "$server" 2>/dev/null || true' EXIT
for _ in $(seq 1 120); do curl -s -m 2 "http://127.0.0.1:$port/api/leafen/v1/health" >/dev/null && break; sleep 1; done
sleep "${SETTLE_SECONDS:-30}"
BENCH_DIR="$run" BENCH_PORT=$port SERVER_PID=$server USERS=$users THINK=$think node --max-old-space-size=4096 "$here/load.mjs" | tee "$run/load.log"
tail -1 "$run/results.jsonl" >> "$dir/results.jsonl"
kill "$server"
wait "$server" 2>/dev/null || true
trap - EXIT
rm -rf "${run:?}/data"
