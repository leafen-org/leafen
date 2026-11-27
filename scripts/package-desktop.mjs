#!/usr/bin/env node

// Packages the desktop app for one system, as Leafen's releases carry it
// (docs/release-lists.md "The desktop update list"): the macOS app bundle
// zipped (`app-zip`), which the desktop update list names, or the Windows
// program folder zipped (`zip`), which people download and which the list
// leaves out: Windows updates only through the Microsoft Store. Linux
// has no desktop app: Leafen runs there as the server alone, in Docker
// (scripts/publish-image.sh). `--store` builds the edition for an app
// store's sandbox, which carries no app platform on its own server
// (desktop/store_appstore.go, leafen_noapps).
// It needs the web build first (npm --prefix web run build).
//
//	node scripts/package-desktop.mjs [--target darwin-arm64] [--version 1.2.3] [--out dist/desktop] [--store] [--msix] [--development]
//
// The version defaults to web/package.json's, without a pre-release. A
// package is a release (leafen_release), which trusts only the production
// signing keys and won't start without them (internal/app/release_keys.go);
// --development packages a development build instead, whose version names
// "dev" (1.2.3-dev unless given), to try the packaging before they exist.
//
// macOS also gets a disk image (`dmg`) to download, holding the app and a
// link to Applications, its window laid out as Notion's disk image is
// (scripts/dmg-layout.mjs). The app is signed with the hardened runtime and
// desktop/build/darwin/entitlements.plist, by the Developer ID Application
// identity LEAFEN_MAC_SIGN_IDENTITY names (in the keychain), else ad hoc. With
// LEAFEN_MAC_NOTARY_PROFILE (a keychain profile `xcrun notarytool
// store-credentials` made) as well, Apple notarizes the app, whose ticket is
// stapled before it is zipped, and the signed disk image, stapled too.
// Neither secret is in the repository. The Windows program is not signed:
// Leafen has no Windows code signing certificate, and the store signs its
// package.
//
// Windows with `--msix` also gets the Microsoft Store's package (`msix`): the
// same program with full trust, its manifest desktop/build/windows/
// AppxManifest.xml and the icons beside it, packed by makemsix (Microsoft's
// msix-packaging, on PATH or at LEAFEN_MAKEMSIX). Its identity is Partner
// Center's — LEAFEN_MSIX_IDENTITY_NAME, LEAFEN_MSIX_PUBLISHER and
// LEAFEN_MSIX_PUBLISHER_DISPLAY_NAME — else a development one, to install
// with a test certificate; the store signs what it is given.
// Each file is printed with its kind, SHA-256 and size, for the release and
// its desktop update list (which takes the macOS `app-zip`).

import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { chmodSync, copyFileSync, cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { basename, dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { makeDiskImage } from './dmg-layout.mjs'

const product = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const args = process.argv.slice(2)
const option = (name, fallback) => {
  const at = args.indexOf(name)
  return at >= 0 && args[at + 1] ? args[at + 1] : fallback
}
const hostTarget = `${process.platform === 'win32' ? 'windows' : process.platform}-${process.arch === 'x64' ? 'amd64' : process.arch}`
const target = option('--target', hostTarget)
const [goos, goarch] = target.split('-')
const development = args.includes('--development')
const baseVersion = JSON.parse(readFileSync(join(product, 'web', 'package.json'), 'utf8')).version.replace(/-.*$/u, '') || '0.0.0'
const version = option('--version', development ? `${baseVersion}-dev` : baseVersion)
const out = resolve(product, option('--out', join('dist', 'desktop')))
const store = args.includes('--store')
const msix = args.includes('--msix')
const appId = 'org.leafen.app'

if (goos === 'linux') fail('Linux has no desktop app: Leafen runs there as the server alone, in Docker (scripts/publish-image.sh)')
if (!['darwin', 'windows'].includes(goos) || !['arm64', 'amd64'].includes(goarch)) fail(`unknown target ${target}`)
if (goos === 'darwin' && process.platform !== 'darwin') fail('a macOS build needs macOS (with Xcode 26 or later, whose actool makes the icon)')
if (!existsSync(join(product, 'web', 'dist', 'index.html'))) fail('build the web first: npm --prefix web run build')
if (msix && (goos !== 'windows' || store)) fail('--msix packages the Windows app: --target windows-amd64 or windows-arm64, without --store')
if (development && !/-.*dev/iu.test(version)) fail(`a development package's version names "dev" (${baseVersion}-dev.1), not ${version}: it would refuse to start`)

function fail(message) {
  console.error(`package-desktop: ${message}`)
  process.exit(1)
}

function run(command, commandArgs, options = {}) {
  const result = spawnSync(command, commandArgs, { stdio: options.quiet ? 'pipe' : 'inherit', encoding: 'utf8', timeout: 1_800_000, ...options })
  if (result.status !== 0) fail(`${command} ${commandArgs.join(' ')} failed${result.error ? ` (${result.error.message})` : ''}${result.stderr ? `: ${result.stderr.trim()}` : ''}`)
  return result
}

function sizeOf(path) {
  const info = statSync(path)
  if (!info.isDirectory()) return info.size
  return readdirSync(path).reduce((total, name) => total + sizeOf(join(path, name)), 0)
}

const megabytes = (bytes) => `${(bytes / 1048576).toFixed(1)} MB`
const work = join(tmpdir(), `leafen-desktop-${process.pid}`)
rmSync(work, { recursive: true, force: true })
mkdirSync(work, { recursive: true })
mkdirSync(out, { recursive: true })

// The program, with the release's version and without the build machine's
// paths, named as the app finds it when it updates (desktop/program_name.go).
const programName = readFileSync(join(product, 'desktop', 'program_name.go'), 'utf8').match(/const desktopProgram = "([^"]+)"/u)?.[1] || fail('desktop/program_name.go names no program')
const executable = goos === 'windows' ? `${programName}.exe` : programName
const program = join(work, executable)
const ldflags = [`-X leafen.org/leafen/internal/app.leafenReleaseVersion=${version}`, '-s', '-w', ...(goos === 'windows' ? ['-H=windowsgui'] : [])]
const tags = [...(store ? ['appstore', 'leafen_noapps'] : []), ...(development ? [] : ['leafen_release'])]
// A Mac program runs on the oldest macOS Go supports (13), its C and
// Objective-C parts too: left alone they take the build machine's version
// and the program would not open on an older Mac. The flags, not only the
// variable, keep the build cache from handing back parts built otherwise.
const macMinimum = '13.0'
const macTarget = goos === 'darwin' ? {
  MACOSX_DEPLOYMENT_TARGET: macMinimum,
  CGO_CFLAGS: `${process.env.CGO_CFLAGS ?? '-O2 -g'} -mmacosx-version-min=${macMinimum}`,
  CGO_LDFLAGS: `${process.env.CGO_LDFLAGS ?? ''} -mmacosx-version-min=${macMinimum}`.trim(),
} : {}
// The server inside says what build it is and what changed since the one before (internal/app/server_build.go).
run(process.execPath, [join(product, 'scripts', 'stamp-build.mjs')])
// A Windows program says its version in its version resource — its file's
// properties, the task manager — so the release's version goes into the
// resource Go links (desktop/rsrc_windows_*.syso, made by go-winres from
// desktop/build/windows/winres.json), which is put back as it was after.
const resource = join(product, 'desktop', `rsrc_windows_${goarch}.syso`)
const keptResource = goos === 'windows' ? readFileSync(resource) : null
try {
  if (goos === 'windows') {
    const numeric = version.replace(/[-+].*$/u, '')
    run('go', ['run', 'github.com/tc-hib/go-winres@v0.3.3', 'make', '--in', join('build', 'windows', 'winres.json'), '--arch', goarch,
      '--out', 'rsrc', '--file-version', numeric, '--product-version', version], { cwd: join(product, 'desktop') })
  }
  run('go', ['build', '-trimpath', ...(tags.length ? ['-tags', tags.join(',')] : []), '-ldflags', ldflags.join(' '), '-o', program, '.'], {
    cwd: join(product, 'desktop'),
    env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: goos === 'windows' ? '0' : '1', ...macTarget },
  })
} finally {
  if (keptResource) writeFileSync(resource, keptResource)
}
// On Windows the program has a console program beside it, named as it is
// with .com, which PowerShell and cmd run for the bare name: a terminal waits
// for it and shows what the program says (desktop/console/main.go).
const consoleProgram = join(work, `${programName}.com`)
if (goos === 'windows') {
  run('go', ['build', '-trimpath', '-ldflags', '-s -w', '-o', consoleProgram, './console'], {
    cwd: join(product, 'desktop'),
    env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' },
  })
}

// Beside the program: its license, its notice and the third-party notices of
// its edition (scripts/generate-third-party-notices.mjs), as the program
// serves them.
const notices = [[join(product, 'LICENSE'), 'LICENSE'], [join(product, 'NOTICE'), 'NOTICE'],
  [join(product, 'web', 'notices', store ? 'THIRD-PARTY-NOTICES-noapps.txt' : 'THIRD-PARTY-NOTICES.txt'), 'THIRD-PARTY-NOTICES.txt']]

const name = `leafen-desktop-${version}-${goos}-${goarch}${store ? '-store' : ''}`
let artifact = ''
let kind = ''
// Files beside the update list's one, as [kind, file]: the macOS disk image.
const extra = []
if (goos === 'darwin') {
  const app = join(work, 'Leafen.app')
  mkdirSync(join(app, 'Contents', 'MacOS'), { recursive: true })
  mkdirSync(join(app, 'Contents', 'Resources'), { recursive: true })
  copyFileSync(program, join(app, 'Contents', 'MacOS', executable))
  chmodSync(join(app, 'Contents', 'MacOS', executable), 0o755)
  for (const [file, named] of notices) copyFileSync(file, join(app, 'Contents', 'Resources', named))
  writeFileSync(join(app, 'Contents', 'Info.plist'), infoPlist())
  macIcon(join(app, 'Contents', 'Resources'))
  const { identity, profile } = macRelease()
  macSign(app, identity)
  artifact = join(out, `${name}.zip`)
  rmSync(artifact, { force: true })
  run('ditto', ['-c', '-k', '--sequesterRsrc', '--keepParent', app, artifact])
  if (profile) {
    // The update list's zip carries the stapled app, which the app's own
    // update checks (desktop/update_fetch.go) and Gatekeeper accept offline.
    macNotarize(artifact, profile)
    run('xcrun', ['stapler', 'staple', app])
    rmSync(artifact, { force: true })
    run('ditto', ['-c', '-k', '--sequesterRsrc', '--keepParent', app, artifact])
  }
  kind = 'app-zip'
  if (!store) {
    const image = join(out, `${name}.dmg`)
    macDiskImage(app, image)
    if (identity) run('codesign', ['--force', '--timestamp', '--sign', identity, image], { quiet: true })
    if (profile) {
      macNotarize(image, profile)
      run('xcrun', ['stapler', 'staple', image])
    }
    extra.push(['dmg', image])
  }
} else {
  const folder = join(work, 'Leafen')
  mkdirSync(folder, { recursive: true })
  copyFileSync(program, join(folder, executable))
  copyFileSync(consoleProgram, join(folder, `${programName}.com`))
  for (const [file, named] of notices) copyFileSync(file, join(folder, named))
  artifact = join(out, `${name}.zip`)
  rmSync(artifact, { force: true })
  if (process.platform === 'win32') {
    run('powershell', ['-NoProfile', '-Command', `Compress-Archive -Path '${folder}' -DestinationPath '${artifact}'`])
  } else {
    // Without macOS's extended attributes, which would arrive as ._ files.
    run('ditto', ['-c', '-k', '--norsrc', '--noextattr', '--keepParent', folder, artifact])
  }
  kind = 'zip'
  if (msix) extra.push(['msix', windowsPackage(folder, join(out, `${name}.msix`))])
}

console.log(JSON.stringify([[kind, artifact], ...extra].map(([each, file]) => ({
  os: goos, arch: goarch, kind: each, file, sha256: createHash('sha256').update(readFileSync(file)).digest('hex'), size: statSync(file).size,
  sizes: { program: megabytes(sizeOf(program)), artifact: megabytes(statSync(file).size) },
})), null, 2))
rmSync(work, { recursive: true, force: true })

// The languages the app has a catalog for (web/src/locales), every one of
// which it offers; the systems' own lists of the app's languages name them.
function catalogLanguages() {
  return readdirSync(join(product, 'web', 'src', 'locales'), { withFileTypes: true })
    .filter((entry) => entry.isDirectory()).map((entry) => entry.name).sort()
}

function infoPlist() {
  const entries = [
    ['CFBundleExecutable', executable], ['CFBundleIdentifier', appId], ['CFBundleName', 'Leafen'], ['CFBundleDisplayName', 'Leafen'],
    ['CFBundlePackageType', 'APPL'], ['CFBundleShortVersionString', version.replace(/-.*$/u, '')], ['CFBundleVersion', version.replace(/-.*$/u, '')], ['CFBundleIconFile', 'Leafen'], ['CFBundleIconName', 'Leafen'],
    ['LSMinimumSystemVersion', macMinimum], ['LSApplicationCategoryType', 'public.app-category.productivity'],
    ['CFBundleDevelopmentRegion', 'en'],
  ]
  // The languages the window speaks, so the items macOS adds to the menu bar
  // itself (AutoFill, dictation, emoji, Close All) speak the system's one too.
  const languages = catalogLanguages()
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
${entries.map(([key, value]) => `  <key>${key}</key><string>${value}</string>`).join('\n')}
  <key>NSHighResolutionCapable</key><true/>
  <key>CFBundleLocalizations</key>
  <array>${languages.map((language) => `<string>${language}</string>`).join('')}</array>
  <key>CFBundleURLTypes</key>
  <array><dict><key>CFBundleURLName</key><string>${appId}.links</string><key>CFBundleURLSchemes</key><array><string>leafen</string></array></dict></array>
</dict>
</plist>
`
}

// The app's icon, by Xcode's actool from the full-bleed picture
// (desktop/build/appicon.png) as a one-layer Icon Composer icon with no glass,
// shine, shadow or see-through: macOS 26 and later cut it to their own
// rounded square from the catalog (Assets.car, CFBundleIconName), as a full
// square .icns is cut; earlier macOS, which cuts no shape and would show a
// square with corners, takes the flat images actool cuts and sets in Apple's
// icon grid from the same picture (the catalog's and Leafen.icns).
function macIcon(resources) {
  const icon = join(work, 'Leafen.icon')
  mkdirSync(join(icon, 'Assets'), { recursive: true })
  copyFileSync(join(product, 'desktop', 'build', 'appicon.png'), join(icon, 'Assets', 'appicon.png'))
  writeFileSync(join(icon, 'icon.json'), JSON.stringify({
    fill: 'automatic',
    groups: [{
      layers: [{ glass: false, 'image-name': 'appicon.png', name: 'appicon' }],
      shadow: { kind: 'none', opacity: 0 }, specular: false, translucency: { enabled: false, value: 0 },
    }],
    'supported-platforms': { squares: 'shared' },
  }, null, 2))
  run('xcrun', ['actool', icon, '--compile', resources, '--app-icon', 'Leafen', '--platform', 'macosx', '--target-device', 'mac',
    '--minimum-deployment-target', macMinimum, '--output-partial-info-plist', join(work, 'icon-info.plist')], { quiet: true })
}

// The signing identity and notarization profile the environment names, each
// said when it is missing; a profile without an identity can't notarize.
function macRelease() {
  const identity = process.env.LEAFEN_MAC_SIGN_IDENTITY || ''
  const profile = process.env.LEAFEN_MAC_NOTARY_PROFILE || ''
  if (!identity) {
    console.error('package-desktop: LEAFEN_MAC_SIGN_IDENTITY is not set: signed ad hoc and not notarized, so macOS warns when the downloaded app opens')
    if (profile) fail('LEAFEN_MAC_NOTARY_PROFILE needs LEAFEN_MAC_SIGN_IDENTITY: Apple notarizes only Developer ID signatures')
    return { identity, profile }
  }
  if (!run('security', ['find-identity', '-v', '-p', 'codesigning'], { quiet: true }).stdout.includes(identity)) {
    fail('LEAFEN_MAC_SIGN_IDENTITY names no signing identity in the keychain (security find-identity -v -p codesigning lists them)')
  }
  if (!profile) console.error('package-desktop: LEAFEN_MAC_NOTARY_PROFILE is not set: signed but not notarized, so macOS warns when the downloaded app opens')
  return { identity, profile }
}

// The app signed with the hardened runtime and the app platform's
// entitlements, by the identity or ad hoc. Ad hoc it runs as the release
// does, but opens without a warning only on the Mac that built it.
function macSign(app, identity) {
  const signing = identity ? ['--timestamp', '--sign', identity] : ['--sign', '-']
  const entitlements = store ? [] : ['--entitlements', join(product, 'desktop', 'build', 'darwin', 'entitlements.plist')]
  run('codesign', ['--force', '--options', 'runtime', ...entitlements, ...signing, app], { quiet: true })
  run('codesign', ['--verify', '--strict', app], { quiet: true })
}

// Apple's notarization of a zip or disk image, waiting for its verdict, which
// is printed with the submission's id (for the release's record, and for
// `xcrun notarytool log <id>` later); a refusal prints Apple's log of why.
function macNotarize(file, profile) {
  const submitted = spawnSync('xcrun', ['notarytool', 'submit', file, '--keychain-profile', profile, '--wait', '--output-format', 'json'],
    { encoding: 'utf8', timeout: 3_600_000 })
  let verdict = {}
  try {
    verdict = JSON.parse(submitted.stdout)
  } catch {
    // notarytool printed no verdict: its error is on stderr.
  }
  if (verdict.id) console.error(`package-desktop: notarization of ${basename(file)}: ${verdict.status || 'no verdict'} (submission ${verdict.id})`)
  if (verdict.status === 'Accepted') return
  const log = verdict.id ? spawnSync('xcrun', ['notarytool', 'log', verdict.id, '--keychain-profile', profile], { encoding: 'utf8' }) : null
  fail(`Apple did not notarize ${file} (${verdict.status || submitted.stderr?.trim() || submitted.error?.message})${log ? `: ${log.stdout || log.stderr}` : ''}`)
}

// The disk image people download: the app and a link to Applications to drag
// it onto, compressed.
function macDiskImage(app, image) {
  try {
    makeDiskImage(app, image, work)
  } catch (error) {
    fail(error.message)
  }
}

// Each of the app's languages (web/src/locales) as the MSIX names it, every
// tag checked twice: it is on the Microsoft Store's list of supported
// languages, and Windows installs a package that lists it (2026-11-19, Windows
// 11 26300). Windows refuses the bare "ha" and "uz" the store's list also
// carries, so Hausa and Uzbek name their script, as Serbian (Cyrillic) and
// Azerbaijani (Latin) do. Burmese (null) is not on the store's list: the store
// distributes no package for a language it does not list and may hold the
// submission back for it, so the package leaves it out while the app still
// offers it inside. This is the only list: a language the app gains stops the
// packaging until its tag is checked both ways and added here.
function storeLanguages() {
  const storeLanguageTags = {
    af: 'af', am: 'am', ar: 'ar', az: 'az-latn', bg: 'bg', bn: 'bn', cs: 'cs', da: 'da', de: 'de', el: 'el',
    en: 'en-us', es: 'es', fa: 'fa', fi: 'fi', fil: 'fil', fr: 'fr', gu: 'gu', ha: 'ha-latn', he: 'he', hi: 'hi',
    hr: 'hr', hu: 'hu', id: 'id', it: 'it', ja: 'ja', kk: 'kk', km: 'km', kn: 'kn', ko: 'ko-kr', ml: 'ml',
    mr: 'mr', ms: 'ms', my: null, nb: 'nb', ne: 'ne', nl: 'nl', pa: 'pa', pl: 'pl', 'pt-BR': 'pt-br',
    'pt-PT': 'pt-pt', ro: 'ro', ru: 'ru', si: 'si', sk: 'sk', sl: 'sl', sr: 'sr-cyrl', sv: 'sv', sw: 'sw',
    ta: 'ta', te: 'te', th: 'th', tr: 'tr', uk: 'uk', ur: 'ur', uz: 'uz-latn', vi: 'vi', 'zh-Hans': 'zh-hans',
    'zh-Hant': 'zh-hant',
  }
  // The package's languages, English first as its default.
  const tags = catalogLanguages().map((language) => {
    if (!Object.hasOwn(storeLanguageTags, language)) fail(`web/src/locales/${language} has no checked Microsoft Store language tag: check one against the store's list of supported languages and a Windows install, then add it to storeLanguageTags`)
    return storeLanguageTags[language]
  }).filter(Boolean)
  return ['en-us', ...tags.filter((tag) => tag !== 'en-us')]
}

// The Microsoft Store's package: the program folder with its manifest and
// icons (desktop/build/windows), packed by makemsix. Its version is the
// release's in the store's four numbers, the last 0 (the store's own), or
// LEAFEN_MSIX_VERSION's when the store needs a package above one it already
// has while the program keeps the release's version; without Partner
// Center's identity it carries a development one (CN=Leafen Development),
// which installs only with a test certificate of that name.
function windowsPackage(folder, file) {
  const identity = {
    identityName: process.env.LEAFEN_MSIX_IDENTITY_NAME || '',
    publisher: process.env.LEAFEN_MSIX_PUBLISHER || '',
    publisherDisplayName: process.env.LEAFEN_MSIX_PUBLISHER_DISPLAY_NAME || '',
  }
  if (Object.values(identity).some((value) => !value)) {
    console.error('package-desktop: LEAFEN_MSIX_IDENTITY_NAME, LEAFEN_MSIX_PUBLISHER and LEAFEN_MSIX_PUBLISHER_DISPLAY_NAME are not all set: the MSIX carries a development identity, for a test install only')
    Object.assign(identity, { identityName: 'LeafenDevelopment.Leafen', publisher: 'CN=Leafen Development', publisherDisplayName: 'Leafen Development' })
  }
  const numbers = version.replace(/-.*$/u, '').split('.').map(Number)
  if (numbers.length !== 3 || numbers.some((each) => !Number.isInteger(each) || each < 0 || each > 65535)) fail(`an MSIX version is three numbers up to 65535, not ${version}`)
  const packageVersion = process.env.LEAFEN_MSIX_VERSION || `${numbers.join('.')}.0`
  const packageNumbers = packageVersion.split('.')
  if (packageNumbers.length !== 4 || packageNumbers.some((each) => !/^\d{1,5}$/u.test(each) || Number(each) > 65535) || Number(packageNumbers[3]) !== 0) fail(`an MSIX version (LEAFEN_MSIX_VERSION) is four numbers up to 65535, the last 0, which the store keeps for itself: not ${packageVersion}`)
  const values = { ...identity, version: packageNumbers.map(Number).join('.'), architecture: goarch === 'amd64' ? 'x64' : 'arm64', program: executable }
  const source = join(product, 'desktop', 'build', 'windows')
  const layout = join(work, 'msix')
  cpSync(folder, layout, { recursive: true })
  mkdirSync(join(layout, 'Assets'))
  for (const icon of ['Square44x44Logo.png', 'Square150x150Logo.png', 'StoreLogo.png']) copyFileSync(join(source, icon), join(layout, 'Assets', icon))
  const escape = (text) => text.replace(/&/gu, '&amp;').replace(/</gu, '&lt;').replace(/>/gu, '&gt;').replace(/"/gu, '&quot;')
  const resources = storeLanguages().map((language) => `<Resource Language="${language}" />`).join('\n    ')
  writeFileSync(join(layout, 'AppxManifest.xml'), readFileSync(join(source, 'AppxManifest.xml'), 'utf8')
    .replace('{{resources}}', resources)
    .replace(/\{\{(\w+)\}\}/gu, (_, key) => key in values ? escape(values[key]) : fail(`the MSIX manifest asks for ${key}, which the script doesn't give`)))
  const makemsix = process.env.LEAFEN_MAKEMSIX || 'makemsix'
  rmSync(file, { force: true })
  // makemsix prints its own progress; a failure says what it refused.
  run(makemsix, ['pack', '-d', layout, '-p', file], { quiet: true })
  return file
}
