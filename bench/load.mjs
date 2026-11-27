// One level of the benchmark's load (docs/benchmark.md), Microsoft's
// capacity-planning user model: USERS people at once, each its own account
// while the kept sign-ins last, each making a request every THINK ms on
// average (its "typical" user 100,000, its "heavy" one 60,000) — a pause of
// THINK × (0.5–1.5) after each answer, the first start anywhere within THINK.
// The requests are MIX by weight. LOAD_SECONDS long; the first THINK, while people
// come in, isn't counted. It appends a line of results to results.jsonl in
// BENCH_DIR, sampling the server's processor and memory by its process ID
// (SERVER_PID, else server.pid there).
import { appendFileSync, existsSync, readFileSync } from 'node:fs'
import { execSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { RUN, call, searchAll, sleep } from './lib.mjs'

const state = JSON.parse(readFileSync(new URL('state.json', RUN)))
const USERS = Number(process.env.USERS), THINK = Number(process.env.THINK ?? 100000), SECONDS = Number(process.env.LOAD_SECONDS ?? 600)
if (!(USERS > 0) || !(THINK > 0) || !(SECONDS * 1000 > THINK)) throw new Error('give USERS, and a THINK shorter than LOAD_SECONDS')
const MIX = process.env.MIX ?? 'read_graph:50,read_page:25,search:10,write_block:15'
const pidFile = new URL('server.pid', RUN)
const pid = process.env.SERVER_PID ?? (existsSync(pidFile) ? readFileSync(pidFile, 'utf8').trim() : '')
const terms = ['계약서', '견적서', '회의록', 'contract', 'invoice', '설치', 'quality', '납품']
const pick = (list) => list[Math.floor(Math.random() * list.length)]
const requests = {
  read_graph: (user) => call('GET', `/pages/${pick(state.pages).id}/content-graph`, user.token),
  read_page: (user) => call('GET', `/pages/${pick(state.pages).id}`, user.token),
  search: (user) => searchAll(state.workspaceId, pick(terms), user.token),
  write_block: (user) => call('POST', `/pages/${pick(state.pages).id}/content-graph/mutations`, user.token, { mutationId: randomUUID(), operations: [{ kind: 'upsertBlock',
    block: { id: randomUUID(), parentId: null, position: `z${Date.now()}${Math.random().toString(36).slice(2, 6)}`, type: 'paragraph', content: { rich: [{ text: `벤치 ${pick(terms)} ${Date.now()}` }] } } }] }),
}
const weights = MIX.split(',').map((part) => { const [name, weight] = part.split(':'); if (!requests[name]) throw new Error(`no request ${name}`); return [Number(weight), name] })
const total = weights.reduce((sum, [weight]) => sum + weight, 0)
const choose = () => { let r = Math.random() * total; for (const [weight, name] of weights) { if ((r -= weight) < 0) return name } return weights[0][1] }
const percentile = (sorted, p) => sorted.length ? sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))] : null

const stats = {}, samples = []
const started = Date.now(), counted = started + THINK, end = started + SECONDS * 1000
const sampler = pid ? setInterval(() => {
  try {
    const [cpu, rss] = execSync(`ps -o %cpu=,rss= -p ${Number(pid)}`).toString().trim().split(/\s+/u).map(Number)
    samples.push({ cpu, rss })
  } catch {}
}, 5000) : null
await Promise.all(Array.from({ length: USERS }, async (_, at) => {
  const user = state.users[at % state.users.length]
  await sleep(Math.random() * THINK)
  while (Date.now() < end) {
    const name = choose(), asked = Date.now()
    const answer = await requests[name](user)
    if (asked >= counted && asked < end) {
      const stat = (stats[name] ??= { ms: [], errors: 0, codes: {} })
      stat.ms.push(answer.ms)
      if (answer.status < 200 || answer.status >= 300) { stat.errors++; stat.codes[answer.status] = (stat.codes[answer.status] ?? 0) + 1 }
    }
    const wait = THINK * (0.5 + Math.random())
    if (Date.now() + wait >= end) break
    await sleep(wait)
  }
}))
if (sampler) clearInterval(sampler)

const window = (end - counted) / 1000
const row = { at: new Date().toISOString(), users: USERS, think: THINK, people: USERS * 10, mix: MIX, window, accounts: state.users.length, requests: {} }
let all = [], errors = 0
for (const [name, stat] of Object.entries(stats)) {
  stat.ms.sort((a, b) => a - b); all = all.concat(stat.ms); errors += stat.errors
  row.requests[name] = { n: stat.ms.length, perSecond: +(stat.ms.length / window).toFixed(1), p50: Math.round(percentile(stat.ms, 0.5)),
    p95: Math.round(percentile(stat.ms, 0.95)), p99: Math.round(percentile(stat.ms, 0.99)), errors: stat.errors, codes: stat.codes }
}
all.sort((a, b) => a - b)
row.total = { n: all.length, perSecond: +(all.length / window).toFixed(1), p50: Math.round(percentile(all, 0.5)), p95: Math.round(percentile(all, 0.95)),
  p99: Math.round(percentile(all, 0.99)), errors }
if (samples.length) {
  row.cpuAverage = Math.round(samples.reduce((sum, sample) => sum + sample.cpu, 0) / samples.length)
  row.cpuMost = Math.max(...samples.map((sample) => sample.cpu))
  row.memoryMostMB = Math.round(Math.max(...samples.map((sample) => sample.rss)) / 1024)
}
appendFileSync(new URL('results.jsonl', RUN), JSON.stringify(row) + '\n')
console.log(JSON.stringify(row))
process.exit(0)
