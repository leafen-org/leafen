// A query of several statements binds each statement's values as its own
// (2026-11-14). dbx writes a query's named values as SQLite's anonymous "?",
// which SQLite numbers within each statement, so a later statement took the
// values of the query's first names: a new page verification cancelled none
// of the last one's email deliveries, and a schema step restarted the page
// file pass with the row page icon pass's name. The server numbers such a
// query's values across it (internal/app/sqlite_statement_values.go); this
// check keeps the product's queries binding right without that as well. A
// query written in the code (its text's literal parts, as NewQuery gets them)
// whose later statement names values other than the query's first ones, in
// their order, fails: write that statement as its own query. Statements are
// told apart as SQLite does: not at a semicolon in a quote, a comment or a
// trigger's body.
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { relativeSlashPath } from './source-paths.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const skipped = new Set(['.git', 'node_modules', 'third_party', 'vendor', 'dist'])
// Stands for what the code adds between literal parts: no semicolon, quote
// or value of the SQL's own.
const opaque = '\u0001'

function goFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return skipped.has(entry.name) ? [] : goFiles(path)
    return entry.name.endsWith('.go') && !entry.name.endsWith('_test.go') ? [path] : []
  })
}

/** The SQL a NewQuery( call at `start` gets: its literal parts in order. */
function queryText(source, start) {
  let text = ''
  let between = false
  let depth = 1
  let index = start
  const literal = (value) => {
    if (between) text += opaque
    between = false
    text += value
  }
  while (index < source.length && depth > 0) {
    const char = source[index]
    if (char === '`') {
      const end = source.indexOf('`', index + 1)
      literal(source.slice(index + 1, end))
      index = end + 1
    } else if (char === '"') {
      let end = index + 1
      while (source[end] !== '"') end += source[end] === '\\' ? 2 : 1
      literal(source.slice(index + 1, end).replace(/\\(.)/gsu, (_, escaped) => ({ n: '\n', t: '\t' })[escaped] ?? escaped))
      index = end + 1
    } else if (char === '\'') {
      let end = index + 1
      while (source[end] !== '\'') end += source[end] === '\\' ? 2 : 1
      between = true
      index = end + 1
    } else if (source.startsWith('//', index)) {
      index = source.indexOf('\n', index) + 1 || source.length
    } else if (source.startsWith('/*', index)) {
      index = source.indexOf('*/', index) + 2
    } else {
      if (char === '(') depth++
      if (char === ')') depth--
      if (depth > 0 && !/[\s+]/u.test(char)) between = true
      index++
    }
  }
  return text
}

/** The named values of each statement of `sql`, in order. */
function statementValues(sql) {
  const statements = [[]]
  let words = [] // the statement's first words, to know a trigger
  let body = 0 // how deep in a trigger's BEGIN … END and its CASE … END
  let index = 0
  while (index < sql.length) {
    const char = sql[index]
    if (sql.startsWith('{{', index) || sql.startsWith('[[', index)) {
      // dbx's table and column names.
      index = sql.indexOf(char === '{' ? '}}' : ']]', index) + 2
    } else if (char === '\'' || char === '"') {
      let end = index + 1
      while (end < sql.length && !(sql[end] === char && sql[end + 1] !== char)) end += sql[end] === char ? 2 : 1
      index = end + 1
    } else if (char === '[') {
      index = sql.indexOf(']', index) + 1 || sql.length
    } else if (sql.startsWith('--', index)) {
      const end = sql.indexOf('\n', index)
      index = end < 0 ? sql.length : end
    } else if (sql.startsWith('/*', index)) {
      const end = sql.indexOf('*/', index + 2)
      index = end < 0 ? sql.length : end + 2
    } else if (sql.startsWith('{:', index)) {
      const end = sql.indexOf('}', index)
      statements.at(-1).push(sql.slice(index + 2, end))
      index = end + 1
    } else if (/[A-Za-z_]/u.test(char)) {
      const word = /^[A-Za-z_][A-Za-z0-9_]*/u.exec(sql.slice(index))[0].toUpperCase()
      if (words.length < 4) words.push(word)
      const trigger = words[0] === 'CREATE' && (words[1] === 'TRIGGER' || words[2] === 'TRIGGER')
      if (trigger && (word === 'BEGIN' || (body > 0 && word === 'CASE'))) body++
      if (trigger && body > 0 && word === 'END') body--
      index += word.length
    } else if (char === ';' && body === 0) {
      statements.push([])
      words = []
      index++
    } else {
      index++
    }
  }
  return statements
}

const problems = []
for (const path of goFiles(root)) {
  const source = readFileSync(path, 'utf8')
  for (const match of source.matchAll(/\bNewQuery\(/gu)) {
    const statements = statementValues(queryText(source, match.index + match[0].length))
    const names = statements.flat()
    statements.slice(1).forEach((own, later) => {
      const taken = names.slice(0, own.length)
      if (own.some((name, position) => name !== taken[position])) {
        const line = source.slice(0, match.index).split('\n').length
        problems.push(`${relativeSlashPath(root, path)}:${line} statement ${later + 2} binds ${own.join(', ')} ` +
          `but takes the values of ${taken.join(', ')}: write it as its own query`)
      }
    })
  }
}

if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log('statement values ok')
