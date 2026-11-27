// A raised surface's shadow reads its token, as Notion's raised things read
// their own (each seen 2026-11-24 in Chrome at 1685, dark, from the thing's
// own style; light from its token sheet): a chart's tip shaOutSm
// (--shadow-outline-small), a timeline's bar, a board card's tools and a
// history change's author shaOutMd (--shadow-raised), a menu shaOutLg
// (--shadow-menu), a window shaOutScr (--shadow-modal). Each token's value is
// one per theme, so a rule that writes one theme's out draws that theme's in
// both: the history's author chip and the chart's tip drew the dark shaOutSm
// in the light theme, the timeline's bar the light one in the dark.
//
// So no rule in a screen's sheet writes out a box-shadow whose drop layers are
// one theme's of those tokens (its ring may read any colour). The rules below
// keep theirs, each with where Notion was seen drawing it — a borSec ring over
// the light shaOutSm's 4% drop, not yet compared in the dark theme.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

// [file, the rule's selector, why].
const KEPT = [
  ['components/ToastStack.module.css', '.toast', "Notion's corner notice keeps one look in both themes, dark ground and light words, under a 10% ink ring (seen 2026-10-31)"],
  ['components/database/DatabaseAutomationPhone.module.css', '.dots', "Notion's phone automation card's \"⋯\" group, as its action cards' (seen 2026-11-15 on an A90)"],
  ['components/editor/EditorLayoutBlocks.module.css', '.buttonConfigWhen', "Notion's page button settings' ringed \"when\" box (seen 2026-10-31)"],
  ['components/automation/automationActionEditor.module.css', ':global(:is([data-automation-dialog], [data-page-button-setup], [data-phone-automation-editor])) .card header::after', "Notion's action card's ringed fold-and-menu group (seen 2026-10-31)"],
]
// The drop layers of each theme's value: [token, theme, layers].
// Each layer is "x y blur spread colour", lengths in px.
const TOKENS = [
  ['--shadow-outline-small', 'light', ['0 2 4 0 rgba(0,0,0,0.04)']],
  ['--shadow-outline-small', 'dark', ['0 2 4 0 rgba(25,25,25,0.08)']],
  ['--shadow-raised', 'light', ['0 8 12 0 rgba(25,25,25,0.027)', '0 2 6 0 rgba(25,25,25,0.027)']],
  ['--shadow-raised', 'dark', ['0 4 12 -2 rgba(25,25,25,0.08)']],
  ['--shadow-menu', 'light', ['0 20 24 0 rgba(25,25,25,0.05)', '0 5 8 0 rgba(25,25,25,0.027)']],
  ['--shadow-menu', 'dark', ['0 14 28 -6 rgba(25,25,25,0.2)', '0 2 4 -1 rgba(25,25,25,0.118)']],
  ['--shadow-modal', 'light', ['0 24 48 0 rgba(25,25,25,0.24)', '0 4 12 0 rgba(25,25,25,0.14)']],
  ['--shadow-modal', 'dark', ['0 24 48 0 rgba(25,25,25,0.64)', '0 4 12 0 rgba(25,25,25,0.56)']],
]

const COLOUR = /(rgba?\([^)]*\)|#[0-9a-f]{3,8}\b|var\(--[\w-]+(?:,[^)]*)?\))/iu
function rgba(colour) {
  const hex = /^#([0-9a-f]{3,8})$/iu.exec(colour)
  if (hex) {
    let digits = hex[1]
    if (digits.length <= 4) digits = [...digits].map((digit) => digit + digit).join('')
    const parts = digits.match(/../gu).map((pair) => parseInt(pair, 16))
    return `rgba(${parts[0]},${parts[1]},${parts[2]},${parts.length > 3 ? +(parts[3] / 255).toFixed(3) : 1})`
  }
  const fn = /^rgba?\(([^)]*)\)$/iu.exec(colour)
  if (!fn) return null
  const parts = fn[1].split(/[\s,/]+/u).filter(Boolean).map(Number)
  return `rgba(${parts[0]},${parts[1]},${parts[2]},${parts.length > 3 ? +parts[3].toFixed(3) : 1})`
}
// A layer as "x y blur spread colour", "ring" for a 1px ring of any colour,
// "var" for a drop in a token's colour, or null for an inset one.
function layer(text) {
  if (/\binset\b/u.test(text)) return null
  const colour = COLOUR.exec(text)?.[1] ?? ''
  const lengths = text.replace(colour, ' ').trim().split(/\s+/u).filter(Boolean).map((length) => length.replace(/px$/u, ''))
  if (lengths.length < 2 || lengths.some((length) => !/^-?[\d.]+$/u.test(length))) return null
  const [x, y, blur = '0', spread = '0'] = lengths.map((length) => String(+length))
  if (x === '0' && y === '0' && blur === '0' && spread === '1') return 'ring'
  const written = rgba(colour)
  return written ? `${x} ${y} ${blur} ${spread} ${written}` : 'var'
}

export function raisedShadowFailures(src) {
  const failures = []
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name)
      if (statSync(path).isDirectory()) { walk(path); continue }
      if (!name.endsWith('.css') || name.endsWith('.generated.module.css')) continue
      const where = relative(src, path)
      const text = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, '')
      for (const rule of text.matchAll(/([^{}]+)\{([^{}]*)\}/gu)) {
        const selector = rule[1].trim().replace(/\s+/gu, ' ')
        if (KEPT.some(([file, kept]) => file === where && kept === selector)) continue
        for (const declaration of rule[2].split(/;(?![^(]*\))/u)) {
          const match = /^\s*box-shadow\s*:\s*(.+)$/su.exec(declaration)
          if (!match) continue
          const layers = match[1].split(/,(?![^(]*\))/u).map(layer)
          if (!layers.includes('ring')) continue
          const drops = layers.filter((each) => each && each !== 'ring' && each !== 'var')
          if (!drops.length) continue
          for (const [token, theme, wanted] of TOKENS) {
            if (wanted.length === drops.length && wanted.every((each) => drops.includes(each))) {
              failures.push(`${where} ${selector}: the ${theme} theme's ${token} written out (read var(${token}), whose value follows the theme)`)
              break
            }
          }
        }
      }
    }
  }
  walk(join(src, 'components'))
  walk(join(src, 'organization'))
  return failures
}
