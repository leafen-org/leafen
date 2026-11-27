#!/usr/bin/env node

// Modules that import one another load in an order the entry decides: when
// a module of such a cycle reads, as it loads, a value another module of the
// cycle exports, that value is not there yet whenever the other module
// comes later (2026-11-10: "Cannot access 'MAX_RECIPIENTS' before
// initialization" in a harness that loaded the automation models the other
// way round). Reading one inside a function, which runs once both have
// loaded, is safe.
//
// This check finds the cycles of the sources' static imports (type-only
// imports aside) and refuses a module of a cycle that uses, outside any
// function, a name it imports from another module of the same cycle. Such a
// shared value belongs in a module of its own that imports none of them.

import { readdirSync, readFileSync, statSync, existsSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')

function sourceFiles(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) files.push(...sourceFiles(path))
    else if (/\.(ts|tsx)$/u.test(name) && !name.endsWith('.d.ts')) files.push(path)
  }
  return files
}

function resolveImport(from, specifier) {
  let base
  if (specifier.startsWith('@/')) base = join(sourceRoot, specifier.slice(2))
  else if (specifier.startsWith('.')) base = resolve(dirname(from), specifier)
  else return null
  for (const candidate of [base, `${base}.ts`, `${base}.tsx`, join(base, 'index.ts'), join(base, 'index.tsx')]) {
    if (existsSync(candidate) && statSync(candidate).isFile() && /\.(ts|tsx)$/u.test(candidate)) return candidate
  }
  return null
}

// A module's value imports: the modules it loads, and the local names it
// binds to each.
function readModule(path) {
  const text = readFileSync(path, 'utf8')
  const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const imports = new Map()
  const add = (target, names) => {
    if (!target) return
    const known = imports.get(target) ?? new Set()
    for (const name of names) known.add(name)
    imports.set(target, known)
  }
  for (const statement of source.statements) {
    if (ts.isImportDeclaration(statement) && ts.isStringLiteral(statement.moduleSpecifier)) {
      const clause = statement.importClause
      if (clause?.isTypeOnly) continue
      const target = resolveImport(path, statement.moduleSpecifier.text)
      const names = []
      if (clause?.name) names.push(clause.name.text)
      const bindings = clause?.namedBindings
      if (bindings && ts.isNamespaceImport(bindings)) names.push(bindings.name.text)
      if (bindings && ts.isNamedImports(bindings)) {
        for (const element of bindings.elements) if (!element.isTypeOnly) names.push(element.name.text)
      }
      if (!clause || names.length) add(target, names)
    } else if (ts.isExportDeclaration(statement) && statement.moduleSpecifier && ts.isStringLiteral(statement.moduleSpecifier)) {
      if (statement.isTypeOnly) continue
      add(resolveImport(path, statement.moduleSpecifier.text), [])
    }
  }
  return { source, imports }
}

// The names of `names` a module uses while it loads: outside any function,
// method or accessor, and not as a type.
function namesReadAtLoad(source, names) {
  const read = new Map()
  const visit = (node, inFunction) => {
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) return
    if (ts.isTypeNode(node) || ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) return
    const opensFunction = ts.isFunctionDeclaration(node) || ts.isFunctionExpression(node) || ts.isArrowFunction(node) ||
      ts.isMethodDeclaration(node) || ts.isGetAccessorDeclaration(node) || ts.isSetAccessorDeclaration(node) ||
      ts.isConstructorDeclaration(node)
    if (!inFunction && ts.isIdentifier(node) && names.has(node.text)) {
      const parent = node.parent
      const named = parent && ((ts.isPropertyAccessExpression(parent) && parent.name === node) ||
        (ts.isPropertyAssignment(parent) && parent.name === node) || (ts.isVariableDeclaration(parent) && parent.name === node))
      if (!named && !read.has(node.text)) {
        read.set(node.text, source.getLineAndCharacterOfPosition(node.getStart()).line + 1)
      }
    }
    ts.forEachChild(node, (child) => visit(child, inFunction || opensFunction))
  }
  visit(source, false)
  return read
}

// Tarjan's strongly connected components of the import graph.
function cycles(graph) {
  let index = 0
  const indexOf = new Map(), lowOf = new Map(), stack = [], onStack = new Set(), found = []
  const connect = (node) => {
    indexOf.set(node, index); lowOf.set(node, index); index++
    stack.push(node); onStack.add(node)
    for (const next of graph.get(node)?.imports.keys() ?? []) {
      if (!graph.has(next)) continue
      if (!indexOf.has(next)) { connect(next); lowOf.set(node, Math.min(lowOf.get(node), lowOf.get(next))) }
      else if (onStack.has(next)) lowOf.set(node, Math.min(lowOf.get(node), indexOf.get(next)))
    }
    if (lowOf.get(node) === indexOf.get(node)) {
      const component = []
      let member
      do { member = stack.pop(); onStack.delete(member); component.push(member) } while (member !== node)
      if (component.length > 1) found.push(component)
    }
  }
  for (const node of graph.keys()) if (!indexOf.has(node)) connect(node)
  return found
}

const graph = new Map(sourceFiles(sourceRoot).map((path) => [path, readModule(path)]))
const problems = []
for (const component of cycles(graph)) {
  const members = new Set(component)
  for (const path of component) {
    const { source, imports } = graph.get(path)
    const names = new Map()
    for (const [target, bound] of imports) {
      if (members.has(target)) for (const name of bound) names.set(name, target)
    }
    if (!names.size) continue
    for (const [name, line] of namesReadAtLoad(source, new Set(names.keys()))) {
      problems.push(`${relative(webRoot, path)}:${line} reads ${name} from ${relative(webRoot, names.get(name))} as it loads, and the two import each other (through ${component.length} modules)`)
    }
  }
}
if (problems.length) {
  console.error('Modules of an import cycle read one another\'s values as they load:\n' + problems.map((line) => `  ${line}`).join('\n'))
  process.exit(1)
}
