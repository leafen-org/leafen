#!/usr/bin/env node
// Builds the app SDK (sdk/app, Apache-2.0) from its TypeScript into
// sdk/app/dist — the module an app's screen loads and its types — and keeps
// a copy of the Go worker helper there for `leafen app init`, or, with
// --check, fails when the committed build differs.
//
//	node scripts/generate-app-sdk.mjs [--check]
import { readFileSync, writeFileSync, mkdirSync, existsSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const product = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sdk = join(product, 'sdk/app')
const ts = createRequire(join(product, 'web/package.json'))('typescript')
const check = process.argv.includes('--check')

const config = ts.getParsedCommandLineOfConfigFile(join(sdk, 'tsconfig.json'), {}, { ...ts.sys, onUnRecoverableConfigFileDiagnostic: (d) => { throw new Error(ts.flattenDiagnosticMessageText(d.messageText, '\n')) } })
const program = ts.createProgram(config.fileNames, config.options)
const problems = ts.getPreEmitDiagnostics(program)
if (problems.length) {
  console.error(ts.formatDiagnostics(problems, { getCanonicalFileName: (name) => name, getCurrentDirectory: () => sdk, getNewLine: () => '\n' }))
  process.exit(1)
}
const built = new Map()
program.emit(undefined, (name, text) => built.set(name, text.endsWith('\n') ? text : `${text}\n`))
// The Go helper's files and the license, for a new app's worker folder.
const helper = Object.fromEntries([
  ...readdirSync(join(sdk, 'go/leafen')).filter((name) => name.endsWith('.go')).sort().map((name) => [`leafen/${name}`, readFileSync(join(sdk, 'go/leafen', name), 'utf8')]),
  ['leafen/LICENSE', readFileSync(join(sdk, 'LICENSE'), 'utf8')],
])
built.set(join(sdk, 'dist/go-helper.json'), `${JSON.stringify(helper, null, 1)}\n`)
let stale = false
for (const [name, text] of built) {
  const label = relative(product, name)
  if (check) {
    if (!existsSync(name) || readFileSync(name, 'utf8') !== text) { console.error(`${label} is stale; run node scripts/generate-app-sdk.mjs`); stale = true }
    continue
  }
  mkdirSync(dirname(name), { recursive: true })
  writeFileSync(name, text)
}
if (stale) process.exit(1)
