#!/usr/bin/env node

// Every translation key the web code names in so many words is in the
// English catalog, which every other language follows: a key no catalog has
// shows as its own name on screen. A literal key counts as `ns:key` or, in a
// file with one `useTranslation`, under that hook's first namespace; a call
// on i18next itself defaults to `common`. Keys a call builds at run time are
// for the code that lists them. No other language leaves a value empty
// where English has words. No string names a tag after an HTML element that
// holds nothing (<link>, <img>, <br>, <input> …) around words: react-i18next
// reads such a tag as that empty element, drawing it empty with the words
// loose beside it (2026-11-11: an invite hint's <link> drew an empty link);
// a component tag takes another name. A lone <br/> stays a line break.
//
// A call with a literal key gives every value its English words name
// ({{name}}) when its options are written out: one left out shows as
// "{{name}}" on screen (2026-11-23, the server's like it: an agent's request
// to connect said "Leafen's %s"); options built elsewhere are for the code
// that builds them.
//
// A key named outside a call counts too: an `ns:key` string anywhere (a
// table of keys, a key handed to a helper) is one the catalog has. A key a
// template builds, `ns:group.${…}.label` anywhere or `group.${…}` as a
// call's key, fits at least one key the catalog has, so a group renamed or
// dropped under it is caught (2026-11-12: lock summaries built keys of a
// group with no such members). Which of a group's keys a value picks is
// still for the code that lists them: the values come from data the check
// does not read.
//
// What each selectable language lacks of English, whole namespace files and
// keys, is counted and said language by language whenever the check passes
// (`node scripts/check-translation-keys.mjs`), as the private repository's
// i18n guard counts it: a plural English key is there when the language has
// its `_other` form. It stops nothing: a language past English and Korean
// shows English there until the translation passes fill it in (user
// decisions of 2026-11-04 and 2026-11-09), and Korean is held complete by
// check-korean-catalog.mjs.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')
const catalogRoot = join(sourceRoot, 'locales', 'en')
const DEFAULT_NS = 'common'
const PLURALS = ['zero', 'one', 'two', 'few', 'many', 'other']

const catalogs = new Map()
for (const name of readdirSync(catalogRoot)) {
  if (name.endsWith('.json')) catalogs.set(name.slice(0, -5), JSON.parse(readFileSync(join(catalogRoot, name), 'utf8')))
}

function has(namespace, key) {
  const catalog = catalogs.get(namespace)
  if (!catalog) return false
  const parts = key.split('.')
  let node = catalog
  for (const [index, part] of parts.entries()) {
    if (node === null || typeof node !== 'object') return false
    if (index === parts.length - 1) {
      return part in node || PLURALS.some((form) => `${part}_${form}` in node)
    }
    node = node[part]
  }
  return false
}

// The English words of a key, with its plural forms.
function wordsOf(namespace, key) {
  let node = catalogs.get(namespace)
  const parts = key.split('.')
  for (const [index, part] of parts.entries()) {
    if (node === null || typeof node !== 'object') return []
    if (index === parts.length - 1) {
      return [node[part], ...PLURALS.map((form) => node[`${part}_${form}`])].filter((value) => typeof value === 'string')
    }
    node = node[part]
  }
  return []
}

const flatKeys = new Map()
function keysOf(namespace) {
  if (!flatKeys.has(namespace)) {
    const keys = []
    const walk = (node, prefix) => {
      for (const [name, value] of Object.entries(node ?? {})) {
        keys.push(prefix + name)
        if (value && typeof value === 'object') walk(value, `${prefix}${name}.`)
      }
    }
    walk(catalogs.get(namespace), '')
    flatKeys.set(namespace, keys)
  }
  return flatKeys.get(namespace)
}

const escapeRegExp = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

// The keys a template could build, as a pattern: its words, anything between.
function templateKeys(node) {
  const parts = [node.head.text, ...node.templateSpans.map((span) => span.literal.text)]
  return parts.map(escapeRegExp).join('.+?')
}

function sourceFiles(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) {
      if (name !== 'locales' && name !== 'node_modules') files.push(...sourceFiles(path))
    } else if (/\.(ts|tsx)$/.test(name) && !/\.(test|spec)\.tsx?$/.test(name) && !name.endsWith('.d.ts')) {
      files.push(path)
    }
  }
  return files
}

const missing = []
const unfilled = []
for (const path of sourceFiles(sourceRoot)) {
  const source = readFileSync(path, 'utf8')
  const hooks = [...source.matchAll(/useTranslation\(\s*(\[[^\]]*\]|["'][^"']+["'])?\s*(,\s*\{[^}]*keyPrefix)?/g)]
  const prefixed = hooks.some((hook) => hook[2])
  const hookNamespace = hooks.length === 1 && !prefixed
    ? (/["']([^"']+)["']/.exec(hooks[0][1] ?? '')?.[1] ?? DEFAULT_NS) : null
  const calls = [
    ...[...source.matchAll(/(?<![\w.$])t\(\s*(["'])([^"'`$\\\n]+)\1/g)].map((match) => ({ key: match[2], namespace: hookNamespace, index: match.index })),
    ...[...source.matchAll(/\bi18n(?:ext)?\.t\(\s*(["'])([^"'`$\\\n]+)\1/g)].map((match) => ({ key: match[2], namespace: DEFAULT_NS, index: match.index })),
    ...[...source.matchAll(/\bi18nKey=(["'])([^"'`$\\\n]+)\1/g)].map((match) => ({ key: match[2], namespace: hookNamespace, index: match.index })),
  ]
  const tree = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const lineOf = (node) => tree.getLineAndCharacterOfPosition(node.getStart(tree)).line + 1
  const visit = (node) => {
    if (ts.isCallExpression(node) && /(^|\.)t$/.test(node.expression.getText(tree)) && node.arguments[0] && ts.isStringLiteral(node.arguments[0])) {
      const [raw, options] = [node.arguments[0].text, node.arguments[1]]
      const separator = raw.indexOf(':')
      const named = separator > 0 && catalogs.has(raw.slice(0, separator))
      const namespace = named ? raw.slice(0, separator) : /\bi18n(ext)?\.t$/.test(node.expression.getText(tree)) ? DEFAULT_NS : hookNamespace
      const given = new Set()
      let open = false
      if (options && ts.isObjectLiteralExpression(options)) {
        for (const property of options.properties) {
          if (ts.isSpreadAssignment(property) || !property.name) open = true
          else given.add(property.name.getText(tree).replace(/^["']|["']$/g, ''))
        }
      } else if (options) open = true
      const key = named ? raw.slice(separator + 1) : raw
      const words = namespace && !open ? wordsOf(namespace, key) : []
      const lacking = [...new Set(words.flatMap((word) => [...word.matchAll(/\{\{\s*([A-Za-z_$][\w$]*)[^}]*\}\}/g)].map((match) => match[1])))]
        .filter((name) => !given.has(name))
      if (lacking.length) unfilled.push(`${relative(webRoot, path)}:${lineOf(node)} ${namespace}:${key} gives no ${lacking.join(', ')}`)
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      const named = /^([A-Za-z]+):([A-Za-z0-9_][\w.-]*)$/.exec(node.text)
      if (named && catalogs.has(named[1]) && !has(named[1], named[2])) missing.push(`${relative(webRoot, path)}:${lineOf(node)} ${node.text}`)
    } else if (ts.isTemplateExpression(node)) {
      // `ns:` alone before the first value fits any key, and names ids as often (`database:${id}`).
      const named = /^([A-Za-z]+):(?=[A-Za-z])/.exec(node.head.text)
      // A call's own key may leave its namespace to the file's one hook.
      const callKey = (ts.isCallExpression(node.parent) && node.parent.arguments[0] === node
        && /(^|\.)t$/.test(node.parent.expression.getText(tree))) || ts.isJsxExpression(node.parent) && ts.isJsxAttribute(node.parent.parent)
        && node.parent.parent.name.getText(tree) === 'i18nKey'
      const namespace = named && catalogs.has(named[1]) ? named[1]
        : callKey && !node.head.text.includes(':') && /^[A-Za-z]/.test(node.head.text) ? hookNamespace : null
      if (namespace) {
        const pattern = new RegExp(`^${templateKeys(node).slice(named && catalogs.has(named[1]) ? named[0].length : 0)}$`)
        if (!keysOf(namespace).some((key) => pattern.test(key))) missing.push(`${relative(webRoot, path)}:${lineOf(node)} ${namespace}:${node.getText(tree)} (no key fits)`)
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(tree)
  for (const call of calls) {
    const separator = call.key.indexOf(':')
    const named = separator > 0 && catalogs.has(call.key.slice(0, separator))
    const namespace = named ? call.key.slice(0, separator) : call.namespace
    const key = named ? call.key.slice(separator + 1) : call.key
    if (!namespace || !key || /\s/.test(key)) continue
    if (!has(namespace, key)) {
      const line = source.slice(0, call.index).split('\n').length
      missing.push(`${relative(webRoot, path)}:${line} ${namespace}:${key}`)
    }
  }
}

// A language's value is never empty where English has words: an empty one
// falls back to English on that language's screen.
const localesRoot = join(sourceRoot, 'locales')
function emptyWhereEnglishSpeaks(english, translated, path, found) {
  for (const [key, value] of Object.entries(translated ?? {})) {
    const said = english?.[key]
    if (typeof value === 'string') {
      if (!value.trim() && typeof said === 'string' && said.trim()) found.push(`${path}${key}`)
    } else if (value && typeof value === 'object') {
      emptyWhereEnglishSpeaks(said && typeof said === 'object' ? said : null, value, `${path}${key}.`, found)
    }
  }
}
const VOID_ELEMENTS = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'keygen', 'link', 'meta', 'param', 'source', 'track', 'wbr'])
function voidTagsAroundWords(value, path, found) {
  if (typeof value === 'string') {
    for (const tag of value.matchAll(/<(\/?)([A-Za-z][\w-]*)\b[^>]*?(\/?)>/g)) {
      if (VOID_ELEMENTS.has(tag[2].toLowerCase()) && !(tag[3] && !tag[1])) found.push(`${path} ${tag[0]}`)
    }
  } else if (value && typeof value === 'object') {
    for (const [key, item] of Object.entries(value)) voidTagsAroundWords(item, `${path}${path.endsWith(':') ? '' : '.'}${key}`, found)
  }
}
const empty = []
const voidTags = []
for (const language of readdirSync(localesRoot)) {
  if (!statSync(join(localesRoot, language)).isDirectory()) continue
  for (const name of readdirSync(join(localesRoot, language))) {
    if (!name.endsWith('.json')) continue
    const translated = JSON.parse(readFileSync(join(localesRoot, language, name), 'utf8'))
    voidTagsAroundWords(translated, `${language}/${name.slice(0, -5)}:`, voidTags)
    if (language === 'en') continue
    emptyWhereEnglishSpeaks(catalogs.get(name.slice(0, -5)) ?? null, translated, `${language}/${name.slice(0, -5)}:`, empty)
  }
}

if (missing.length) {
  console.error(`Translation keys missing from the English catalog (${missing.length}):\n${missing.join('\n')}`)
  process.exit(1)
}
if (unfilled.length) {
  console.error(`Translation calls that leave a value their English words name unfilled, which shows as {{name}} on screen (${unfilled.length}):\n${unfilled.join('\n')}`)
  process.exit(1)
}
if (voidTags.length) {
  console.error(`Translation tags named after an empty HTML element, which react-i18next draws empty with the words beside it; rename them (${voidTags.length}):\n${voidTags.join('\n')}`)
  process.exit(1)
}
if (empty.length) {
  console.error(`Translations left empty where English has words (${empty.length}):\n${empty.join('\n')}`)
  process.exit(1)
}
console.log(`Translation keys verified against ${catalogs.size} English namespaces; no translation is left empty.`)

// The languages one can choose, as the language menu lists them.
const authWire = readFileSync(join(sourceRoot, 'authWire.generated.ts'), 'utf8')
const selectableList = /selectableAccountLanguagePreferences\b[^=]*=\s*\[([^\]]*)\]/.exec(authWire)
if (!selectableList) {
  console.error('src/authWire.generated.ts has no selectableAccountLanguagePreferences list to count translations for')
  process.exit(1)
}
const selectable = [...selectableList[1].matchAll(/"([^"]+)"/g)].map((match) => match[1]).filter((code) => code !== 'system' && code !== 'en')
const leafKeys = (node, prefix = '', out = []) => {
  for (const [name, value] of Object.entries(node ?? {})) {
    if (value && typeof value === 'object' && !Array.isArray(value)) leafKeys(value, `${prefix}${name}.`, out)
    else out.push(prefix + name)
  }
  return out
}
const pluralOther = (key) => {
  const plural = /_(zero|one|two|few|many|other)$/.exec(key)
  return plural ? `${key.slice(0, plural.index)}_other` : null
}
const englishLeaves = [...catalogs].map(([namespace, catalog]) => [namespace, leafKeys(catalog)])
const englishKeyCount = englishLeaves.reduce((sum, [, keys]) => sum + keys.length, 0)
const lacking = []
const whole = []
for (const language of selectable) {
  let files = 0
  let keys = 0
  for (const [namespace, englishKeys] of englishLeaves) {
    const path = join(localesRoot, language, `${namespace}.json`)
    if (!existsSync(path)) {
      files += 1
      keys += englishKeys.length
      continue
    }
    const own = new Set(leafKeys(JSON.parse(readFileSync(path, 'utf8'))))
    for (const key of englishKeys) if (!own.has(key) && !own.has(pluralOther(key))) keys += 1
  }
  if (keys) lacking.push(`  ${language.padEnd(8)}${String(files).padStart(3)} of ${catalogs.size} namespace files, ${String(keys).padStart(5)} of ${englishKeyCount} keys`)
  else whole.push(language)
}
if (lacking.length) console.log(`Selectable languages lacking English text, which shows in English there (${lacking.length} of ${selectable.length}):\n${lacking.join('\n')}`)
console.log(`Selectable languages with every English key: ${whole.length ? whole.join(', ') : 'none'}.`)
