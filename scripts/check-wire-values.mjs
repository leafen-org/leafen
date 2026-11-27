// A wire contract names, for a field the server fills with one of a few
// words, the words it may hold: a union of quoted words or one of the
// contract's enums as the field's TypeScript type. The server fills such a
// field in Go, so a word the code writes that the contract doesn't name is
// an answer its type says can't come (2026-11-24: a Notion move's check had
// come to say "relation", "cell" and "column" differences and "empty in
// Notion" and "one way only" settings, which notion-import-wire.json still
// typed as rows, templates, view and linked view). This checks every
// contract's records: where the product's Go code builds a record with such
// a field written out — a quoted word, or a string constant — the word is
// one the contract names. A word that comes another way (a variable, a call)
// isn't seen here.
//
//   node scripts/check-wire-values.mjs
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { relativeSlashPath } from './source-paths.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const skipped = new Set(['.git', 'node_modules', 'third_party', 'vendor', 'dist', 'web'])

function goFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return skipped.has(entry.name) ? [] : goFiles(path)
    return entry.name.endsWith('.go') && !entry.name.endsWith('_test.go') ? [path] : []
  })
}

/** The words a field's TypeScript type allows, or null for any. */
function wordsOf(tsType, enums) {
  const type = String(tsType ?? '').trim()
  if (enums.has(type)) return enums.get(type)
  const parts = type.split('|').map((part) => part.trim()).filter((part) => part !== 'null')
  if (parts.length === 0 || !parts.every((part) => /^"[^"]*"$/u.test(part))) return null
  return new Set(parts.map((part) => part.slice(1, -1)))
}

// Each record a contract types a field of with words: by its Go name, the
// field's Go name and the words, with the contract.
const records = new Map()
for (const entry of readdirSync(join(root, 'contracts'))) {
  if (!entry.endsWith('.json')) continue
  let contract
  try {
    contract = JSON.parse(readFileSync(join(root, 'contracts', entry), 'utf8'))
  } catch {
    continue
  }
  if (!Array.isArray(contract?.records)) continue
  const enums = new Map((contract.enums ?? []).filter((item) => item?.tsName && Array.isArray(item.values))
    .map((item) => [item.tsName, new Set(item.values.map((value) => value.value))]))
  for (const record of contract.records) {
    for (const field of record.fields ?? []) {
      const words = field.goType === 'string' ? wordsOf(field.tsType, enums) : null
      if (!words) continue
      if (!records.has(record.goName)) records.set(record.goName, new Map())
      records.get(record.goName).set(field.goName, { words, contract: entry })
    }
  }
}

const files = goFiles(root).map((path) => ({ path, source: readFileSync(path, 'utf8') }))

// The string constants the code declares, by package folder and name; one
// declared with two words stands for neither.
const constants = new Map()
for (const { path, source } of files) {
  const declare = (name, value) => {
    const key = `${dirname(path)}\0${name}`
    constants.set(key, constants.has(key) && constants.get(key) !== value ? null : value)
  }
  for (const block of source.matchAll(/^const \(\n([\s\S]*?)^\)/gmu)) {
    for (const line of block[1].matchAll(/^\s*([A-Za-z_]\w*)\s*(?:string\s*)?=\s*"((?:[^"\\]|\\.)*)"\s*(?:\/\/.*)?$/gmu)) declare(line[1], line[2])
  }
  for (const line of source.matchAll(/^const ([A-Za-z_]\w*)\s*(?:string\s*)?=\s*"((?:[^"\\]|\\.)*)"/gmu)) declare(line[1], line[2])
}

/** The index after the brace that closes the one before `start`. */
function closing(source, start) {
  let depth = 1
  let index = start
  while (index < source.length && depth > 0) {
    const char = source[index]
    if (char === '"' || char === '\'') {
      index++
      while (index < source.length && source[index] !== char) index += source[index] === '\\' ? 2 : 1
    } else if (char === '`') {
      index = source.indexOf('`', index + 1)
    } else if (source.startsWith('//', index)) {
      index = source.indexOf('\n', index)
    } else if (char === '{' || char === '(' || char === '[') {
      depth++
    } else if (char === '}' || char === ')' || char === ']') {
      depth--
    }
    if (index < 0) return source.length
    index++
  }
  return index
}

/** A composite literal's own elements: its text cut at its top-level commas. */
function elements(body) {
  const parts = []
  let start = 0
  let index = 0
  while (index < body.length) {
    const char = body[index]
    if (char === '{' || char === '(' || char === '[') {
      index = closing(body, index + 1)
      continue
    }
    if (char === '"' || char === '\'') {
      index++
      while (index < body.length && body[index] !== char) index += body[index] === '\\' ? 2 : 1
    } else if (char === '`') {
      index = body.indexOf('`', index + 1)
      if (index < 0) break
    } else if (char === ',') {
      parts.push(body.slice(start, index))
      start = index + 1
    }
    index++
  }
  parts.push(body.slice(start))
  return parts
}

const problems = []
for (const { path, source } of files) {
  for (const [name, fields] of records) {
    for (const match of source.matchAll(new RegExp(`\\b${name}\\{`, 'gu'))) {
      const start = match.index + match[0].length
      for (const element of elements(source.slice(start, closing(source, start) - 1))) {
        const keyed = /^\s*([A-Za-z_]\w*)\s*:\s*([\s\S]*?)\s*$/u.exec(element)
        const field = keyed && fields.get(keyed[1])
        if (!field) continue
        const written = keyed[2]
        let word
        if (/^"((?:[^"\\]|\\.)*)"$/u.test(written)) word = JSON.parse(written)
        else if (/^`[^`]*`$/u.test(written)) word = written.slice(1, -1)
        else if (/^[A-Za-z_]\w*$/u.test(written) && typeof constants.get(`${dirname(path)}\0${written}`) === 'string') {
          word = constants.get(`${dirname(path)}\0${written}`)
        }
        if (word === undefined || field.words.has(word)) continue
        const line = source.slice(0, start).split('\n').length
        problems.push(`${relativeSlashPath(root, path)}:${line} ${name}.${keyed[1]} is written "${word}", ` +
          `which contracts/${field.contract} doesn't name: add it to the field's type there`)
      }
    }
  }
}

if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log('wire values ok')
