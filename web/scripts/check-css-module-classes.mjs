#!/usr/bin/env node

// Every class a CSS module defines is put on an element somewhere. A class
// nothing reads is a rule that never applies, and it reads as if it did: on
// 2026-11-24 DatabaseRules.module.css still held two screens of rules under
// `.toolbarMenuInputScope`, a class no element carried, that said a toolbar
// menu hid its hover or focus ground by how it was being used; the menus
// never did, and the next reader trusted the sheet. So this check refuses a
// class no source reaches.
//
// A class is reached when
// - a source file reads it off a binding that holds its module: the module's
//   own import (`styles.row`, `styles["row"]`, `styles?.row`), a sheet that
//   merges modules (mergeStyleModules, databaseSheet.ts) or a module a file
//   exports as a value of its own (`export const phoneSheetCloseStyles =
//   styles`), followed through every file that imports it;
// - another class composes it (`composes: row` or `composes: row from
//   "./Other.module.css"`);
// - its module is handed on whole (`sheet={styles}`, `pick(styles)`) and the
//   name is read as a property or written as a string anywhere in the sources;
// - it is listed below: a name a source builds while it runs
//   (`styles[\`tone${color}\`]`), with where it is built.
// Classes written only inside :global(...) are not the module's.
//
// The other way too: a class read off a module's own import that the module
// doesn't define is `undefined`, and joined into a class list it is the word
// "undefined" on the element (2026-11-25: AppSelect's wrapper read
// "undefined  _choice_…" in the first run's language step after its
// .appSelect rule had gone, and a dozen other reads named classes no sheet
// held). Such reads are found with TypeScript's parser, so a comment or a
// string is no read, and a local name that hides the import is not it.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const src = join(webRoot, 'src')

/**
 * Names a source builds while it runs, by module (from web/src): each is put
 * on an element by a computed key, so no source writes it out.
 */
export const builtClassNames = {
  // AppSelect.tsx: styles[look], styles[`${look}Button`] for the "value" and "setting" looks.
  'components/AppSelect.module.css': ['value', 'setting', 'valueButton', 'settingButton'],
  // LegalNotice.tsx: styles[place].
  'components/LegalNotice.module.css': ['panel', 'settings'],
  // NotionMoveLive.tsx: styles[way].
  'components/import/NotionMoveLive.module.css': ['out', 'in', 'between'],
  // PhoneSheet.tsx: headFit[titleFit].
  'components/shell/PhoneSheetHeadFit.module.css': ['spills', 'shares'],
}

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path, out)
    else out.push(path)
  }
  return out
}

const files = walk(src)
const sheets = files.filter((path) => path.endsWith('.module.css'))
const sources = files.filter((path) => /\.(?:ts|tsx)$/u.test(path) && !path.endsWith('.d.ts'))

const stripComments = (text) => text.replace(/\/\*[\s\S]*?\*\//gu, '')

/** The text with each :global(...) taken out, parentheses balanced. */
function withoutGlobals(selector) {
  let out = ''
  let index = 0
  while (index < selector.length) {
    const at = selector.indexOf(':global(', index)
    if (at < 0) return out + selector.slice(index)
    out += selector.slice(index, at)
    let depth = 0
    let end = at + ':global'.length
    for (; end < selector.length; end += 1) {
      if (selector[end] === '(') depth += 1
      else if (selector[end] === ')' && --depth === 0) break
    }
    index = end + 1
  }
  return out
}

/** A module's classes, and the classes it composes, here or from another file. */
function readSheet(path) {
  const text = stripComments(readFileSync(path, 'utf8'))
  const defined = new Set()
  const composes = []
  let start = 0
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index]
    if (char === '{') {
      const prelude = text.slice(start, index).trim()
      if (!prelude.startsWith('@')) {
        // `:global .x` leaves the rest of its compound global as well.
        const local = withoutGlobals(prelude).replace(/:global\s+[^\s,]+/gu, '')
        for (const [, name] of local.matchAll(/\.([A-Za-z_][\w-]*)/gu)) defined.add(name)
      }
      start = index + 1
    } else if (char === '}' || char === ';') {
      if (char === ';') {
        const declaration = text.slice(start, index)
        const composed = /^\s*composes\s*:\s*([^;]*?)(?:\s+from\s+(["'])([^"']+)\2)?\s*$/u.exec(declaration)
        if (composed && composed[3] !== undefined) {
          composes.push([resolve(dirname(path), composed[3]), composed[1].trim().split(/\s+/u)])
        } else if (composed && !/\bfrom\s+global\b/u.test(composed[1])) {
          composes.push([path, composed[1].trim().split(/\s+/u)])
        }
      }
      start = index + 1
    }
  }
  return { defined, composes }
}

const sheetInfo = new Map(sheets.map((path) => [path, readSheet(path)]))
const reached = new Map(sheets.map((path) => [path, new Set()]))
const reach = (path, name) => reached.get(path)?.add(name)
for (const { composes } of sheetInfo.values()) {
  for (const [path, names] of composes) for (const name of names) reach(path, name)
}

function resolveSpec(from, spec) {
  if (spec.startsWith('@/')) return join(src, spec.slice(2))
  if (spec.startsWith('.')) return resolve(dirname(from), spec)
  return null
}

function sourceFile(base) {
  for (const candidate of [base, `${base}.ts`, `${base}.tsx`, join(base, 'index.ts'), join(base, 'index.tsx')]) {
    if (sources.includes(candidate)) return candidate
  }
  return null
}

const importPattern = /import\s+(?:type\s+)?([\s\S]*?)\s+from\s+["']([^"']+)["']/gu
const sourceText = new Map(sources.map((path) => [path, readFileSync(path, 'utf8')]))

/** Each file's imports: a CSS module's binding, or the names it takes from another source. */
function importsOf(path) {
  const cssBindings = new Map()
  const fromSources = []
  for (const [, clause, spec] of sourceText.get(path).matchAll(importPattern)) {
    const target = resolveSpec(path, spec)
    if (!target) continue
    if (target.endsWith('.module.css')) {
      const binding = /^(?:\*\s+as\s+)?(\w+)$/u.exec(clause.trim())
      if (binding) cssBindings.set(binding[1], new Set([target]))
      continue
    }
    const file = sourceFile(target)
    if (!file) continue
    const names = []
    const defaultName = /^(\w+)/u.exec(clause.trim())
    if (defaultName && !clause.trim().startsWith('{')) names.push(defaultName[1])
    const named = /\{([\s\S]*)\}/u.exec(clause)
    if (named) {
      for (const part of named[1].split(',')) {
        const alias = /(?:\w+\s+as\s+)?(\w+)\s*$/u.exec(part.replace(/^\s*type\s+/u, '').trim())
        if (alias) names.push(alias[1])
      }
    }
    fromSources.push([file, names])
  }
  return { cssBindings, fromSources }
}

const imports = new Map(sources.map((path) => [path, importsOf(path)]))

// The style objects each source binds: its CSS imports, a local merge or alias of them, and what it imports from a source
// that hands modules on; and the modules each source hands on to its importers, found again until nothing changes.
const handsOn = new Map()
const bindingsOf = new Map()
const escapes = (text, name) => new RegExp(`(?<![\\w.$])${name}\\b(?!\\s*(?:\\?\\.|\\.|\\[))`, 'u').test(
  text.replace(new RegExp(`import\\s+(?:\\*\\s+as\\s+)?${name}\\s+from[^;\\n]*`, 'gu'), ''))
for (let changed = true; changed;) {
  changed = false
  for (const path of sources) {
    const text = sourceText.get(path)
    const { cssBindings, fromSources } = imports.get(path)
    const bindings = new Map([...cssBindings].map(([name, set]) => [name, new Set(set)]))
    for (const [file, names] of fromSources) {
      const modules = handsOn.get(file)
      if (!modules?.size) continue
      for (const name of names) bindings.set(name, new Set(modules))
    }
    // A merge: mergeStyleModules(a, b) or { ...a, ...b }, its type written or not.
    const merges = /(?:const|let)\s+(\w+)\s*(?::[^=;]+)?=\s*(?:mergeStyleModules\(([^)]*)\)|\{((?:\s*\.\.\.\s*\w+\s*,?)+)\s*\})/gu
    for (const [, name, called, spread] of text.matchAll(merges)) {
      const merged = new Set()
      for (const arg of (called ?? spread).split(',')) for (const module of bindings.get(arg.replace('...', '').trim()) ?? []) merged.add(module)
      if (merged.size) bindings.set(name, merged)
    }
    // An alias: a name given one of them whole, chosen or not (`const panel = phone ? panelStyles : null`).
    for (let pass = 0; pass < 2; pass += 1) {
      for (const [, name, value] of text.matchAll(/(?:const|let)\s+(\w+)\s*(?::[^=;]+)?=\s*([^;]+);/gu)) {
        const whole = [...value.matchAll(/(?<![\w$.])([A-Za-z_$][\w$]*)\b(?!\s*(?:\?\.|\.|\[|\())/gu)]
          .flatMap(([, other]) => [...(bindings.get(other) ?? [])])
        if (whole.length) bindings.set(name, new Set([...(bindings.get(name) ?? []), ...whole]))
      }
    }
    bindingsOf.set(path, bindings)
    const exported = new Set()
    for (const [name, modules] of bindings) {
      const out = new RegExp(`export\\s+(?:default\\s+${name}\\b|const\\s+${name}\\b|const\\s+\\w+\\s*=\\s*${name}\\s*;|\\{[^}]*\\b${name}\\b[^}]*\\})`, 'u')
      if (out.test(text)) for (const module of modules) exported.add(module)
    }
    const before = handsOn.get(path)?.size ?? 0
    if (exported.size !== before) {
      handsOn.set(path, exported)
      changed = true
    }
  }
}

// The editor's sheets ship as one generated module (generate-editor-styles.mjs): a class of one is reached as the
// generated module's, and is told against the sheet it is written in.
const editorDir = join(src, 'components', 'editor')
const generatedEditorSheet = join(editorDir, 'editorStyles.generated.module.css')
const editorSheetSources = [...readFileSync(join(webRoot, 'scripts', 'generate-editor-styles.mjs'), 'utf8')
  .matchAll(/^\s*'(Editor\w+\.module\.css)',$/gmu)].map((match) => join(editorDir, match[1]))

const handedOnWhole = new Set()
const dynamicReads = []
for (const path of sources) {
  const text = sourceText.get(path)
  for (const [name, modules] of bindingsOf.get(path)) {
    const read = new RegExp(`(?<![\\w$.])${name}\\s*(?:\\?\\.|\\.)\\s*([A-Za-z_$][\\w$]*)|(?<![\\w$.])${name}\\s*(?:\\?\\.)?\\[\\s*(?:"([^"]+)"|'([^']+)'|\`([^\`$]+)\`)\\s*\\]`, 'gu')
    for (const match of text.matchAll(read)) for (const module of modules) reach(module, match[1] ?? match[2] ?? match[3] ?? match[4])
    const computed = new RegExp(`(?<![\\w$.])${name}\\s*(?:\\?\\.)?\\[(?!\\s*(?:"[^"]+"|'[^']+'|\`[^\`$]+\`)\\s*\\])`, 'gu')
    for (const match of text.matchAll(computed)) {
      dynamicReads.push(`${relative(webRoot, path)}:${text.slice(0, match.index).split('\n').length}`)
    }
    if (escapes(text, name)) for (const module of modules) handedOnWhole.add(module)
  }
}

// A module handed on whole is read by whatever takes it: its names written as a property or a string anywhere count.
const allText = [...sourceText.values()].join('\n')
const mentioned = new Set([
  ...[...allText.matchAll(/\.\s*([A-Za-z_$][\w$]*)/gu)].map((match) => match[1]),
  ...[...allText.matchAll(/["'`]([A-Za-z_][\w-]*)["'`]/gu)].map((match) => match[1]),
])

for (const path of editorSheetSources) {
  for (const name of reached.get(generatedEditorSheet) ?? []) reach(path, name)
  if (handedOnWhole.has(generatedEditorSheet)) handedOnWhole.add(path)
}

const failures = []
const listed = (table, key, name) => (table[key] ?? []).includes(name)
for (const [path, { defined }] of sheetInfo) {
  if (path === generatedEditorSheet) continue
  const key = relative(src, path)
  for (const name of defined) {
    const read = reached.get(path).has(name) || (handedOnWhole.has(path) && mentioned.has(name))
    if (listed(builtClassNames, key, name)) {
      if (read) failures.push(`${key}: .${name} is listed as built while it runs but is also read by name; take it off the list`)
      continue
    }
    if (!read) failures.push(`${key}: .${name} is put on no element`)
  }
}
// The names a node declares for what it holds: a function's parameters, and what a block, a case or the file
// declares directly (variables, functions, classes), destructured names included.
function declaredIn(node) {
  const names = new Set()
  const bind = (name) => {
    if (ts.isIdentifier(name)) names.add(name.text)
    else if (ts.isObjectBindingPattern(name) || ts.isArrayBindingPattern(name)) {
      for (const element of name.elements) if (!ts.isOmittedExpression(element)) bind(element.name)
    }
  }
  if (ts.isFunctionLike(node)) for (const parameter of node.parameters ?? []) bind(parameter.name)
  const statements = ts.isBlock(node) || ts.isModuleBlock(node) || ts.isCaseClause(node) || ts.isDefaultClause(node) ? node.statements : []
  for (const statement of statements) {
    if (ts.isVariableStatement(statement)) for (const declaration of statement.declarationList.declarations) bind(declaration.name)
    else if ((ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement)) && statement.name) names.add(statement.name.text)
  }
  if ((ts.isForStatement(node) || ts.isForOfStatement(node) || ts.isForInStatement(node)) && node.initializer
    && ts.isVariableDeclarationList(node.initializer)) for (const declaration of node.initializer.declarations) bind(declaration.name)
  if (ts.isCatchClause(node) && node.variableDeclaration) bind(node.variableDeclaration.name)
  return names
}

for (const path of sources) {
  const own = imports.get(path).cssBindings
  if (!own.size) continue
  const file = ts.createSourceFile(path, sourceText.get(path), ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const visit = (node, hidden) => {
    const read = ts.isPropertyAccessExpression(node) && ts.isIdentifier(node.expression) ? [node.expression.text, node.name.text]
      : ts.isElementAccessExpression(node) && ts.isIdentifier(node.expression) && ts.isStringLiteralLike(node.argumentExpression)
        ? [node.expression.text, node.argumentExpression.text] : null
    if (read && own.has(read[0]) && !hidden.has(read[0])) {
      const [module] = own.get(read[0])
      if (sheetInfo.has(module) && !sheetInfo.get(module).defined.has(read[1])) {
        const line = file.getLineAndCharacterOfPosition(node.getStart()).line + 1
        failures.push(`${relative(webRoot, path)}:${line}: ${read[0]}.${read[1]} — ${relative(src, module)} defines no .${read[1]}`)
      }
    }
    const declared = ts.isSourceFile(node) ? new Set() : declaredIn(node)
    const inner = declared.size ? new Set([...hidden, ...declared]) : hidden
    ts.forEachChild(node, (child) => visit(child, inner))
  }
  visit(file, new Set())
}

for (const [key, names] of Object.entries(builtClassNames)) {
  const info = sheetInfo.get(join(src, key))
  for (const name of names) if (!info?.defined.has(name)) failures.push(`builtClassNames: ${key} .${name} is no longer defined; take it off the list`)
}

if (failures.length) {
  process.stderr.write(`CSS module classes nothing puts on an element (remove the rule, or list a name built while it runs in web/scripts/check-css-module-classes.mjs), or read where no rule defines them (define it, or read none):\n${failures.join('\n')}\n`)
  if (dynamicReads.length) process.stderr.write(`Computed reads that may build them:\n${dynamicReads.join('\n')}\n`)
  process.exit(1)
}
