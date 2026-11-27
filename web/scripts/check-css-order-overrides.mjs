#!/usr/bin/env node

// Two stylesheets' classes on one element must not depend on which stylesheet
// loads last. The bundle orders stylesheets by how the screens happen to be
// imported, and that order changes with unrelated edits (2026-10-17: the
// Notion migration window's "as tall as its content" lost to the import
// dialog's fixed height, so its lower half stood empty; a library menu row's
// gray text and the narrow settings layout held only by luck). Where both
// classes set the same property differently, the one meant to win is more
// specific — it names its element or the context it applies in — or is
// !important, or the two looks become one class in one stylesheet, or the
// part takes the look as a prop of its own. This covers classes written
// together in a className and classes a style helper joins by name from
// several stylesheets. Classes that only stand in for each other (a ? x : y)
// are never both applied. A class composed from another file is on the
// element too (composed-sheets-once.mjs), so it counts with the classes
// written next to the one that composes it.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const src = join(webRoot, 'src')

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path, out)
    else out.push(path)
  }
  return out
}

const importPattern = /import\s+(\w+)\s+from\s+"([^"]+\.module\.css)"/gu
const helperImportPattern = /import\s*\{\s*styles(?:\s+as\s+(\w+))?\s*\}\s*from\s*"([^"]+)"/gu

function resolveSpec(base, spec) {
  if (spec.startsWith('@/')) return join(src, spec.slice(2))
  return resolve(dirname(base), spec)
}

// A selector list's selectors: split at its own commas, not those inside
// :has(), :not(), :is() or :where() (".row:has(.menu, .picker)" is one
// selector, and ".menu" in it no rule of its own).
function selectorList(selector) {
  const out = []
  let depth = 0
  let start = 0
  for (let index = 0; index < selector.length; index += 1) {
    const char = selector[index]
    if (char === '(') depth += 1
    else if (char === ')') depth -= 1
    else if (char === ',' && depth === 0) {
      out.push(selector.slice(start, index))
      start = index + 1
    }
  }
  out.push(selector.slice(start))
  return out
}

const ruleCache = new Map()
// Declarations of rules whose selector is exactly one class, anywhere in the
// file (also inside @media and @container).
function rules(path) {
  if (ruleCache.has(path)) return ruleCache.get(path)
  const text = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, '')
  const out = new Map()
  for (const match of text.matchAll(/([^{}]*)\{([^{}]*)\}/gu)) {
    const selector = match[1].trim()
    if (selector.startsWith('@')) continue
    for (const item of selectorList(selector)) {
      const one = /^\.([A-Za-z_][\w-]*)$/u.exec(item.trim())
      if (!one) continue
      const declarations = out.get(one[1]) ?? new Map()
      for (const part of match[2].split(';')) {
        const colon = part.indexOf(':')
        if (colon < 0) continue
        const property = part.slice(0, colon).trim()
        if (property === 'composes') continue
        const values = declarations.get(property) ?? new Set()
        values.add(part.slice(colon + 1).trim())
        declarations.set(property, values)
      }
      out.set(one[1], declarations)
    }
  }
  ruleCache.set(path, out)
  return out
}

const composedCache = new Map()
// The classes a class puts on its element: itself, and what it composes,
// in its own file or from another, all the way down.
function composed(path, name, seen = new Set()) {
  const key = `${path}#${name}`
  if (seen.has(key)) return []
  seen.add(key)
  if (!composedCache.has(path)) {
    const text = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, '')
    const out = new Map()
    for (const match of text.matchAll(/([^{}]*)\{([^{}]*)\}/gu)) {
      const one = /^\.([A-Za-z_][\w-]*)$/u.exec(match[1].trim())
      if (!one) continue
      for (const [, list, from] of match[2].matchAll(/composes\s*:\s*([^;]*?)(?:\s+from\s+"([^"]+)")?\s*(?:;|$)/gu)) {
        const file = from ? resolve(dirname(path), from) : path
        const names = out.get(one[1]) ?? []
        for (const other of list.trim().split(/\s+/u)) names.push([file, other])
        out.set(one[1], names)
      }
    }
    composedCache.set(path, out)
  }
  return [[path, name], ...(composedCache.get(path).get(name) ?? []).flatMap(([file, other]) => composed(file, other, seen))]
}

function clashes(left = new Map(), right = new Map()) {
  const out = []
  for (const [property, values] of left) {
    const other = right.get(property)
    if (!other) continue
    const same = values.size === other.size && [...values].every((value) => other.has(value))
    if (same) continue
    const importantLeft = [...values].every((value) => value.includes('!important'))
    const importantRight = [...other].every((value) => value.includes('!important'))
    if (importantLeft !== importantRight) continue
    out.push(property)
  }
  return out.sort()
}

function* classNameExpressions(text) {
  for (const match of text.matchAll(/className=\{/gu)) {
    let index = match.index + match[0].length
    let depth = 1
    const start = index
    while (index < text.length && depth > 0) {
      if (text[index] === '{') depth += 1
      else if (text[index] === '}') depth -= 1
      index += 1
    }
    yield { line: text.slice(0, match.index).split('\n').length, expression: text.slice(start, index - 1) }
  }
}

const escape = (value) => value.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&')
const alternatives = (expression, left, right) =>
  new RegExp(`\\?\\s*${escape(left)}\\s*:\\s*${escape(right)}\\b`, 'u').test(expression)

const files = walk(src).filter((path) => /\.tsx?$/u.test(path) && !path.includes('.test.'))
const sources = new Map(files.map((path) => [path, readFileSync(path, 'utf8')]))

// A style helper joins the same-named classes of the stylesheets it lists.
const helpers = new Map()
for (const [path, text] of sources) {
  if (!path.endsWith('.ts') || !text.includes('join(" ")')) continue
  const list = /=\s*\[\s*((?:\w+,\s*)+\w*)\s*\]/u.exec(text)
  if (!list) continue
  const modules = new Map([...text.matchAll(importPattern)].map(([, name, spec]) => [name, resolveSpec(path, spec)]))
  const listed = list[1].split(',').map((name) => name.trim()).filter((name) => modules.has(name)).map((name) => modules.get(name))
  if (listed.length >= 2) helpers.set(path.replace(/\.ts$/u, ''), listed)
}

const findings = new Set()
const report = (path, line, left, right, properties) =>
  findings.add(`${relative(webRoot, path)}${line ? `:${line}` : ''} ${left} vs ${right}: ${properties.join(', ')}`)

for (const [path, list] of helpers) {
  for (let i = 0; i < list.length; i += 1) {
    for (const other of list.slice(i + 1)) {
      for (const [key, declarations] of rules(list[i])) {
        const found = clashes(declarations, rules(other).get(key))
        if (found.length) report(`${path}.ts`, 0, `${relative(src, list[i])} .${key}`, `${relative(src, other)} .${key}`, found)
      }
    }
  }
}

for (const [path, text] of sources) {
  const modules = new Map([...text.matchAll(importPattern)]
    .map(([, name, spec]) => [name, resolveSpec(path, spec)])
    .filter(([, file]) => existsSync(file)))
  const merged = new Map()
  for (const [, alias, spec] of text.matchAll(helperImportPattern)) {
    const list = helpers.get(resolveSpec(path, spec).replace(/\.tsx?$/u, ''))
    if (list) merged.set(alias || 'styles', list)
  }
  if (modules.size < 2 && merged.size === 0) continue
  for (const { line, expression } of classNameExpressions(text)) {
    const references = [...expression.matchAll(/\b(\w+)\.(\w+)\b/gu)].filter(([, name]) => modules.has(name))
    for (let i = 0; i < references.length; i += 1) {
      for (const other of references.slice(i + 1)) {
        const [left, leftName, leftClass] = references[i]
        const [right, rightName, rightClass] = other
        if (alternatives(expression, left, right)) continue
        for (const [leftFile, leftKey] of composed(modules.get(leftName), leftClass)) {
          for (const [rightFile, rightKey] of composed(modules.get(rightName), rightClass)) {
            if (leftFile === rightFile) continue
            const found = clashes(rules(leftFile).get(leftKey), rules(rightFile).get(rightKey))
            if (!found.length) continue
            const name = (reference, file, key) => (file === modules.get(reference.split('.')[0]) && reference.endsWith(`.${key}`)
              ? reference : `${reference} (composes ${relative(src, file)} .${key})`)
            report(path, line, name(left, leftFile, leftKey), name(right, rightFile, rightKey), found)
          }
        }
      }
    }
    for (const [name, list] of merged) {
      const keys = [...expression.matchAll(new RegExp(`\\b${escape(name)}\\.(\\w+)\\b`, 'gu'))].map((match) => match[1])
      for (let i = 0; i < keys.length; i += 1) {
        for (const otherKey of keys.slice(i + 1)) {
          if (otherKey === keys[i] || alternatives(expression, `${name}.${keys[i]}`, `${name}.${otherKey}`)) continue
          for (const leftFile of list) {
            for (const rightFile of list) {
              if (leftFile === rightFile) continue
              const found = clashes(rules(leftFile).get(keys[i]), rules(rightFile).get(otherKey))
              if (found.length) report(path, line, `${relative(src, leftFile)} .${keys[i]}`, `${relative(src, rightFile)} .${otherKey}`, found)
            }
          }
        }
      }
    }
  }
}

if (findings.size) {
  console.error('Two stylesheets set the same property on one element at the same weight (the one that wins is whichever loads last):')
  for (const finding of [...findings].sort()) console.error(`  ${finding}`)
  console.error('Make the one meant to win more specific (its element or context), put both looks in one stylesheet, or give the part a prop for its look.')
  process.exit(1)
}
