#!/usr/bin/env node
// Builds the license notices of the software Leafen is distributed with —
// the packages bundled into its screens, the Go modules linked into its
// executable and into its desktop and phone apps, the libraries in the
// screens the executable carries (the built-in apps', the file preview's) and
// those its Android app carries — one
// file per edition, each naming only what that edition holds:
// web/notices/THIRD-PARTY-NOTICES.txt for Leafen with the app platform and
// web/notices/THIRD-PARTY-NOTICES-noapps.txt for a build tagged
// leafen_noapps (the phone apps, the Mac App Store's). The executable serves
// its edition's file at /THIRD-PARTY-NOTICES.txt (web/embed.go). With
// --check it fails when a committed file differs. A license the script can't
// name stops it, to be reviewed before anything ships with it.
//
// Needs web/node_modules, the Go module cache and, for Gradle, a JDK
// (JAVA_HOME, else Homebrew's openjdk@21).
//
//	node scripts/generate-third-party-notices.mjs [--check]
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const product = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const web = join(product, 'web')
const outputs = { full: join(web, 'notices', 'THIRD-PARTY-NOTICES.txt'), noapps: join(web, 'notices', 'THIRD-PARTY-NOTICES-noapps.txt') }
const check = process.argv.includes('--check')
const ownModule = 'leafen.org/leafen'
const licenseFile = /^(licen[cs]e|copying|notice|patents)([._-][a-z0-9.]+)?$/i

// The license a text grants, by its words; anything else is reviewed first.
function licenseOf(original) {
  const text = original.replace(/\s+/g, ' ')
  if (/Apache License\s+Version 2\.0/i.test(text)) return 'Apache-2.0'
  if (/Licensed under the Apache License,? Version 2\.0/i.test(text)) return 'Apache-2.0 (its text in the appendix)'
  if (/Mozilla Public License,? (version|v\.?)?\s*2\.0/i.test(text)) return 'MPL-2.0'
  // elkjs (in Mermaid 12) is offered under EPL-2.0 or GPL-3.0-or-later; its file holds the EPL's text.
  if (/Eclipse Public License - v 2\.0/i.test(text)) return 'EPL-2.0'
  if (/SIL OPEN FONT LICENSE Version 1\.1/i.test(text)) return 'OFL-1.1'
  if (/Blue Oak Model License/i.test(text)) return 'BlueOak-1.0.0'
  if (/Permission is hereby granted, free of charge|Licensed under the MIT license/i.test(text)) return 'MIT'
  if (/Permission to use, copy, modify, (and\/or|and) distribute this software for any purpose/i.test(text)) return 'ISC-like'
  if (/Redistribution and use in source and binary forms/i.test(text)) return 'BSD'
  if (/This is free and unencumbered software released into the public domain/i.test(text)) return 'Unlicense'
  if (/Creative Commons (Legal Code|Zero|CC0)|^CC0 1\.0 Universal/i.test(text)) return 'CC0-1.0'
  if (/The author disclaims copyright to this source code|has been dedicated to the public\s+domain/i.test(text)) return 'public domain'
  if (/Additional IP Rights Grant \(Patents\)/i.test(text)) return 'patent grant'
  if (/^\s*(Copyright|This product includes|[A-Za-z0-9 .,-]+ Copyright)/i.test(text) && text.length < 4000) return 'notice'
  return ''
}

const problems = []

function readLicenses(directory, label) {
  try {
    return licenseTexts(directory, label)
  } catch (error) {
    problems.push(error.message)
    return ''
  }
}

function licenseTexts(directory, label) {
  // A logo's license covers artwork Leafen doesn't ship; a source file named
  // so (tailscale.com's license_test.go) is code.
  const files = readdirSync(directory).filter((name) => licenseFile.test(name) && !/logo/i.test(name) && !/\.(go|js|mjs|ts|py|rs|c|h)$/i.test(name)).sort()
  const texts = files.map((name) => readFileSync(join(directory, name), 'utf8').replace(/\r\n/g, '\n').trim())
  // A few packages keep their license in their README's License section.
  const readme = readdirSync(directory).find((name) => /^readme(\.md)?$/i.test(name))
  if (!texts.length && readme) {
    const section = readFileSync(join(directory, readme), 'utf8').replace(/\r\n/g, '\n').match(/^#+ *Licen[cs]e *\n([\s\S]*?)(?=\n#+ |(?![\s\S]))/im)?.[1]?.trim()
    if (section && licenseOf(section) && licenseOf(section) !== 'notice') texts.push(section)
  }
  const kinds = texts.map(licenseOf)
  if (!texts.length) throw new Error(`no license file: ${label}`)
  const unknown = kinds.findIndex((kind) => !kind)
  if (unknown >= 0) throw new Error(`review the license of ${label} (${files[unknown]})`)
  if (!kinds.some((kind) => kind !== 'notice' && kind !== 'patent grant')) throw new Error(`no license text, only notices: ${label}`)
  return texts.join('\n\n')
}


// The bundled web packages: every package the screens' dependencies need.
// The npm packages a lock file installs at a root, with their licenses.
function npmPackages(root) {
  const lock = JSON.parse(readFileSync(join(root, 'package-lock.json'), 'utf8'))
  const located = (name, parent) => {
    for (;;) {
      const path = `${parent ? `${parent}/` : ''}node_modules/${name}`
      if (lock.packages[path]) return path
      if (!parent) return ''
      const index = parent.lastIndexOf('/node_modules/')
      parent = index < 0 ? '' : parent.slice(0, index)
    }
  }
  const seen = new Set()
  // pdf.js draws in Node with @napi-rs/canvas; the screens draw on the
  // browser's own canvas, so it and its builds aren't shipped.
  const nodeOnly = new Set(['node_modules/@napi-rs/canvas'])
  const visit = (path) => {
    if (!path || seen.has(path) || nodeOnly.has(path)) return
    seen.add(path)
    const entry = lock.packages[path]
    for (const name of [...Object.keys(entry.dependencies ?? {}), ...Object.keys(entry.optionalDependencies ?? {})]) visit(located(name, path))
  }
  for (const name of Object.keys(lock.packages[''].dependencies ?? {})) {
    const path = located(name, '')
    if (!path) throw new Error(`missing locked dependency: ${name}`)
    visit(path)
  }
  const entries = []
  for (const path of [...seen].sort()) {
    const directory = join(root, path)
    // Optional packages for other platforms aren't installed or shipped.
    if (!existsSync(directory)) {
      if (lock.packages[path].optional) continue
      throw new Error(`not installed: ${path}`)
    }
    const pkg = JSON.parse(readFileSync(join(directory, 'package.json'), 'utf8'))
    if (pkg.version !== lock.packages[path].version) throw new Error(`installed version differs from the lock: ${pkg.name}`)
    let text = ''
    // The client-only 0.0.1 tarball declares MIT and leaves its LICENSE out:
    // the upstream React repository's (v19.2.4) stands in.
    if (pkg.name === 'client-only' && pkg.version === '0.0.1') text = readFileSync(join(web, 'vendor-licenses/client-only-0.0.1.LICENSE'), 'utf8').trim()
    // @wailsio/runtime declares MIT and leaves its LICENSE out: the Wails
    // repository's (v3.1.0) stands in.
    else if (pkg.name === '@wailsio/runtime' && pkg.version === '3.1.0') text = readFileSync(join(web, 'vendor-licenses/wailsio-runtime-3.1.0.LICENSE'), 'utf8').trim()
    else if (existsSync(join(root, 'vendor-licenses', `${pkg.name.replace(/^@/u, '').replace('/', '__')}-${pkg.version}.LICENSE`))) {
      text = readFileSync(join(root, 'vendor-licenses', `${pkg.name.replace(/^@/u, '').replace('/', '__')}-${pkg.version}.LICENSE`), 'utf8').trim()
    } else text = readLicenses(directory, `${pkg.name} ${pkg.version}`)
    // DOMPurify keeps its copyright line in its build.
    if (pkg.name === 'dompurify') text = `${readFileSync(join(directory, 'dist/purify.es.mjs'), 'utf8').split('\n')[0]}\n\n${text}`
    entries.push({ name: `${pkg.name} ${pkg.version}`, text })
  }
  return entries
}

// The Go modules linked into the builds, each a folder, a package pattern and
// its platforms and build tags. A linked package that keeps a license of its
// own between it and its module's root — code a module carries from another
// project, as go-keyring 0.2.7 took shellescape in — has that text after the
// module's.
function goModules(builds) {
  const modules = new Map()
  const packages = new Map()
  for (const [cwd, pattern, goos, goarch, tags] of builds) {
    const listed = execFileSync('go', ['list', '-deps', ...(tags ? ['-tags', tags] : []), '-f',
      '{{.Dir}}\t{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}\t{{with .Replace}}{{.Path}}\t{{.Dir}}{{end}}{{end}}', pattern], {
      cwd, encoding: 'utf8', maxBuffer: 64 << 20,
      env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '1', GOFLAGS: '-mod=readonly' },
    })
    for (const line of listed.split('\n')) {
      const [packageDirectory, path, version, directory, replacement, replacedDirectory] = line.split('\t')
      if (!path || path === ownModule || path.startsWith(`${ownModule}/`)) continue
      const copy = replacement && (replacement.startsWith('.') || replacement.startsWith('/'))
      const name = copy ? `${path} (Leafen's modified copy, ${relative(product, replacedDirectory)})` : `${path} ${version}`
      modules.set(name, replacedDirectory || directory)
      if (!packages.has(name)) packages.set(name, new Set())
      packages.get(name).add(packageDirectory)
    }
  }
  return [...modules].sort(([left], [right]) => left.localeCompare(right))
    .map(([name, directory]) => {
      const own = readLicenses(directory, name)
      return { name, text: [own, ...carriedLicenses(directory, packages.get(name), name, own)].join('\n\n') }
    })
}

// The license texts kept in the folders from each package up to, not
// including, its module's root, each under the folder it covers.
function carriedLicenses(root, packageDirectories, label, own) {
  const folders = new Set()
  for (let directory of packageDirectories) {
    while (directory && directory !== root && relative(root, directory) && !relative(root, directory).startsWith('..')) {
      folders.add(directory)
      directory = dirname(directory)
    }
  }
  return [...folders].sort().flatMap((folder) => {
    if (!readdirSync(folder).some((name) => licenseFile.test(name) && !/logo/i.test(name) && !/\.(go|js|mjs|ts|py|rs|c|h)$/i.test(name))) return []
    const text = readLicenses(folder, `${label} ${relative(root, folder)}`)
    return text && text !== own ? [`${relative(root, folder)}:\n\n${text}`] : []
  })
}

// The libraries the Android app carries, by their Maven licenses.
function androidLibraries() {
  const javaHome = process.env.JAVA_HOME || '/opt/homebrew/opt/openjdk@21'
  const tree = execFileSync('./gradlew', ['-q', '--offline', ':app:dependencies', '--configuration', 'releaseRuntimeClasspath'], {
    cwd: join(product, 'desktop/build/android'), encoding: 'utf8', maxBuffer: 16 << 20,
    env: { ...process.env, JAVA_HOME: javaHome, PATH: `${javaHome}/bin:${process.env.PATH}` },
  })
  const coordinates = new Set()
  for (const match of tree.matchAll(/[+\\]--- ([\w.-]+):([\w.-]+):([\w.-]+)(?: -> ([\w.-]+))?/g)) {
    coordinates.add(`${match[1]}:${match[2]}:${match[4] ?? match[3]}`)
  }
  if (!coordinates.size) throw new Error('Gradle listed no Android libraries')
  const cache = join(homedir(), '.gradle/caches/modules-2/files-2.1')
  const pom = (group, artifact, version) => {
    const folder = join(cache, group, artifact, version)
    if (!existsSync(folder)) return ''
    for (const hash of readdirSync(folder)) {
      const file = join(folder, hash, `${artifact}-${version}.pom`)
      if (existsSync(file)) return readFileSync(file, 'utf8')
    }
    return ''
  }
  // Gradle's own cache cleanup drops the POMs of libraries it keeps the
  // binaries of (2026-11-08: 63 of them; 2026-11-22: all 73, those below
  // included), so an offline run can't read them. The licenses their POMs
  // name (or their parents' do), reviewed, by version: a new version is read
  // from its POM again.
  const reviewed = {
    'org.jetbrains:annotations:13.0': ['The Apache Software License, Version 2.0 (JetBrains java-annotations)'],
    'androidx.camera:camera-core:1.3.4': ['The Apache Software License, Version 2.0 http://www.apache.org/licenses/LICENSE-2.0.txt',
      'BSD License https://chromium.googlesource.com/libyuv/libyuv/+/refs/heads/main/README.chromium'],
    'com.google.auto.value:auto-value-annotations:1.6.3': ['Apache 2.0 http://www.apache.org/licenses/LICENSE-2.0.txt'],
    'com.google.zxing:core:3.5.3': ['The Apache Software License, Version 2.0 https://www.apache.org/licenses/LICENSE-2.0.txt'],
  }
  // The rest are from projects that publish every library under Apache-2.0
  // — reviewed: AndroidX, Material Components, Kotlin and kotlinx, Gson,
  // Tink, Error Prone and Guava. One that carries another's code (`carried`
  // below) needs its POM or a reviewed entry above.
  const apacheGroups = /^(androidx\.[\w.]+|com\.google\.android\.material|com\.google\.code\.gson|com\.google\.crypto\.tink|com\.google\.errorprone|com\.google\.guava|org\.jetbrains\.kotlinx?)$/u
  const licensesOf = (group, artifact, version, depth = 0) => {
    const text = pom(group, artifact, version)
    if (!text && reviewed[`${group}:${artifact}:${version}`]) return reviewed[`${group}:${artifact}:${version}`]
    if (!text && apacheGroups.test(group) && !carried[`${group}:${artifact}:${version}`]) {
      return [`The Apache Software License, Version 2.0 (${group}, reviewed by its project)`]
    }
    if (!text) throw new Error(`no POM in the Gradle cache: ${group}:${artifact}:${version}`)
    const block = text.match(/<licenses>([\s\S]*?)<\/licenses>/)?.[1]
    if (block) return [...block.matchAll(/<license>([\s\S]*?)<\/license>/g)].map(([, license]) => `${license.match(/<name>([^<]*)<\/name>/)?.[1] ?? ''} ${license.match(/<url>([^<]*)<\/url>/)?.[1] ?? ''}`)
    const parent = text.match(/<parent>([\s\S]*?)<\/parent>/)?.[1]
    if (!parent || depth > 5) return []
    const field = (name) => parent.match(new RegExp(`<${name}>([^<]*)</${name}>`))?.[1]
    return licensesOf(field('groupId'), field('artifactId'), field('version'), depth + 1)
  }
  // A library under Apache-2.0 that carries another's code under its own
  // license names it beside, reviewed: its text, from that project, is in
  // desktop/build/android/vendor-licenses.
  const carried = {
    'androidx.camera:camera-core:1.3.4': [{ license: /^BSD License https:\/\/chromium\.googlesource\.com\/libyuv\//u,
      name: 'libyuv (in androidx.camera:camera-core:1.3.4)', file: 'libyuv.LICENSE' }],
  }
  const apache = []
  const others = []
  for (const coordinate of [...coordinates].sort()) {
    const [group, artifact, version] = coordinate.split(':')
    const isApache = (license) => /apache/i.test(license) && /2\.0/.test(license)
    const licenses = licensesOf(group, artifact, version)
    const besides = licenses.filter((license) => !isApache(license))
    const known = besides.map((license) => (carried[coordinate] ?? []).find((each) => each.license.test(license)))
    if (licenses.length === besides.length || known.some((each) => !each)) {
      throw new Error(`review the license of ${coordinate}: ${licenses.join('; ') || 'none named'}`)
    }
    apache.push(coordinate)
    for (const each of known) {
      others.push({ name: each.name, text: readFileSync(join(product, 'desktop/build/android/vendor-licenses', each.file), 'utf8').trim() })
    }
  }
  return [{ name: apache.join('\n'), text: 'Licensed under the Apache License, Version 2.0 (its text is in the appendix).' }, ...others]
}

// The files internal/app embeds in a build with these tags, as paths under
// internal/app: an edition names only the libraries it carries.
function embeddedFiles(tags) {
  const listed = execFileSync('go', ['list', ...(tags ? ['-tags', tags] : []), '-f', '{{join .EmbedFiles "\\n"}}', './internal/app'], {
    cwd: product, encoding: 'utf8', maxBuffer: 64 << 20, env: { ...process.env, GOFLAGS: '-mod=readonly' },
  })
  return new Set(listed.split('\n').filter(Boolean))
}

// Libraries a screen the executable carries keeps in a vendor folder — a
// built-in app's (builtinapps/<app>/ui/vendor) or the file preview's
// (documentpreview/vendor) — each with its license: beside it as
// <name>-<version>-LICENSE.txt, or in its own <name>-<version>/ folder with
// its license files and those of what it bundles (any LICENSE…, NOTICE…,
// COPYING… or THIRD_PARTY… file, and every file under a licenses/ folder).
// Only those the build embeds are listed.
function vendoredLibraries(embedded, folders) {
  const root = join(product, 'internal/app')
  const carried = (path) => {
    const own = relative(root, path)
    return [...embedded].some((file) => file === own || file.startsWith(`${own}/`))
  }
  const apacheOnly = (text, license) => license === 'Apache-2.0' && /limitations under the License\.$/.test(text)
  const entries = []
  const add = (owner, name, version, label, path) => {
    const text = readFileSync(path, 'utf8').replace(/\r\n/g, '\n').trim()
    const license = licenseOf(text)
    if (!license) problems.push(`${relative(root, path)}: name its license before shipping it`)
    if (!carried(path)) return
    entries.push({ name: `${name} ${version} (${owner}${label ? `: ${label}` : ''})`,
      text: apacheOnly(text, license) ? 'Licensed under the Apache License, Version 2.0 (its text is in the appendix).' : text })
  }
  for (const { owner, vendor } of folders) {
    if (!existsSync(vendor)) continue
    for (const entry of readdirSync(vendor, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const named = entry.name.match(entry.isDirectory() ? /^(.+)-(\d[^-]*)$/ : /^(.+)-(\d[^-]*)-LICENSE\.txt$/)
      if (!named) {
        if (/LICENSE/i.test(entry.name) || entry.isDirectory()) problems.push(`${relative(root, vendor)}/${entry.name}: name its library and version`)
        continue
      }
      if (!entry.isDirectory()) {
        add(owner, named[1], named[2], '', join(vendor, entry.name))
        continue
      }
      const folder = join(vendor, entry.name)
      const found = []
      const walk = (directory, inLicenses) => {
        for (const item of readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
          const path = join(directory, item.name)
          if (item.isDirectory()) walk(path, inLicenses || item.name === 'licenses')
          else if (inLicenses || /^(licen[cs]e|notice|copying|third[_-]party)/i.test(item.name)) found.push(path)
        }
      }
      walk(folder, false)
      if (!found.length) problems.push(`${relative(root, vendor)}/${entry.name}: no license file`)
      for (const path of found) add(owner, named[1], named[2], relative(folder, path) === 'LICENSE' ? '' : relative(folder, path), path)
    }
  }
  return entries
}

// The built-in apps' screens' libraries.
function builtinAppLibraries(embedded) {
  const root = join(product, 'internal/app/builtinapps')
  const apps = readdirSync(root, { withFileTypes: true }).filter((entry) => entry.isDirectory()).map((entry) => entry.name).sort()
  return vendoredLibraries(embedded, apps.map((app) => ({ owner: `the ${app} app`, vendor: join(root, app, 'ui', 'vendor') })))
}

// The file preview's libraries, which draw Word, Excel and PowerPoint files.
function filePreviewLibraries(embedded) {
  return vendoredLibraries(embedded, [{ owner: 'the file preview', vendor: join(product, 'internal/app/documentpreview/vendor') }])
}

// Packages under the same license text are listed together.
function section(title, entries) {
  const groups = new Map()
  for (const { name, text } of entries) groups.set(text, [...(groups.get(text) ?? []), name])
  return `${'='.repeat(72)}\n${title}\n${'='.repeat(72)}\n\n` +
    [...groups].map(([text, names]) => `${names.join('\n')}\n\n${text}`).join(`\n\n${'-'.repeat(72)}\n\n`)
}

// Each edition's build embeds its own file (web/notices*.go), so listing the
// build needs the file there; a new one starts empty.
for (const output of Object.values(outputs)) {
  if (existsSync(output)) continue
  if (check) {
    console.error(`${relative(product, output)} is missing; run node scripts/generate-third-party-notices.mjs`)
    process.exit(1)
  }
  writeFileSync(output, '')
}

// The desktop module builds the desktop and phone apps; Linux, which has no
// desktop app, gets the server alone (cmd/leafen), as the Docker image does.
const desktopPlatforms = [['darwin', 'arm64'], ['darwin', 'amd64'], ['windows', 'amd64'], ['windows', 'arm64'], ['ios', 'arm64'], ['android', 'arm64']]
const serverPlatforms = [['linux', 'amd64'], ['linux', 'arm64']]
const programModules = (tags, extra = []) => {
  return goModules([
    ...[...desktopPlatforms.map(([goos, goarch]) => [goos, goarch, tags]), ...extra].map((platform) => [join(product, 'desktop'), '.', ...platform]),
    ...serverPlatforms.map(([goos, goarch]) => [product, './cmd/leafen', goos, goarch, tags]),
  ])
}
const embedded = { full: embeddedFiles(''), noapps: embeddedFiles('leafen_noapps') }
const screens = section('Leafen\'s screens (JavaScript packages bundled into the web client)', npmPackages(web))
const android = section('Leafen\'s Android app (Android libraries)', androidLibraries())
const appendix = section('Appendix: the Apache License, Version 2.0, which the entries above that name it refer to',
  [{ name: 'Apache License, Version 2.0', text: readFileSync(join(product, 'sdk/app/LICENSE'), 'utf8').trim() }])
const about = [
  'Leafen is licensed under AGPL-3.0-only. It is distributed with the software',
  'below, each under its own license, listed with the license text it comes',
  'with. Leafen batches DOM selection snapshots in the bundled @tiptap/react',
  'contentDOM attachment helper; DOMPurify is used under its Apache-2.0 option.',
  'The Go module github.com/reearth/ygo is Leafen\'s modified copy; its changes',
  'are listed in third_party/ygo/LEAFEN_UPSTREAM.md of Leafen\'s source.',
  'A device\'s country comes from DB-IP\'s IP to Country Lite (https://db-ip.com),',
  'under CC BY 4.0 (https://creativecommons.org/licenses/by/4.0/): IP Geolocation',
  'by DB-IP.',
]
const notices = {
  // Leafen is one program: its desktop and phone apps (the desktop module's main) and its Docker image (cmd/leafen).
  full: [
    'Third-party software notices for Leafen',
    '',
    ...about,
    '',
    screens,
    '',
    section('The Leafen program — its desktop and phone apps and its Docker image (Go modules)',
      programModules('')),
    '',
    section('Leafen\'s built-in apps (libraries in their screens, inside the program)', builtinAppLibraries(embedded.full)),
    '',
    section('Leafen\'s file preview (libraries in its screen, inside the program)', filePreviewLibraries(embedded.full)),
    '',
    android,
    '',
    appendix,
  ].join('\n') + '\n',
  // The program without the app platform: the phone apps, the Mac App
  // Store's edition and any build tagged leafen_noapps. It carries no app
  // runtime and, of the built-in apps, only the PDF reader its file preview
  // draws with.
  noapps: [
    'Third-party software notices for Leafen without the app platform',
    '',
    ...about,
    '',
    screens,
    '',
    section('The Leafen program without the app platform — its phone apps, its Mac App Store edition and any build tagged leafen_noapps (Go modules)',
      programModules('leafen_noapps', [['darwin', 'arm64', 'appstore,leafen_noapps'], ['darwin', 'amd64', 'appstore,leafen_noapps']])),
    '',
    section('Leafen\'s built-in apps (the libraries of their screens this edition carries, inside the program)', builtinAppLibraries(embedded.noapps)),
    '',
    section('Leafen\'s file preview (libraries in its screen, inside the program)', filePreviewLibraries(embedded.noapps)),
    '',
    android,
    '',
    appendix,
  ].join('\n') + '\n',
}

if (problems.length) {
  console.error(problems.join('\n'))
  process.exit(1)
}
for (const [edition, output] of Object.entries(outputs)) {
  if (!check) {
    writeFileSync(output, notices[edition])
  } else if (!existsSync(output) || readFileSync(output, 'utf8') !== notices[edition]) {
    console.error(`${relative(product, output)} is stale; run node scripts/generate-third-party-notices.mjs`)
    process.exitCode = 1
  }
}
