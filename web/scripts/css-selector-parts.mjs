// What the CSS checks share: the style sources they read (the modules'
// sources, the generated editor styles left out, and index.html's own
// styles) and how they take a selector apart, as Chrome ties a rule to the
// elements a change may reach (check-css-sibling-invalidation.mjs,
// check-css-pseudo-invalidation.mjs).

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

export const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

export function styleFiles(directory) {
  const found = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) found.push(...styleFiles(path))
    else if (name.endsWith('.css') && !name.endsWith('.generated.module.css')) found.push(path)
  }
  return found
}

export const withoutComments = (text) => text.replace(/\/\*[\s\S]*?\*\//gu, (comment) => comment.replace(/[^\n]/gu, ' '))

/** Each rule's selector text and the line it starts on (at-rules' own preludes and keyframe steps left out). */
export function selectors(text) {
  const found = []
  let start = 0
  let depth = 0
  const frames = []
  for (let i = 0; i < text.length; i++) {
    const char = text[i]
    if (char === '"' || char === "'") {
      const end = text.indexOf(char, i + 1)
      i = end < 0 ? text.length : end
    } else if (char === '{') {
      const prelude = text.slice(start, i).trim()
      const inKeyframes = frames.at(-1) === 'keyframes'
      frames.push(prelude.startsWith('@') ? (/^@(?:-\w+-)?keyframes\b/u.test(prelude) ? 'keyframes' : 'at') : 'rule')
      if (!prelude.startsWith('@') && !inKeyframes && prelude) {
        const offset = start + text.slice(start, i).search(/\S/u)
        found.push({ selector: prelude, line: text.slice(0, offset).split('\n').length })
      }
      depth++
      start = i + 1
    } else if (char === '}') {
      frames.pop()
      depth = Math.max(0, depth - 1)
      start = i + 1
    } else if (char === ';' && frames.at(-1) !== 'rule') start = i + 1
  }
  return found
}

/** Splits at top-level commas. */
export function list(selector) {
  const parts = []
  let depth = 0
  let part = ''
  for (const char of selector) {
    if (char === '(' || char === '[') depth++
    if (char === ')' || char === ']') depth--
    if (char === ',' && depth === 0) { parts.push(part); part = '' } else part += char
  }
  parts.push(part)
  return parts.map((value) => value.trim()).filter(Boolean)
}

/** A complex selector's compounds and the combinators between them, at its top level; the arguments of its functions apart. */
export function compounds(selector) {
  const items = []
  const nested = []
  let depth = 0
  let compound = ''
  let pending = null
  let fn = ''
  let argument = ''
  const close = () => {
    if (compound.trim()) { items.push({ combinator: pending, text: compound.trim() }); pending = null }
    compound = ''
  }
  for (let i = 0; i < selector.length; i++) {
    const char = selector[i]
    if (depth > 0) {
      if (char === '(' || char === '[') depth++
      if (char === ')' || char === ']') depth--
      if (depth === 0) {
        compound += char
        if (/^:(?:not|is|where|matches|-webkit-any)$/iu.test(fn)) nested.push(...list(argument))
        fn = ''
        argument = ''
      } else { compound += char; argument += char }
      continue
    }
    if (char === '(' || char === '[') {
      if (char === '(') fn = (compound.match(/:[\w-]+$/u) || [''])[0]
      depth++
      compound += char
    } else if (char === '>' || char === '+' || char === '~') {
      close()
      pending = char
    } else if (/\s/u.test(char)) {
      close()
      if (pending === null && items.length) pending = ' '
    } else compound += char
  }
  close()
  return { items, nested }
}

/** A CSS module's :global(…) and :local(…) taken off, as the built style has them. */
export function unwrapped(selector) {
  let out = selector.replace(/:(?:global|local)(?![\w(-])\s*/gu, '')
  for (let at = out.search(/:(?:global|local)\(/u); at >= 0; at = out.search(/:(?:global|local)\(/u)) {
    const open = out.indexOf('(', at)
    let depth = 0
    let end = open
    for (; end < out.length; end++) {
      if (out[end] === '(') depth++
      else if (out[end] === ')' && --depth === 0) break
    }
    out = out.slice(0, at) + out.slice(open + 1, end) + out.slice(end + 1)
  }
  return out
}

/**
 * Whether a compound names a class, an id or an attribute at its own level,
 * or in an :is() or :where() each of whose selectors ends in one that does.
 */
export function tied(compound) {
  let depth = 0
  let fn = ''
  let argument = ''
  for (let i = 0; i < compound.length; i++) {
    const char = compound[i]
    if (char === '(') {
      if (depth === 0) { fn = (compound.slice(0, i).match(/:[\w-]+$/u) || [''])[0]; argument = '' } else argument += char
      depth++
    } else if (char === ')') {
      depth--
      if (depth === 0) {
        if (/^:(?:is|where|matches|-webkit-any)$/iu.test(fn) && list(argument).every((inner) => tied(compounds(inner).items.at(-1)?.text ?? ''))) return true
      } else argument += char
    } else if (depth > 0) argument += char
    else if (char === '.' || char === '#' || char === '[') return true
  }
  return false
}


// Every editable and every block an editor draws carries `contenteditable`.
export const everyEditable = /\[\s*(?:type|role|aria-[\w-]+|tabindex|href|title|style|hidden|disabled|name|value|id|class|dir|lang|contenteditable)\s*(?:[~|^$*]?=|\])/iu

/**
 * Whether a rule's last compound is tied by a class or an id, or by an
 * attribute not every element carries; one with no class or id of its own
 * whose :is() or :where() holds such an untied selector puts that into the
 * set too: `… :has(…) … :is([contenteditable], [data-x])` restyled every
 * text cell of a table at each press in it (2026-11-15).
 */
export function bareEnd(own) {
  const named = /[.#]/u.test(own.replace(/\[[^\]]*\]|\([^)]*\)/gu, ''))
  if (!tied(own) || (!named && everyEditable.test(own) && !own.replace(new RegExp(everyEditable.source, 'giu'), '').replace(/:(?:is|where)\((?:[^()]|\([^()]*\))*\)/gu, '').includes('['))) return true
  if (named) return false
  for (const [, argument] of own.matchAll(/:(?:is|where)\(((?:[^()]|\([^()]*\))*)\)/gu)) {
    if (list(argument).some((inner) => bareEnd(compounds(inner).items.at(-1)?.text ?? ''))) return true
  }
  return false
}

/** index.html's own styles, each at the line it starts on. */
export function pageStyles(path) {
  const text = readFileSync(path, 'utf8')
  return [...text.matchAll(/<style>([\s\S]*?)<\/style>/gu)].map((match) => {
    const before = text.slice(0, match.index + '<style>'.length).split('\n').length - 1
    return '\n'.repeat(before) + match[1]
  }).join('\n')
}

/** Every style source the checks read, as [path, text]. */
export function styleSources() {
  return [...styleFiles(join(webRoot, 'src')).map((path) => [path, readFileSync(path, 'utf8')]), [join(webRoot, 'index.html'), pageStyles(join(webRoot, 'index.html'))]]
}
