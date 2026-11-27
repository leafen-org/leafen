#!/usr/bin/env node

// An event's currentTarget is the element whose listener runs, and only
// while it runs: once dispatch moves on — React's events included — it is
// null. A function made in a handler may run later (a state updater React
// calls again as it renders, in StrictMode twice; a timer; a promise), and
// one that read the handler's event.currentTarget then found null: a library
// column's menu threw "Cannot read properties of null (reading
// 'isConnected')" from its updater and never opened (2026-11-21, gate lap 6).
//
// So a handler reads its event's currentTarget itself, into a name the later
// function uses, and this check refuses a read of a parameter's
// `.currentTarget` inside a function nested in the one that has the
// parameter. A function that receives the event as its own parameter reads
// it as before.

import { readdirSync, readFileSync, statSync } from 'node:fs'
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

const isFunction = (node) => ts.isArrowFunction(node) || ts.isFunctionExpression(node) || ts.isFunctionDeclaration(node)
  || ts.isMethodDeclaration(node) || ts.isGetAccessorDeclaration(node) || ts.isSetAccessorDeclaration(node) || ts.isConstructorDeclaration(node)
const declares = (fn, name) => fn.parameters.some((parameter) => ts.isIdentifier(parameter.name) && parameter.name.text === name)

const failures = []
for (const path of sourceFiles(sourceRoot)) {
  const text = readFileSync(path, 'utf8')
  if (!text.includes('currentTarget')) continue
  const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const visit = (node) => {
    if (ts.isPropertyAccessExpression(node) && node.name.text === 'currentTarget' && ts.isIdentifier(node.expression)) {
      const name = node.expression.text
      let nested = false
      for (let parent = node.parent; parent; parent = parent.parent) {
        if (!isFunction(parent)) continue
        if (declares(parent, name)) {
          if (nested) {
            const { line } = source.getLineAndCharacterOfPosition(node.getStart(source))
            failures.push(`${relative(webRoot, path)}:${line + 1}: ${name}.currentTarget is read in a function made inside the one that has ${name}; read it there, before`)
          }
          break
        }
        nested = true
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
}

if (failures.length) {
  process.stderr.write(`${failures.join('\n')}\n\nAn event's currentTarget is null once its listener has run: read it in the handler and hand the element on.\n`)
  process.exit(1)
}
