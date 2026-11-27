#!/usr/bin/env node

// A screen's stylesheet styles its own elements. A rule like ".menu button" also
// lands on every button a part held inside it draws — a submenu row, a picker's
// options, a shared field — and paints over that part's own look, so the same part
// looks different from screen to screen (2026-10-17: about ninety such rules, among
// them row menus repainting held submenu rows and a form reaching into the share
// role picker). Such a rule takes only its own children (".menu > button"), resets
// at zero weight (":where(.menu button)"), names its element with a class, or the
// part takes the look as its own variant.
//
// Left alone on purpose: icons (svg, img) take their size from where they stand; a
// bare field rule (":not([class])") styles unclassed fields only; and a rule that
// names a part's own data hook, on the tag or the step before it
// ("[data-page-header-root] h1", "h1[data-row-page-title]"), reaches it on purpose.
//
// A part is a component the screen's element holds (written <Part …/> in it), and
// it is reached when it, or a component it holds two levels down, writes the tag.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, normalize, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const src = join(resolve(dirname(fileURLToPath(import.meta.url)), '..'), 'src')
const TAGS = ['button', 'input', 'textarea', 'a', 'select', 'label', 'span', 'p', 'li', 'ul', 'h1', 'h2', 'h3', 'strong', 'small', 'kbd']

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path, out)
    else out.push(path)
  }
  return out
}

const all = walk(src)
const files = new Map(all.filter((path) => /\.tsx?$/u.test(path) && !path.includes('.test.')).map((path) => [normalize(path), readFileSync(path, 'utf8')]))
const escape = (value) => value.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&')

function resolveImport(base, spec) {
  let path
  if (spec.startsWith('@/')) path = join(src, spec.slice(2))
  else if (spec.startsWith('.')) path = join(dirname(base), spec)
  else return null
  path = normalize(path)
  return [path, `${path}.tsx`, `${path}.ts`, join(path, 'index.tsx'), join(path, 'index.ts')].find((candidate) => files.has(candidate)) ?? null
}

// A component's own source: its function, or where its name is imported from.
function componentBody(base, name, hops = 0) {
  const text = files.get(base)
  const own = new RegExp(`(?:function\\s+${escape(name)}\\s*[(<]|const\\s+${escape(name)}\\s*=\\s*(?:\\([^)]*\\)\\s*(?::[^=]+)?=>|\\w+\\s*=>|(?:React\\.)?(?:memo|forwardRef)\\(|function\\b))`, 'u').exec(text)
  if (own) {
    const rest = text.slice(own.index + own[0].length)
    const next = /\n(?:export\s+)?(?:default\s+)?(?:function|const)\s+[A-Z]\w*\s*[=(<]/u.exec(rest)
    return { path: base, body: text.slice(own.index, own.index + own[0].length + (next ? next.index : rest.length)) }
  }
  // A local alias (an icon picked by a condition) draws no element of its own.
  if (new RegExp(`\\bconst\\s+${escape(name)}\\s*=`, 'u').test(text)) return null
  if (hops >= 3) return null
  for (const match of text.matchAll(/import\s+(?:type\s+)?\{([^}]*)\}\s+from\s+["']([^"']+)["']/gu)) {
    for (const entry of match[1].split(',').map((value) => value.trim()).filter(Boolean)) {
      const parts = entry.split(' as ').map((value) => value.trim())
      if (parts.at(-1) !== name) continue
      const target = resolveImport(base, match[2])
      return target ? componentBody(target, parts[0], hops + 1) : null
    }
  }
  for (const match of text.matchAll(/import\s+(\w+)\s+from\s+["']([^"']+)["']/gu)) {
    if (match[1] !== name) continue
    const target = resolveImport(base, match[2])
    const named = target ? /export\s+default\s+(?:function\s+)?(\w+)/u.exec(files.get(target)) : null
    return named ? componentBody(target, named[1], hops + 1) : null
  }
  return null
}

const drawn = new Map()
function draws(base, name, tag, depth = 0) {
  const key = `${base}\0${name}\0${tag}`
  if (drawn.has(key)) return drawn.get(key)
  drawn.set(key, false)
  const found = componentBody(base, name)
  if (!found) return false
  let hit = new RegExp(`<${tag}[\\s>/]`, 'u').test(found.body)
  if (!hit && depth < 2) {
    const held = new Set([...found.body.matchAll(/<([A-Z]\w*)/gu)].map((match) => match[1]))
    hit = [...held].some((other) => other !== name && draws(found.path, other, tag, depth + 1))
  }
  drawn.set(key, hit)
  return hit
}

// The JSX element opening at start, through its matching close.
function element(text, start) {
  let index = start
  let depth = 0
  while (index < text.length) {
    if (text[index] === '<' && /[A-Za-z/>]/u.test(text[index + 1] ?? '')) {
      const closing = text[index + 1] === '/'
      let cursor = closing ? index + 2 : index + 1
      let braces = 0
      let quote = ''
      for (; cursor < text.length; cursor++) {
        const char = text[cursor]
        if (quote) {
          if (char === '\\') cursor++
          else if (char === quote) quote = ''
        } else if (`"'\``.includes(char)) quote = char
        else if (char === '{') braces++
        else if (char === '}') braces--
        else if (char === '>' && braces === 0) break
      }
      if (closing) depth--
      else if (text[cursor - 1] !== '/') depth++
      index = cursor + 1
      if (depth === 0) return text.slice(start, index)
      continue
    }
    index++
  }
  return text.slice(start)
}

// Just past the opening tag that starts at start.
function tagEnd(text, start) {
  let braces = 0
  let quote = ''
  for (let index = start + 1; index < text.length; index++) {
    const char = text[index]
    if (quote) {
      if (char === '\\') index++
      else if (char === quote) quote = ''
    } else if (`"'\``.includes(char)) quote = char
    else if (char === '{') braces++
    else if (char === '}') braces--
    else if (char === '>' && braces === 0) return index + 1
  }
  return text.length
}

// The start of the JSX tag whose class attribute holds position, if one does
// (a class among others in a template string counts).
function opening(text, position) {
  let index = position
  for (let tries = 0; tries < 40; tries++) {
    index = text.lastIndexOf('<', index - 1)
    if (index < 0) return null
    if (/[A-Za-z]/u.test(text[index + 1] ?? '')) {
      return tagEnd(text, index) > position && /[cC]lassName=/u.test(text.slice(index, position)) ? index : null
    }
  }
  return null
}

// Each stylesheet's users, by the name they import it as.
const users = new Map()
for (const [path, text] of files) {
  for (const match of text.matchAll(/import\s+(\w+)\s+from\s+["']([^"']+\.module\.css)["']/gu)) {
    const spec = match[2]
    const sheet = normalize(spec.startsWith('@/') ? join(src, spec.slice(2)) : join(dirname(path), spec))
    users.set(sheet, [...(users.get(sheet) ?? []), [path, match[1]]])
  }
}
// The database sheet merges its modules: whoever imports it uses each of them.
const sheet = normalize(join(src, 'components/database/databaseSheet.ts'))
if (files.has(sheet)) {
  const merged = [...files.get(sheet).matchAll(/from\s+"(\.\/\w+\.module\.css)"/gu)].map((match) => normalize(join(dirname(sheet), match[1])))
  const sheetUsers = [...files].flatMap(([path, text]) => [...text.matchAll(/import\s+(\w+)\s+from\s+["']([^"']*databaseSheet)["']/gu)]
    .filter((match) => resolveImport(path, match[2]) === sheet).map((match) => [path, match[1]]))
  for (const module of merged) users.set(module, [...(users.get(module) ?? []), ...sheetUsers])
}

const rule = new RegExp(`\\.([A-Za-z_][\\w-]*)[^\\s>+~]*(?:\\s+[^\\s>+~]+)*\\s+(${TAGS.join('|')})((?:[:\\[][^\\s]*)?)$`, 'u')

const found = new Set()
for (const path of all.filter((candidate) => candidate.endsWith('.module.css'))) {
  const css = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, '')
  for (const block of css.matchAll(/([^{};]+)\{/gu)) {
    for (const part of block[1].split(',')) {
      const selector = part.split(/\s+/u).filter(Boolean).join(' ')
      if (selector.startsWith('@') || selector.includes(':where(')) continue
      const match = rule.exec(selector)
      if (!match || match[3].includes(':not([class])') || match[3].includes('[data-')) continue
      const tag = match[2]
      const steps = selector.slice(0, match.index + match[0].length - match[2].length - match[3].length).split(' ').filter(Boolean)
      if (steps.at(-1)?.startsWith('[data-')) continue
      // The nearest class before the tag is the element that holds it.
      const holder = [...steps].reverse().map((step) => /\.([A-Za-z_][\w-]*)/u.exec(step)?.[1]).find(Boolean) ?? match[1]
      for (const [tsx, alias] of users.get(normalize(path)) ?? []) {
        const text = files.get(tsx)
        for (const use of text.matchAll(new RegExp(`\\b${escape(alias)}\\.${escape(holder)}\\b`, 'gu'))) {
          const start = opening(text, use.index)
          if (start === null) continue
          const held = new Set([...element(text, start).slice(1).matchAll(/<([A-Z]\w*)/gu)].map((entry) => entry[1]))
          const reached = [...held].filter((name) => draws(tsx, name, tag)).sort()
          if (reached.length) found.add(`${relative(src, path)} "${selector}" reaches ${reached.slice(0, 3).join(', ')} in ${relative(src, tsx)}`)
        }
      }
    }
  }
}

// The walk itself still finds a part's own element two levels down: a tab
// block's menu holds menu items, each a button.
const menu = normalize(join(src, 'components/editor/TabBlockMenu.tsx'))
if (!files.has(menu) || !draws(menu, 'NativeMenuItem', 'button')) {
  console.error('check-css-reach-into-parts.mjs no longer sees the button a tab block menu\'s items draw: the walk is broken.')
  process.exit(1)
}

if (found.size) {
  console.error('Screen stylesheets reach into the parts they hold (the rule paints the part\'s own element):')
  for (const finding of [...found].sort()) console.error(`  ${finding}`)
  console.error('Take only the own children (".x > button"), reset at zero weight (":where(.x button)"), name the element with a class, or give the part the look as a variant of its own.')
  process.exit(1)
}
