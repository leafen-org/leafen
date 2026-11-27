import http from 'node:http'
import { mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'

// What the benchmark's scripts share (docs/benchmark.md): the server they
// talk to on this computer, the folder a run keeps its state and results in,
// and a client that times each request.

export const PORT = Number(process.env.BENCH_PORT ?? 18995)
if (!Number.isInteger(PORT) || PORT <= 0) throw new Error('BENCH_PORT names no port')
if (PORT === 49163) throw new Error('49163 is the port a production Leafen listens on; benchmark a server of its own on another port')
export const RUN = pathToFileURL(resolve(process.env.BENCH_DIR ?? 'bench-run') + '/')
mkdirSync(RUN, { recursive: true })
export const api = '/api/leafen/v1'

// Synthetic accounts only: their addresses and one password of the run's.
export const PASSWORD = process.env.BENCH_PASSWORD ?? 'bench-synthetic-password-123'
export const email = (name) => `bench-${name}@example.test`

const agent = new http.Agent({ keepAlive: true, maxSockets: Infinity })

// call is one request to the server, its answer and how long it took.
export function call(method, path, token, body) {
  return new Promise((done) => {
    const data = body === undefined ? null : Buffer.from(JSON.stringify(body))
    const started = performance.now()
    const request = http.request({
      host: '127.0.0.1', port: PORT, agent, method, path: api + path,
      headers: {
        ...(token ? { authorization: `Bearer ${token}` } : {}),
        ...(data ? { 'content-type': 'application/json', 'content-length': data.length } : {}),
      },
    }, (response) => {
      const chunks = []
      response.on('data', (chunk) => chunks.push(chunk))
      response.on('end', () => {
        const text = Buffer.concat(chunks).toString()
        let json = null
        try { json = JSON.parse(text) } catch {}
        done({ status: response.statusCode, json, text, ms: performance.now() - started })
      })
    })
    request.on('error', (error) => done({ status: 0, json: null, text: String(error), ms: performance.now() - started }))
    request.setTimeout(30000, () => { request.destroy(new Error('timeout')) })
    if (data) request.write(data)
    request.end()
  })
}

// pool runs fn over items, limit at a time.
export async function pool(items, limit, fn) {
  let next = 0
  const out = []
  await Promise.all(Array.from({ length: limit }, async () => {
    while (next < items.length) { const at = next++; out[at] = await fn(items[at], at) }
  }))
  return out
}

// searchAll searches as the app does: while the server answers "indexing"
// (more matches to look through), it asks again with its cursor.
export async function searchAll(workspaceId, q, token, most = 50) {
  let cursor = null, ms = 0, calls = 0, answer
  do {
    answer = await call('GET', `/workspaces/${workspaceId}/search?q=${encodeURIComponent(q)}${cursor ? `&indexCursor=${encodeURIComponent(cursor)}` : ''}`, token)
    ms += answer.ms; calls++
    cursor = answer.json?.indexing ? answer.json.indexCursor : null
  } while (cursor && calls < most && answer.status === 200)
  return { ...answer, ms, calls }
}

export const sleep = (ms) => new Promise((done) => setTimeout(done, ms))

// The words pages are written with, Korean and English, so search finds them.
export const words = '계약서 견적서 회의록 일정 과제 고객 거래처 설치 점검 보고 품질 납품 contract invoice meeting schedule task customer supplier install review quality delivery'.split(' ')
export const sentence = (n) => Array.from({ length: 8 + (n % 7) }, (_, i) => words[(n * 7 + i * 3) % words.length]).join(' ')
