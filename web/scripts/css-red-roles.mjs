// Red has its roles, read with Notion's own red scale (its theme rules, read
// 2026-11-24 in Chrome: redTexAccPri, redIcoAccPri and redBacAccPri #e56458 in
// both themes, redTexSec #cf5148, redIcoSec #cf5148 light and #d0988d dark):
//
// - A failure — its words, a wrong field's ring, an error box, a required mark,
//   a lost connection's dot — takes --danger (#e56458 in both themes, as
//   Notion writes a formula's problem and an automation's refusal). Its soft
//   ground is a mix of it (--danger-wash, color-mix(… var(--danger) …)) or
//   Notion's soft red tint where Notion was seen drawing one.
// - A dangerous action takes the danger tokens: a menu row --menu-danger, a
//   filled answer --danger-fill under --danger-fill-text, an outlined one
//   --danger-outline-text, a soft red button --danger-soft-*; never the
//   failure red nor the palette's.
// - Text, block and data colours keep the palette: --c-red, --c-accent-red,
//   --c-bg-red, the option, chart and icon tokens, the red scale's steps
//   (app/redScale.css), an unread mark's --unread-mark.
//
// So no screen writes a red out: a red is a token's value in the token sheets
// (src/app/*.css). A rule that picks a tone by its name ([data-color="red"],
// [data-tone="red"]) is a palette's row and keeps its tone beside its sibling
// tones; the pictures and borrowed looks below keep theirs, each with why. A
// rule named for a failure (error, invalid, wrong, required, missing …) draws
// its words and lines in --danger, one named for a dangerous action (danger,
// delete, remove, discard …) in the danger tokens; the exceptions below say
// where Notion was seen doing otherwise. A red token nothing sets is refused
// even with a fallback, which then draws instead (2026-11-24: --text-red).

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

// A red written out where it belongs to a picture or a borrowed look: [file,
// the rule's selector (CSS) or the line's words (scripts), why].
const KEPT = [
  ['components/editor/codeTheme.css', '', "a code block's syntax colours, Notion's code theme token by token"],
  ['components/agentPasteGuide/AgentPasteGuide.module.css', '.terminal', "a picture of an agent's terminal in that terminal's own palette"],
  ['components/guide/GuideParts.module.css', '.trafficLights', "a picture of a Mac window's buttons"],
  ['components/remoteAccess/TailscaleAddressGuidePages.module.css', '.providerIcon[data-provider=', "a sign-in provider's own mark (Google's, Microsoft's)"],
  ['components/editor/SlashTipPreview.module.css', '', "the '/' menu's preview pictures, drawn in the colours of Notion's"],
  ['components/database/FormulaCode.module.css', '[data-formula-problem]', "Notion's formula editor marks a problem under a pure red wave (seen 2026-11-03)"],
  ['components/database/chartExport.ts', '', "an exported chart picture's grounds, drawn on its canvas in Notion's picture colours"],
  ['components/developers/ConnectionIcon.tsx', 'MARK_COLORS', "a connection's letter mark in Notion's seven mark colours, the same in both themes"],
  ['components/sidebar/MembershipAskPicture.tsx', 'const RED', "Leafen's picture over a guest's ask, its marks the same fills in both themes"],
  ['lib/covers.ts', 'linear-gradient', 'cover values kept in pages, never changed once given'],
  ['lib/covers.ts', '"#e6a2a2"', 'cover values kept in pages, never changed once given'],
]
// A failure drawn in another red where Notion draws it so: [file, selector, why].
const FAILURE_ELSEWHERE = [
  ['components/database/FormulaEditor.module.css', '.errorPlace', "where a formula's problem is, in Notion's redTexSec after its words (seen 2026-11-14)"],
  ['components/automation/AutomationWebhookVariables.module.css', '.error', "a webhook address's note, in Notion's pale redIcoSec (seen 2026-10-24)"],
  ['components/database/DatabaseTaskConvertDialog.module.css', '.required', "the task conversion window's required mark, in the older red Notion writes out there (seen 2026-11-18)"],
  ['components/database/DatabaseTaskDatabaseScreen.module.css', '.required', 'the task database screen asks as the conversion window does'],
]

const FAILURE = /error|invalid|fail|wrong|required|missing|refus|problem|warn|gone/iu
const DANGER = /danger|destruct|delete(?!d)|remove(?!d)|discard/iu
const LINES = /^(?:color|border(?:-[\w-]+)?|outline(?:-color)?|box-shadow|fill|stroke|text-decoration(?:-color)?|caret-color|accent-color|-webkit-text-fill-color|--[\w-]+)$/u
const NAMED_RED = /(?<![\w-])(?:red|darkred|crimson|firebrick|indianred|orangered|tomato)(?![\w-])/giu
const LITERAL = /(?<![&\w])#(?:[0-9a-f]{8}|[0-9a-f]{6}|[0-9a-f]{3,4})(?![\w-])|\b(?:rgba?|hsla?)\(\s*[\d.]+[^)]*\)/giu

function hsl(r, g, b) {
  const [R, G, B] = [r / 255, g / 255, b / 255]
  const max = Math.max(R, G, B), min = Math.min(R, G, B), l = (max + min) / 2, d = max - min
  if (!d) return [0, 0, l]
  const h = max === R ? ((G - B) / d + 6) % 6 : max === G ? (B - R) / d + 2 : (R - G) / d + 4
  return [h * 60, d / (1 - Math.abs(2 * l - 1)), l]
}
// A red: its hue within 18° of red's, coloured enough to tell, not white nor
// black — a pale red ground counts (2026-11-24: an unfinished chip's
// rgb(253, 245, 242), lightness 0.97, slipped past a 0.95 bound).
const redHue = ([h, s, l]) => (h < 18 || h > 340) && s >= 0.3 && l > 0.12 && l < 0.985
function isRedLiteral(text) {
  const hex = /^#([0-9a-f]+)$/iu.exec(text)
  if (hex) {
    const v = hex[1].length <= 4 ? [...hex[1]].map((c) => c + c).join('') : hex[1]
    return redHue(hsl(parseInt(v.slice(0, 2), 16), parseInt(v.slice(2, 4), 16), parseInt(v.slice(4, 6), 16)))
  }
  const parts = text.replace(/^[a-z]+\(|\)$/giu, '').split(/[\s,/]+/u).filter(Boolean).map((p) => parseFloat(p))
  if (/^hsl/iu.test(text)) return redHue([parts[0] % 360, parts[1] / 100, parts[2] / 100])
  return redHue(hsl(parts[0], parts[1], parts[2]))
}
const redsIn = (value) => [
  ...[...value.matchAll(LITERAL)].map((m) => m[0]).filter(isRedLiteral),
  ...[...value.replace(/(["'])[^"']*\1/gu, '').replace(/url\([^)]*\)/gu, '').matchAll(NAMED_RED)].map((m) => m[0]),
]

const walk = (dir, visit) => {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path, visit)
    else visit(path, name)
  }
}
const withoutComments = (text) => text.replace(/\/\*[\s\S]*?\*\//gu, (c) => c.replace(/[^\n]/gu, ' '))
const declarations = (body) => body.split(';').map((d) => /^\s*([\w-]+)\s*:([\s\S]*)$/u.exec(d)).filter(Boolean).map((m) => [m[1], m[2].trim()])
const reads = (value) => [...value.matchAll(/var\(\s*(--[\w-]+)/gu)].map((m) => m[1])
const kept = (list, where, words) => list.find(([file, part]) => file === where && words.includes(part))

export function redRoleFailures(src) {
  const failures = []
  const sheets = []
  walk(src, (path, name) => {
    if (name.endsWith('.css') && !name.endsWith('.generated.module.css')) sheets.push([relative(src, path), withoutComments(readFileSync(path, 'utf8'))])
  })

  // The red tokens and the ones a failure may take, from the token sheets.
  const values = new Map()
  for (const [where, text] of sheets) {
    if (!where.startsWith('app/')) continue
    for (const m of text.matchAll(/(?<![\w-])(--[\w-]+)\s*:([^;{}]*)/gu)) values.set(m[1], [...(values.get(m[1]) ?? []), m[2]])
  }
  const red = new Set()
  for (let grew = true; grew;) {
    grew = false
    for (const [name, list] of values) {
      if (red.has(name) || !list.some((v) => redsIn(v).length || reads(v).some((r) => red.has(r)))) continue
      red.add(name)
      grew = true
    }
  }
  const failureFamily = (name) => name === '--danger' || (values.get(name) ?? []).some((v) => reads(v).includes('--danger'))
  const defined = new Set(values.keys())
  for (const [, text] of sheets) for (const m of text.matchAll(/(?<![\w-])(--[\w-]+)\s*:/gu)) defined.add(m[1])

  for (const [where, text] of sheets) {
    const tokenSheet = where.startsWith('app/')
    for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/gu)) {
      const selector = m[1].trim().replace(/\s+/gu, ' ')
      const selectors = selector.split(',').map((s) => s.trim())
      const palette = selectors.every((s) => /\[data-[\w-]*(?:color|tone)[\w-]*[~|^]?="red/u.test(s))
      const last = selectors.map((s) => s.split(/\s+/u).at(-1) ?? '')
      const failure = last.some((s) => FAILURE.test(s))
      const danger = last.some((s) => DANGER.test(s))
      for (const [property, value] of declarations(m[2])) {
        const written = redsIn(value)
        if (written.length && !(tokenSheet && property.startsWith('--')) && !palette && !kept(KEPT, where, selector)) {
          failures.push(`${where} ${selector}: ${property} writes out ${written[0]} (a red is a token's value: --danger for a failure, the danger tokens for a dangerous action, the palette for text, block and data colours)`)
        }
        for (const name of reads(value)) {
          if (/red|danger/u.test(name) && !defined.has(name)) failures.push(`${where} ${selector}: reads ${name}, a red nothing sets`)
        }
        if (tokenSheet) continue
        const redReads = reads(value).filter((name) => red.has(name))
        if (failure && LINES.test(property) && redReads.some((name) => !failureFamily(name)) && !kept(FAILURE_ELSEWHERE, where, selector)) {
          failures.push(`${where} ${selector}: a failure's ${property} in ${redReads.find((name) => !failureFamily(name))} (failures take --danger)`)
        }
        if (danger && !failure && redReads.some((name) => failureFamily(name) || /^--c-(?:accent-)?red$/u.test(name))) {
          failures.push(`${where} ${selector}: a dangerous action's ${property} in ${redReads.find((name) => failureFamily(name) || /^--c-(?:accent-)?red$/u.test(name))} (use --menu-danger, --danger-fill and --danger-fill-text, --danger-outline-text or --danger-soft-*)`)
        }
      }
    }
  }

  // Scripts draw inline in tokens too: a red written out in a script's code.
  walk(src, (path, name) => {
    if (!/\.tsx?$/u.test(name) || /\.test\.|\.generated\./u.test(name)) return
    const where = relative(src, path)
    const text = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, (c) => c.replace(/[^\n]/gu, ' '))
    text.split('\n').forEach((line, index) => {
      const code = line.replace(/(^|[^:"'`\\])\/\/.*$/u, '$1')
      const written = [...code.matchAll(LITERAL)].map((m) => m[0]).filter(isRedLiteral)
      if (written.length && !kept(KEPT, where, code)) failures.push(`${where}:${index + 1}: writes out ${written[0]} (draw with a red token, var(--danger) and the like)`)
    })
  })
  return failures
}
