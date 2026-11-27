import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// The emoji picker's catalog, drawn from emojibase-data (MIT; Unicode CLDR
// names): every emoji up to the version Notion's picker offers, in Notion's
// picker groups and order, with its Korean and English names and search
// words and, where it has them, its five skin tones. Notion's English words
// beyond CLDR's are iamcal's emoji-data name and short names
// (emoji-datasource, MIT: "tornado", "tornado_cloud") and emojilib's name
// and keywords (MIT). Notion's picker finds by emojilib 3.0.0's (read
// 2026-11-16 by the words it finds); this is 4.x, which adds other
// platforms' short names to 1492 of the 1843 emoji, so an English filter
// finds more than Notion's — 24 of 59 common words typed 2026-11-26 ("cat"
// 103 to its 68, "ok" 85 to its 73), Korean ones the same: a difference
// approved 2026-11-26 (the user: the latest one). Each
// emoji's words are listed in the order Notion's emoji search ranks by (its
// code, read 2026-11-02, and its lists, 2026-11-16 and 2026-11-17): the
// Korean name and words, emoticons, iamcal's name, the short names, the
// English name and words, emoticons, emojilib's name, the few Notion has
// that no list here gives (NOTION_WORDS), then emojilib's other words — so
// a filter finds what Notion's finds in its order (typed 2026-11-17: "sun"
// 23, "rain" 26, "heart" 37, "ok" 73, "blue" 94, "tt" 166, "flag" 275,
// "man" 258, "t_" 1741, "o" 1732, "a" 1808, each in Notion's order). As
// Notion's own lists are (its Korean and English emoji words, read
// 2026-11-16), the Korean words are the name, its words in the root
// collation's order (a jamo word such as "ㅎㅎ" among the syllables it
// spells, Latin first) and the emoticons; the English ones, kept as its
// search reads a filter ("upside down face", "- -"), repeat the emoticons
// after the English words; each list holds a word once. The picker loads it only
// when it opens.
//
// Usage: node scripts/generate-emoji-catalog.mjs [--check]

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = resolve(productRoot, 'web/node_modules/emojibase-data')
const wordsRoot = resolve(productRoot, 'web/node_modules/emojilib')
const iamcalRoot = resolve(productRoot, 'web/node_modules/emoji-datasource')
const output = resolve(productRoot, 'web/src/emojiCatalog.generated.json')

// Emoji newer than this are left out, as Notion's picker has none (seen
// 2026-11-02: no 🩷, 🫨 or 🐦‍🔥).
const MAX_VERSION = 14
// Notion's groups (seen 2026-09-30): people with the smileys, then nature,
// food, activities, travel, objects, symbols and flags.
const GROUPS = ['people', 'nature', 'food', 'activity', 'travel', 'objects', 'symbols', 'flags']
const GROUP_OF = { 0: 0, 1: 0, 3: 1, 4: 2, 6: 3, 5: 4, 7: 5, 8: 6, 9: 7 }
// Notion's picker lists the emoji in Unicode 14.0's order (its emoji-test.txt),
// which later Unicode versions, and emojibase-data with them, changed: seen
// 2026-11-16 in its unfiltered page icon picker (two tabs), the kiss mark
// opens the hearts, the bomb follows the hole, the family follows the couple
// with heart, the mushroom, crab, lobster, shrimp, squid and oyster are food
// after the takeout box, the nazar amulet and the hamsa are activities after
// the magic wand, the trumpet follows the piano, the money bag the label and
// the water pistol is an object after the crossed swords. Each moves in turn
// to just after the emoji named, into its group, so that each group holds
// what Notion's does (people 518, nature 144, food 132, activities 86,
// objects 255) in its order.
const UNICODE_14_ORDER = [
  ['💋', '🙊'], ['💣', '🕳'], ['👪', '💑'], ['🍄', '🧅'], ['🦀', '🥡'], ['🦞', '🦀'], ['🦐', '🦞'], ['🦑', '🦐'],
  ['🦪', '🦑'], ['🧿', '🪄'], ['🪬', '🧿'], ['🎺', '🎹'], ['💰', '🏷'], ['🔫', '⚔'],
]
// The catalog carries its sources' notices, as their licenses ask of copies.
const NAMES_NOTICE = 'Emoji names and search words come from the Unicode CLDR annotations, '
  + 'Copyright © 1991-2025 Unicode, Inc., under the Unicode License v3 (https://www.unicode.org/license.txt).'

const wordsNotice = (version, iamcalVersion) => `English names and short names come from iamcal's emoji-data (emoji-datasource ${iamcalVersion}), `
  + 'Copyright (c) 2013 Cal Henderson, under the MIT License; more English search words '
  + `come from emojilib ${version}, Copyright (c) 2014 Mu-An Chiou, under the MIT License.`

function read(locale) {
  return JSON.parse(readFileSync(resolve(sourceRoot, locale, 'data.json'), 'utf8'))
}

// A kiss or a couple with a heart naming its two people (👩‍❤️‍👨) is left out,
// 💏 and 💑 kept, as Notion's picker (seen 2026-11-02); families stay.
function namesTheCouple(emoji) {
  return emoji.includes('\u2764') && /[\u{1F468}\u{1F469}]/u.test(emoji)
}

// emojilib's name and keywords for each emoji, by the emoji without U+FE0F.
// Notion's lists hold the name after the English words and emoticons, not
// first (read 2026-11-17: "woman s hat" for 👒, "plus sign" for ➕, "a
// button" for 🅰️, "man beard" for 🧔, which "man" finds); an emoji
// emojilib doesn't have has none there.
function libWords() {
  const data = JSON.parse(readFileSync(resolve(wordsRoot, 'dist/emoji-en-US.json'), 'utf8'))
  return new Map(Object.entries(data).map(([emoji, [name, ...keywords]]) => [emoji.replaceAll('\uFE0F', ''), { name, keywords }]))
}

// Search words Notion's picker finds these emoji by that no public list here
// gives them, Leafen's own table (measured 2026-11-16 in Notion's page icon
// picker, Chrome, by the words typed: "qq" finds 😿😢😭🥲 and "tt" and "t_t"
// find them among others; "coeur" finds eleven hearts while "amour", "herz"
// and "corazon" find no heart; "oof" finds 😣😰😥😓😅😐😑😒 besides the words
// holding it, "merp" 😶😓😅😐😑 besides the merperson, "neutral" 😐😶😓😅😑
// besides the gender-neutral people, "lmao" 💀🤪😹🤣😂🫠😝😆😜 and "rofl"
// 🤪😹💀🤣😂🫠😆😝😜 besides the helicopter and Montenegro; 2026-11-17:
// "._." finds 😶😓😅😐😑, "lol" 💀🤪😝😜 among the words holding it,
// "yellow" and "orange" ⚠️ among the yellow and orange things). They come
// after the English words and emojilib's name and before emojilib's other
// words, where Notion's lists hold them: "love" finds 🧡 behind its "coeur",
// "indifferent" 🤷‍♂️ and 🤷‍♀️ before 😑, whose emojilib words follow four of
// these, and each of the words above finds its emoji in Notion's order.
const NOTION_WORDS = new Map([
  ...['😿', '😢', '😭', '🥲'].map((emoji) => [emoji, ['t_t', 'tt', 'qq']]),
  ...['🧡', '🤎', '🤍', '🖤', '❤', '💜', '♥', '💛', '💚', '💙', '💕'].map((emoji) => [emoji, ['coeur']]),
  ...['😑', '😓', '😅', '😐'].map((emoji) => [emoji, ['oof', '._.', 'merp', 'neutral']]),
  ...['😣', '😰', '😥', '😒'].map((emoji) => [emoji, ['oof']]),
  ['😶', ['._.', 'merp', 'neutral']],
  ...['🫠', '😹'].map((emoji) => [emoji, ['rofl', 'lmao']]),
  ...['🤪', '😝', '😜'].map((emoji) => [emoji, ['rofl', 'lmao', 'lol']]),
  ['💀', ['rofl', 'lol']],
  ['😆', ['lmao']],
  ['⚠', ['!', 'yellow', 'orange']],
])

// More of Leafen's own table, typed 2026-11-17 in Notion's page icon picker:
// words Notion's lists give right after an emoji's name ("checl", "cehck",
// "chekc" and "todo" find ☑️ alone, "false" ❌ and 👎, "image" 🖼️, "usa"
// 🇺🇸 first), the one they give before it (❤️'s "red heart", which ranks it
// behind ❤️‍🩹 for "heart"), emoticons their English lists add (":-)" finds
// 🙂 and 😊) and a word they leave out ("pride" finds no 😤).
const NOTION_NAMES = new Map([
  ['☑', ['todo', 'checl', 'cehc', 'checj', 'checkl', 'chekc', 'cehck']],
  ...['❌', '👎'].map((emoji) => [emoji, ['false']]),
  ['🖼', ['image']],
  ['🇺🇸', ['usa']],
])
const NOTION_FIRST = new Map([['❤', 'red heart']])
const NOTION_EMOTICONS = new Map([['😊', [':)', ':-)']], ['🙂', [':-)']]])
const NOTION_LEFT_OUT = new Map([['😤', ['pride']]])

// What Notion's lists keep where emojibase-data 18 (CLDR 49) changed an
// English word: they still say CLDR 48's — typed 2026-11-26 in its page icon
// picker, "bang" finds 🇧🇩‼️⁉️ but none of ❣️❕❗, which 18 gives "bang", and
// "antarctic" 🇦🇶 and 🐧 but not 🇹🇫, which 18 names "French Southern and
// Antarctic Lands". So these three flags keep 17's English names, and the
// three marks go without "bang" (emojilib 4's own words still add 💥 to
// "bang" and 🇹🇫 to "antarctic", within the difference approved above).
const NOTION_ENGLISH_NAMES = new Map([
  ['🇭🇲', 'flag: Heard & McDonald Islands'], ['🇸🇭', 'flag: St. Helena'], ['🇹🇫', 'flag: French Southern Territories'],
])
for (const mark of ['❣', '❕', '❗']) NOTION_LEFT_OUT.set(mark, ['bang'])

// emojibase's English entry as Notion's lists have it: the names and words above.
function notionEnglish(en) {
  if (!en) return en
  const plain = en.emoji.replaceAll('\uFE0F', '')
  const leftOut = NOTION_LEFT_OUT.get(plain) ?? []
  return { ...en, label: NOTION_ENGLISH_NAMES.get(plain) ?? en.label, tags: (en.tags ?? []).filter((tag) => !leftOut.includes(tag)) }
}

// iamcal's name and short names for each emoji (emoji-datasource), by its
// code points without U+FE0F. The name a list first gives an emoji is
// iamcal's, as Notion's lists name them (read 2026-11-16, and typed
// 2026-11-17 in its picker): mostly the character's Unicode name ("white
// smiling face", "heavy black heart", "white heavy check mark"), but
// Unicode 7's weather, tools and signs by their CLDR names — "white sun"
// and "cloud with tornado" find nothing, "sun behind small cloud" and
// "tornado" 🌤️ and 🌪️, "permanent" no ♾️, "stroke" no ⚧ — a country's
// or region's flag after it ("japan flag"), another sequence by its English
// name ("family: woman, boy"), a keycap as "keycap 1" or "hash key".
function iamcalNames() {
  const data = JSON.parse(readFileSync(resolve(iamcalRoot, 'emoji.json'), 'utf8'))
  return new Map(data.map((entry) => [entry.unified.replaceAll('-FE0F', ''), { name: entry.name?.toLowerCase(), shortNames: entry.short_names }]))
}

// Notion's emoticons are emojibase's, except that ":D" is 😀's rather than
// 😄's (its Korean and English words, read 2026-11-16).
const EMOTICON_OWNERS = new Map([[':D', '1F600']])
const rootOrder = new Intl.Collator('en')

function emoticonsOf(entry) {
  const own = (Array.isArray(entry.emoticon) ? entry.emoticon : entry.emoticon ? [entry.emoticon] : [])
    .filter((emoticon) => (EMOTICON_OWNERS.get(emoticon) ?? entry.hexcode) === entry.hexcode)
  const given = [...EMOTICON_OWNERS].filter(([, owner]) => owner === entry.hexcode).map(([emoticon]) => emoticon)
  return [...own, ...given.filter((emoticon) => !own.includes(emoticon))]
}

// A word as Notion's lists keep it, and as its search reads a filter
// (emojiSearchText in emojiCatalogModel.ts): lowercase, each "_" a space, a
// "-" before a letter a space ("upside down face"), one ending a word after
// a letter or number dropped, spaces run together ("-_-": "- -").
function searchText(word) {
  return word.toLowerCase().replace(/_/g, ' ').replace(/([a-z0-9])-$/g, '$1').replace(/-([a-z])/g, ' $1')
    .replace(/\s+/g, ' ').trim()
}

// Words as Notion's lists keep them, each once, in their order.
function eachOnce(words) {
  return [...new Set(words.filter(Boolean).map(searchText).filter(Boolean))]
}

// A flag's region code, which emojibase gives a country's or region's flag
// as a word ("AC" for 🇦🇨, "gbeng" for 🏴󠁧󠁢󠁥󠁮󠁧󠁿): its regional indicator or tag
// letters.
function regionCode(entry) {
  return String.fromCodePoint(...entry.hexcode.split('-').map((hex) => Number.parseInt(hex, 16)).flatMap((point) => {
    if (point >= 0x1F1E6 && point <= 0x1F1FF) return [point - 0x1F1E6 + 0x61]
    if (point >= 0xE0061 && point <= 0xE007A) return [point - 0xE0000]
    return []
  }))
}

// An emoji's words in the root collation's order, a flag's region code left
// out as Notion's lists leave it (read 2026-11-17; typed: "tt" finds 🇹🇹
// beside 🇰🇳 and 🇾🇹 in their order, "flag" every country's flag but the
// ten with a short name of their own ("jp") at one rank).
function tagsOf(entry) {
  const code = regionCode(entry)
  return (entry.tags ?? []).filter((tag) => !code || tag.toLowerCase() !== code).sort(rootOrder.compare)
}

// An emoji's search words in their ranking order: its Korean list, then its
// English one — iamcal's name and short names (around them the few names
// NOTION_FIRST and NOTION_NAMES give), the English name, its words in the
// root collation's order ("o’clock" before "one", as Notion's), the
// emoticons, emojilib's name, Notion's own words, then emojilib's other
// words. A short name reads as a filter does ("tornado cloud", "flag us",
// and "-1" kept).
function searchWords(entry, en, iamcal, lib) {
  const plain = entry.emoji.replaceAll('\uFE0F', '')
  const emoticons = emoticonsOf(entry)
  const korean = [entry.label, ...tagsOf(entry), ...emoticons]
  const leftOut = NOTION_LEFT_OUT.get(plain) ?? []
  const english = [
    NOTION_FIRST.get(plain), iamcal?.name, ...(NOTION_NAMES.get(plain) ?? []), ...(iamcal?.shortNames ?? []),
    en.label, ...tagsOf(en), ...emoticons, ...(NOTION_EMOTICONS.get(plain) ?? []), lib?.name,
    ...(NOTION_WORDS.get(plain) ?? []), ...(lib?.keywords ?? []).filter((word) => !leftOut.includes(word)),
  ]
  return [...eachOnce(korean), ...eachOnce(english)]
}

// emojibase writes U+FE0F after emoji that show as emoji anyway (👍️).
// Unicode's fully qualified form (emoji-test.txt), which reactions and icons
// stored elsewhere use, keeps it only where the character alone shows as
// text (❤️), so a pick matches the same emoji from anywhere else.
function qualified(emoji) {
  return emoji.replace(/(\p{Emoji_Presentation})️/gu, '$1')
}

function catalog() {
  const version = JSON.parse(readFileSync(resolve(sourceRoot, 'package.json'), 'utf8')).version
  const license = readFileSync(resolve(sourceRoot, 'LICENSE'), 'utf8').trim()
  const wordsLicense = readFileSync(resolve(wordsRoot, 'LICENSE'), 'utf8').trim()
  const wordsVersion = JSON.parse(readFileSync(resolve(wordsRoot, 'package.json'), 'utf8')).version
  const english = new Map(read('en').map((entry) => [entry.hexcode, entry]))
  const korean = read('ko')
  const keywords = libWords()
  const iamcal = iamcalNames()
  const iamcalVersion = JSON.parse(readFileSync(resolve(iamcalRoot, 'package.json'), 'utf8')).version
  const iamcalLicense = readFileSync(resolve(iamcalRoot, 'LICENSE'), 'utf8').trim()
  const rows = []
  for (const entry of korean) {
    const group = GROUP_OF[entry.group]
    if (group === undefined || entry.version > MAX_VERSION || namesTheCouple(entry.emoji)) continue
    const en = notionEnglish(english.get(entry.hexcode))
    if (!en) throw new Error(`no English entry for ${entry.hexcode}`)
    const tones = (entry.skins ?? [])
      .filter((skin) => typeof skin.tone === 'number' && skin.version <= MAX_VERSION)
      .sort((left, right) => left.tone - right.tone)
    const row = [
      qualified(entry.emoji), group, entry.label, en.label,
      searchWords(entry, en, iamcal.get(entry.hexcode.replaceAll('-FE0F', '')), keywords.get(entry.emoji.replaceAll('\uFE0F', ''))),
    ]
    if (tones.length === 5 && tones.every((skin, index) => skin.tone === index + 1)) {
      row.push(tones.map((skin) => qualified(skin.emoji)))
    }
    rows.push({ group, order: entry.order, row })
  }
  rows.sort((left, right) => left.group - right.group || left.order - right.order)
  const plain = ({ row }) => row[0].replaceAll('\uFE0F', '')
  for (const [emoji, after] of UNICODE_14_ORDER) {
    const from = rows.findIndex((entry) => plain(entry) === emoji)
    if (from < 0) throw new Error(`no ${emoji} to move`)
    const [moved] = rows.splice(from, 1)
    const to = rows.findIndex((entry) => plain(entry) === after)
    if (to < 0) throw new Error(`no ${after} to move ${emoji} after`)
    moved.group = moved.row[1] = rows[to].group
    rows.splice(to + 1, 0, moved)
  }
  const lines = rows.map(({ row }) => JSON.stringify(row))
  const head = `"source":"emojibase-data@${version}","license":${JSON.stringify(license)},"names":${JSON.stringify(NAMES_NOTICE)}`
    + `,"words":${JSON.stringify(wordsNotice(wordsVersion, iamcalVersion))},"wordsLicense":${JSON.stringify(wordsLicense)}`
    + `,"namesLicense":${JSON.stringify(iamcalLicense)}`
  return `{${head},"maxVersion":${MAX_VERSION},"groups":${JSON.stringify(GROUPS)},"emoji":[\n${lines.join(',\n')}\n]}\n`
}

const generated = catalog()
if (process.argv.includes('--check')) {
  let current = ''
  try {
    current = readFileSync(output, 'utf8')
  } catch {
    // Missing output fails below.
  }
  if (current !== generated) {
    console.error(`${relative(productRoot, output)} is stale; run node scripts/generate-emoji-catalog.mjs`)
    process.exit(1)
  }
} else {
  writeFileSync(output, generated)
  console.log(`wrote ${relative(productRoot, output)}`)
}
