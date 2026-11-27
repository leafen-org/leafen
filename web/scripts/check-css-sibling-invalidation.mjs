#!/usr/bin/env node

// Rules Chrome works out again for the whole page at each element put in
// anywhere in it — each new line of a long page — rather than for the
// elements around the new one:
//
// A `+` or `~` after a compound with no class, id or attribute (`p + .x`,
// `* + .x`, `input:checked + .x`), or after one holding a :not()
// (`.icon:not(:empty) ~ .label`, `#root:not(:empty) ~ #boot-screen`), is one
// Chrome can't tie to the element before: it keeps what follows in one set it
// sweeps under the parent of every element put in, anywhere. Each such
// element in the page is restyled at every insertion, and the parent's whole
// subtree walked while the set holds anything: `p + [data-notion-select]`
// and `.notionSelectIcon:not(:empty) ~ .notionSelectLabel` restyled each code
// block's language choice twice at every Enter of a 770-block page (92 of
// the 133 elements, 2026-11-14), `.linkField input + div` all its 14,300
// elements (153 ms at CPU ×2). So this check refuses a sibling combinator
// whose left compound has no class, id or attribute, or holds a :not(). Name
// the element before by its class or an attribute (`input[type]:checked +`,
// `h3[data-…] +`), or say it from the element itself or its parent
// (`.x:not(:first-child)` for `* + .x`, `.row:has(> svg:first-child) > .x`
// for `.row > svg:first-child ~ .x`, an attribute set by the code). An :is()
// or :where() ties a compound when each of its selectors ends in a class, id
// or attribute; a :not() never does. The selectors inside :not(), :is() and
// :where() are looked at the same way.
//
// An attribute nearly every element may carry — a button's `type`, a `role`,
// an `aria-…`, a `tabindex` — ties nothing: Chrome keeps the set under the
// attribute's name alone, so each such element put in anywhere sweeps what
// follows under its parent, as above, whatever its value or class. A
// `<button type="button">` behind a popover put in over a page, with
// `input[type="checkbox"]:checked + span` in the forms' styles, restyled every
// span of the page, and a dialog's `role` every element with a role
// (2026-11-14: 6,500 elements, 87 ms of the trash's first frame at CPU ×2).
// So such an attribute in the compound before a `+` or `~` is refused even
// beside a class: name the element by a class of its own.
//
// A :has() on what holds the page — the document's root, its body, the
// app's #root and page frame (AppShell .shell and .main, [data-app-main]),
// the page's scroll ([data-page-scroll]), the editor's body (.editor,
// [data-editor-root], NativeTiptap .surface, .tiptap) — is worked out again
// at every element put in under it, which is every new line: `:root:has(…)`,
// `.main:has(> [data-page-scroll])` and the editor root's
// `:has(~ .pageStarterHint)` restyled those three at each Enter, and
// `[data-page-scroll]:has([data-view-type="feed"])` looked through the whole
// page for a feed (2026-11-14). Have the code set an attribute on it instead
// (AppShell's :root[data-app-notice], pageScrollHost.ts, EditorSurface).
//
// An attribute the code changes on what holds the page — the page area's
// name (aria-label, set at each screen and at each settings window or trash
// opened or closed, AppShell RouteLabel), what a modal window shuts outside
// it (aria-hidden, inert: lib/modalFocus.ts), a section's aria-busy — in a
// compound before a descendant or child combinator has Chrome work out again,
// at each change of it anywhere, every element under the changed one that
// the rest names, whatever the compound's class: `button[aria-label] svg`
// restyled every mark (svg) in the page area, some 400, at each screen and
// settings window (2026-11-15). Name the element by a class of its own.
//
// A :has() in a compound before a descendant or child combinator puts what
// the rule ends in into the one set Chrome works out again under every
// element whose :has() may have changed — a block whose hover or focus moved,
// at each press in it: a rule ending in a bare type there (`… :has(…) … >
// span`, `> input`, `td`) restyled every span, input or cell under it, the
// whole of a page's database at each press in it (2026-11-15: 8,300 spans
// and 300 inputs, `.viewTabs:has(~ * [data-form-preview-toggle]) … > span`,
// 60–140 ms of a grouped table's "+ 새 페이지" at CPU ×2). So this check
// refuses such a rule whose last compound has no class, id or attribute of
// its own (or only one nearly every element carries, above). Let the element
// the :has() is on carry the value (a custom property its children take), or
// ask the element itself (`.field:focus`), or name it by a class.
//
// A :has() on an editor block's row, block, body or handle that looks past a
// fixed chain of children (`:has(.menu)`, `:has(~ * :focus-visible)`) goes
// through all a database's block holds when it is that block's: some 12,000
// elements of a table of 30 rows by 40 properties, at every change in it
// (2026-11-15: 5.6 ms a focus move in the table, 3 ms a row put in) — and so
// it does on a block a database stands in, a toggle, a column group or a
// callout (10.4 ms a focus move there, 3.8 ms a row put in). So such a :has()
// must first, in its own compound, say the block isn't a database's nor holds
// one (`:not([data-type="inline_database"], [data-holds-database])`, the
// handle's `:not(:is([data-type="inline_database"], [data-holds-database]) >
// *)`), name another type, or wait on a state a block takes only for a while
// (`:where(body[data-layer-open] *)`, `[data-selected]`, `:hover`); what it
// looked for in a database, the database tells its row
// (databaseBlockRowMarks.ts).
//
// A :has() of any reach on what holds blocks — a block's row, group or body,
// a column, a callout's or toggle's lines — with more of the rule after it
// (`.row:has(…) > .gutter`) has Chrome, at each element put in or taken out
// anywhere under it, go through all it holds for what the rest names, matched
// or not (once looked, the element keeps it): under a database's block or one
// a database stands in, its whole table at each row put in (2026-11-15: 0.55
// ms a rule, a row put in a table in a toggle 1.45 ms and in a callout 2.05,
// 0.27 and 0.29 without). So such a :has() must first, in its own compound,
// keep both out for good (`:not([data-type="inline_database"],
// [data-holds-database])`; a state waited on is not enough), or be asked on
// the element the rule styles (`.row > .gutter:has(~ .body > …)`) or on a
// part beside what is held (`.icon:has(…) ~ .lines`).
//
// The marks lib/lastPress.ts moves with every focus — data-key-focus while
// the keys lead, data-pointer-focus while the pointer does — inside a :has()
// have Chrome work out again, at each focus anywhere, every :has() over the
// element the mark left or took, a database's whole table under a block it
// stands in among them: `.fileCard:has(> button.fileInfo[data-key-focus]:focus)`
// cost 2.5 ms of each focus move in a table inside a toggle, 0.4 without it
// (2026-11-15). So this check refuses either mark inside a :has(): ask the
// element itself (`button.x[data-key-focus]:focus::after` drawn over its row).
//
// A mark the code moves on the page's root at a press — lib/overlayLayer's
// body[data-layer-open] as the first layer opens and the last closes,
// usePeekRoom's :root[data-side-row-peek-room] as a side peek opens and
// closes, typingFade's :root[data-typing], pointerHover's
// :root[data-pointer-still] — before a descendant or child combinator in a
// selector ending in a compound with no class, id or attribute (`*`, a type)
// has Chrome work out again every element of the page (or every one of that
// type) at that press, even inside :where() or :not() of a rule that names
// its element: `:where(body[data-layer-open] *)` restyled the whole page as
// an icon picker closed, its 7,300 elements forced by their own removal, 100
// ms of the icon's pick at CPU ×2 (2026-11-18). End it in the element's own
// class (`:where(body[data-layer-open] .blockRow)`).
//
// CSS modules' :global() counts as what it holds. The generated editor
// styles are made from sources this check reads; index.html's own styles are
// read too.

import { relative } from 'node:path'

import { bareEnd, compounds, list, selectors, styleSources, tied, unwrapped, webRoot, withoutComments } from './css-selector-parts.mjs'

// The attributes above that every kind of element carries.
const everywhere = /\[\s*(?:type|role|aria-[\w-]+|tabindex|href|title|style|hidden|disabled|name|value|id|class|dir|lang)\s*(?:[~|^$*]?=|\])/iu

function untiedSweeps(selector) {
  const found = []
  const { items, nested } = compounds(selector)
  for (let i = 1; i < items.length; i++) {
    if (items[i].combinator !== '+' && items[i].combinator !== '~') continue
    const before = items[i - 1].text
    if (tied(before) && !/:not\(/u.test(before) && !everywhere.test(before)) continue
    found.push(`${before} ${items[i].combinator} …`)
  }
  for (const inner of nested) found.push(...untiedSweeps(inner))
  return found
}

// The attributes above that the code changes on what holds the page.
const changedOnHolders = /\[\s*(?:aria-label|aria-hidden|aria-busy|inert)\s*(?:[~|^$*]?=|\])/iu

function holderAttributeScans(selector) {
  const found = []
  const { items, nested } = compounds(selector)
  for (let i = 0; i < items.length - 1; i++) {
    const next = items[i + 1].combinator
    if ((next === ' ' || next === '>') && changedOnHolders.test(items[i].text)) found.push(`${items[i].text} ${next === ' ' ? '' : '> '}…`)
  }
  for (const inner of nested) found.push(...holderAttributeScans(inner))
  return found
}

// The marks above that the code moves on the page's root at a press.
const pressMarksOnRoot = /\[\s*data-(?:layer-open|side-row-peek-room|typing|pointer-still)\s*(?:[~|^$*]?=|\])/u

function rootMarkSweeps(selector) {
  const found = []
  const { items, nested } = compounds(selector)
  const last = (items.at(-1)?.text ?? '').replace(/::[\w-]+(?:\([^)]*\))?$/u, '')
  if (bareEnd(last)) {
    for (let i = 0; i < items.length - 1; i++) {
      if ((items[i + 1].combinator === ' ' || items[i + 1].combinator === '>') && pressMarksOnRoot.test(items[i].text)) found.push(`${items[i].text} … ${items.at(-1).text}`)
    }
  }
  for (const inner of nested) found.push(...rootMarkSweeps(inner))
  return found
}

// The rule's own last compound after a :has() anywhere before it (one inside
// an :is() or :not() there too); a selector inside an :is() ends nothing.
function untiedAfterHas(selector) {
  const { items } = compounds(selector)
  const last = items.at(-1)?.text ?? ''
  const own = last.replace(/::[\w-]+(?:\([^)]*\))?$/u, '')
  return items.slice(0, -1).some(({ text }) => /:has\(/u.test(text)) && bareEnd(own) ? [`:has(…) … ${last}`] : []
}

/** What holds the page anywhere, and in the modules that draw it, by their own classes. */
const pageHolders = [/^:root$/u, /^html$/u, /^body$/u, /^#root$/u, /^\[data-app-main(?:[~|^$*]?=[^\]]*)?\]$/u,
  /^\[data-page-scroll(?:[~|^$*]?=[^\]]*)?\]$/u,
  /^\[data-editor-root(?:[~|^$*]?=[^\]]*)?\]$/u, /^\.tiptap$/u, /^\.ProseMirror$/u]
const pageHoldersIn = {
  'src/components/shell/AppShell.module.css': [/^\.shell$/u, /^\.main$/u],
  'src/components/editor/NativeTiptap.module.css': [/^\.surface$/u],
  'src/components/editor/EditorCanvas.module.css': [/^\.editor$/u],
}

/** A compound's simple selectors at its own level, its functions left out (`functions`: kept whole). */
function simples(compound, functions = false) {
  const parts = []
  let depth = 0
  let part = ''
  for (const char of compound) {
    if (depth === 0 && (char === '.' || char === '#' || char === '[' || char === ':') && part && !part.endsWith(':')) { parts.push(part); part = '' }
    if (char === '(' || char === '[') depth++
    if (char === ')' || char === ']') depth--
    part += char
  }
  if (part) parts.push(part)
  return functions ? parts : parts.filter((simple) => !/^::?[\w-]+\(/u.test(simple))
}

function pageHasRules(selector, holders) {
  const found = []
  const { items, nested } = compounds(selector)
  for (const { text } of items) {
    if (!/:has\(/u.test(text)) continue
    const held = simples(text).find((simple) => holders.some((holder) => holder.test(simple)))
    if (held) found.push(`${held} :has(…)`)
  }
  for (const inner of nested) found.push(...pageHasRules(inner, holders))
  return found
}

const editorBlockParts = /^\.(?:blockRow|blockGroup|blockBody|gutter)$/u

/** Whether a :has() argument looks past a fixed chain of children and siblings. */
function looksDeep(argument) {
  return list(argument).some((inner) => compounds(inner).items.some(({ combinator }, at) => combinator === ' ' || (at === 0 && combinator === null)))
}

/** An editor block's :has() looking deep with nothing before it, in its compound, that keeps a database's block out. */
function databaseLooks(selector, name) {
  if (!name.startsWith('src/components/editor/')) return []
  const found = []
  const { items, nested } = compounds(selector)
  for (const { text } of items) {
    if (!simples(text).some((simple) => editorBlockParts.test(simple))) continue
    for (const match of text.matchAll(/:has\(/gu)) {
      let depth = 0
      let end = match.index + 4
      for (; end < text.length; end++) {
        if (text[end] === '(') depth++
        else if (text[end] === ')' && --depth === 0) break
      }
      if (!looksDeep(text.slice(match.index + 5, end))) continue
      const before = simples(text.slice(0, match.index), true)
      const keptOut = before.some((simple) => (/^:(?:not|where)\(/u.test(simple) && /inline_database/u.test(simple) && /data-holds-database|:hover/u.test(simple))
        || /^\[data-(?:block-)?type=["']?(?!inline_database\b)[\w-]+["']?\]$/u.test(simple) || /data-layer-open|data-selected|^:hover$/u.test(simple))
      if (!keptOut) found.push(`${text.slice(0, match.index)}:has(…)`)
    }
  }
  for (const inner of nested) found.push(...databaseLooks(inner, name))
  return found
}

const blockHolders = /^\.(?:blockRow|blockGroup|blockBody|blockContent|column|columns|calloutChildren|children|syncedBlock|syncedContent|node-nativeChildren|node-nativeBlock)$/u

/** A :has() on an editor element holding blocks, before a combinator, not keeping a database's block and its holders out first. */
function holderAnchors(selector, name) {
  if (!name.startsWith('src/components/editor/')) return []
  const found = []
  const { items, nested } = compounds(selector)
  for (const { text } of items.slice(0, -1)) {
    const at = text.indexOf(':has(')
    if (at < 0 || !simples(text).some((simple) => blockHolders.test(simple))) continue
    const before = simples(text.slice(0, at), true)
    const guard = before.find((simple) => /^:(?:not|where)\(/u.test(simple) && /data-holds-database/u.test(simple))
    const notDatabase = /inline_database/u.test(guard ?? '') || before.some((simple) => /^\[data-(?:block-)?type=["']?(?!inline_database\b)[\w-]+["']?\]$/u.test(simple))
    if (!guard || !notDatabase) found.push(`${text.slice(0, at)}:has(…) …`)
  }
  for (const inner of nested) found.push(...holderAnchors(inner, name))
  return found
}

/** A :has() holding a focus mark of lib/lastPress.ts. */
function focusMarkHas(selector) {
  const found = []
  for (const match of selector.matchAll(/:has\(/gu)) {
    let depth = 0
    let end = match.index + 4
    for (; end < selector.length; end++) {
      if (selector[end] === '(') depth++
      else if (selector[end] === ')' && --depth === 0) break
    }
    if (/\[\s*data-(?:key|pointer)-focus\b/u.test(selector.slice(match.index, end))) found.push(selector.slice(match.index, end + 1))
  }
  return found
}

const sweeps = []
const holds = []
const scans = []
const afterHas = []
const databaseLooksIn = []
const focusMarks = []
const rootMarks = []
const sources = styleSources()
for (const [path, text] of sources) {
  const name = relative(webRoot, path)
  const holders = [...pageHolders, ...(pageHoldersIn[name] ?? [])]
  for (const { selector, line } of selectors(withoutComments(text))) {
    for (const complex of list(unwrapped(selector))) {
      for (const sweep of untiedSweeps(complex)) sweeps.push(`${name}:${line}: ${complex} (${sweep})`)
      for (const held of pageHasRules(complex, holders)) holds.push(`${name}:${line}: ${complex} (${held})`)
      for (const scan of holderAttributeScans(complex)) scans.push(`${name}:${line}: ${complex} (${scan})`)
      for (const after of untiedAfterHas(complex)) afterHas.push(`${name}:${line}: ${complex} (${after})`)
      for (const look of [...databaseLooks(complex, name), ...holderAnchors(complex, name)]) databaseLooksIn.push(`${name}:${line}: ${complex} (${look})`)
      for (const mark of focusMarkHas(complex)) focusMarks.push(`${name}:${line}: ${complex} (${mark})`)
      for (const sweep of rootMarkSweeps(complex)) rootMarks.push(`${name}:${line}: ${complex} (${sweep})`)
    }
  }
}

if (sweeps.length) {
  console.error('Sibling rules Chrome can\'t tie to the element before, swept under the parent of every element put in anywhere in the page:')
  for (const failure of sweeps) console.error(`  ${failure}`)
  console.error('Give the compound before the + or ~ a class or an attribute of its own (not a type, role or aria-…) and no :not(), or say it from the element itself or its parent (check-css-sibling-invalidation.mjs).')
}
if (holds.length) {
  console.error(':has() rules on what holds the page, worked out again at every element put in under it:')
  for (const failure of holds) console.error(`  ${failure}`)
  console.error('Have the code set an attribute on it instead (check-css-sibling-invalidation.mjs).')
}
if (scans.length) {
  console.error('Attributes the code changes on what holds the page, before a descendant or child, each change working out what is under it again:')
  for (const failure of scans) console.error(`  ${failure}`)
  console.error('Name the element by a class of its own instead (check-css-sibling-invalidation.mjs).')
}
if (afterHas.length) {
  console.error('Rules ending in a bare type after a :has(), worked out again under every element whose :has() may have changed:')
  for (const failure of afterHas) console.error(`  ${failure}`)
  console.error('Let the :has() element carry the value as a custom property, ask the element itself, or name it by a class (check-css-sibling-invalidation.mjs).')
}
if (databaseLooksIn.length) {
  console.error('Editor block :has() rules that would look through a database block\'s whole table at every change in it:')
  for (const failure of databaseLooksIn) console.error(`  ${failure}`)
  console.error('Keep a database\'s block and one a database stands in out before the :has() (:not([data-type="inline_database"], [data-holds-database])), ask it on the element the rule styles or beside what is held, or have the database tell its row (databaseBlockRowMarks.ts, check-css-sibling-invalidation.mjs).')
}
if (focusMarks.length) {
  console.error(':has() rules holding a focus mark (data-key-focus, data-pointer-focus), every :has() over a focus worked out again at each focus move:')
  for (const failure of focusMarks) console.error(`  ${failure}`)
  console.error('Ask the focused element itself, its ::after spread over what it stands in (check-css-sibling-invalidation.mjs).')
}
if (rootMarks.length) {
  console.error('Marks moved on the page\'s root at a press, before a selector ending in no class, id or attribute, the whole page worked out again at that press:')
  for (const failure of rootMarks) console.error(`  ${failure}`)
  console.error('End it in the element\'s own class, as :where(body[data-layer-open] .blockRow) (check-css-sibling-invalidation.mjs).')
}
if (sweeps.length || holds.length || scans.length || afterHas.length || databaseLooksIn.length || focusMarks.length || rootMarks.length) process.exit(1)
