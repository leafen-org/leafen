import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, unlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// Every time zone list names each zone by its place in the reader's language
// ("서울", "뉴욕"). Browsers have no such names, so they come from the Unicode
// CLDR (user decisions 2026-09-30 and 2026-11-21): for every language Leafen's
// screens speak, one small table of zone to city, loaded only when a zone list
// is about to open, with "UTC" named by CLDR's names for it in the form that
// language's list writes it (utcName). A zone a table
// lacks keeps the name its ID gives, as CLDR's root does. The lists read the
// places of English, Korean, Arabic and Hebrew at once, without the table
// (locales/<language>/timeZones.json), written here from the same tables.
//
// The CLDR package isn't one of the web app's dependencies: npm would then
// fetch it, and the number data it asks for beside it, on every install.
// Writing the tables unpacks the pinned package instead (from npm's cache when
// it is there); checking them needs nothing but the tables and their manifest.
//
// Usage: node scripts/generate-zone-city-names.mjs --write [--from <package dir or .tgz>]
//        node scripts/generate-zone-city-names.mjs --check [--from <package dir or .tgz>]

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const localesRoot = resolve(productRoot, 'web/src/locales')
const outputRoot = resolve(productRoot, 'web/src/components/database/zoneCities')
const manifestPath = resolve(outputRoot, 'manifest.json')

const PACKAGE = 'cldr-dates-full'
const VERSION = '48.2.0'
const INTEGRITY = 'sha512-zMrKvjMO414HDftLr6vKH1fNQHakFNe+F1h4S0eg+zmL4WRi9pI41fxB3UrTJDY42MEM87jCuAw/qlqCgYKcAQ=='
// Each table carries its source's notice, as the Unicode License asks of copies.
const NOTICE = `Time zone city names come from the Unicode CLDR (${PACKAGE} ${VERSION}), `
  + 'Copyright © 2004-2026 Unicode, Inc., under the Unicode License v3 (https://www.unicode.org/license.txt).'

// CLDR's locale for each of Leafen's languages, parents first: Brazilian
// Portuguese is CLDR's "pt", which European Portuguese builds on.
const CLDR_LOCALES = {
  'pt-BR': ['pt'],
  'pt-PT': ['pt', 'pt-PT'],
  'zh-Hans': ['zh'],
  'zh-Hant': ['zh-Hant'],
}

// The languages whose places the lists read at once (locales/<language>/
// timeZones.json), written here from their table rather than translated.
// English and Korean name every zone the runtime knows, and "UTC", as their
// table does, else by the name its ID gives (the zones English names are
// every list's keys). Arabic and Hebrew name those of English's zones their
// table names (2026-11-09); the rest of a zone list reads its table.
const OWN_PLACE_LANGUAGES = ['en', 'ko']
const CLDR_PLACE_LANGUAGES = ['ar', 'he']

// The name a zone's ID gives, as CLDR's root names a zone it has no city for:
// "America/Argentina/La_Rioja" is "La Rioja".
function ianaPlace(zone) {
  return (zone.split('/').at(-1) ?? zone).replace(/_/gu, ' ')
}

// The zones English's places are written for: every zone the runtime that
// writes them knows (Node's ICU, CLDR's IDs: "Asia/Calcutta"), and "UTC",
// which it does not list and the lists offer.
function runtimeZones() {
  return [...Intl.supportedValuesOf('timeZone').filter((zone) => !zone.includes('.') && !zone.includes(':')), 'UTC']
}

function leafenLanguages() {
  return readdirSync(localesRoot)
    .filter((name) => name.endsWith('.ts') && statSync(resolve(localesRoot, name)).isFile())
    .map((name) => name.slice(0, -3))
    .sort()
}

function sha256(text) {
  return createHash('sha256').update(text).digest('hex')
}

function stableJSON(value) {
  return `${JSON.stringify(value, null, 2)}\n`
}

function cldrLocales() {
  return [...new Set(leafenLanguages().flatMap((language) => CLDR_LOCALES[language] ?? [language]))]
}

// The pinned package's directory: the one given, or the package unpacked from
// npm (its cache first) after its integrity is checked, with only the files
// the tables read.
function packageRoot(from, scratch) {
  if (from && statSync(from).isDirectory()) return from
  let tarball = from
  if (!tarball) {
    const packed = JSON.parse(execFileSync('npm', ['pack', `${PACKAGE}@${VERSION}`, '--prefer-offline', '--json', '--pack-destination', scratch],
      { cwd: scratch, encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] }))
    tarball = resolve(scratch, packed[0].filename)
  }
  const integrity = `sha512-${createHash('sha512').update(readFileSync(tarball)).digest('base64')}`
  if (integrity !== INTEGRITY) throw new Error(`${tarball} is not ${PACKAGE}@${VERSION} (${integrity})`)
  const entries = ['package/package.json', ...cldrLocales().map((locale) => `package/main/${locale}/timeZoneNames.json`)]
  execFileSync('tar', ['-xzf', tarball, '-C', scratch, ...entries], { stdio: 'inherit' })
  return resolve(scratch, 'package')
}

// "UTC" in each language as Notion's zone lists write it there, in CLDR's
// words (Notion's lists, read 2026-11-25): by CLDR's short name, "UTC", as
// English and most of them do; by CLDR's long name alone in the languages
// whose list spells it out (Japanese, Chinese, Arabic); in Korean by both,
// the long name in the language's own parentheses, CLDR's fallbackFormat
// ("{1}({0})": "UTC(협정 세계시)"). A language Notion's lists don't speak
// takes the short name, as most of them write it.
const UTC_SPELLED_OUT = ['ar', 'ja', 'zh-Hans', 'zh-Hant']
const UTC_GLOSSED = ['ko']

function utcName(language, { short, long, fallbackFormat }) {
  const abbreviation = short || 'UTC'
  if (UTC_SPELLED_OUT.includes(language) && long) return long
  if (UTC_GLOSSED.includes(language) && long && fallbackFormat) {
    return fallbackFormat.replace('{1}', abbreviation).replace('{0}', long)
  }
  return abbreviation
}

const named = (value) => (typeof value === 'string' && value ? value : undefined)

// Every zone's exemplar city in one CLDR locale, keyed by CLDR's zone ID, and
// what the locale says of UTC: its short and long names and the form it
// writes a name with another in parentheses (fallbackFormat).
function exemplarCities(root, locale) {
  const path = resolve(root, 'main', locale, 'timeZoneNames.json')
  if (!existsSync(path)) throw new Error(`${PACKAGE} has no ${locale} time zone names`)
  const names = JSON.parse(readFileSync(path, 'utf8')).main[locale].dates.timeZoneNames
  const zones = names.zone
  const cities = {}
  const utc = {
    short: named(zones.Etc?.UTC?.short?.standard),
    long: named(zones.Etc?.UTC?.long?.standard),
    fallbackFormat: named(names.fallbackFormat),
  }
  const walk = (node, prefix) => {
    for (const [key, value] of Object.entries(node)) {
      if (!value || typeof value !== 'object') continue
      const id = prefix ? `${prefix}/${key}` : key
      if (typeof value.exemplarCity === 'string') {
        if (id !== 'Etc/Unknown') cities[id] = value.exemplarCity
      } else if (value._type !== 'zone') {
        walk(value, id)
      }
    }
  }
  walk(zones, '')
  return { cities, utc }
}

// A place language's list (locales/<language>/timeZones.json), keyed by
// English's zones in their order: English's and Korean's every zone, as their
// table names it, else as its ID; Arabic's and Hebrew's the zones their table
// names.
function placesOf(language, tableText, zones) {
  const cities = JSON.parse(tableText).cities
  const named = OWN_PLACE_LANGUAGES.includes(language) ? zones : zones.filter((zone) => zone in cities)
  return stableJSON(Object.fromEntries(named.map((zone) => [zone, cities[zone] ?? ianaPlace(zone)])))
}

function englishZones() {
  return Object.keys(JSON.parse(readFileSync(resolve(localesRoot, 'en', 'timeZones.json'), 'utf8')))
}

function tables(root) {
  const version = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8')).version
  if (version !== VERSION) throw new Error(`${PACKAGE} ${version} is not the pinned ${VERSION}`)
  const result = {}
  for (const language of leafenLanguages()) {
    const merged = {}
    const utc = {}
    for (const locale of CLDR_LOCALES[language] ?? [language]) {
      const found = exemplarCities(root, locale)
      Object.assign(merged, found.cities)
      for (const [part, value] of Object.entries(found.utc)) if (value) utc[part] = value
    }
    merged.UTC = utcName(language, utc)
    const cities = Object.fromEntries(Object.keys(merged).sort().map((zone) => [zone, merged[zone]]))
    result[language] = stableJSON({ notice: NOTICE, cities })
  }
  return result
}

function manifestOf(written) {
  return stableJSON({
    source: `${PACKAGE}@${VERSION}`,
    integrity: INTEGRITY,
    tables: Object.fromEntries(Object.entries(written).map(([language, text]) => [language, sha256(text)])),
  })
}

function withPackage(from, use) {
  const scratch = mkdtempSync(resolve(tmpdir(), 'leafen-zone-cities-'))
  try {
    return use(packageRoot(from && resolve(from), scratch))
  } finally {
    rmSync(scratch, { recursive: true, force: true })
  }
}

function write(from) {
  const written = withPackage(from, tables)
  mkdirSync(outputRoot, { recursive: true })
  for (const name of readdirSync(outputRoot)) {
    if (name.endsWith('.json') && name !== 'manifest.json' && !(name.slice(0, -5) in written)) unlinkSync(resolve(outputRoot, name))
  }
  for (const [language, text] of Object.entries(written)) writeFileSync(resolve(outputRoot, `${language}.json`), text)
  writeFileSync(manifestPath, manifestOf(written))
  const zones = runtimeZones()
  for (const language of [...OWN_PLACE_LANGUAGES, ...CLDR_PLACE_LANGUAGES]) {
    writeFileSync(resolve(localesRoot, language, 'timeZones.json'), placesOf(language, written[language], zones))
  }
  console.log(`Wrote ${Object.keys(written).length} time zone city tables from ${PACKAGE}@${VERSION}.`)
}

// Every language has its table, as the manifest pins it; with the package at
// hand, the tables are also what it gives now.
function check(from) {
  if (!existsSync(manifestPath)) throw new Error('zoneCities/manifest.json is missing; run --write')
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
  const languages = leafenLanguages()
  const listed = Object.keys(manifest.tables ?? {}).sort()
  if (manifest.source !== `${PACKAGE}@${VERSION}` || manifest.integrity !== INTEGRITY
    || JSON.stringify(listed) !== JSON.stringify(languages)) {
    throw new Error('zoneCities/manifest.json does not list one table per language from the pinned package; run --write')
  }
  const found = readdirSync(outputRoot).filter((name) => name.endsWith('.json') && name !== 'manifest.json').map((name) => name.slice(0, -5)).sort()
  if (JSON.stringify(found) !== JSON.stringify(languages)) throw new Error('zoneCities holds tables for other languages; run --write')
  for (const language of languages) {
    const text = readFileSync(resolve(outputRoot, `${language}.json`), 'utf8')
    const table = JSON.parse(text)
    if (sha256(text) !== manifest.tables[language] || table.notice !== NOTICE
      || Object.values(table.cities ?? {}).some((city) => typeof city !== 'string' || !city)) {
      throw new Error(`zoneCities/${language}.json is not the table the manifest pins; run --write`)
    }
  }
  const zones = existsSync(resolve(localesRoot, 'en', 'timeZones.json')) ? englishZones() : []
  if (!zones.includes('UTC')) throw new Error('locales/en/timeZones.json does not name UTC; run --write')
  for (const language of [...OWN_PLACE_LANGUAGES, ...CLDR_PLACE_LANGUAGES]) {
    const placesPath = resolve(localesRoot, language, 'timeZones.json')
    const table = readFileSync(resolve(outputRoot, `${language}.json`), 'utf8')
    if (!existsSync(placesPath) || readFileSync(placesPath, 'utf8') !== placesOf(language, table, zones)) {
      throw new Error(`locales/${language}/timeZones.json is not its CLDR table's places; run --write`)
    }
  }
  if (from) {
    const expected = withPackage(from, tables)
    for (const language of languages) {
      if (readFileSync(resolve(outputRoot, `${language}.json`), 'utf8') !== expected[language]) {
        throw new Error(`zoneCities/${language}.json is stale against ${PACKAGE}@${VERSION}; run --write`)
      }
    }
  }
  console.log(`Time zone city tables verified: ${languages.length} languages.`)
}

const mode = process.argv[2]
const fromIndex = process.argv.indexOf('--from')
const from = fromIndex > 0 ? process.argv[fromIndex + 1] : undefined
if (mode === '--write') write(from)
else if (mode === '--check') check(from)
else throw new Error('Usage: node scripts/generate-zone-city-names.mjs --write|--check [--from <package dir or .tgz>]')
