# Benchmark: how many people one server holds

One 10-core server holds up to a 300,000-person organization under
Microsoft's typical capacity-planning user model — 295 requests a second, p95
48 ms — and about 180,000 people under its heavy model. This page says how to
make that number again first, then how it was measured, what came out and
what it leaves out.

## The model

Microsoft's capacity planning for SharePoint
([Capacity planning](https://learn.microsoft.com/en-us/previous-versions/tn-archive/cc287743(v=office.12)),
in Microsoft Learn's archive) sizes a server by its users' requests: a tenth of
all the users at work at once, each making a request every 100 seconds for a
"typical" user and every 60 for a "heavy" one. One request a second is then
1,000 typical users or 600 heavy ones.

## Making it again

The tool is in [`bench/`](../bench/README.md), with synthetic data only.

1. Build the server: `go build -o bench-run/leafen ./desktop`.
2. Make the data: `LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/data.sh`.
   It starts a server of its own on a fresh folder, signs up 500,000 accounts,
   puts them in one workspace, makes the pages, and waits until the server has
   worked through the jobs the new members made — it prints the jobs left
   every half minute, and is done at 0 — then stops it. Here this took about
   100 minutes and 2.9 GB.
3. Run the level on a copy of that data:
   `LEAFEN_BIN=bench-run/leafen BENCH_DIR=bench-run sh bench/level.sh 300k 30000`.
   It takes about 11 minutes; the same command runs any other level.
4. Read the results: `BENCH_DIR=bench-run node bench/report.mjs` prints the
   table below.

Each step's sizes — accounts, pages, people at once, think time, the request
mix, the length — are the tool's variables (`bench/README.md`); a small run
of a few hundred accounts goes through every step in minutes.

## How it was measured

- **Machine:** a Mac Studio (M1 Max: 8 performance and 2 efficiency cores,
  64 GiB of memory). One server, `LEAFEN_MEMORY_LIMIT=1536m`. The load came
  from the same machine (Node v26.7.0, one process).
- **Data (synthetic):** 500,000 accounts, all members of one workspace; the
  owner's 200 private pages; 200 pages in an open teamspace the whole
  workspace may edit (`openPageRole: edit`), 30 paragraphs each. Members
  don't join the teamspace.
- **Making it:** sign-ups 64 at a time, about 114 a second, 75 minutes;
  members 32 at a time, about 800 a second, 8 minutes; then the server left
  to work through the visibility list's 997,000 jobs, 16 minutes, the database
  2.92 GB. The server stopped, the level ran on an APFS clone of that data.
- **Load:** each person at once uses an account of their own (the first
  60,000 kept logins). Each starts at a random moment within the think time
  and after each answer waits the think time × 0.5–1.5. Requests by weight:
  reading a page's graph 50 (`GET /pages/{id}/content-graph`), reading a page
  25 (`GET /pages/{id}`), searching 10 (`GET /workspaces/{id}/search`, asking
  again with its cursor while it answers "indexing"), writing a block 15
  (`POST /pages/{id}/content-graph/mutations`, a paragraph at the end), each
  on one of the shared pages at random. Ten minutes; the first 100 seconds,
  while people come in, aren't counted.
- **Server settings:** `LEAFEN_RATE_LIMIT_PROFILE=test` (the request limits
  off, for a benchmark only — never for a server people use),
  `LEAFEN_SETUP_OPEN_MODE=print`.
- **Measured:** product commit `c1710ada3`, 2026-10-22.

## Results

Microsoft's typical model, a think time of 100 seconds:

| People at once (organization) | Requests a second | All p50 / p95 / p99 | Graph p95 | Page p95 | Search p50 / p95 / p99 | Writes p50 / p95 / p99 | Failed |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 30,000 (300,000) | 295.1 | 4 / 48 / 409 ms | 10 ms | 3 ms | 44 ms / 607 ms / 1.1 s | 6 / 28 / 68 ms | 2 searches (400, a stale cursor) |

The server's processor averaged 306%, its memory peaked at 984 MB, and the
machine sat 56% idle.

One server holds up to 300,000 people under the typical model. Under the
heavy model (60 seconds), 295 requests a second are 295 × 600 ≈ 180,000
people.

## What it leaves out

- This measurement confirms the scale up to 300,000 people.
- The load ran on the same machine as the server, which takes processor time
  from it.
- The data is synthetic: one workspace, short pages of the same words, no
  files, databases or history of years.
- Not in the load: editing together live (the collaborative documents'
  WebSocket), files, imports and exports, agents and automations.
- Results follow a core's own speed: a server with slower cores holds fewer
  people for the same count.
