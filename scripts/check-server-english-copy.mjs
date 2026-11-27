// The English the server writes for people — a report's reasons, inbox
// notices, mail, the words an agent is given to tell its person, settings and
// refusals said in the person's language — writes Notion's typographic marks
// as the web's English does (web/scripts/check-english-copy.mjs): an
// apostrophe ’, a quoted word or value “ ” or ‘ ’ (2026-11-19: the web's
// English moved to ’ while the server's same words kept the typewriter ' and
// ", so the finish window and the agent's report said one sentence two
// ways). The server's text for people is the English beside its Korean: a
// Go declaration holding a Korean string literal says its English to people
// too, and each English sentence in it is checked. A straight mark stays in
// what is copied or read by a program as it is — markup, a page's security
// policy, SQL, a struct tag, JSON, a format verb — and in the files the list
// below names, whose English is for agents, not people.
//
// And what Leafen says to an agent states what is so and what is left to do;
// it doesn't tell the agent how to talk to its person (2026-11-23, the rule
// for answers in internal/app/mcp_answer_rule.go): no "tell your person …",
// "tell the person …", "tell them …" in any string the server writes — an
// answer, a tool's or a field's description, an instruction, a command's
// output —, read across the pieces a Go expression joins with +. What the
// person has to hear goes in as a fact with "Left to do: …". The text a
// person pastes into their agent — the connection text, the restore
// sentence — is the person's own message, in which they may well ask their
// agent to tell them something ("tell me …"); it is written in the web's
// catalogs (web/src/locales/*/agentPrompt.json …), which this scan of the
// server's Go doesn't read, and none is in the Go. The guides a Go file
// embeds are read with it.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { relativeSlashPath } from './source-paths.mjs'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const sources = ['internal', 'organization']
const hangul = /[가-힣]/u

// Files whose English beside Korean isn't the server's words for people: the
// API's field shapes for agents, with their JSON (a Korean name of a screen
// is the only Korean there), and the names Notion gives views in each
// language, matched as Notion writes them.
const NOT_FOR_PEOPLE = new Set([
  'internal/app/native_operation_field_shapes.go',
  'internal/app/native_operation_field_shapes_more.go',
  'internal/app/workspace_import_archive_view_names.go',
])

// What a program reads as it is: markup, its style and script, a page's
// security policy, SQL, a struct tag, JSON, a format verb, a single token.
const code = /<[a-zA-Z!/]|\b[a-z-]+="|[{;]\s*[a-z-]+\s*[:=(]|document\.|function\s*\(|'(?:none|self|unsafe-inline|nonce-|strict-dynamic)|-src\b|^\s*(?:SELECT|INSERT|UPDATE|DELETE|CREATE|ALTER|WITH)\b|\{\{\w+\}\}|\[\[\w+\]\]|^\w+:"|\{"|":\s|%q|^[^ ]*$/u

// The string literals of a Go file, past its comments, with where each starts.
function* literals(text) {
  for (let at = 0; at < text.length; at += 1) {
    const char = text[at]
    if (char === '/' && text[at + 1] === '/') {
      const end = text.indexOf('\n', at)
      at = end === -1 ? text.length : end
    } else if (char === '/' && text[at + 1] === '*') {
      const end = text.indexOf('*/', at + 2)
      at = end === -1 ? text.length : end + 1
    } else if (char === "'") {
      at = text.indexOf("'", at + (text[at + 1] === '\\' ? 3 : 2))
    } else if (char === '"' || char === '`') {
      let end = at + 1
      while (end < text.length && text[end] !== char && (char === '`' || text[end] !== '\n')) end += text[end] === '\\' && char === '"' ? 2 : 1
      const value = text.slice(at + 1, end)
      yield { index: at, end: end + 1, value: char === '"' ? value.replace(/\\"/gu, '"') : value }
      at = end
    }
  }
}

const failures = []
function checkFile(path) {
  const where = relativeSlashPath(root, path)
  if (NOT_FOR_PEOPLE.has(where)) return
  const text = readFileSync(path, 'utf8')
  if (!hangul.test(text)) return
  const all = [...literals(text)]
  const starts = [...text.matchAll(/^(?:func|var|const|type)\b/gmu)].map((match) => match.index)
  starts.push(text.length)
  for (let declaration = 0; declaration + 1 < starts.length; declaration += 1) {
    const inside = all.filter((literal) => literal.index >= starts[declaration] && literal.index < starts[declaration + 1])
    if (!inside.some((literal) => hangul.test(literal.value))) continue
    for (const { index, value } of inside) {
      if (hangul.test(value) || code.test(value) || !/[A-Za-z]{2}/u.test(value)) continue
      const line = text.slice(0, index).split('\n').length
      if (/[A-Za-z]'|'[A-Za-z]/u.test(value)) failures.push(`${where}:${line}: write an apostrophe as ’ and a quote as ‘ ’: ${value.slice(0, 80)}`)
      if (value.includes('"')) failures.push(`${where}:${line}: quote a word or a value with “ ”: ${value.slice(0, 80)}`)
    }
  }
}

// An instruction to talk to the person, in any casing (“tells them”, a
// screen's or an inbox's, is a fact), or a sentence handed over to be said
// as it is ("in these words: “…”"): the person hears the facts in their
// own language, in the agent's sentence, with only another service's
// button and menu names quoted as its screen shows them (the user,
// 2026-11-23).
const tellsHowToTalk = /\btell(?:ing)?\s+(?:your|the|their|its)\s+person\b|\btell(?:ing)?\s+them\b|\bin\s+these\s+words\b/iu

// Each string a Go expression joins from literals with +, with where it starts.
function* joinedStrings(text) {
  let chain = null
  for (const literal of literals(text)) {
    const between = chain ? text.slice(chain.end, literal.index) : ''
    if (chain && /^\s*\+\s*$/u.test(between)) {
      chain.value += literal.value
      chain.end = literal.end
      continue
    }
    if (chain) yield chain
    chain = { ...literal }
  }
  if (chain) yield chain
}

// A guide a Go file embeds (//go:embed name.md) is the server's words too.
function embeddedGuides(path, text) {
  return [...text.matchAll(/^\/\/go:embed\s+(\S+\.md)\s*$/gmu)].map((match) => join(dirname(path), match[1]))
}

function checkTalk(path) {
  const where = relativeSlashPath(root, path)
  const text = readFileSync(path, 'utf8')
  for (const guide of embeddedGuides(path, text)) {
    const words = readFileSync(guide, 'utf8')
    for (const found of words.matchAll(new RegExp(tellsHowToTalk.source, 'giu'))) {
      const line = words.slice(0, found.index).split('\n').length
      failures.push(`${relativeSlashPath(root, guide)}:${line}: say what is so and “Left to do: …”, not how to talk to the person (“${found[0]}”)`)
    }
  }
  for (const { index, value } of joinedStrings(text)) {
    const found = tellsHowToTalk.exec(value)
    if (!found) continue
    const line = text.slice(0, index).split('\n').length
    failures.push(`${where}:${line}: say what is so and “Left to do: …”, not how to talk to the person (“${found[0]}”): `
      + value.slice(Math.max(0, found.index - 40), found.index + 60))
  }
}

function walk(directory) {
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) {
      if (name !== 'testdata' && name !== 'vendor') walk(path)
    } else if (name.endsWith('.go') && !name.endsWith('_test.go') && !name.endsWith('.generated.go')) {
      checkFile(path)
      checkTalk(path)
    }
  }
}

const absent = []
for (const source of sources) {
  const directory = join(root, source)
  if (statSync(directory, { throwIfNoEntry: false })?.isDirectory()) walk(directory)
  else absent.push(source)
}
// A file named above that is gone would let its new name skip the list unseen.
for (const where of NOT_FOR_PEOPLE) {
  if (!statSync(join(root, where), { throwIfNoEntry: false })) failures.push(`${where}: no such file — take it off this check's list`)
}

if (failures.length > 0) {
  console.error(`Server English copy rules failed:\n${failures.join('\n')}`)
  process.exit(1)
}
console.log(absent.length ? `Server English copy rules verified, without ${absent.join(', ')} (not in this tree).` : 'Server English copy rules verified.')
