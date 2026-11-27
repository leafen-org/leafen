# The benchmark's tool

These scripts make the numbers in [docs/benchmark.md](../docs/benchmark.md)
again: synthetic data on a server of their own, then each load level on a
copy of it. They use synthetic accounts (`bench-…@example.test`) and refuse
the port a production Leafen listens on (49163); run them on a computer with
nothing else to do, never against a server people use.

You need Node 22 or later, Go (to build the server), `curl` and `sqlite3`.
From the repository's top folder:

```sh
go build -o bench-run/leafen ./desktop
```

## 1. The data

```sh
LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/data.sh
```

This starts the server on a fresh data folder (`bench-run/base/data`, port
`BENCH_PORT`, 18995 unless set), signs up `ACCOUNTS` accounts (500,000) and
puts them all in one workspace, makes the owner's `PRIVATE` private pages
(200) and `SHARED` pages (200) in an open teamspace the whole workspace may
edit, each with `PARAGRAPHS` paragraphs (30), and keeps the first `TOKENS`
members' logins (60,000) for the load in `bench-run/base/state.json`. Then
it waits — printing the jobs left every half minute — until the server has
worked through the visibility list's jobs the new members made, and stops
it. With 500,000 accounts this took 75 minutes of sign-ups, 8 of members and
16 of jobs on the machine in docs/benchmark.md, and 2.9 GB of disk.

`SIGNUP_AT_ONCE` (64) and `MEMBERS_AT_ONCE` (32) set how many it asks at once.

## 2. The levels

```sh
LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/level.sh 300k 30000
LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/level.sh 400k 40000
LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/level.sh 500k 50000
```

Each level copies the stopped data (a clone where the disk can, so it costs
nothing), starts the server on the copy, gives it half a minute, and runs the
load: the second number of people at once, each its own account, each asking
about every `THINK` milliseconds — the third argument, 100,000 for Microsoft's
typical user (the default) and 60,000 for its heavy one. `LOAD_SECONDS` (600) sets
how long; the first think time isn't counted. `MIX` sets the requests by
weight (`read_graph:50,read_page:25,search:10,write_block:15`). The server
runs with `LEAFEN_MEMORY_LIMIT` (1536m unless set) and the request limits off
(`LEAFEN_RATE_LIMIT_PROFILE=test`, never for a server people use). The copy
goes when the level ends.

## 3. The results

```sh
BENCH_DIR=bench-run node bench/report.mjs
```

prints each level's line of `bench-run/results.jsonl` as the table in
docs/benchmark.md.

A small run to see the steps work, in minutes:

```sh
ACCOUNTS=300 TOKENS=300 SHARED=20 PRIVATE=20 LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/data.sh
LOAD_SECONDS=90 LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/level.sh small 300 10000
BENCH_DIR=bench-run node bench/report.mjs
```
