#!/usr/bin/env node
// Builds the Office Documents app (catalog-apps/office) as a package: CryptPad's
// serverless builds of ONLYOFFICE's editor and its x2t converter (AGPL-3.0) as
// published, trimmed to the Word, Excel and PowerPoint editors in English and
// Korean (no help, other languages, source maps, PDF or Visio editors, macro
// editor), with this app's bridge as the editor pages' first script and a
// font list without Monotype's Arial or Microsoft's Calibri — their names
// open the metric-compatible Arimo and Carlito — and with Noto Sans KR and
// Noto Serif KR (SIL OFL 1.1) for Korean, given TrueType outlines. Needs
// curl, unzip, Go, Python with fontTools and the network on the first run;
// downloads stay in the work folder.
//
//	node catalog-apps/office/build-onlyoffice.mjs <work folder> [<package.leafenapp>]
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { decodeSelection, encodeSelection, readFont } from './fontlist.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const product = resolve(here, '../..')
if (!process.argv[2]) throw new Error('Name a work folder.')
const work = resolve(process.argv[2])
const out = resolve(process.argv[3] ?? join(work, 'office.leafenapp'))
const release = '9.3.2'
const downloads = [
  { name: 'onlyoffice-editor.zip', url: 'https://github.com/cryptpad/onlyoffice-editor/releases/download/v9.3.2%2B2/onlyoffice-editor.zip',
    sha512: '7a8b4d32b000454ac304088ba042cb6fc2007e90282b7b51bf85365e4405a33b64f7f29f5aa88628fb4b94fa1e68077c56897116b2098c8b56ec34dcdcc356df' },
  { name: 'x2t.zip', url: 'https://github.com/cryptpad/onlyoffice-x2t-wasm/releases/download/v9.3.2%2B2/x2t.zip',
    sha512: '9e5eda173958b0bb6cc110d9356724e8ac11737949d20d49f25f879f9217ffe8d65828cc50122380d1c9633998ba87d5858c86cc23d47fe90c3a3ba43ef82af8' },
  { name: 'NotoSansKR-Regular.otf', url: 'https://raw.githubusercontent.com/notofonts/noto-cjk/Sans2.004/Sans/SubsetOTF/KR/NotoSansKR-Regular.otf',
    sha256: '69975a0ac8472717870aefeab0a4d52739308d90856b9955313b2ad5e0148d68' },
  { name: 'NotoSansKR-Bold.otf', url: 'https://raw.githubusercontent.com/notofonts/noto-cjk/Sans2.004/Sans/SubsetOTF/KR/NotoSansKR-Bold.otf',
    sha256: '5a6ceb287ed2fc6cfc6213144ebea68cbd94b20fc9eb873d8486493bf02d9bda' },
  { name: 'NotoSerifKR-Regular.otf', url: 'https://raw.githubusercontent.com/notofonts/noto-cjk/Serif2.003/Serif/SubsetOTF/KR/NotoSerifKR-Regular.otf',
    sha256: '5ea012e15cb7eacc1f680aee1703f3b164791b1443ea3e52b65080cca5d179cf' },
  { name: 'NotoSerifKR-Bold.otf', url: 'https://raw.githubusercontent.com/notofonts/noto-cjk/Serif2.003/Serif/SubsetOTF/KR/NotoSerifKR-Bold.otf',
    sha256: '263408609ef416d597718ea91055379867d3e7086fa32f049996995d4e715e55' },
  { name: 'NotoCJK-LICENSE.txt', url: 'https://raw.githubusercontent.com/notofonts/noto-cjk/Sans2.004/LICENSE',
    sha256: '6a73f9541c2de74158c0e7cf6b0a58ef774f5a780bf191f2d7ec9cc53efe2bf2' },
]
// Fonts left out: Monotype's and Microsoft's (not ours to give), a GPL one,
// and families no Office document is likely to name.
const droppedFonts = ['Arial', 'Calibri', 'WenQuanYi Zen Hei Mono', 'Ubuntu', 'Ubuntu Condensed', 'Ubuntu Light', 'Ubuntu Mono']
// Names documents use, opened with metric-compatible fonts in the package.
const aliases = { Arial: 'Arimo', Calibri: 'Carlito' }
// The Korean fonts as TrueType: x2t's print PDF leaves out text set in their
// published CFF form and can run without end (seen 2026-10-11).
const koreanSources = ['NotoSansKR-Regular.otf', 'NotoSansKR-Bold.otf', 'NotoSerifKR-Regular.otf', 'NotoSerifKR-Bold.otf']
const koreanFonts = koreanSources.map((name) => name.replace(/\.otf$/u, '.ttf'))
const hangul = [[0x1100, 0x11ff], [0x3130, 0x318f], [0xa960, 0xa97f], [0xac00, 0xd7af], [0xd7b0, 0xd7ff]]

const run = (command, args, options = {}) => execFileSync(command, args, { stdio: 'inherit', ...options })
const digest = (file, kind) => createHash(kind).update(readFileSync(file)).digest('hex')

// 1. The inputs, checked against their hashes.
const dl = join(work, 'dl')
mkdirSync(dl, { recursive: true })
for (const item of downloads) {
  const file = join(dl, item.name)
  if (!existsSync(file)) run('curl', ['-fL', '-o', file, item.url])
  const [kind, want] = item.sha512 ? ['sha512', item.sha512] : ['sha256', item.sha256]
  if (digest(file, kind) !== want) throw new Error(`${item.name} doesn't match its ${kind}`)
}
const unpacked = join(work, 'unpacked')
if (!existsSync(join(unpacked, 'editor/web-apps'))) {
  rmSync(unpacked, { recursive: true, force: true })
  mkdirSync(join(unpacked, 'editor'), { recursive: true })
  mkdirSync(join(unpacked, 'x2t'), { recursive: true })
  run('unzip', ['-q', join(dl, 'onlyoffice-editor.zip'), '-d', join(unpacked, 'editor')])
  run('unzip', ['-q', join(dl, 'x2t.zip'), '-d', join(unpacked, 'x2t')])
}
const editor = join(unpacked, 'editor')

// 2. The package folder: the app's own files, then the trimmed engine.
const stage = join(work, 'package')
rmSync(stage, { recursive: true, force: true })
mkdirSync(stage, { recursive: true })
cpSync(join(here, 'leafen-app.json'), join(stage, 'leafen-app.json'))
cpSync(join(here, 'ui'), join(stage, 'ui'), { recursive: true })
// The blank documents "새 문서" starts from.
cpSync(join(here, 'templates'), join(stage, 'templates'), { recursive: true })
cpSync(join(product, 'sdk/app/dist/index.js'), join(stage, 'ui/leafen-app-sdk.js'))
const target = join(stage, `ui/vendor/onlyoffice-${release}`)
const editors = ['documenteditor', 'spreadsheeteditor', 'presentationeditor']
const keep = (path) => {
  const rel = relative(editor, path).split('\\').join('/')
  if (/\.(map|br|gz)$/u.test(rel) || rel.endsWith('/.DS_Store')) return false
  const [top, second, third, fourth] = rel.split('/')
  if (rel === '' || top === undefined) return true
  if (top === 'web-apps') {
    if (second === undefined) return true
    if (second === 'vendor') return third !== 'monaco'
    if (second !== 'apps') return false
    if (third === undefined || third === 'api') return true
    if (third === 'common') return fourth === undefined || fourth === 'main'
    if (!editors.includes(third)) return false
    if (fourth === undefined) return true
    if (fourth !== 'main') return false
    const inMain = rel.split('/').slice(4)
    if (inMain[0] === 'ie' || (inMain[0] === 'resources' && inMain[1] === 'help')) return false
    if (inMain[0] === 'locale' && inMain[1]) return /^(en|ko)\.json$/u.test(inMain[1])
    if (inMain[0] === 'resources' && inMain[1] === 'formula-lang' && inMain[2]) return /^(en|ko)(_desc)?\.json$/u.test(inMain[2])
    return true
  }
  if (top === 'sdkjs') {
    if (second === undefined) return true
    return ['common', 'word', 'cell', 'slide', 'vendor'].includes(second)
  }
  if (top === 'dictionaries') return second === undefined || ['en_US', 'ko_KR'].includes(second)
  if (top === 'fonts') return second === undefined || second === 'fonts'
  return false
}
cpSync(editor, target, { recursive: true, filter: keep })

// 3. The bridge first in each editor page.
for (const name of editors) {
  const main = join(target, 'web-apps/apps', name, 'main')
  for (const page of readdirSync(main).filter((file) => /^index.*\.html$/u.test(file))) {
    const path = join(main, page)
    const bridge = relative(main, join(stage, 'ui/bridge.js')).split('\\').join('/')
    const html = readFileSync(path, 'utf8')
    if (!html.includes('<head>')) throw new Error(`${page} has no <head>`)
    writeFileSync(path, html.replace('<head>', `<head><script src="${bridge}"></script>`))
  }
}

// 4. The fonts and their list, the Korean ones given TrueType outlines first
// (kept in the work folder).
const python = ['python3', '/usr/bin/python3'].find((command) => {
  try { execFileSync(command, ['-c', 'import fontTools'], { stdio: 'ignore' }); return true } catch { return false }
})
if (!python) throw new Error('Needs Python with fontTools (pip install fonttools) to give the Korean fonts TrueType outlines.')
const ttfDir = join(work, 'ttf')
mkdirSync(ttfDir, { recursive: true })
for (const [at, source] of koreanSources.entries()) {
  if (!existsSync(join(ttfDir, koreanFonts[at]))) run(python, [join(here, 'otf2ttf.py'), join(dl, source), join(ttfDir, koreanFonts[at])])
}
const fontsDir = join(target, 'fonts/fonts')
const allFontsPath = join(target, 'sdkjs/common/AllFonts.js')
const allFonts = readFileSync(allFontsPath, 'utf8')
const read = (name) => JSON.parse(new RegExp(`window\\["${name}"\\] = (\\[[\\s\\S]*?\\]);`).exec(allFonts)[1])
const oldFiles = read('__fonts_files'), oldInfos = read('__fonts_infos'), oldRanges = read('__fonts_ranges')
const oldRecords = decodeSelection(/window\["g_fonts_selection_bin"\] = "([^"]*)"/.exec(allFonts)[1])
const records = oldRecords.filter((record) => !droppedFonts.includes(record.name))
for (const [alias, real] of Object.entries(aliases)) {
  for (const record of oldRecords.filter((each) => each.name === real)) records.push({ ...record, name: alias })
}
for (const file of koreanFonts) {
  cpSync(join(ttfDir, file), join(fontsDir, file))
  const font = readFont(join(fontsDir, file))
  const scale = (value) => Math.trunc((value * 1000) / font.unitsPerEm)
  records.push({
    name: font.family, names: [], path: `/fonts/${file}`, index: 0, italic: (font.macStyle & 2) ? 1 : 0, bold: (font.macStyle & 1) ? 1 : 0,
    fixed: font.fixed ? 1 : 0, panose: font.panose, unicodeRanges: font.unicodeRanges, codePages: font.codePages,
    weight: font.weight, width: font.width, familyClass: font.familyClass, format: font.cff ? 2 : 1, avgCharWidth: scale(font.avgCharWidth),
    ascent: scale(font.typoAscent), descent: scale(font.typoDescent), lineGap: scale(font.typoLineGap),
    xHeight: scale(font.xHeight), capHeight: scale(font.capHeight), fsType: font.fsType,
  })
}
const used = new Set(records.map((record) => record.path.replace(/^.*\//u, '')))
for (const file of readdirSync(fontsDir)) if (!used.has(file)) rmSync(join(fontsDir, file))
const files = [...new Set(records.map((record) => record.path))].sort((a, b) => a.localeCompare(b))
const families = new Map()
for (const record of records) {
  const slots = families.get(record.name) ?? [-1, -1, -1, -1, -1, -1, -1, -1]
  const slot = (record.bold ? 2 : 0) + (record.italic ? 1 : 0) // regular, italic, bold, bold italic
  slots[slot * 2] = files.indexOf(record.path)
  slots[slot * 2 + 1] = record.index
  families.set(record.name, slots)
}
const infos = [...families.entries()].sort(([a], [b]) => a.toLowerCase().localeCompare(b.toLowerCase())).map(([name, slots]) => [name, ...slots])
const infoIndex = new Map(infos.map((info, at) => [info[0], at]))
// Ranges: as CryptPad's list had them, a dropped family's to Noto Sans KR
// where it has the character, and Hangul to Noto Sans KR.
const korean = readFont(join(fontsDir, 'NotoSansKR-Regular.ttf')).coverage()
const covered = (start, end) => korean.filter(([a, b]) => b >= start && a <= end).map(([a, b]) => [Math.max(a, start), Math.min(b, end)])
let runs = []
for (let at = 0; at < oldRanges.length; at += 3) {
  const [start, end, family] = [oldRanges[at], oldRanges[at + 1], oldInfos[oldRanges[at + 2]][0]]
  if (families.has(family)) runs.push([start, end, family])
  else for (const [a, b] of covered(start, end)) runs.push([a, b, 'Noto Sans KR'])
}
for (const [start, end] of hangul) {
  runs = runs.flatMap(([a, b, family]) => (b < start || a > end ? [[a, b, family]] : [
    ...(a < start ? [[a, start - 1, family]] : []), ...(b > end ? [[end + 1, b, family]] : []),
  ]))
  for (const [a, b] of covered(start, end)) runs.push([a, b, 'Noto Sans KR'])
}
runs.sort((a, b) => a[0] - b[0])
const ranges = runs.flatMap(([a, b, family]) => [a, b, infoIndex.get(family)])
const list = (values) => values.map((value) => JSON.stringify(value)).join(',\n')
// sdkjs puts "../../../../fonts/" before each file; a leading slash would make "fonts//fonts/…",
// which the server answers by redirecting, without the CORS header a frame with no origin needs.
writeFileSync(allFontsPath, `window["__all_fonts_js_version__"] = 2;\n\nwindow["__fonts_files"] = [\n${list(files.map((file) => file.replace(/^\//u, '')))}\n];\n\n`
  + `window["__fonts_infos"] = [\n${list(infos)}\n];\n\nwindow["__fonts_ranges"] = [\n${ranges.join(',')}\n];\n\n`
  + `window["g_fonts_selection_bin"] = "${encodeSelection(records)}";\n`)

// 5. x2t, and what the package owes its sources.
const x2tTarget = join(stage, `ui/vendor/x2t-${release}`)
mkdirSync(x2tTarget, { recursive: true })
const x2tFolder = existsSync(join(unpacked, 'x2t/x2t.js')) ? join(unpacked, 'x2t') : join(unpacked, 'x2t', readdirSync(join(unpacked, 'x2t'))[0])
for (const file of ['x2t.js', 'x2t.wasm']) cpSync(join(x2tFolder, file), join(x2tTarget, file))
cpSync(join(product, 'LICENSE'), join(target, 'LICENSE'))
cpSync(join(product, 'LICENSE'), join(x2tTarget, 'LICENSE'))
writeFileSync(join(target, 'SOURCE.md'), `# ONLYOFFICE editor ${release}, CryptPad's build\n\n`
  + `ONLYOFFICE Docs (sdkjs, web-apps) by Ascensio System SIA, GNU AGPL v3 (LICENSE), as CryptPad builds it to run without a\n`
  + `document server: ${downloads[0].url} (SHA-512 ${downloads[0].sha512}), from https://github.com/cryptpad/onlyoffice-editor\n`
  + `and https://github.com/ONLYOFFICE/sdkjs, https://github.com/ONLYOFFICE/web-apps at v${release}.\n`
  + 'Leafen left out files (help, other languages, source maps, the PDF, Visio and macro editors), put its bridge first in the\n'
  + 'editor pages (web-apps/apps/*/main/index*.html) and rebuilt sdkjs/common/AllFonts.js for the fonts in fonts/fonts\n'
  + '(fonts/FONTS.md); catalog-apps/office/build-onlyoffice.mjs in Leafen\'s source does it.\n')
writeFileSync(join(x2tTarget, 'SOURCE.md'), `# x2t ${release}, CryptPad's WebAssembly build\n\nONLYOFFICE core's converter by Ascensio System SIA, `
  + `GNU AGPL v3 (LICENSE): ${downloads[1].url} (SHA-512 ${downloads[1].sha512}), from https://github.com/cryptpad/onlyoffice-x2t-wasm.\n`)
const licenses = join(target, 'fonts/licenses')
mkdirSync(licenses, { recursive: true })
cpSync(join(dl, 'NotoCJK-LICENSE.txt'), join(licenses, 'OFL-1.1-NotoCJK.txt'))
cpSync(join(product, 'sdk/app/LICENSE'), join(licenses, 'Apache-2.0.txt'))
cpSync(join(here, 'licenses/LGPL-3.0.txt'), join(licenses, 'LGPL-3.0.txt'))
cpSync(join(here, 'licenses/GPL-3.0.txt'), join(licenses, 'GPL-3.0.txt'))
const fontNotes = ['# Fonts', '', 'Each font as its own name table states it. OFL 1.1 and Apache 2.0 texts are in licenses/;',
  'OpenSymbol (LibreOffice) is LGPL v3 (licenses/LGPL-3.0.txt with GPL-3.0.txt). Arial and Calibri, which documents name,',
  'open Arimo and Carlito here. Noto Sans KR and Noto Serif KR are the published OpenType fonts given TrueType outlines',
  '(fontTools cu2qu, catalog-apps/office/otf2ttf.py in Leafen\'s source), otherwise unchanged.', '']
for (const file of readdirSync(fontsDir).sort()) {
  const font = readFont(join(fontsDir, file))
  fontNotes.push(`## ${file} — ${font.family} ${font.subfamily}`, '', (font.copyright || '').trim(), '', (font.license || (file === 'opens___.ttf' ? 'GNU LGPL v3' : '')).trim(),
    font.licenseUrl ? `(${font.licenseUrl})` : '', '')
}
writeFileSync(join(target, 'fonts/FONTS.md'), fontNotes.join('\n'))

// 6. The package.
let size = 0
const walk = (folder) => { for (const name of readdirSync(folder)) { const path = join(folder, name); const stat = statSync(path); if (stat.isDirectory()) walk(path); else size += stat.size } }
walk(stage)
console.log(`package folder ${stage}: ${(size / 1e6).toFixed(1)} MB`)
run('go', ['run', './desktop', 'app', 'build', stage, '--output', out], { cwd: product })
