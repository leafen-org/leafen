#!/usr/bin/env node

// Every floating layer names its kind — a dialog, a question, a button's menu, a list beside a menu's row, a picker,
// a database bar's window, a list at the text's caret, a card by the text, a side peek, a panel, a hover card, a tip,
// the selection's toolbar or a part of its holder — and takes what Escape, a press beside it, the pointer and the
// wheel under it, the keys inside it and the focus after it do from that kind, set once in src/lib/overlayKinds.ts
// as Notion was measured to do for windows of that kind. A layer that set those itself, place by place, drifted
// from the rest of its kind each time one place was fixed (2026-11-26: 338 layers set their own of 23 options).
//
// So this check refuses:
//   - a layer that names no kind: useOverlayLayer's options, usePopoverPlacement's fifth argument (`false` for one
//     placed without a layer of its own) and LayerBox's `layer`, followed to the object they are given in their
//     file (a variable, either side of a condition, a spread of one);
//   - a kind no row of overlayKinds.ts holds;
//   - an option a layer sets against what its kind gives (a menu that gives no focus back, a dialog that is not
//     modal) — but where a line below names the file, the option and why: a window Notion was measured to do
//     otherwise, one that gives the focus back itself, or one kept as it was before the kinds where Notion was not
//     measured for it; an entry that no longer matches a layer fails the check too.
// Usage: node scripts/check-overlay-kinds.mjs [source root]

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = process.argv[2] ? resolve(process.argv[2]) : join(webRoot, 'src')
// The boundary and the two that hand a caller's options on to it.
const BOUNDARY = new Set(['lib/overlayLayer.ts', 'lib/overlayKinds.ts', 'lib/popoverPlacement.ts', 'lib/layerBox.tsx'])

// [file under src, option, why it may differ from its kind] — each one an exception to the floating layers'
// rules, in the option named.
const ALLOWED = [
  // pressGoesOn: where the press beside it goes on to the page, or is taken, by where it stands.
  ['components/CommentsPanel.tsx', 'pressGoesOn', "a new comment's card floats and takes the press beside it; one reading a block's threads lets it go on (Notion two tabs, 2026-11-26)"],
  ['components/database/DatabaseQuickFilterBar.tsx', 'pressGoesOn', "a phone's sheet takes the press beside it; on a desktop it goes on, as Notion's bar windows (2026-11-12)"],
  ['components/editor/slashMenuPlace.ts', 'pressGoesOn', "a phone's menu or the template menu stands in its own place, not at the caret, and takes the press beside it"],
  // floats: false — it draws a shade of its own, which takes the press beside it and the pointer under it.
  ['components/RowMenuPlaced.tsx', 'floats', "the page's and a sidebar row's ⋯ menu draws a shade of its own over the page"],
  ['components/editor/BlockActionMenu.tsx', 'floats', "the block menu draws a shade of its own over the page"],
  ['components/page/PageIconPicker.tsx', 'floats', "the icon picker draws a shade of its own over the page"],
  ['components/sidebar/SidebarCustomizeList.tsx', 'floats', "the sidebar's add-a-section menu draws a shade of its own, which closes a list beside it too"],
  ['components/sidebar/SidebarHeader.tsx', 'floats', "the workspace menu draws a shade of its own over the page"],
  ['components/sidebar/SidebarShortcuts.tsx', 'floats', "the sidebar's add-a-shortcut menu draws a shade of its own, which closes a list beside it too"],
  // closesHost: false — a list beside a row that a press beside it closes alone; its holder closes itself.
  ['components/database/DashboardWidgetMenu.tsx', 'closesHost', "a list beside a row of a widget's menu, which closes itself"],
  ['components/database/TablePropertyMenu.tsx', 'closesHost', "a list beside a row of a column's menu, whose own shade closes it"],
  ['components/editor/LinkMentionPreview.tsx', 'closesHost', "a link card's ⋯ list, which goes with the card"],
  ['components/sidebar/SidebarActionMenu.tsx', 'closesHost', "a list beside a row of a sidebar menu, whose own shade closes it"],
  // restoreFocus: false — it gives the focus back itself as it closes, or gives none back.
  ['components/SharedPageMore.tsx', 'restoreFocus', "gives none back: a public page's ⋯, kept as before the kinds (Notion not measured)"],
  ['components/comments/DiscussionSideView.tsx', 'restoreFocus', "gives none back: the thread's view takes the focus into its reply row as it opens, kept as before the kinds"],
  ['components/database/CardPreviewChoice.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its button, a press beside it leaves it where pressed"],
  ['components/database/CardValuePress.tsx', 'restoreFocus', "gives none back: it takes the keys itself as a card's value opens it, kept as before the kinds"],
  ['components/database/ConditionalColorCard.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its button, a press beside it leaves it where pressed"],
  ['components/database/ConditionalColorMenu.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its button, a press beside it leaves it where pressed"],
  ['components/database/DashboardMultiSourceWindow.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/database/DashboardWidgetMenu.tsx', 'restoreFocus', "gives none back: a list beside a row, its widget's menu holding the focus"],
  ['components/database/DatabaseAutomationList.tsx', 'restoreFocus', "gives none back: its search takes the focus as it opens, kept as before the kinds"],
  ['components/database/DatabaseAutomationReaderPopover.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/database/DatabaseLinkedStarter.tsx', 'restoreFocus', "gives none back: leaving it leaves the new linked view unchosen, kept as before the kinds"],
  ['components/database/DatabaseOptionFilterChoices.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its button, a press beside it leaves it where pressed"],
  ['components/database/DatabaseQuickFilterBar.tsx', 'restoreFocus', "gives it back itself to the pill it opened from, ringed after keys, unless a press opened something (giveFocusBack)"],
  ['components/database/DatabaseStartPanel.tsx', 'restoreFocus', "gives it itself: put away, the caret goes to the page title's start, as Notion's (2026-11-17, 11-21)"],
  ['components/database/DatabaseTemplateMenu.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/database/DatabaseTemplateRepeatChooser.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/database/FormQuestionCard.tsx', 'restoreFocus', "gives none back: lists beside the card menu's rows, the menu giving it back to its button"],
  ['components/database/FormShareMenu.tsx', 'restoreFocus', "gives none back: a row's list in the form's share menu, kept as before the kinds"],
  ['components/database/FormSubmitSettings.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/database/NotionSelect.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its button, a press beside it leaves it where pressed"],
  ['components/database/PageLayoutCardAdvanced.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its row, quiet when the pointer opened it"],
  ['components/database/PageLayoutCardPopover.tsx', 'restoreFocus', "gives none back: it takes the focus on its module as it opens, kept as before the kinds"],
  ['components/database/PropertySettingRow.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its button, a press beside it leaves it where pressed"],
  ['components/database/RelationPropertyCreationDialog.tsx', 'restoreFocus', "gives none back: placed beside the settings, kept as before the kinds"],
  ['components/database/RollupCalculationSelect.tsx', 'restoreFocus', "gives it back itself: Escape gives it to its button, a press beside it leaves it where pressed"],
  ['components/database/TableAddProperty.tsx', 'restoreFocus', "gives it back itself to its +, a frame on or once the new column has come"],
  ['components/database/TablePropertyMenu.tsx', 'restoreFocus', "gives it back itself as it and its side menu close"],
  ['components/database/UndatedRowsMenu.tsx', 'restoreFocus', "gives it back itself to its button, only where a key opened it"],
  ['components/database/useDatabaseToolbarController.ts', 'restoreFocus', "gives it back itself to the bar's button, once the menu is gone (focusOpener)"],
  ['components/desktop/DesktopTextMenu.tsx', 'restoreFocus', "gives it back itself: Escape puts back the text chosen at the press"],
  ['components/editor/BlockActionMenu.tsx', 'restoreFocus', "gives it back only where keys opened it; from the pointer no ring on its handle, as Notion's (2026-10-31)"],
  ['components/editor/DateMentionEditor.tsx', 'restoreFocus', "gives it back itself through its holder: the date takes it, ringed after Escape, unringed after a press beside it, as Notion's (2026-11-15, 11-26)"],
  ['components/editor/LinkMentionPreview.tsx', 'restoreFocus', "gives none back: a link card's ⋯ list, which goes with the card"],
  ['components/editor/MentionCard.tsx', 'restoreFocus', "gives none back: it takes no focus from the text, its keys coming through the caret's"],
  ['components/editor/MermaidBlockTools.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/editor/NativeTiptapLinkPill.tsx', 'restoreFocus', "gives it back itself to the text, its words chosen"],
  ['components/editor/PageLinkPicker.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/forms/FormDropdown.tsx', 'restoreFocus', "gives it back itself to its field"],
  ['components/forms/FormReferencePicker.tsx', 'restoreFocus', "gives it back itself to its field"],
  ['components/page/PageIconPicker.tsx', 'restoreFocus', "gives it back only where its opener asks: not where it leaves the focus, from keys only where asked so"],
  ['components/page/TopBarTeamspaceCrumb.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/settings/PublicPagesSection.tsx', 'restoreFocus', "gives none back: a row's menu in the settings window, kept as before the kinds"],
  ['components/settings/SidebarPresetsSection.tsx', 'restoreFocus', "gives none back: a small menu in the settings window, kept as before the kinds"],
  ['components/settings/WorkspaceConnectionsSection.tsx', 'restoreFocus', "gives none back: a menu in the settings window, kept as before the kinds"],
  ['components/settings/WorkspaceLandingPage.tsx', 'restoreFocus', "gives it back itself to its button as Escape closes it"],
  ['components/shortcuts/ShortcutsTabRow.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  ['components/sidebar/SidebarActionMenu.tsx', 'restoreFocus', "gives it back itself: to its button in a window, on the page as its caller's close says"],
  ['components/updates/InboxRowCard.tsx', 'restoreFocus', "gives none back, kept as before the kinds (Notion not measured)"],
  // followsText: false — the selection toolbar's color and turn-into menus, opened from its buttons.
  ['components/editor/SelectionToolbarView.tsx', 'followsText', "the toolbar's color and turn-into menus, opened from its buttons, not drawn for the selection itself"],
  // closesAfterClick: false — docked beside the page as a view's settings, a press beside it closes it at once.
  ['components/database/WikiHomeSettingsPanel.tsx', 'closesAfterClick', "docked as a view's settings: a press beside it closes it and goes on, as Notion's docked settings"],
  // modal: false — a question or window that holds no page still where it opens.
  ['components/PageHistoryRestorePanel.tsx', 'modal', "the version restore question over the history window: the focus on its 복원 시작 behind it, Tab going on there, as Notion's (2026-11-26, two tabs)"],
  ['components/database/RelationPropertyCreationDialog.tsx', 'modal', "a window only where it opens as one (a phone, no toolbar); docked beside its database it holds no page still"],
]

function sourceFiles(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) files.push(...sourceFiles(path))
    else if (/\.tsx?$/u.test(name) && !name.includes('.generated.') && !name.endsWith('.d.ts')) files.push(path)
  }
  return files
}

// What each kind gives, read from overlayKinds.ts's KINDS: { kind: { option: literal text } }.
function kindsGiven() {
  const path = join(sourceRoot, 'lib/overlayKinds.ts')
  const source = ts.createSourceFile(path, readFileSync(path, 'utf8'), ts.ScriptTarget.Latest, true)
  const kinds = {}
  const visit = (node) => {
    if (ts.isVariableDeclaration(node) && node.name.getText(source) === 'KINDS' && node.initializer && ts.isObjectLiteralExpression(node.initializer)) {
      for (const entry of node.initializer.properties) {
        if (!ts.isPropertyAssignment(entry) || !ts.isObjectLiteralExpression(entry.initializer)) continue
        const name = entry.name.getText(source).replace(/^["']|["']$/gu, '')
        kinds[name] = Object.fromEntries(entry.initializer.properties.filter(ts.isPropertyAssignment)
          .map((option) => [option.name.getText(source), option.initializer.getText(source)]))
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  return kinds
}

const KINDS = kindsGiven()
const failures = []
const used = new Set()

for (const path of sourceFiles(sourceRoot)) {
  const file = relative(sourceRoot, path)
  if (BOUNDARY.has(file)) continue
  const text = readFileSync(path, 'utf8')
  if (!/useOverlayLayer\(|usePopoverPlacement\(|<LayerBox\b/u.test(text)) continue
  const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const lineOf = (node) => source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1
  // The object a layer's options are, in this file: through parentheses, a condition's sides, a variable's value.
  const declared = new Map()
  const collect = (node) => {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) declared.set(node.name.text, node.initializer)
    ts.forEachChild(node, collect)
  }
  collect(source)
  const objectsOf = (node, depth = 0) => {
    if (!node || depth > 6) return []
    if (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isSatisfiesExpression?.(node)) return objectsOf(node.expression, depth + 1)
    if (ts.isConditionalExpression(node)) return [...objectsOf(node.whenTrue, depth + 1), ...objectsOf(node.whenFalse, depth + 1)]
    if (ts.isBinaryExpression(node) && [ts.SyntaxKind.AmpersandAmpersandToken, ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(node.operatorToken.kind)) return objectsOf(node.right, depth + 1)
    if (ts.isIdentifier(node) && declared.has(node.text)) return objectsOf(declared.get(node.text), depth + 1)
    if (node.kind === ts.SyntaxKind.NullKeyword || node.kind === ts.SyntaxKind.FalseKeyword || ts.isIdentifier(node) && node.text === 'undefined') return []
    return [node]
  }
  // The kinds an options object names (its own `kind`, or a spread's), and the options it sets itself.
  const read = (object) => {
    // Handed on from the caller (a prop, a parameter): its kind is the caller's to name.
    if (ts.isIdentifier(object) || ts.isPropertyAccessExpression(object)) return { kinds: [], set: {}, handedOn: true }
    if (!ts.isObjectLiteralExpression(object)) return { kinds: [], set: {}, opaque: true }
    const kinds = [], set = {}
    for (const property of object.properties) {
      if (ts.isSpreadAssignment(property)) {
        for (const inner of objectsOf(property.expression)) {
          const spread = read(inner)
          if (!spread.opaque) { kinds.push(...spread.kinds); Object.assign(set, spread.set) }
        }
      } else if (ts.isPropertyAssignment(property) && property.name.getText(source) === 'kind') {
        const values = objectsOf(property.initializer).map((value) => ts.isStringLiteral(value) ? value.text : null)
        kinds.push(...values)
      } else if (ts.isPropertyAssignment(property)) set[property.name.getText(source)] = property.initializer.getText(source)
    }
    return { kinds, set, opaque: false }
  }
  const judge = (node, what, given) => {
    const objects = objectsOf(given)
    if (!objects.length) return
    for (const object of objects) {
      const { kinds, set, opaque, handedOn } = read(object)
      if (handedOn) continue
      if (opaque) { failures.push(`${file}:${lineOf(node)}: ${what}'s options are not an object written in this file`); continue }
      if (!kinds.length) { failures.push(`${file}:${lineOf(node)}: ${what} names no kind (src/lib/overlayKinds.ts)`); continue }
      for (const kind of kinds) {
        if (kind === null || !KINDS[kind]) { failures.push(`${file}:${lineOf(node)}: ${what} names a kind overlayKinds.ts doesn't hold`); continue }
        for (const [option, value] of Object.entries(KINDS[kind])) {
          if (!(option in set) || set[option] === value) continue
          // A layer whose kind turns with a condition (a peek beside the page or in its centre) sets the options that
          // turn with it by the same condition.
          if (kinds.length > 1 && !/^(true|false)$/u.test(set[option])) continue
          const allowed = ALLOWED.find(([where, name]) => where === file && name === option)
          if (allowed) { used.add(allowed); continue }
          failures.push(`${file}:${lineOf(node)}: ${what} of kind "${kind}" sets ${option}: ${set[option]} where its kind gives ${value} — list it below with Notion's reason, or take its kind's`)
        }
      }
    }
  }
  const visit = (node) => {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression)) {
      if (node.expression.text === 'useOverlayLayer') judge(node, 'useOverlayLayer', node.arguments[1])
      if (node.expression.text === 'usePopoverPlacement') {
        if (node.arguments.length < 5) failures.push(`${file}:${lineOf(node)}: usePopoverPlacement takes a layer of no kind — pass its options with a kind, or false`)
        else judge(node, 'usePopoverPlacement', node.arguments[4])
      }
    }
    if ((ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) && node.tagName.getText(source) === 'LayerBox') {
      const layer = node.attributes.properties.find((attribute) => ts.isJsxAttribute(attribute) && attribute.name.getText(source) === 'layer')
      const given = layer?.initializer && ts.isJsxExpression(layer.initializer) ? layer.initializer.expression : null
      if (given) judge(node, 'LayerBox', given)
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
}

for (const entry of ALLOWED) if (!used.has(entry)) failures.push(`${entry[0]}: listed for ${entry[1]}, which no layer there sets against its kind any more`)
if (failures.length) {
  process.stderr.write(`Floating layers off their kinds (src/lib/overlayKinds.ts):\n${failures.map((failure) => `  ${failure}`).join('\n')}\n`)
  process.exit(1)
}
