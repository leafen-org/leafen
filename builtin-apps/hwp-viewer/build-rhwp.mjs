#!/usr/bin/env node
// Puts rhwp into the built-in HWP app (internal/app/builtinapps/hwp-viewer):
// @rhwp/core's glue and @rhwp/editor as published, and rhwp-studio built from
// its source at the same tag with @rhwp/core's WebAssembly, keeping stable
// file names (so the viewer and the editor share one module) and leaving out
// the service worker, the sample documents, the CanvasKit renderer and the
// fonts the app doesn't list. Needs git, npm and the network.
//
//	node builtin-apps/hwp-viewer/build-rhwp.mjs <work folder>
import { execFileSync } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const version = '0.8.6'
const here = dirname(fileURLToPath(import.meta.url))
const app = resolve(here, '../../internal/app/builtinapps/hwp-viewer/ui/vendor')
const work = resolve(process.argv[2] ?? '')
if (!process.argv[2]) throw new Error('Name a work folder.')
// The fonts the HWP fallbacks name (all SIL OFL 1.1).
const fonts = ['NotoSansKR-Regular', 'NotoSansKR-Bold', 'NotoSansKR-ExtraLight', 'NotoSerifKR-Regular', 'NotoSerifKR-Bold',
  'NanumGothic-Regular', 'NanumGothic-Bold', 'NanumMyeongjo-Regular', 'NanumMyeongjo-Bold', 'Pretendard-Regular', 'Pretendard-Bold',
  'GowunBatang-Regular', 'GowunDodum-Regular']
const run = (command, args, cwd, env = {}) => execFileSync(command, args, { cwd, stdio: 'inherit', env: { ...process.env, ...env } })

mkdirSync(work, { recursive: true })
for (const name of ['core', 'editor']) {
  if (!existsSync(join(work, name))) {
    run('npm', ['pack', `@rhwp/${name}@${version}`, '--silent'], work)
    mkdirSync(join(work, name))
    run('tar', ['-xzf', `rhwp-${name}-${version}.tgz`, '-C', name], work)
  }
}
const source = join(work, 'rhwp')
if (!existsSync(source)) {
  run('git', ['clone', '-q', '--depth', '1', '--branch', `v${version}`, '--filter=blob:none', '--sparse', 'https://github.com/edwardkim/rhwp.git', source], work)
  run('git', ['sparse-checkout', 'set', 'rhwp-studio', 'rhwp-shared', 'npm/editor', 'scripts', 'assets/fonts'], source)
}
// The studio takes the WebAssembly build from ../pkg, here @rhwp/core's.
mkdirSync(join(source, 'pkg'), { recursive: true })
for (const file of ['rhwp.js', 'rhwp_bg.wasm', 'rhwp.d.ts', 'rhwp_bg.wasm.d.ts']) cpSync(join(work, 'core/package', file), join(source, 'pkg', file))
const studio = join(source, 'rhwp-studio')
run('npm', ['ci', '--ignore-scripts', '--no-audit', '--no-fund'], studio)
writeFileSync(join(studio, 'leafen.vite.config.ts'), `import original from './vite.config';
// Relative paths, stable asset names and no service worker, for a frame under Leafen.
const plugins = (original.plugins ?? []).flat().filter((plugin) => !String(plugin?.name ?? '').startsWith('vite-plugin-pwa'));
const output = { assetFileNames: 'assets/[name][extname]', chunkFileNames: 'assets/[name].js', entryFileNames: 'assets/[name].js' };
export default { ...original, plugins, base: './', build: { ...(original.build ?? {}), rollupOptions: { output }, rolldownOptions: { output } } };
`)
run('npx', ['tsc'], studio, { RHWP_WITHOUT_HWPCTRL: '1' })
run('npx', ['vite', 'build', '--config', 'leafen.vite.config.ts'], studio, { RHWP_WITHOUT_HWPCTRL: '1', RHWP_DISABLE_EXTERNAL_WEBFONTS: '1' })

const dist = join(studio, 'dist')
const target = join(app, `rhwp-studio-${version}`)
rmSync(target, { recursive: true, force: true })
mkdirSync(target, { recursive: true })
for (const entry of readdirSync(dist)) {
  if (['samples', 'fonts', 'sw.js', 'registerSW.js', 'manifest.webmanifest', 'rhwp.js', 'rhwp.d.ts', 'rhwp_bg.wasm.d.ts'].includes(entry) || entry.startsWith('workbox-')) continue
  cpSync(join(dist, entry), join(target, entry), { recursive: true })
}
for (const entry of readdirSync(join(target, 'assets'))) {
  if (entry.startsWith('canvaskit')) rmSync(join(target, 'assets', entry))
}
mkdirSync(join(target, 'fonts'))
for (const font of fonts) cpSync(join(dist, 'fonts', `${font}.woff2`), join(target, 'fonts', `${font}.woff2`))
cpSync(join(source, 'assets/fonts/FONTS.md'), join(target, 'fonts/FONTS.md'))
cpSync(join(source, 'assets/fonts/SourceHanSerifK-OFL.txt'), join(target, 'fonts/LICENSE-OFL-1.1.txt'))
// The bridge first, so the studio takes the app's handshake in a frame with no origin.
cpSync(join(here, 'studio-bridge.js'), join(target, 'leafen-bridge.js'))
const page = join(target, 'index.html')
writeFileSync(page, readFileSync(page, 'utf8').replace('<head>\n', '<head>\n  <script src="./leafen-bridge.js"></script>\n'))
cpSync(join(source, 'LICENSE'), join(target, 'LICENSE'))
cpSync(join(source, 'THIRD_PARTY_LICENSES.md'), join(target, 'THIRD_PARTY_LICENSES.md'))

const core = join(app, `rhwp-core-${version}`)
rmSync(core, { recursive: true, force: true })
mkdirSync(core, { recursive: true })
for (const file of ['rhwp.js', 'LICENSE']) cpSync(join(work, 'core/package', file), join(core, file))
const editor = join(app, `rhwp-editor-${version}`)
rmSync(editor, { recursive: true, force: true })
mkdirSync(editor, { recursive: true })
for (const file of ['index.js', 'transport.js', 'document-agent-contract.js']) cpSync(join(work, 'editor/package', file), join(editor, file))
cpSync(join(work, 'core/package/LICENSE'), join(editor, 'LICENSE'))
// The blank HWPX "새 문서" starts from: rhwp's own blank document (its
// built-in template), written as HWPX.
const rhwp = await import(pathToFileURL(join(core, 'rhwp.js')).href)
rhwp.initSync({ module: readFileSync(join(target, 'assets/rhwp_bg.wasm')) })
const blank = rhwp.HwpDocument.createEmpty()
blank.createBlankDocument()
const templates = resolve(app, '../../templates')
mkdirSync(templates, { recursive: true })
writeFileSync(join(templates, 'blank.hwpx'), blank.exportHwpx())
console.log(`rhwp ${version} is in ${app}`)
