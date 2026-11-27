// A text field's edge has its role, as Notion's TextInput draws it (seen
// 2026-11-24 in Chrome at 1685, dark, in its ⌘K link field, emoji search,
// property name, share invite and page menu search, and its token sheet for
// light): 1px of borPriTra outside at rest and under the pointer, Notion's
// input ring inpBluFocRin while typed in (app/globals.css --name-field-*).
//
// So a field's rule draws its edge — a border, a border colour, an outline or
// a 1px ring — only from the field's tokens: --name-field-ring… (its rest,
// focus, danger and apart rings), --button-outline (borPriTra, for a field
// drawn with its own 1px border, as the developer pages' are), --focus,
// --danger, the keys' ring, and a menu's or a filter's own field tokens
// (--menu-field-ring, the --menu-search-… rings, --filter-field-ring and
// --filter-text-focus), and a form's answer its --form-answer-ring (Notion's
// form page rings a typed, dated or dropped-down answer inside in borStr, seen
// 2026-11-24). A rule is a field's when each of its selectors ends on a text
// input or textarea, or on a class named …Input, …Search, …Field, …Textarea
// or …Composer(Box) — a comment's field, which Notion draws with no edge at
// rest and its input ring while typed in (seen 2026-11-24) — or when it sets a
// menu search's --menu-search-rest-ring. A line on one side only (a phone
// sheet's row rule) is not an edge. The fields below keep another edge where
// Notion was seen drawing one, each with where; the pictures of other apps'
// screens keep theirs.
//
// A button's edge reads a colour by its role too, and no button's rule
// writes a colour out, so a theme and high contrast reach every edge
// (2026-11-24: the share panel's copy button wrote borPriTra out for each
// theme, and five outlined buttons read a field's old input border that no
// Notion edge uses). Notion's buttons take three edges (each seen 2026-11-24
// in Chrome at 1685, dark, from their style, light from its token sheet):
// - an outlined button's 1px line is borPriTra, --button-outline — settings
//   rows' choices and buttons, a device's sign-out, the cover's upload, a
//   view's and a page's icon button beside its name, a form's link copy and
//   file upload, a split button's line (and 2026-10-30, 2026-11-18);
// - a choice tile's is borPri, --choice-tile-edge — a view's layout tiles and
//   a new database's feature chips;
// - a raised one's is its small outline shadow shaOutSm,
//   --shadow-outline-small — the sidebar's card, the icon button.
// So a button's edge never reads the border colour itself (--border-default,
// borPri): 31 rules did, never compared with Notion's. Nor does a line at a
// button's start or end, the line between buttons side by side (a row's
// line under it is the list's, not the button's): an outlined group's is
// borPriTra, --button-outline (the split "새로 만들기"), a raised tool group's
// borSecTra, --tool-group-line (a board card's tools; both seen 2026-11-24,
// and two such lines read --border-default). A rule is a button's
// when each of its selectors ends on a button or a class named …Button,
// button, outlined, …Secondary, secondary, …Action, action, retry, reopen or
// deny, or it composes a button's line (ButtonLine.module.css).

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

// Fields drawn with another edge: [file, the rule's selector, why].
const KEPT = [
  ['components/database/AutomationConditionPicks.module.css', ':global([data-theme="dark"]) .field', "Notion's automation condition fields ring in the older dark rgba(255,255,255,.094) (seen 2026-11-17)"],
  ['components/database/AutomationConditionValuePopover.module.css', ':global([data-theme="dark"]) .search', "Notion's automation condition fields ring in the older dark rgba(255,255,255,.094) (seen 2026-11-17)"],
  ['components/database/AutomationRelationCondition.module.css', ':global([data-theme="dark"]) .search', "Notion's automation condition fields ring in the older dark rgba(255,255,255,.094) (seen 2026-11-17)"],
  ['components/developers/InternalConnection.module.css', '.search', "the developer pages' text field line, borPriTra inside (InternalConnectionWebhooks .textField, seen 2026-11-21)"],
]
// Buttons drawn with a colour written out: [file, the rule's selector, why].
const KEPT_BUTTONS = [
  ['components/database/DatabasePeopleDateCells.module.css', '.dateGrid button:hover', "Notion's date grid day under the pointer, #2783de 2px inside in every contrast (seen 2026-11-21)"],
]
// Pictures of other apps' screens and the public site's own look.
const PICTURES = [
  'components/NotionTokenGuide', 'components/agentConnectorGuide/', 'components/guide/',
  'components/remoteAccess/TailscaleAddressGuidePages', 'components/PublicSiteView', 'components/editor/SlashTipPreview',
]
const ALLOWED = /^var\(--(?:name-field-ring[\w-]*|form-answer-ring|button-outline|focus|danger|keyboard-focus-ring|menu-field-ring|menu-search-[\w-]+|filter-field-ring|filter-text-focus)(?:,[^)]*)?\)$/u
const NOT_TEXT = /\[type="?(?:checkbox|radio|range|color|file|hidden|button|submit|reset|image)"?\]|\[role="?switch"?\]/u
const FIELD_CLASS = /\.(?:[\w-]*(?:Input|Search|Field|Textarea|Composer|ComposerBox)|input|search|field|textarea|composer)(?![\w-])/u
const BUTTON_PART = /^(?:button\b|.*\.(?:[\w-]*Button|button|outlined|[\w-]*Secondary|secondary|[\w-]*Action|action|retry|reopen|deny)(?![\w-]))/u
// The border colour itself, borPri: a choice tile reads it as --choice-tile-edge.
const BORDER_ITSELF = /var\(--border-default\b/u
// A colour by its role: a custom property read whole or mixed with another or
// none, the words' own colour, or none.
const ROLE_COLOUR = /^(?:var\(--[\w-]+(?:,[^)]*)?\)|currentcolor|transparent|color-mix\(in \w+, var\(--[\w-]+\)(?: \d+%)?, (?:transparent|var\(--[\w-]+\))(?: \d+%)?\))$/iu

function lastPart(selector) {
  return selector.trim().split(/\s+|>|\+|~/u).filter(Boolean).at(-1) ?? ''
}
function buttonSelector(selector) {
  const part = lastPart(selector).replace(/::?[\w-]+(\((?:[^()]|\([^()]*\))*\))?/gu, '').replace(/\[[^\]]*\]/gu, '')
  return BUTTON_PART.test(part)
}
function fieldSelector(selector) {
  const part = lastPart(selector)
  if (NOT_TEXT.test(part)) return false
  const bare = part.replace(/::?[\w-]+(\((?:[^()]|\([^()]*\))*\))?/gu, '').replace(/\[[^\]]*\]/gu, '')
  return /^(?:input|textarea)\b/u.test(bare) || FIELD_CLASS.test(bare)
}
// Each colour an edge declaration draws: a var(--…) read whole, or a colour
// written out; with sides, a line at its start or end too.
const EDGE = /^\s*(border|border-color|outline|outline-color|box-shadow|--menu-search-rest-ring)\s*:\s*(.+)$/su
const EDGE_OR_SIDE = /^\s*(border(?:-inline-(?:start|end)|-(?:right|left))?(?:-color)?|outline|outline-color|box-shadow|--menu-search-rest-ring)\s*:\s*(.+)$/su
function edgeColours(body, sides = false) {
  const colours = []
  for (const declaration of body.split(';')) {
    const match = (sides ? EDGE_OR_SIDE : EDGE).exec(declaration)
    if (!match) continue
    const [, property, value] = match
    if (/^(?:0|none|transparent|inherit|initial|unset)$/u.test(value.trim())) continue
    if (property.startsWith('box-shadow') || property === '--menu-search-rest-ring') {
      for (const layer of value.split(/,(?![^(]*\))/u)) {
        if (/^\s*(?:none|0 0 #0000)\s*$/u.test(layer)) continue
        const lone = /^\s*(var\(--[\w-]+(?:,[^)]*)?\))\s*$/u.exec(layer)
        if (lone) { colours.push(lone[1]); continue }
        // A ring: no offset, no blur, its spread a pixel or two.
        if (!/(?:^|\s)0(?:px)? 0(?:px)? (?:0(?:px)? )?[12]px\b/u.test(layer)) continue
        const colour = /(var\(--[\w-]+(?:,[^)]*)?\)|rgba?\([^)]*\)|#[0-9a-f]{3,8}\b|color-mix\((?:[^()]|\([^()]*\))*\))/iu.exec(layer)
        if (colour) colours.push(colour[1])
      }
    } else {
      const colour = /(var\(--[\w-]+(?:,[^)]*)?\)|rgba?\([^)]*\)|#[0-9a-f]{3,8}\b|color-mix\((?:[^()]|\([^()]*\))*\)|currentcolor)/iu.exec(value)
      if (colour && !/^\s*(?:0|none)\b/u.test(value)) colours.push(colour[1])
    }
  }
  return colours
}

export function fieldEdgeFailures(src) {
  const failures = []
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name)
      if (statSync(path).isDirectory()) { walk(path); continue }
      if (!name.endsWith('.css') || name.endsWith('.generated.module.css')) continue
      const where = relative(src, path)
      if (PICTURES.some((prefix) => where.startsWith(prefix))) continue
      const text = readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, '')
      for (const rule of text.matchAll(/([^{}]+)\{([^{}]*)\}/gu)) {
        const selector = rule[1].trim().replace(/\s+/gu, ' ')
        if (selector.startsWith('@') || /^(?:from|to|\d+%)/u.test(selector)) continue
        const selectors = selector.split(/,(?![^(]*\))/u)
        const menuSearch = /--menu-search-rest-ring\s*:/u.test(rule[2])
        const buttonLine = /composes:\s*line\s+from\s+["'][^"']*ButtonLine\.module\.css/u.test(rule[2])
        if (buttonLine || selectors.every(buttonSelector)) {
          if (KEPT_BUTTONS.some(([file, kept]) => file === where && kept === selector)) continue
          for (const colour of edgeColours(rule[2], true)) {
            if (BORDER_ITSELF.test(colour)) {
              failures.push(`${where} ${selector}: a button's edge in ${colour} (read its role: --button-outline outlined, --choice-tile-edge a choice tile, --shadow-outline-small raised, --tool-group-line between a raised group's buttons)`)
              continue
            }
            if (ROLE_COLOUR.test(colour.replace(/\s+/gu, ' '))) continue
            failures.push(`${where} ${selector}: a button's edge in ${colour} written out (read its role, --button-outline for an outlined one)`)
          }
          continue
        }
        if (!menuSearch && !selectors.every(fieldSelector)) continue
        if (KEPT.some(([file, kept]) => file === where && kept === selector)) continue
        for (const colour of edgeColours(rule[2])) {
          if (ALLOWED.test(colour.replace(/\s+/gu, ' '))) continue
          failures.push(`${where} ${selector}: a text field's edge in ${colour} (use the field's ring, --name-field-ring…, or --button-outline)`)
        }
      }
    }
  }
  walk(join(src, 'components'))
  walk(join(src, 'organization'))
  return failures
}
