// Checks that the core never depends on the organization module: the
// Go core package does not import it, the core-only build compiles, and no
// web file outside the module imports from it. And that each program's entry
// attaches it, since the web it serves carries the module's screens
// (2026-10-30: the server alone left it out and its screens met no routes).
import { execFileSync } from 'node:child_process'
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const modulePackage = 'leafen.org/leafen/organization'
const go = (...args) => execFileSync('go', args, { cwd: productRoot, encoding: 'utf8' })
const problems = []

if (go('list', '-deps', './internal/app').split('\n').some((line) => line.startsWith(modulePackage))) {
  problems.push('internal/app depends on the organization module')
}
for (const entry of ['./cmd/leafen', './desktop']) {
  if (!go('list', '-deps', entry).split('\n').some((line) => line.startsWith(modulePackage))) {
    problems.push(`${entry} does not attach the organization module`)
  }
}
// The core-only desktop app is built as Windows builds it on every system, so
// this check answers alike on the Linux CI, where there is no desktop app and
// building one would need GTK, and on a maintainer's Mac before a push. A Mac
// builds it its own way too, as its app is built.
const coreBuilds = [{ name: 'as Windows builds it', env: { GOOS: 'windows', GOARCH: 'amd64', CGO_ENABLED: '0' } }]
if (process.platform === 'darwin') coreBuilds.push({ name: 'as macOS builds it', env: {} })
for (const { name, env } of coreBuilds) {
  try {
    execFileSync('go', ['build', '-tags', 'leafen_core', '-o', process.platform === 'win32' ? 'NUL' : '/dev/null', './desktop'],
      { cwd: productRoot, encoding: 'utf8', env: { ...process.env, ...env } })
  } catch (error) {
    problems.push(`the core-only build ${name} fails: ${error.message}`)
  }
}

const webSource = join(productRoot, 'web', 'src')
const moduleImport = /(?:from\s+|import\s*\(\s*|import\s+)["'](?:@\/organization\/|(?:\.\.?\/)+organization\/)/u
const walk = (directory) => readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
  const path = join(directory, entry.name)
  if (entry.isDirectory()) return entry.name === 'organization' && directory === webSource ? [] : walk(path)
  return /\.(?:ts|tsx)$/u.test(entry.name) ? [path] : []
})
for (const path of walk(webSource)) {
  if (moduleImport.test(readFileSync(path, 'utf8'))) problems.push(`${relative(productRoot, path)} imports the organization module`)
}

if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log('organization boundary ok')
