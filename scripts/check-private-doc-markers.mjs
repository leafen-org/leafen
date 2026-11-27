// Leafen's public tree holds no marker that points at its maintainers'
// private notes, which this repository doesn't carry: their issue IDs and
// folders, their decision numbers, their screen-comparison notes, the old
// checkout's folder, roadmap stages and the names of their audit rounds and
// probes. A comment gives a decision's reason in words and keeps the number
// out (2026-11-27: three weeks after the tree was swept, about 150 files
// carried such markers again). Words about how the work is done — a worker,
// the main loop, a judgment principle — are no marker.
//
// It reads every tracked file (git ls-files; on a copy without git, such as
// the Docker build's, every file but node_modules and build output), except
// other projects' code kept here (third_party and vendor folders). A notation
// in the allowed list — the private address range — is a fact, not a marker.
//
//   node scripts/check-private-doc-markers.mjs [--markers <file>]
//
// --markers adds a list of its own, one regular expression a line (# starts a
// comment), kept outside this repository: a maintainer's release run adds the
// names that must not be published. A line it finds is named without its
// text.
//
// The patterns are written so that this file doesn't match them.
import { execFileSync } from 'node:child_process'
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

const markers = [
  { what: 'an issue ID of the private notes', pattern: /ISSUE-2\d/u },
  { what: "the private notes' issue folder", pattern: /docs\/issues/u },
  { what: 'the old checkout folder', pattern: /hanji-v[2]/u },
  { what: 'the screen-comparison notes', pattern: /ui-baselin[e]|UI baselin[e]/u },
  { what: 'a decision number of the private notes', pattern: /DIV-\d{3}/u },
  // The letters op, a dash or none and two digits, in either case: an audit
  // round, one of its parts or the name of a probe made in one.
  { what: 'an audit round or probe name', pattern: /(?<![\p{L}\p{N}])op-?\d{2}(?!\d)/iu },
  { what: 'a roadmap stage', pattern: /Stage \d/u },
  { what: 'an address on a home network', pattern: /192\.168\.0/u },
]
const allowed = ['192.168.0.0/16']
const skippedFolders = new Set(['third_party', 'vendor'])
// Formats that hold no text are not read (about a third of the tree's bytes);
// any other file with a NUL byte near its start is taken as binary too.
const binaryFormats = /\.(?:woff2?|ttf|otf|wasm|png|jpe?g|gif|webp|ico|icns|pdf|zip|gz|jar|mp3|mp4|webm)$/iu
const walkSkipped = new Set([...skippedFolders, '.git', 'node_modules', 'dist'])

const markersAt = process.argv.indexOf('--markers')
if (markersAt !== -1) {
  const list = process.argv[markersAt + 1]
  if (!list) {
    console.error('--markers needs a file')
    process.exit(2)
  }
  readFileSync(list, 'utf8').split('\n').forEach((line, index) => {
    const source = line.trim()
    if (source && !source.startsWith('#')) {
      markers.push({ what: `a name on line ${index + 1} of ${list}`, pattern: new RegExp(source, 'iu'), quiet: true })
    }
  })
}

function trackedFiles() {
  try {
    const listed = execFileSync('git', ['ls-files', '-z'], { cwd: productRoot, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'], maxBuffer: 64 << 20 })
    return listed.split('\0').filter(Boolean)
  } catch {
    const files = []
    const walk = (directory) => {
      for (const entry of readdirSync(directory, { withFileTypes: true })) {
        const path = join(directory, entry.name)
        if (entry.isDirectory()) {
          if (!walkSkipped.has(entry.name)) walk(path)
        } else if (entry.isFile()) {
          files.push(relative(productRoot, path).split(sep).join('/'))
        }
      }
    }
    walk(productRoot)
    return files
  }
}

const any = new RegExp(markers.map(({ pattern }) => `(?:${pattern.source})`).join('|'), 'iu')
const problems = []
for (const file of trackedFiles()) {
  if (binaryFormats.test(file) || file.split('/').some((part) => skippedFolders.has(part))) continue
  let bytes
  try {
    bytes = readFileSync(join(productRoot, file))
  } catch {
    continue
  }
  // A file with a NUL byte near its start is binary, as git and grep take it.
  if (bytes.subarray(0, 8000).includes(0)) continue
  let text = bytes.toString('utf8')
  for (const notation of allowed) text = text.replaceAll(notation, ' '.repeat(notation.length))
  if (!any.test(text)) continue
  text.split('\n').forEach((line, index) => {
    for (const { what, pattern, quiet } of markers) {
      const found = pattern.exec(line)
      if (found) problems.push(`${file}:${index + 1}: ${what}${quiet ? '' : ` (${found[0]})`}`)
    }
  })
}

if (problems.length) {
  console.error("These lines point at the maintainers' private notes, which this repository doesn't carry.")
  console.error('Give the reason in words and leave the number or name out:')
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log('no private note markers')
