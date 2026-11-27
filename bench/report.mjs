// Prints the benchmark's results (BENCH_DIR/results.jsonl) as the table
// docs/benchmark.md shows: each level's requests a second, its latencies and
// what failed, the server's processor and memory.
import { readFileSync } from 'node:fs'
import { RUN } from './lib.mjs'

const rows = readFileSync(new URL('results.jsonl', RUN), 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line))
const ms = (value) => value >= 1000 ? `${(value / 1000).toFixed(1)} s` : `${value} ms`
const three = (r) => r ? `${ms(r.p50)} / ${ms(r.p95)} / ${ms(r.p99)}` : '—'
const failed = (row) => Object.entries(row.requests).filter(([, r]) => r.errors).map(([name, r]) => `${name} ${r.errors} (${Object.keys(r.codes).join(', ')})`).join('; ') || 'none'
console.log('| People at once (organization) | Requests a second | All p50 / p95 / p99 | Graph p95 | Page p95 | Search p50 / p95 / p99 | Writes p50 / p95 / p99 | Failed | Server CPU / memory |')
console.log('| --- | --- | --- | --- | --- | --- | --- | --- | --- |')
for (const row of rows) {
  const r = row.requests
  console.log(`| ${row.users.toLocaleString('en')} (${row.people.toLocaleString('en')}) | ${row.total.perSecond} | ${three(row.total)} | ${r.read_graph ? ms(r.read_graph.p95) : '—'} | ` +
    `${r.read_page ? ms(r.read_page.p95) : '—'} | ${three(r.search)} | ${three(r.write_block)} | ${failed(row)} | ${row.cpuAverage ?? '—'}% / ${row.memoryMostMB ?? '—'} MB |`)
}
