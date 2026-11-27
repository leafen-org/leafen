#!/usr/bin/env node

// Korean keeps up with English at every build. English is the source; every
// Korean string records, in ko/_meta.json, a hash of the English it was
// translated from. A build stops when English has a key Korean lacks, Korean
// keeps a key English dropped, or English changed since a Korean string was
// translated — or one was never recorded — so whoever changed the English
// carries the Korean in the same change, rather than a pile left for later.
// Korean needs only a plural's "_other" form.
//
//   node scripts/check-korean-catalog.mjs                     check
//   node scripts/check-korean-catalog.mjs --stamp ns:key ...  record that
//       these Korean strings say what their English says now
//
// ko/_meta.json stays in key order, so changes made side by side land apart.
// Git merges it by keeping both sides' lines (.gitattributes), which can leave
// a key twice or a comma out; it is read line by line and put back in order
// here, at the next build, and a key that came out wrong shows as stale below.
//
// The private repository's i18n guard covers every language; this is its
// English and Korean part, early.

import { createHash } from 'node:crypto'
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const localesRoot = join(resolve(dirname(fileURLToPath(import.meta.url)), '..'), 'src', 'locales')
const metaPath = join(localesRoot, 'ko', '_meta.json')
const PLURAL = /_(zero|one|two|few|many|other)$/u

const hash = (value) => createHash('sha1').update(String(value)).digest('hex').slice(0, 12)

function flatten(node, prefix, out) {
  for (const [key, value] of Object.entries(node)) {
    const id = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === 'object' && !Array.isArray(value)) flatten(value, id, out)
    else out[id] = value
  }
  return out
}

function catalog(language) {
  const keys = {}
  for (const file of readdirSync(join(localesRoot, language))) {
    if (!file.endsWith('.json') || file === '_meta.json') continue
    const namespace = file.slice(0, -5)
    const flat = flatten(JSON.parse(readFileSync(join(localesRoot, language, file), 'utf8')), '', {})
    for (const [key, value] of Object.entries(flat)) keys[`${namespace}:${key}`] = value
  }
  return keys
}

const pluralOther = (id) => {
  const match = PLURAL.exec(id)
  return match ? `${id.slice(0, match.index)}_other` : null
}

function readMeta(text) {
  const stored = {}
  for (const line of text.split('\n')) {
    const match = /^\s*("(?:[^"\\]|\\.)*"): ("[0-9a-f]+"),?\s*$/u.exec(line)
    if (match) stored[JSON.parse(match[1])] = JSON.parse(match[2])
  }
  return stored
}

function metaText(meta) {
  const inOrder = Object.fromEntries(Object.entries(meta).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)))
  return `${JSON.stringify(inOrder, null, 2)}\n`
}

const english = catalog('en')
const korean = catalog('ko')
const storedMeta = readFileSync(metaPath, 'utf8')
const meta = readMeta(storedMeta)

const args = process.argv.slice(2)
if (args[0] === '--stamp') {
  const ids = args.slice(1)
  const unknown = ids.filter((id) => !(id in english) || !(id in korean))
  if (ids.length === 0 || unknown.length > 0) {
    console.error(ids.length === 0 ? 'Name the keys to stamp, as ns:key.' : `Not in both English and Korean: ${unknown.join(', ')}`)
    process.exit(2)
  }
  for (const id of ids) meta[id] = hash(english[id])
  writeFileSync(metaPath, metaText(meta))
  console.log(`Stamped ${ids.length} Korean string(s) as current with their English.`)
  process.exit(0)
}

if (storedMeta !== metaText(meta)) {
  writeFileSync(metaPath, metaText(meta))
  console.log('Put ko/_meta.json back in key order; commit it with this change.')
}

const missing = Object.keys(english).filter((id) => !(id in korean) && !(pluralOther(id) in korean))
const orphan = Object.keys(korean).filter((id) => !(id in english) && !(pluralOther(id) in english))
const stale = Object.keys(korean).filter((id) => id in english && meta[id] !== hash(english[id]))

if (missing.length + orphan.length + stale.length > 0) {
  const list = (label, ids) => ids.length ? `${label} (${ids.length}):\n${ids.slice(0, 30).map((id) => `  ${id}`).join('\n')}${ids.length > 30 ? '\n  …' : ''}\n` : ''
  console.error(
    'Korean is behind English.\n'
    + list('English keys Korean lacks — translate them', missing)
    + list('Korean keys English no longer has — remove them', orphan)
    + list('Korean strings translated from other English, or never recorded — check each says what its English says now, fix it if not', stale)
    + 'Then record them: node scripts/check-korean-catalog.mjs --stamp ns:key ...',
  )
  process.exit(1)
}
