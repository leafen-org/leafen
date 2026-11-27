#!/usr/bin/env node

// The screens turn over with their language: in Arabic, Hebrew, Persian or
// Urdu the sidebar, the menus and every row read from the right. So the
// styles say where a thing sits in the reading direction, not on the page's
// left or right: `margin-inline-start`, `inset-inline-end`,
// `padding-inline: 16px 12px`, `border-start-end-radius`, `text-align: start`.
// In a left-to-right language they are the left and right they replaced.
//
// This check refuses a style that names the left or right side:
//   - margin-, padding-, border-, scroll-margin-, scroll-padding-left/right,
//     left, right and the border-top/bottom-left/right-radius corners;
//   - text-align, float and clear set to left or right;
//   - margin, padding, inset, scroll-margin, scroll-padding and border-width,
//     -style or -color with four values whose right and left differ, and a
//     border-radius whose left and right corners differ.
// A style that must keep the page's own side says why on its line, as
// `left: 50%; /* physical: centred on its own box in either direction */`.
// A sideways shadow, slide or fade multiplies its offset by
// `var(--inline-sign, 1)` (styles.css). The generated editor styles are
// made from sources this check reads.
//
// Leafen's own app packages (the built-in apps and the catalog apps kept
// here) follow the same rule in their screens' stylesheets; the published
// builds they carry unchanged (`vendor/`) and the Vault's web vault (`site/`)
// keep their makers' styles.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')
const appRoots = [
  resolve(webRoot, '..', 'internal', 'app', 'builtinapps'),
  resolve(webRoot, '..', 'catalog-apps'),
]
const NOT_OURS = new Set(['vendor', 'site', 'node_modules'])

const SIDE_PROPERTY = /^(?:(?:margin|padding|scroll-margin|scroll-padding)-(?:left|right)|border-(?:left|right)(?:-(?:width|style|color))?|left|right|border-(?:top|bottom)-(?:left|right)-radius)$/u
const SIDE_VALUE = new Set(['text-align', 'float', 'clear'])
const FOUR_SIDES = new Set(['margin', 'padding', 'inset', 'scroll-margin', 'scroll-padding', 'border-width', 'border-style', 'border-color'])
const ALLOWED = /\/\*\s*physical:\s*\S[^*]*\*\//u

function styleFiles(directory, skip = new Set()) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) {
      if (!skip.has(name)) files.push(...styleFiles(path, skip))
    } else if (name.endsWith('.css') && !name.endsWith('.generated.module.css')) files.push(path)
  }
  return files
}

// Values split at their spaces, a function's own spaces kept.
function words(value) {
  const out = []
  let depth = 0
  let word = ''
  for (const char of value.trim()) {
    if (char === '(') depth++
    if (char === ')') depth--
    if (/\s/u.test(char) && depth === 0) {
      if (word) out.push(word)
      word = ''
    } else word += char
  }
  if (word) out.push(word)
  return out
}

function physical(property, value) {
  const plain = value.replace(/!important\s*$/u, '').trim()
  if (SIDE_PROPERTY.test(property)) return true
  if (SIDE_VALUE.has(property)) return /^(?:left|right)$/iu.test(plain)
  const sides = words(plain)
  if (FOUR_SIDES.has(property)) return sides.length === 4 && sides[1] !== sides[3]
  if (property === 'border-radius' && !plain.includes('/') && sides.length >= 2 && sides.length <= 4) {
    const [topLeft, topRight = topLeft, bottomRight = topLeft, bottomLeft = topRight] = sides
    return topLeft !== topRight || bottomRight !== bottomLeft
  }
  return false
}

const offenders = []
for (const path of [styleFiles(sourceRoot), ...appRoots.map((root) => styleFiles(root, NOT_OURS))].flat()) {
  const source = readFileSync(path, 'utf8')
  const lines = source.split('\n')
  // Comments become spaces, so a commented-out style is not read and every
  // declaration keeps its line.
  const code = source.replace(/\/\*[\s\S]*?\*\//gu, (comment) => comment.replace(/[^\n]/gu, ' '))
  for (const match of code.matchAll(/(?<=[{;]\s*)(-?[a-zA-Z][a-zA-Z-]*)\s*:\s*([^;{}]*)/gu)) {
    const property = match[1].toLowerCase()
    if (!physical(property, match[2])) continue
    const line = code.slice(0, match.index).split('\n').length
    if (ALLOWED.test(lines[line - 1] ?? '')) continue
    offenders.push(`${relative(webRoot, path)}:${line}: ${match[1]}: ${match[2].trim()}`)
  }
}

// A style written in code — React's style object, a style object assigned
// to an element — names its sides the same way, in camel case
// (`paddingInlineStart`, `marginInlineEnd`); a key that names the left or
// right side, or `textAlign`/`float` set to "left" or "right", is refused
// unless its line says why it keeps the page's side (`// physical: ...`)
// (2026-11-09: the sidebar's indents were inline paddingLeft, which the
// stylesheet check never read).
const SIDE_KEY = /(?<![.\w$])((?:margin|padding|scrollMargin|scrollPadding)(?:Left|Right)|border(?:Left|Right)(?:Width|Style|Color)?|border(?:Top|Bottom)(?:Left|Right)Radius)\s*:/gu
const SIDE_KEY_VALUE = /(?<![.\w$])(textAlign|float|clear)\s*:\s*["'`](left|right)["'`]/gu
const ALLOWED_CODE = /\/\/\s*physical:\s*\S|\/\*\s*physical:\s*\S/u
function codeFiles(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) files.push(...codeFiles(path))
    else if (/\.(?:ts|tsx)$/u.test(name) && !/\.generated\.ts$/u.test(name) && !name.endsWith('.d.ts')) files.push(path)
  }
  return files
}
for (const path of codeFiles(sourceRoot)) {
  const lines = readFileSync(path, 'utf8').split('\n')
  lines.forEach((text, index) => {
    if (ALLOWED_CODE.test(text)) return
    for (const match of [...text.matchAll(SIDE_KEY), ...text.matchAll(SIDE_KEY_VALUE)]) {
      offenders.push(`${relative(webRoot, path)}:${index + 1}: ${match[0].trim()}`)
    }
  })
}

if (offenders.length > 0) {
  console.error('Say where a style sits in the reading direction (margin-inline-start, inset-inline-end, padding-inline, text-align: start ...),')
  console.error('or give the reason it keeps the page\'s own side on its line (/* physical: ... */):')
  for (const offender of offenders) console.error(`  ${offender}`)
  process.exit(1)
}
console.log('logical sides verified')
