#!/usr/bin/env node

// Each colour token has its role, as Notion's (seen 2026-10-11, 한지테스트페이지):
//
// - An icon paints with an icon token (--icon-primary, --icon-secondary,
//   --icon-tertiary), not the faint text one (--text-tertiary) nor the
//   palette's gray (--c-gray, --c-accent-gray), which matches icoSec in light
//   and reads darker than Notion's in dark. A rule counts as an icon's when
//   each of its selectors ends on one by name: svg, …Icon, …Glyph,
//   …Chevron, …Caret, …Handle, …Grip, …Disclosure. Notion's own icons say
//   which they take in their style (fill: var(--c-icoTer)): a template's
//   row in an empty row page is icoTer (seen 2026-11-17). Notion paints a
//   few in its faint text instead — a phone table head's type mark, in the
//   names' colour (seen 2026-11-17) — listed below with where it was seen.
// - A menu or popover (a rule with the menu or popover shadow) sits on the
//   raised surface (--bg-elevated), not the page's own ground (--bg-app,
//   --bg-default). Notion's import windows are the page's own ground (seen
//   2026-11-05: #191919 dark), and so are its cards over a page: the list
//   of a page's headings beside their marks (seen 2026-11-14 and again
//   2026-11-17 at 1440, dark: rgb(25, 25, 25), 232 wide, 14 round, the
//   menus' shadow) and the search's highlight card (seen 2026-11-14, dark:
//   rgb(25, 25, 25) under its rgb(32, 32, 32) band).
// - Notion's older ink, rgba(55, 53, 47, …), is a token's value in the token
//   sheets (src/app/*.css, app/olderInk.css for a screen's spot), each where
//   Notion still draws in it; a screen's sheet or script uses the token and
//   never writes the ink out. The Markdown export's tables keep their own
//   colours until compared with Notion's export.
//
// - Red has its roles — a failure --danger, a dangerous action the danger
//   tokens, text, block and data colours the palette — and is never written
//   out in a screen's sheet or script (css-red-roles.mjs).
// - A text field's edge is Notion's TextInput's: the field ring's tokens
//   (--name-field-ring…, --button-outline, --focus), never another border
//   colour nor one written out (css-field-edge-roles.mjs).
// - A raised surface's shadow reads its token, never one theme's value
//   written out (css-raised-shadow-roles.mjs).
//
// Generated sheets are left to their sources.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { fieldEdgeFailures } from './css-field-edge-roles.mjs'
import { raisedShadowFailures } from './css-raised-shadow-roles.mjs'
import { redRoleFailures } from './css-red-roles.mjs'

const src = join(resolve(dirname(fileURLToPath(import.meta.url)), '..'), 'src')
// The rules on the page's own ground on purpose, as Notion's (above).
const PAGE_GROUND = [
  ['components/import/ImportKindWindow.module.css', '.dialog'],
  ['components/editor/FloatingPageOutline.module.css', '.panel'],
  ['components/search/SearchHighlightPanel.module.css', '.highlightCard'],
]
// Icons Notion paints in its faint text colour (above).
const ICON_FAINT = new Set([
  'components/database/DatabaseTableChrome.module.css :global([data-phone="true"]) .headGlyphIcon',
])
const ICON = /(svg|[Ii]con|[Gg]lyph|[Cc]hevron|[Cc]aret|[Hh]andle|[Gg]rip|[Dd]isclosure)$/u

const failures = []
const walk = (dir, visit) => {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path, visit)
    else visit(path, name)
  }
}

// Each rule's selectors and declarations, comments left out.
const rules = (text) => text.replace(/\/\*[\s\S]*?\*\//gu, '').matchAll(/([^{}]+)\{([^{}]*)\}/gu)

walk(join(src, 'components'), (path, name) => {
  if (!name.endsWith('.css') || name.endsWith('.generated.module.css')) return
  for (const match of rules(readFileSync(path, 'utf8'))) {
    const selectors = match[1].split(',').map((part) => part.trim().split(/\s+/u).at(-1) ?? '')
    if (selectors.every((part) => ICON.test(part.replace(/[:[].*$/u, '')))
      && /(?:^|;)\s*(?:color|fill|stroke):[^;]*var\(--(?:text-tertiary|c-accent-gray|c-gray)\)/u.test(match[2])
      && !ICON_FAINT.has(`${relative(src, path)} ${match[1].trim()}`)) {
      failures.push(`${relative(src, path)} ${match[1].trim()}: an icon painted with ${/var\(--(text-tertiary|c-accent-gray|c-gray)\)/u.exec(match[2])[1]} (use --icon-secondary or --icon-tertiary)`)
    }
  }
})

for (const root of ['components', 'app']) {
  walk(join(src, root), (path, name) => {
    if (!name.endsWith('.css') || name.endsWith('.generated.module.css')) return
    const where = relative(src, path)
    for (const match of rules(readFileSync(path, 'utf8'))) {
      if (PAGE_GROUND.some(([file, selector]) => where === file && match[1].trim().endsWith(selector))) continue
      if (/box-shadow:\s*var\(--shadow-(?:menu|popover)\)/u.test(match[2]) && /background(?:-color)?:\s*var\(--bg-(?:app|default)\)/u.test(match[2])) {
        failures.push(`${where} ${match[1].trim()}: a menu on the page's ground (use --bg-elevated)`)
      }
    }
  })
}

const OLDER_INK = /rgba?\(55, 53, 47\b/u
for (const root of ['components', 'app']) {
  walk(join(src, root), (path, name) => {
    if (!/\.(css|tsx?)$/u.test(name) || /\.test\.|blockMarkdown(Html|Tables)\.ts$/u.test(name)) return
    const where = relative(src, path)
    const text = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, '')
    text.split('\n').forEach((line, index) => {
      if (!OLDER_INK.test(line)) return
      if (root === 'app' && name.endsWith('.css') && /^\s*--[\w-]+\s*:/u.test(line)) return
      failures.push(`${where}:${index + 1}: Notion's older ink written out (make it a token in app/olderInk.css)`)
    })
  })
}

failures.push(...redRoleFailures(src))
failures.push(...fieldEdgeFailures(src))
failures.push(...raisedShadowFailures(src))

if (failures.length) {
  console.error('Colours outside their token\'s role:')
  for (const failure of failures) console.error(`  ${failure}`)
  process.exit(1)
}
