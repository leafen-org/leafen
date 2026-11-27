#!/usr/bin/env node

// A style that reads a custom property no style sets is dropped whole by the
// browser, silently: `border-top: 1px solid var(--border-light)` with no
// `--border-light` anywhere drew no line at all (2026-11-11, seven such
// names in nine files). Given a fallback, `var(--x, …)`, the declaration
// stays but the fallback is all it ever draws: a value written by hand that
// no theme reaches (2026-11-24: --bg-input in nineteen fields, --accent,
// --bg-popover and seven more drew their fallbacks in both themes). So this
// check refuses a read of a name nothing sets, with a fallback or without.
//
// A name counts as set when a style sheet under src/ (or index.html)
// declares it, or when a script names it as a string — a style object's key
// (`{ "--row-peek-width": … }`) or `style.setProperty("--x", …)`. A name a
// script makes up from parts (`--${name}`) is set under no name this check
// can see: it goes in SET_BY_PARTS below with the script that sets it.
// Reads are looked for in style sheets and in scripts' strings alike.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// Names a script sets from parts, which a style reads with a fallback for
// where no script set it: [name, the script that sets it and why].
const SET_BY_PARTS = []

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')

function files(directory, extensions) {
  const found = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) found.push(...files(path, extensions))
    else if (extensions.some((extension) => name.endsWith(extension))) found.push(path)
  }
  return found
}

const styles = [...files(sourceRoot, ['.css']), join(webRoot, 'index.html')]
const scripts = files(sourceRoot, ['.ts', '.tsx']).filter((path) => !/\.test\.tsx?$/u.test(path))

// Comments say nothing a browser reads.
const withoutComments = (text) => text.replace(/\/\*[\s\S]*?\*\//gu, (comment) => comment.replace(/[^\n]/gu, ' '))

const defined = new Set(SET_BY_PARTS.map(([name]) => name))
for (const path of styles) {
  for (const match of withoutComments(readFileSync(path, 'utf8')).matchAll(/(?<![\w-])(--[\w-]+)\s*:/gu)) defined.add(match[1])
}
for (const path of scripts) {
  for (const match of readFileSync(path, 'utf8').matchAll(/["'`](--[\w-]+)["'`]/gu)) defined.add(match[1])
}

const failures = []
for (const path of [...styles, ...scripts]) {
  const text = path.endsWith('.css') || path.endsWith('.html') ? withoutComments(readFileSync(path, 'utf8')) : readFileSync(path, 'utf8')
  for (const match of text.matchAll(/var\(\s*(--[\w-]+)\s*([,)])/gu)) {
    if (defined.has(match[1])) continue
    const line = text.slice(0, match.index).split('\n').length
    const drawn = match[2] === ',' ? 'its fallback is all it draws' : 'the browser drops the whole declaration'
    failures.push(`${relative(webRoot, path)}:${line}: var(${match[1]}${match[2] === ',' ? ', …' : ''}) — nothing sets ${match[1]}, ${drawn}`)
  }
}

if (failures.length) {
  console.error('Styles read custom properties nothing sets:')
  for (const failure of failures) console.error(`  ${failure}`)
  console.error('Read the token of that role the styles define (app/*.css, styles.css); a name a script sets from parts goes in SET_BY_PARTS with that script.')
  process.exit(1)
}
