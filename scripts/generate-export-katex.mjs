import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// The KaTeX an HTML export draws its equations with, the same KaTeX the web
// app draws them with (web/node_modules/katex), kept beside the server as it
// is (internal/app/exportkatex): its script, its style, its fonts' woff2
// files and its license. The server puts the fonts inside the style it
// writes (workspace_export_document_html_equations.go). An export with
// equations carries them, so the file draws its equations with no request
// outside the export, where Notion's loads KaTeX's style from a
// CDN. --check fails when the copy differs from the web app's KaTeX;
// --write makes it again.

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const from = path.join(root, 'web/node_modules/katex')
const to = path.join(root, 'internal/app/exportkatex')

const mode = process.argv[2]
if (mode !== '--check' && mode !== '--write') throw new Error('Usage: node scripts/generate-export-katex.mjs --check|--write')

const fonts = fs.readdirSync(path.join(from, 'dist/fonts')).filter((name) => name.endsWith('.woff2')).sort()
const sources = {
  'katex.min.js': 'dist/katex.min.js',
  'katex.min.css': 'dist/katex.min.css',
  LICENSE: 'LICENSE',
  ...Object.fromEntries(fonts.map((name) => [`fonts/${name}`, `dist/fonts/${name}`])),
}

function list(base) {
  if (!fs.existsSync(base)) return []
  return fs.readdirSync(base, { withFileTypes: true }).flatMap((entry) => entry.isDirectory()
    ? fs.readdirSync(path.join(base, entry.name)).map((name) => `${entry.name}/${name}`)
    : [entry.name])
}

const differ = Object.entries(sources).filter(([name, source]) => !fs.existsSync(path.join(to, name))
  || !fs.readFileSync(path.join(to, name)).equals(fs.readFileSync(path.join(from, source)))).map(([name]) => name)
const extra = list(to).filter((name) => !(name in sources))

if (mode === '--write') {
  for (const name of differ) {
    fs.mkdirSync(path.dirname(path.join(to, name)), { recursive: true })
    fs.copyFileSync(path.join(from, sources[name]), path.join(to, name))
  }
  for (const name of extra) fs.rmSync(path.join(to, name))
  console.log(`Export KaTeX written: ${differ.length} copied, ${extra.length} removed.`)
} else if (differ.length || extra.length) {
  console.error("The export's copy of KaTeX is stale:")
  for (const name of [...differ, ...extra]) console.error(`  ${name}`)
  console.error('Run: node scripts/generate-export-katex.mjs --write')
  process.exit(1)
} else {
  console.log(`Export KaTeX verified: ${Object.keys(sources).length} files.`)
}
