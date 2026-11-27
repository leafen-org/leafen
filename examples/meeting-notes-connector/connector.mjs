#!/usr/bin/env node
// An example Leafen connector, outside Leafen: when a page is created or
// renamed with a title that starts with "Meeting" while it is still empty,
// it fills the page with an agenda, notes and action items. It shows the native API's pieces end to end —
// a key or an app token, a signed event webhook, reading a page and writing
// blocks with an idempotent content-graph mutation.
//
//   node connector.mjs authorize   get an app token for a registered app (OAuth + PKCE)
//   node connector.mjs subscribe   send a workspace's new and renamed pages to this connector
//   node connector.mjs serve       receive the events and fill new meeting pages
//
// Node 20 or later; no dependencies. See README.md.

import { createHash, createHmac, randomBytes, randomUUID, timingSafeEqual } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'

const env = process.env
const leafenUrl = (env.LEAFEN_URL ?? 'http://127.0.0.1:8090').replace(/\/+$/, '')
const api = `${leafenUrl}/api/leafen/v1`
const tokenFile = env.CONNECTOR_TOKEN_FILE ?? '.connector-token.json'

function token() {
  if (env.LEAFEN_TOKEN) return env.LEAFEN_TOKEN
  try {
    return JSON.parse(readFileSync(tokenFile, 'utf8')).access_token
  } catch {
    throw new Error('Set LEAFEN_TOKEN to a key (lmc_…) or run "node connector.mjs authorize" first.')
  }
}

// One native API call. A request that can't be undone answers 428 with a
// Leafen-Confirmation header; this connector never makes one.
async function call(method, path, body) {
  const response = await fetch(api + path, {
    method,
    headers: {
      authorization: `Bearer ${token()}`,
      ...(body === undefined ? {} : { 'content-type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await response.text()
  const json = text ? JSON.parse(text) : null
  if (!response.ok) {
    const error = json?.error ?? {}
    throw new Error(`${method} ${path} → ${response.status} ${error.code ?? ''} ${error.message ?? text}`.trim())
  }
  return json
}

// --- authorize: a registered app's token, with the person's consent -------

async function authorize() {
  const clientId = env.LEAFEN_APP_ID
  const port = Number(env.CONNECTOR_PORT ?? 8787)
  if (!clientId) throw new Error('Set LEAFEN_APP_ID to the app id (lapp_…): the Client ID of its OAuth connection in the developer tools (Settings → Developer → Open developer tools → Connections).')
  const redirectUri = `http://127.0.0.1:${port}/callback`
  const verifier = randomBytes(32).toString('base64url')
  const state = randomBytes(16).toString('base64url')
  const address = new URL(`${leafenUrl}/oauth/authorize`)
  address.search = new URLSearchParams({
    response_type: 'code', client_id: clientId, redirect_uri: redirectUri, state,
    scope: env.LEAFEN_SCOPE ?? 'content', resource: api,
    code_challenge: createHash('sha256').update(verifier).digest('base64url'), code_challenge_method: 'S256',
  }).toString()
  console.log(`Register ${redirectUri} as the app's return address, then open:\n\n  ${address}\n`)
  const code = await new Promise((resolve, reject) => {
    const server = createServer((request, response) => {
      const url = new URL(request.url, redirectUri)
      if (url.pathname !== '/callback') return void response.writeHead(404).end()
      response.end('You can close this tab.')
      server.close()
      if (url.searchParams.get('state') !== state) reject(new Error('The answer was not for this request.'))
      else if (url.searchParams.get('error')) reject(new Error(`Not allowed: ${url.searchParams.get('error')}`))
      else resolve(url.searchParams.get('code'))
    }).listen(port, '127.0.0.1')
  })
  const exchanged = await fetch(`${leafenUrl}/oauth/token`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'authorization_code', code, redirect_uri: redirectUri, client_id: clientId,
      code_verifier: verifier, resource: api,
    }),
  })
  const tokens = await exchanged.json()
  if (!exchanged.ok) throw new Error(`Token exchange failed: ${JSON.stringify(tokens)}`)
  writeFileSync(tokenFile, JSON.stringify(tokens, null, 2), { mode: 0o600 })
  console.log(`Saved the app token to ${tokenFile} (scope ${tokens.scope}). Keep it out of version control.`)
}

// --- subscribe: send this workspace's new and renamed pages here ---------

async function subscribe() {
  const workspaceId = env.LEAFEN_WORKSPACE_ID
  const receiver = env.CONNECTOR_PUBLIC_URL
  if (!workspaceId || !receiver) throw new Error('Set LEAFEN_WORKSPACE_ID and CONNECTOR_PUBLIC_URL (where "serve" is reachable).')
  // A key needs the structure scope to subscribe; the same request id
  // answers with the same subscription and secret.
  const created = await call('POST', `/workspaces/${encodeURIComponent(workspaceId)}/webhooks`, {
    requestId: `meeting-notes-connector:${workspaceId}:${receiver}`,
    url: receiver, eventTypes: ['page.created', 'page.properties_updated'], description: 'Meeting notes connector',
  })
  console.log(`Subscribed ${created.webhook.id}. Start "serve" with:\n\n  CONNECTOR_WEBHOOK_SECRET=${created.secret}\n`)
  console.log('The secret is shown once; keep it like a password.')
}

// --- serve: receive signed events and fill new meeting pages -------------

// Leafen-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of "t.body"> under
// the subscription's secret. Old timestamps are refused so a captured
// delivery can't be replayed later.
function verified(header, body, secret, now = Date.now()) {
  const parts = Object.fromEntries(String(header ?? '').split(',').map((part) => part.split('=', 2)))
  const sent = Number(parts.t)
  if (!parts.v1 || !Number.isFinite(sent) || Math.abs(now / 1000 - sent) > 300) return false
  const expected = createHmac('sha256', secret).update(`${parts.t}.${body}`).digest()
  const given = Buffer.from(parts.v1, 'hex')
  return given.length === expected.length && timingSafeEqual(given, expected)
}

async function fillMeetingPage(pageId, eventId) {
  const page = await call('GET', `/pages/${encodeURIComponent(pageId)}`)
  if (!/^meeting/i.test(page.title ?? '')) return 'not a meeting page'
  const graph = await call('GET', `/pages/${encodeURIComponent(pageId)}/content-graph`)
  if (graph.items.some((item) => item.kind === 'block')) return 'already has content'
  const blocks = [
    ['heading_2', 'Agenda'], ['bulleted_list_item', ''],
    ['heading_2', 'Notes'], ['paragraph', ''],
    ['heading_2', 'Action items'], ['to_do', ''],
  ].map(([type, text], index) => ({ id: randomUUID(), type, text, position: `a${index}` }))
  // The event's id names the change, so a redelivered event writes nothing twice.
  await call('POST', `/pages/${encodeURIComponent(pageId)}/content-graph/mutations`, {
    mutationId: `meeting-notes:${eventId}`,
    operations: blocks.map((block, index) => ({
      kind: 'upsertBlock',
      block: {
        id: block.id, parentId: null, afterId: index === 0 ? null : blocks[index - 1].id,
        position: block.position, type: block.type, content: block.type === 'to_do' ? { checked: false } : {},
      },
    })),
    textUpdates: blocks.filter((block) => block.text).map((block) => ({ blockId: block.id, spans: [{ text: block.text }] })),
  })
  return 'filled'
}

function serve() {
  const secret = env.CONNECTOR_WEBHOOK_SECRET
  const port = Number(env.CONNECTOR_PORT ?? 8787)
  if (!secret) throw new Error('Set CONNECTOR_WEBHOOK_SECRET to the secret "subscribe" printed.')
  createServer((request, response) => {
    const chunks = []
    request.on('data', (chunk) => chunks.push(chunk))
    request.on('end', async () => {
      const body = Buffer.concat(chunks).toString('utf8')
      if (request.method !== 'POST' || !verified(request.headers['leafen-signature'], body, secret)) {
        return void response.writeHead(401).end()
      }
      const event = JSON.parse(body)
      if (event.entity?.type !== 'page') return void response.writeHead(204).end()
      // A failure answers 503, so Leafen delivers the event again later (after
      // 10 seconds, then minutes, then hours); the mutation id keeps a second
      // delivery from writing twice. A subscription's events come one at a
      // time, so a page created and renamed at once is filled only once.
      try {
        console.log(`${event.type} ${event.entity.id}: ${await fillMeetingPage(event.entity.id, event.id)}`)
        response.writeHead(204).end()
      } catch (error) {
        console.error(`${event.type} ${event.entity.id}: ${error.message}`)
        response.writeHead(503).end()
      }
    })
  }).listen(port, () => console.log(`Listening for Leafen events on port ${port}.`))
}

const commands = { authorize, subscribe, serve }
const command = commands[process.argv[2]]
if (!command) {
  console.error('Usage: node connector.mjs authorize | subscribe | serve')
  process.exit(2)
}
Promise.resolve().then(command).catch((error) => {
  console.error(error.message)
  process.exit(1)
})
