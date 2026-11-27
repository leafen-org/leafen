#!/usr/bin/env node

// Every place that names another page — a path, a sidebar row, a link, a
// mention, a relation's chip, a filter's page, a search hit, a notice, a tab
// — names it as the screen knows it now, from one owner: src/lib/pageNames.ts
// with pageNameRecords.ts and graphRowNames.ts. A place that worked a name
// out of the page it held named it as it was when it took it: a page made
// from a template was titled, and the filter in its own body still named it
// "새 페이지" (user, 2026-11-20 on the NAS).
//
// So this check refuses, outside the owner:
//   - importing the formatters that name a page from what is held
//     (pageDisplayTitle, listedPageDisplayTitle, databaseDisplayTitle from
//     lib/pageTitle.ts): a name is had through the owner's readers;
//   - importing the owner's records (lib/pageNameRecords.ts), but where a
//     line below says why;
//   - a component or hook (a top-level function named in capitals or
//     use…) naming a page by pageNamesNow, which isn't drawn again as the
//     name changes: it names through usePageNames, usePageName,
//     useNamedPage or PageTitleText;
//   - a page's title drawn as it is held: {page.title}, {row.title} and the
//     like in a .tsx file's markup.
// A line that must do otherwise is listed below with the reason, by the
// words it holds; an entry that no longer matches a line fails the check too.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')
const OWNER = new Set(['lib/pageNames.ts', 'lib/pageNameRecords.ts', 'lib/graphRowNames.ts', 'lib/pageTitle.ts'])
const FORMATTERS = /\b(?:pageDisplayTitle|listedPageDisplayTitle|databaseDisplayTitle)\b/u
const HELD_TITLE = /(?:^|[^=\w$])\{\s*(?:[\w$]+\??\.)*(?:page|[a-z][\w$]*Page|row|[a-z][\w$]*Row|holder|target)\??\.title\s*\}/u

// [file under src, words on the line, why it may].
const ALLOWED = [
  ['lib/closedMentions.ts', 'import { knownPageName, typedPageTitle } from "./pageNameRecords";',
    "a mention's name as the owner knows it; the readers' module would close a cycle through the people directory"],
  ['components/database/richTextCellEngine.ts', 'import { followPageName } from "@/lib/pageNameRecords";',
    "a field's mention drawn outside React follows its page's name by the records alone"],
  ['components/sidebar/SidebarFavoritePeopleRow.tsx', '{page.title}</span>',
    'the People page: its fixed name, which no one renames'],
  ['components/account/AccountNotificationSettings.tsx', '<strong>{row.title}</strong><span>{row.description}</span>',
    "a notification setting's row, not a page"],
  ['components/settings/SettingsSearchResults.tsx', '<strong>{row.title}</strong>',
    "a settings search result's row, not a page"],
]

function sourceFiles(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) files.push(...sourceFiles(path))
    else if (/\.tsx?$/u.test(name) && !name.includes('.generated.')) files.push(path)
  }
  return files
}

// A line's code: a line of a comment holds none.
function code(line) {
  const trimmed = line.trim()
  return trimmed.startsWith('//') || trimmed.startsWith('*') || trimmed.startsWith('/*') ? '' : line
}

// The lines of the file's components and hooks that name pages by pageNamesNow.
function nowInDrawing(path, text) {
  const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const lines = new Set()
  const named = (statement) => {
    if (ts.isFunctionDeclaration(statement) && statement.name) return [statement.name.text, statement.body]
    if (ts.isVariableStatement(statement)) {
      for (const declaration of statement.declarationList.declarations) {
        let value = declaration.initializer
        while (value && ts.isCallExpression(value) && value.arguments.length) value = value.arguments[0]
        if (value && (ts.isArrowFunction(value) || ts.isFunctionExpression(value)) && ts.isIdentifier(declaration.name)) return [declaration.name.text, value.body]
      }
    }
    return [null, null]
  }
  for (const statement of source.statements) {
    const [name, body] = named(statement)
    if (!name || !body || !/^(?:use[A-Z]|[A-Z])/u.test(name)) continue
    ;(function walk(node) {
      if (ts.isIdentifier(node) && node.text === 'pageNamesNow') lines.add(source.getLineAndCharacterOfPosition(node.getStart()).line)
      ts.forEachChild(node, walk)
    })(body)
  }
  return lines
}

const problems = []
const used = new Set()
for (const path of sourceFiles(sourceRoot)) {
  const file = relative(sourceRoot, path).split('\\').join('/')
  if (OWNER.has(file)) continue
  const text = readFileSync(path, 'utf8')
  const drawing = text.includes('pageNamesNow') ? nowInDrawing(path, text) : new Set()
  text.split('\n').forEach((line, index) => {
    const words = code(line)
    if (!words) return
    const found = []
    if (/^\s*import\b/u.test(words) && FORMATTERS.test(words) && /pageTitle["']/u.test(words)) found.push('a page named by what is held (the formatters of lib/pageTitle.ts) — use the readers of lib/pageNames.ts')
    if (/^\s*import\b/u.test(words) && /pageNameRecords["']/u.test(words)) found.push("the owner's records — use lib/pageNames.ts")
    if (drawing.has(index)) found.push('pageNamesNow in a component or hook, not drawn again as the name changes — use usePageNames, usePageName or useNamedPage')
    if (file.endsWith('.tsx') && HELD_TITLE.test(words)) found.push("a page's title drawn as held — name it through lib/pageNames.ts (usePageName, PageTitleText)")
    if (!found.length) return
    const allowed = ALLOWED.findIndex(([at, wording]) => at === file && line.includes(wording))
    if (allowed >= 0) { used.add(allowed); return }
    problems.push(`src/${file}:${index + 1}: ${found.join('; ')}\n    ${line.trim()}`)
  })
}
ALLOWED.forEach(([file, words], index) => {
  if (!used.has(index)) problems.push(`check-page-names.mjs: the allowed line "${words}" is no longer in src/${file}`)
})

if (problems.length) {
  console.error(`Pages named outside the owner of pages' names (${problems.length}):\n${problems.join('\n')}`)
  process.exit(1)
}
