import { execFileSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

// What the server says of its own build (internal/app/server_build.go): the
// commit it is built from, that commit's time, when it was built and whether
// the source had changes beside the commit, and the commits before it, so a
// server can say what changed since the build it ran before. Run it right
// before every build of the server: the files it writes are ignored by Git
// and embedded by the build, and a stamp left from an earlier build would
// name that build's commit.
//
//   node scripts/stamp-build.mjs                      this checkout's HEAD
//   node scripts/stamp-build.mjs --commit <rev> --out <folder>/internal/app/build_stamp
//       a commit put out with git archive into <folder>, to build there
//
// A source put out with git archive carries its commit and the commit's time
// without this (build_stamp/archive.txt, export-subst), but no change list.
// Only the commits' subjects go in: the product's history is public.

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const argument = (name) => {
  const at = process.argv.indexOf(name)
  return at > 0 ? process.argv[at + 1] : undefined
}
const repository = resolve(argument('--repo') ?? productRoot)
const commit = argument('--commit') ?? 'HEAD'
const out = resolve(argument('--out') ?? join(productRoot, 'internal', 'app', 'build_stamp'))
// Enough commits to reach a build that ran days before, at a busy project's pace.
const kept = Number(argument('--commits') ?? 8000)

const git = (...args) => execFileSync('git', ['-C', repository, ...args], { encoding: 'utf8', maxBuffer: 256 << 20 })
const [hash, time] = git('show', '-s', '--format=%H %ct', commit).trim().split(' ')
// Changes beside the commit count only when the build is of the checkout itself.
const modified = !argument('--commit') && git('status', '--porcelain', '--untracked-files=no').trim() !== ''
const log = git('log', '--topo-order', `--max-count=${kept}`, '--format=%h%x09%p%x09%ct%x09%s', '--abbrev=12', hash)
// One commit a line: its hash, its parents (commas between, "-" for none),
// its time and its subject.
const lines = log.split('\n').filter(Boolean).map((line) => {
  const [short, parents, at, ...subject] = line.split('\t')
  return [short, parents.trim().split(' ').filter(Boolean).join(',') || '-', at, subject.join(' ')].join(' ')
})
mkdirSync(out, { recursive: true })
writeFileSync(join(out, 'stamp.json'), `${JSON.stringify({
  commit: hash, commitTime: Number(time), builtAt: Math.floor(Date.now() / 1000), modified,
})}\n`)
writeFileSync(join(out, 'changes.txt.gz'), gzipSync(`${lines.join('\n')}\n`, { level: 9 }))
console.log(`stamped ${hash.slice(0, 12)}${modified ? ' (modified)' : ''} with ${lines.length} commits in ${out}`)
