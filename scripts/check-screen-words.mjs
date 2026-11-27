// Screen words keep one word for each meaning (2026-10-28: a chat's "갈라 새
// 채팅", "turns", "places" and "판" beside "버전" were words the code's own
// records had coined). docs/screen-words.md holds the words and those they
// stand for; this check keeps the latter out of the catalogs, what the server
// writes and the apps' screens.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { relativeSlashPath } from './source-paths.mjs'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const hangul = /[가-힣]/u

function cells(line) {
  return line.trim().replace(/^\||\|$/gu, '').split(/(?<!\\)\|/u).map((cell) => cell.trim().replace(/\\\|/gu, '|'))
}

function patterns(cell) {
  return cell.split(' · ').map((item) => item.trim().replace(/^`|`$/gu, '')).filter(Boolean).map((item) => {
    const regex = /^\/(.+)\/$/u.exec(item)
    return { text: item, pattern: regex ? new RegExp(regex[1], 'u') : null }
  })
}

const avoid = { ko: [], en: [] }
const lines = readFileSync(join(root, 'docs', 'screen-words.md'), 'utf8').split('\n')
for (const line of lines.slice(lines.findIndex((each) => /^\|\s*-/u.test(each)) + 1)) {
  if (!line.startsWith('|')) break
  const [meaning, korean, english, , koreanAvoid = '', englishAvoid = ''] = cells(line)
  for (const item of patterns(koreanAvoid)) avoid.ko.push({ ...item, use: korean, meaning })
  for (const item of patterns(englishAvoid)) avoid.en.push({ ...item, use: english, meaning })
}

function found(item, text) {
  return item.pattern ? item.pattern.test(text) : text.includes(item.text)
}

const failures = []
function check(where, text, languages) {
  for (const language of languages) {
    for (const item of avoid[language]) {
      if (found(item, text)) failures.push(`${where}: write “${item.use}” (${item.meaning}), not ${item.text}: ${text.replace(/\s+/gu, ' ').slice(0, 90)}`)
    }
  }
}

// The catalogs: each language against its own words.
for (const language of ['ko', 'en']) {
  const directory = join(root, 'web', 'src', 'locales', language)
  for (const file of readdirSync(directory).filter((name) => name.endsWith('.json') && !name.startsWith('_')).sort()) {
    const visit = (value, path) => {
      if (typeof value === 'string') check(`${language}/${path}`, value, [language])
      else if (value && typeof value === 'object') for (const [key, child] of Object.entries(value)) visit(child, `${path}.${key}`)
    }
    visit(JSON.parse(readFileSync(join(directory, file), 'utf8')), file.replace(/\.json$/u, ''))
  }
}

// What the server writes, the web code holds and the apps show: string
// literals in source and strings in shipped JSON. Korean words are checked where Korean is written,
// English ones in text with a space in it (a sentence or a label, not a key).
const sources = ['internal', 'desktop', 'organization', 'cmd', 'builtin-apps', 'catalog-apps', 'web/src']
const skippedDirectories = new Set(['node_modules', 'vendor', 'dist', 'testdata', '.git', 'locales'])
const content = /(?:\.generated\.json|(?:^|\/)(?:blueprints\/[^/]+\.json|recommended_structure[^/]*\.json|database_starter_templates\.json|business_calendars\.json|builtinapps\/[^/]+\/data\/[^/]+\.json))$/u

// The string literals of a Go or JavaScript file, past its comments: a Go
// rune and a JavaScript string are quoted alike, and a raw or template string
// keeps its lines.
function* literals(text, javascript) {
  const quotes = javascript ? '"\'`' : '"`'
  for (let at = 0; at < text.length; at += 1) {
    const char = text[at]
    if (char === '/' && text[at + 1] === '/') at = text.indexOf('\n', at) === -1 ? text.length : text.indexOf('\n', at)
    else if (char === '/' && text[at + 1] === '*') at = text.indexOf('*/', at + 2) === -1 ? text.length : text.indexOf('*/', at + 2) + 1
    else if (char === "'" && !javascript) at = text.indexOf("'", at + (text[at + 1] === '\\' ? 3 : 2))
    else if (quotes.includes(char)) {
      let end = at + 1
      while (end < text.length && text[end] !== char && (char === '`' || text[end] !== '\n')) end += text[end] === '\\' && !(char === '`' && !javascript) ? 2 : 1
      yield { index: at, value: text.slice(at + 1, end) }
      at = end
    }
  }
}

// Queries, markup, patterns and the "leafen …" errors the server logs are not
// text a person reads, nor a line marked "screen words: not shown" (a list of
// column names, icon names), nor one marked "screen words: another service's"
// (another service's page drawn as it is, in its own words). Content a structure, blueprint or template lays in a
// workspace becomes the workspace's own pages, which its people edit; it is
// not the product's wording.
const notShown = /\{\{\w+\}\}|\[\[\w+\]\]|^\s*(?:SELECT|INSERT|UPDATE|DELETE|CREATE|ALTER|WITH)\b|<!--|xmlns|^leafen\b|^\^/u

// A stretch read past a script's regular expression is code, not text.
const code = /[;{}]\s*(?:return|const|let|if|var)\b|=>/u

function shown(where, text) {
  if (notShown.test(text) || code.test(text)) return
  // A literal mixing languages (a page in both) is read sentence by sentence.
  for (const sentence of text.split(/\n|<[^>]*>|(?<=[.!?])\s/u)) {
    if (hangul.test(sentence)) check(where, sentence, ['ko'])
  }
  if (/[A-Za-z]+ [A-Za-z]+/u.test(text)) check(where, text, ['en'])
}

function walk(directory) {
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) {
      if (!skippedDirectories.has(name) && !name.startsWith('vendor')) walk(path)
      continue
    }
    const where = relativeSlashPath(root, path)
    if (name.endsWith('_test.go') || name.endsWith('.generated.go') || name.endsWith('.min.js') || content.test(where)) continue
    const script = /\.(?:js|ts|tsx)$/u.test(name) && !name.endsWith('.d.ts')
    if (name.endsWith('.go') || script) {
      const text = readFileSync(path, 'utf8')
      const lines = text.split('\n')
      for (const { index, value } of literals(text, script)) {
        const line = text.slice(0, index).split('\n').length
        if (!/screen words: (?:not shown|another service's)/u.test(lines[line - 1])) shown(`${where}:${line}`, value)
      }
    } else if (name.endsWith('.json')) {
      const visit = (value, at) => {
        if (typeof value === 'string') shown(`${where} ${at}`, value)
        else if (value && typeof value === 'object') for (const [key, child] of Object.entries(value)) visit(child, `${at}.${key}`)
      }
      visit(JSON.parse(readFileSync(path, 'utf8')), '')
    }
  }
}
// A build that copies only part of the tree (the container image) checks
// what it has and says what it did not.
const absent = []
for (const source of sources) {
  const directory = join(root, source)
  if (statSync(directory, { throwIfNoEntry: false })?.isDirectory()) walk(directory)
  else absent.push(source)
}

if (failures.length > 0) {
  console.error(`Screen words (docs/screen-words.md) failed:\n${failures.join('\n')}`)
  process.exit(1)
}
console.log(absent.length ? `Screen words verified, without ${absent.join(', ')} (not in this tree).` : 'Screen words verified.')
