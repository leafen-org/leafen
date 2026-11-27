#!/usr/bin/env node

// Korean, Japanese, Chinese, Vietnamese, a Mac's ⌥ accents and a phone
// keyboard's words are typed through an input method's composition, and
// browsers end a composition differently: macOS Chrome's Korean ends each
// syllable and starts the next in one key press, and sends an Enter typed
// while composing as keydown 229, compositionend and keydown 13 together;
// Safari ends a composition before the key that ended it. A field that
// looked at a composition its own way, or sent on Enter straight away, sent
// a comment with the words up to its last pause.
//
// So one file meets compositions for all of web/src — src/lib/fieldKeyBoundary.ts
// — and this check refuses, anywhere else:
//   - a composition check of its own: `isComposing`, keyCode or which 229,
//     the "Process" key, an editor view's `composing` (imeComposing instead);
//   - a key compared with Enter (fieldEnter or fieldKey instead, which leave
//     the input method its keys and publish a field's words before the key
//     sends, confirms or moves).
// A line that must do otherwise is listed below with the reason, by the
// words it holds; an entry that no longer matches a line fails the check too.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')
const BOUNDARY = 'lib/fieldKeyBoundary.ts'

const COMPOSITION = [
  [/\.isComposing\b/u, 'an isComposing check'],
  [/\bisComposingKeyEvent\b/u, 'the old isComposingKeyEvent'],
  [/\b(?:keyCode|which)\s*[!=]==?\s*229\b|\b229\s*[!=]==?\s*[\w$.?]*\b(?:keyCode|which)\b/u, 'a keyCode 229 check'],
  [/["'`]Process["'`]/u, 'the "Process" key'],
  [/\b[\w$]*[vV]iew\)?\??\.composing\b/u, "an editor view's composing"],
]
const ENTER = [
  /\.(?:key|code)\s*[!=]==?\s*["'`](?:Numpad)?Enter["'`]/u,
  /["'`](?:Numpad)?Enter["'`]\s*[!=]==?\s*[\w$.?]*\b(?:key|code)\b/u,
  /(?:^|[^.\w$])key\s*[!=]==?\s*["'`]Enter["'`]/u,
  /\bcase\s+["'`]Enter["'`]/u,
]

// [file under src, words on the line, why it may].
const ALLOWED = [
  ['components/database/viewChoiceSteps.ts', 'key === "Enter" || key === " " || key === "Escape"',
    'names Enter among the keys a view choice leaves alone; it acts on none of them'],
  ['components/library/LibraryRowEdits.tsx', 'if (key === "Enter") onEnter?.(name.page)',
    "the name editor's own word for how it closed, its Enter already through the boundary (RenameTitleField)"],
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

// A line's code: a line of a comment holds none, and a trailing comment is
// left off where no quote is open before it.
function code(line) {
  const trimmed = line.trim()
  if (trimmed.startsWith('//') || trimmed.startsWith('*') || trimmed.startsWith('/*')) return ''
  let quote = null
  for (let index = 0; index < line.length; index += 1) {
    const char = line[index]
    if (quote) {
      if (char === '\\') index += 1
      else if (char === quote) quote = null
    } else if (char === '"' || char === "'" || char === '`') quote = char
    else if (char === '/' && line[index + 1] === '/') return line.slice(0, index)
  }
  return line
}

const problems = []
const used = new Set()
for (const path of sourceFiles(sourceRoot)) {
  const file = relative(sourceRoot, path).split('\\').join('/')
  if (file === BOUNDARY) continue
  readFileSync(path, 'utf8').split('\n').forEach((line, index) => {
    const text = code(line)
    if (!text) return
    const found = COMPOSITION.filter(([pattern]) => pattern.test(text)).map(([, what]) => what)
    if (ENTER.some((pattern) => pattern.test(text))) found.push('a key compared with Enter')
    if (!found.length) return
    const allowed = ALLOWED.findIndex(([at, words]) => at === file && line.includes(words))
    if (allowed >= 0) { used.add(allowed); return }
    problems.push(`src/${file}:${index + 1}: ${found.join(', ')} — use ${BOUNDARY} (imeComposing, fieldKey, fieldEnter, fieldEscape)\n    ${line.trim()}`)
  })
}
ALLOWED.forEach(([file, words], index) => {
  if (!used.has(index)) problems.push(`check-composition-keys.mjs: the allowed line "${words}" is no longer in src/${file}`)
})

if (problems.length) {
  console.error(`Keys and compositions outside the boundary (${problems.length}):\n${problems.join('\n')}`)
  process.exit(1)
}
