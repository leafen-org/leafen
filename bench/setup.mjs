// Makes the benchmark's data on a fresh server (docs/benchmark.md): an owner
// and ACCOUNTS synthetic accounts, all members of one workspace; the owner's
// PRIVATE private pages and, in an open teamspace (its pages editable by the
// whole workspace), SHARED shared pages, each with PARAGRAPHS paragraphs.
// Members don't join the teamspace. It keeps the first TOKENS members'
// sign-ins for the load, in state.json of BENCH_DIR.
import { writeFileSync } from 'node:fs'
import { randomUUID } from 'node:crypto'
import { PASSWORD, RUN, call, email, pool, sentence, sleep, words } from './lib.mjs'

const ACCOUNTS = Number(process.env.ACCOUNTS ?? 500000)
const PRIVATE = Number(process.env.PRIVATE ?? 200)
const SHARED = Number(process.env.SHARED ?? 200)
const PARAGRAPHS = Number(process.env.PARAGRAPHS ?? 30)
const TOKENS = Number(process.env.TOKENS ?? 60000)
const t0 = Date.now()
const since = () => `${Math.round((Date.now() - t0) / 1000)} s`

for (let tries = 0; tries < 120; tries++) { if ((await call('GET', '/health')).status === 200) break; await sleep(500) }

// An account by the server's own sign-up, tried again while it is busy.
async function register(name) {
  let answer
  for (let tries = 0; tries < 20; tries++) {
    answer = await call('POST', '/auth/register', null, { email: email(name), password: PASSWORD, name: `Bench ${name}` })
    if (answer.status !== 503 && answer.status !== 429) break
    await sleep(2000)
  }
  if (answer.status !== 201) throw new Error(`sign-up ${name}: ${answer.status} ${answer.text.slice(0, 200)}`)
  return { token: answer.json.token, email: email(name) }
}

async function fill(page, n, token) {
  const operations = Array.from({ length: PARAGRAPHS }, (_, k) => ({ kind: 'upsertBlock', block: { id: randomUUID(), parentId: null,
    position: `a${String(k).padStart(4, '0')}`, type: 'paragraph', content: { rich: [{ text: sentence(n * 31 + k) }] } } }))
  const done = await call('POST', `/pages/${page}/content-graph/mutations`, token, { mutationId: randomUUID(), operations })
  if (done.status >= 300) throw new Error(`filling a page: ${done.status} ${done.text.slice(0, 200)}`)
}

async function pages(count, workspaceId, teamspaceId, token, title) {
  return pool(Array.from({ length: count }, (_, i) => i), 8, async (i) => {
    const made = await call('POST', '/pages', token, { requestId: randomUUID(), workspaceId, ...(teamspaceId ? { teamspaceId } : {}), title: `${title} ${i} ${words[i % words.length]}` })
    if (made.status !== 201) throw new Error(`a page: ${made.status} ${made.text.slice(0, 200)}`)
    await fill(made.json.page.id, i, token)
    return { id: made.json.page.id }
  })
}

const owner = await register('owner')
const workspace = await call('POST', '/workspaces', owner.token, { requestId: randomUUID(), name: 'Bench workspace', skipDefaultPages: true })
if (workspace.status !== 201) throw new Error(`workspace: ${workspace.status} ${workspace.text.slice(0, 200)}`)
const workspaceId = workspace.json.workspace?.id ?? workspace.json.id
console.log('workspace', workspaceId)

let made = 0
const accounts = await pool(Array.from({ length: ACCOUNTS }, (_, i) => i), Number(process.env.SIGNUP_AT_ONCE ?? 64), async (i) => {
  const account = await register(`u${i}`)
  if (++made % 25000 === 0) console.log('accounts', made, since())
  return i < TOKENS ? account : { email: account.email }
})
console.log('accounts', accounts.length, since())

let added = 0, refused = 0
await pool(accounts, Number(process.env.MEMBERS_AT_ONCE ?? 32), async (account) => {
  const answer = await call('POST', `/workspaces/${workspaceId}/members`, owner.token, { email: account.email, role: 'member' })
  if (answer.status >= 300 && ++refused <= 3) console.log('a member refused', answer.status, answer.text.slice(0, 200))
  if (++added % 25000 === 0) console.log('members', added, since())
})
console.log('members', added - refused, 'refused', refused, since())

await pages(PRIVATE, workspaceId, '', owner.token, '페이지')
console.log('private pages', PRIVATE, since())
const teamspace = await call('POST', `/workspaces/${workspaceId}/teamspace-mutations`, owner.token, { requestId: randomUUID(), action: 'create', expectedRevision: 0,
  name: '전사', access: 'open', memberPageRole: 'full_access', openPageRole: 'edit', membersCanInvite: true, membersCanEditSidebar: true })
if (teamspace.status >= 300) throw new Error(`teamspace: ${teamspace.status} ${teamspace.text.slice(0, 200)}`)
const teamspaceId = teamspace.json.teamspaceId
const shared = await pages(SHARED, workspaceId, teamspaceId, owner.token, '공유 페이지')
console.log('shared pages', shared.length, since())

// A member reads and writes a shared page, as the load will.
const member = accounts.find((account) => account.token)
if (member) {
  const read = await call('GET', `/pages/${shared[0].id}/content-graph`, member.token)
  console.log('a member reads a shared page', read.status)
}
writeFileSync(new URL('state.json', RUN), JSON.stringify({ workspaceId, teamspaceId, owner, users: accounts.filter((account) => account.token), pages: shared }))
console.log('state.json written', since())
process.exit(0)
