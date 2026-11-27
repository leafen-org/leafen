#!/usr/bin/env node
// Builds the built-in apps' package files from their sources: each Go
// worker in builtin-apps/<app>/worker to internal/app/builtinapps/<app>/workers/main.wasm,
// the app SDK's screen module into each app's ui/, and the shared screen
// modules (builtin-apps/shared-ui) an app's shared-ui.json names. With --check it
// builds them aside and fails when the committed files differ, or when Git
// ignores a file the binary embeds from internal/app/builtinapps (a clone
// would build the app without it). A wasm
// build is the same only from the same Go release (`go version`); it leaves
// out the Git revision, which would change it with every commit.
//
//	node scripts/build-builtin-apps.mjs [--check]
import { execFileSync } from 'node:child_process'
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const product = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sources = join(product, 'builtin-apps')
const packages = join(product, 'internal/app/builtinapps')
const sdk = join(product, 'sdk/app/dist/index.js')
const sharedUI = join(sources, 'shared-ui')
const check = process.argv.includes('--check')
const aside = check ? mkdtempSync(join(tmpdir(), 'leafen-builtin-apps-')) : ''
let stale = false

function place(name) {
  return check ? join(aside, relative(product, name)) : name
}

function compare(built, committed) {
  if (!check) return
  if (!existsSync(committed) || !readFileSync(built).equals(readFileSync(committed))) {
    console.error(`${relative(product, committed)} is stale; run node scripts/build-builtin-apps.mjs`)
    stale = true
  }
}

try {
  for (const app of readdirSync(sources, { withFileTypes: true }).filter((entry) => entry.isDirectory() && entry.name !== 'shared-ui').map((entry) => entry.name).sort()) {
    const target = join(packages, app)
    if (!existsSync(join(target, 'leafen-app.json'))) {
      console.error(`builtin-apps/${app} has no package folder at ${relative(product, target)}`)
      process.exit(1)
    }
    const worker = join(sources, app, 'worker')
    if (existsSync(join(worker, 'go.mod'))) {
      const wasm = join(target, 'workers/main.wasm')
      mkdirSync(dirname(place(wasm)), { recursive: true })
      execFileSync('go', ['build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w', '-buildmode=c-shared', '-o', place(wasm), '.'], {
        cwd: worker, stdio: 'inherit', env: { ...process.env, GOOS: 'wasip1', GOARCH: 'wasm', GOWORK: 'off' },
      })
      compare(place(wasm), wasm)
    }
    if (existsSync(join(target, 'ui'))) {
      const module = join(target, 'ui/leafen-app-sdk.js')
      mkdirSync(dirname(place(module)), { recursive: true })
      copyFileSync(sdk, place(module))
      compare(place(module), module)
      const shared = join(sources, app, 'shared-ui.json')
      for (const name of existsSync(shared) ? JSON.parse(readFileSync(shared, 'utf8')) : []) {
        const copy = join(target, 'ui', name)
        copyFileSync(join(sharedUI, name), place(copy))
        compare(place(copy), copy)
      }
    }
  }
  if (check) {
    // Every package file is the product's: none may be left out of Git.
    const files = readdirSync(packages, { recursive: true, withFileTypes: true }).filter((entry) => entry.isFile())
      .map((entry) => relative(product, join(entry.parentPath, entry.name)))
    let ignored = ''
    try {
      ignored = execFileSync('git', ['check-ignore', '--stdin'], { cwd: product, input: files.join('\n'), encoding: 'utf8' })
    } catch (error) {
      // git check-ignore exits 1 when it ignores none of them.
      if (error.status !== 1) throw error
    }
    for (const name of ignored.split('\n').filter(Boolean)) {
      console.error(`${name} is ignored by Git; the built-in app needs it committed`)
      stale = true
    }
  }
} finally {
  if (aside) rmSync(aside, { recursive: true, force: true })
}
if (stale) process.exit(1)
