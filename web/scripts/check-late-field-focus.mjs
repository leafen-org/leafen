#!/usr/bin/env node

// A field to write in that a press opens takes the caret once it is drawn,
// and what is typed from the press until then is held for it and typed there
// — src/components/editor/openedFieldCaret.ts (focusOpenedField, and a new
// comment's box). Focused a frame on with nothing held, the first keys went
// where the caret had been — the page's text, a code block, a table's cell —
// or nowhere (2026-11-22, two dozen fields).
//
// So this check refuses, anywhere in web/src, a focus given a frame or a
// task on: a requestAnimationFrame or setTimeout callback that focuses, of
// any length, written in place or a function of the same file named there;
// an effect that focuses when a request counted up elsewhere comes
// (`…Request`, `…Turn` among its dependencies), a render or more after the
// press that counted it (2026-11-25: a thread's reply box took the caret two
// renders after the press, empty, and the letters typed meanwhile were
// lost; a margin card's reply field a frame on, retried); and the editor's
// own focus command (tiptap's `commands.focus(…)`, `chain().focus()`),
// which focuses in a requestAnimationFrame of its own: a field's editor
// takes the caret at once through focusEditorNow
// (src/components/editor/nativeTiptapFocus.ts; 2026-11-23: ⌘⇧R's rename
// window took the caret a frame after it showed, and an Enter typed then
// split the page's title) — but where the focus goes to no field to write
// in (a button, a menu, a window, a list), is given at once already, is the
// editor's that has it (a mark on chosen words), or is no press's (shown as
// the app starts, opened at an address, given back once an answer comes).
// The focus given back as a window, menu or question closes goes through one
// function, giveFocusBack (src/lib/focusGiveBack.ts), not a frame of its own.
// Such a focus is listed below with the reason, by words its callback or
// effect holds; an entry that no longer matches one fails the check too.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')

// [file, words the line holds, why it may].
const ALLOWED = [
  ['components/UpdatesPanel.tsx', 'closeRef.current?.focus()', 'the window\'s close button'],
  ['components/WorkspaceCreateDialog.tsx', 'nameRef.current?.focus()', 'focused at once in the same effect; again a frame on only if the layer moved it'],
  ['components/TrashView.tsx', 'listRef.current?.focus()', 'a list'],
  ['components/CommentsPanel.tsx', '(() => controller.draftRef.current?.focus())', 'a reaction thread gone turns the card into a new comment\'s box, no press opening it'],
  ['components/CommentsPanel.tsx', 'if (!autoFocusRef.current || docked || input.threadIds) return;', 'the hold\'s own landing (giveNewCommentCaret), or the panel itself'],
  ['components/CommentsPanel.tsx', 'askedToWrite.current = true;', 'the hold\'s own landing (giveNewCommentCaret)'],
  ['components/CommentsPanel.tsx', 'if (reply) linkedCaret.current = true;', 'a comment opened at its address (a notice, a link), no press opening it'],
  ['components/PublicSiteView.tsx', 'menuToggleRef.current)?.focus()', 'a button'],
  ['components/admin/AdministratorApps.tsx', 'row?.scrollIntoView({ block: "center" })', 'a row\'s button'],
  ['components/admin/AdministratorApps.tsx', 'if (setUp) setSecretsFor(installed);', 'a row\'s button'],
  ['components/admin/AdministratorApps.tsx', '[data-app-restore]', 'a button'],
  ['components/admin/AdministratorUserManagement.tsx', 'newButton.current?.focus()', 'a button'],
  ['components/comments/MarginDiscussions.tsx', 'const reply = openCard.current?.querySelector<HTMLElement>("[data-comment-reply-input]");', 'a card opened at its address, no press opening it (a press opens one through replyRowCaret.ts)'],
  ['components/database/DatabaseViewTabs.tsx', 'tab?.focus();', 'a tab'],
  ['components/database/DatabaseViewTabs.tsx', 'rootRef.current?.contains(active))) name.focus()', 'the name the press gave the caret (focusOpenedField) kept while the closing menu hands the focus back to its tab'],
  ['components/database/RowPagePropertyMenu.tsx', 'if (root && !root.contains(document.activeElement)) root.focus', 'a menu'],
  ['components/database/RowPropertyComments.tsx', 'giveNewCommentCaret(panelRef.current', 'the hold\'s own landing (openedFieldCaret)'],
  ['components/database/peekFocusReturn.ts', 'if (back?.isConnected && lost) back.focus();', 'the focus back where it was as the peek closes'],
  ['components/database/rowPageTitleCaret.ts', 'peekFieldFocused(panel.current)) panel.current?.focus()', 'the peek\'s panel, when no caret is asked'],
  ['components/database/tableCellKeys.ts', 'In a table its cell is chosen again', 'a table cell, not its words'],
  ['components/database/useCellChosenAfterPressAway.ts', 'nowhere(document.activeElement)) cell.focus', 'a table cell, not its words'],
  ['components/desktop/DesktopStripMenus.tsx', 'filter.focus({ preventScroll: true })', 'the keys come from the strip as the app gives them to the page: nothing is typed in the page before'],
  ['components/editor/useBlockMenuCaretReturn.ts', 'editor.view.focus();', 'the caret back in the editor as the block menu closes'],
  ['components/forms/FormPeek.tsx', 'if (!panelRef.current?.contains(document.activeElement)) panelRef.current?.focus();', 'a window'],
  ['components/page/PagePeek.tsx', 'if (holdingTyping() || peekFieldFocused(panelRef.current)) return;', 'the peek\'s panel, the title\'s caret given by caretAtPeekTitle'],
  ['components/page/TopBarShare.tsx', 'dialog.focus({ preventScroll: true })', 'a window'],
  ['components/shell/sheetFocusReturn.ts', 'place.focus({ preventScroll: true })', 'the focus back where it was as a sheet closes'],
  ['components/sidebar/sidebarViewNewRow.ts', 'placeCaret(title, "select")', 'a new row\'s title under its own hold (holdTypingForField, requestPageTitleFocus), kept once its page takes the keys'],
  ['lib/focusGiveBack.ts', 'place.focus(options)', 'the focus given back as a window, menu or question closes, where and how its site says'],
  ['lib/focusThroughWaits.ts', 'function settle()', 'the focus given back once a locked control\'s request is answered'],
  ['lib/focusThroughWaits.ts', 'function giveBack()', 'the focus given back once a locked control\'s request is answered'],
  ['lib/focusThroughWaits.ts', 'field && usable(field) && control.isConnected', 'the focus given back once a locked control\'s request is answered'],
  ['lib/nativeActionMenu.tsx', 'if (document.activeElement === menu.current) field.focus', 'the menu holds the keys until its search is drawn'],
  ['lib/nativeMenu.tsx', 'owner instanceof HTMLElement && owner.isConnected && lost', 'the focus back to the menu\'s owner'],
  ['lib/nativeTableRowSteps.ts', '[data-table-cell][data-title="true"]', 'a table cell, not its words'],
  ['lib/overlayLayer.ts', 'if (nowhere()) root.focus', 'a layer\'s own frame, while the focus is nowhere'],
  ['lib/overlayLayer.ts', 'holdsPlaced(root, document.activeElement)', 'the layer\'s entry, focused in the pass it is drawn; a frame on only for one drawn later or a phone\'s'],
  ['lib/webkitPressFocus.ts', 'control.focus({ preventScroll: true })', 'a pressed button WebKit leaves unfocused'],
  ['components/LocalLockGate.tsx', 'inputRef.current?.focus()', 'shown as the app starts, no press opening it'],
  ['components/PageHistoryDialog.tsx', 'startRestoreRef.current?.focus()', 'a button'],
  ['components/sidebar/PageTreeItem.tsx', 'rowRef.current?.focus()', 'a row'],
  ['components/sidebar/SidebarTopActions.tsx', 'button.current?.focus({ preventScroll: true })', 'a button'],
  ['components/sidebar/SidebarFooter.tsx', 'button.current?.focus({ preventScroll: true })', 'a button'],
  ['components/settings/WorkspaceAIConnections.tsx', 'keyOpenRef.current?.focus()', 'a button'],
  ['components/settings/WorkspaceSettingsOverview.tsx', 'iconButtonRef.current?.focus()', 'a button'],
  ['components/settings/WorkspaceLandingPage.tsx', 'buttonRef.current?.focus()', 'a button'],
  ['components/database/TablePropertyMenu.tsx', 'menuRef.current?.focus({ preventScroll: true })', 'a menu'],
  ['components/database/PropertyNumberCell.tsx', 'inputRef.current?.focus({ preventScroll: true })', 'focused at once in the same effect; again a frame on'],
  ['components/database/CardValuePress.tsx', 'menuRef.current?.focus({ preventScroll: true })', 'a menu'],
  ['components/database/TableAddProperty.tsx', 'buttonRef.current?.focus()', 'a button'],
  ['components/database/FormPreviewDialog.tsx', '(firstRef.current ?? backdropRef.current)?.focus()', 'a button or the window'],
  ['components/database/RowPageAddProperty.tsx', 'buttonRef.current?.focus()', 'a button'],
  ['components/database/PropertyTextCells.tsx', 'inputRef.current?.focus({ preventScroll: true })', 'focused at once in the same effect; again a frame on'],
  ['components/database/TablePropertyMenuHead.tsx', '(menu ?? button)?.focus({ preventScroll: true })', 'a menu or a button'],
  ['components/database/FeedPostThreads.tsx', 'giveNewCommentCaret(panelRef.current', 'the hold\'s own landing (openedFieldCaret)'],
  ['components/database/TablePropertyHeader.tsx', 'cellOnLine(grid, line, column)?.focus({ preventScroll: true })', 'a table cell, not its words'],
  ['components/database/freshRows.ts', 'above.focus({ preventScroll: true })', 'a row'],
  ['components/comments/MarginCommentDraft.tsx', 'giveNewCommentCaret(box.current', 'the hold\'s own landing (openedFieldCaret)'],
  ['components/comments/MarginCommentDraft.tsx', 'if (caretBack()) draftRef.current?.focus();', 'the caret back once a send is answered (caretMayComeBack)'],
  ['components/comments/useCommentsPanelController.ts', 'if (caretBack()) draftRef.current?.focus();', 'the caret back once a send is answered (caretMayComeBack)'],
  ['components/page/TopBar.tsx', 'else trigger?.focus()', 'a button'],
  ['components/page/InlineDatabasePageTitle.tsx', 'iconButtonRef.current?.focus()', 'a button'],
  ['components/page/PageCover.tsx', 'coverRef.current?.focus()', 'the cover'],
  ['components/page/PageCover.tsx', 'changeButtonRef.current?.focus()', 'a button'],
  ['components/forms/FormDropdown.tsx', 'if (document.activeElement === document.body) field.current?.focus();', 'focused at once as the list closes; again a frame on only if the list took it'],
  ['components/forms/FormReferencePicker.tsx', 'if (document.activeElement === document.body) field.current?.focus();', 'focused at once as the list closes; again a frame on only if the list took it'],
  ['components/mcp/MCPAuthorizationPage.tsx', 'headingRef.current?.focus()', 'a heading'],
  ['components/mcp/NotionAuthorizationPage.tsx', 'headingRef.current?.focus()', 'a heading'],
  ['components/people/PersonPeek.tsx', 'panelRef.current?.focus()', 'a window'],
  ['components/people/PeoplePeek.tsx', 'panelRef.current?.focus()', 'a window'],
  ['components/automation/AutomationVariablesFields.tsx', 'menu.focus({ preventScroll: true })', 'a menu'],
  ['components/account/AccountSettingsPage.tsx', 'profileIconButtonRef.current?.focus()', 'a button'],
  ['components/account/AccountPasskeysSection.tsx', 'managerRef.current?.focus()', 'a button'],
  ['components/account/AccountEmailManagement.tsx', '(returnFocus.current?.isConnected ? returnFocus.current : addRef.current)?.focus()', 'a button'],
  ['components/account/AccountEmailManagement.tsx', 'addRef.current?.focus({ preventScroll: true })', 'a button'],
  ['components/import/ToolImportWindow.tsx', 'closeRef.current?.focus()', 'a button'],
  ['components/apps/DocumentPreview.tsx', 'button.current?.focus({ preventScroll: true })', 'a button'],
  ['components/editor/TabBlockContent.tsx', 'tabButtonId(block.id, itemId))?.focus()', 'a tab'],
  // The editor's own focus command where the editor has the focus already: a
  // mark put on the chosen words from their bar, which keeps the focus while
  // it is pressed; on a phone tiptap gives it at once (iOS, Android).
  ['components/editor/NativeTiptapSelectionToolbar.tsx', 'editor.chain().focus()', 'the chosen words\' bar: the editor keeps the focus'],
  ['components/editor/chosenWordsActions.ts', 'editor.chain().focus()', 'the chosen words\' bar: the editor keeps the focus'],
  ['components/editor/PhoneSelectionTools.tsx', 'editor.chain().focus()', 'a phone\'s tools: tiptap focuses at once there'],
  ['components/editor/PhoneSelectionTools.tsx', 'field.chain().focus()', 'a phone\'s tools: tiptap focuses at once there'],
  ['components/editor/phoneTitleTools.ts', 'editor.chain().focus()', 'a phone\'s tools: tiptap focuses at once there'],
]

function sources(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) return sources(path)
    return /\.(?:ts|tsx)$/u.test(name) && !/\.test\.tsx?$/u.test(name) ? [path] : []
  })
}

const FOCUS = /\.focus(?:End|Start)?\(|commands\.focus\(|chain\(\)\.focus\(/u
const EDITOR_FOCUS = /commands\.focus\(|chain\(\)\.focus\(/u

// Where the bracket opened at `open` closes, past strings and comments.
function closing(text, open) {
  let depth = 0
  for (let at = open; at < text.length; at++) {
    const char = text[at]
    if (char === '"' || char === "'" || char === '`') {
      for (at++; at < text.length && text[at] !== char; at++) if (text[at] === '\\') at++
    } else if (char === '/' && text[at + 1] === '/') at = text.indexOf('\n', at) === -1 ? text.length : text.indexOf('\n', at)
    else if (char === '/' && text[at + 1] === '*') at = text.indexOf('*/', at) + 1
    else if ('([{'.includes(char)) depth++
    else if (')]}'.includes(char) && --depth === 0) return at
  }
  return text.length - 1
}

// A function of the file by its name, from its name to its body's end.
function namedFunction(text, name) {
  const found = new RegExp(`(?:function\\s+${name}\\s*\\(|(?:const|let)\\s+${name}\\s*=)`, 'u').exec(text)
  if (!found) return null
  const body = text.indexOf('{', found.index)
  return body < 0 ? null : text.slice(found.index, closing(text, body) + 1)
}

// The late focuses of a file: [where it starts, the words it holds].
function lateFocuses(text) {
  const late = []
  for (const call of text.matchAll(/(?<![\w.])(?:window\.)?(?:requestAnimationFrame|setTimeout)\(/gu)) {
    const open = call.index + call[0].length - 1
    const argument = text.slice(open, closing(text, open) + 1)
    if (FOCUS.test(argument)) { late.push([call.index, argument]); continue }
    const name = /^\(\s*([A-Za-z_$][\w$]*)\s*[,)]/u.exec(argument)?.[1]
    const named = name ? namedFunction(text, name) : null
    if (named && FOCUS.test(named)) late.push([call.index, named])
  }
  for (const effect of text.matchAll(/\buseEffect\(/gu)) {
    const open = effect.index + effect[0].length - 1
    const argument = text.slice(open, closing(text, open) + 1)
    const dependencies = argument.slice(argument.lastIndexOf('['))
    if (/\b\w+(?:Request|Turn)\b/u.test(dependencies) && FOCUS.test(argument)) late.push([effect.index, argument])
  }
  for (const line of text.matchAll(/^.*$/gmu)) if (EDITOR_FOCUS.test(line[0])) late.push([line.index, line[0]])
  return late
}

const used = new Set()
const refused = []
for (const path of sources(sourceRoot)) {
  const file = relative(sourceRoot, path)
  const text = readFileSync(path, 'utf8')
  for (const [at, words] of lateFocuses(text)) {
    const allowed = ALLOWED.findIndex(([place, held]) => place === file && words.includes(held))
    if (allowed >= 0) { used.add(allowed); continue }
    const line = text.slice(0, at).split('\n').length
    const shown = words.split('\n').find((part) => FOCUS.test(part))?.trim() ?? words.split('\n')[0].trim()
    refused.push(`${file}:${line}: a focus given a frame or a task on — a field a press opens takes it through focusOpenedField (components/editor/openedFieldCaret.ts), an editor's caret through focusEditorNow (components/editor/nativeTiptapFocus.ts), the focus given back as something closes through giveFocusBack (lib/focusGiveBack.ts), or list it here with why\n  ${shown}`)
  }
}
const stale = ALLOWED.filter((_, index) => !used.has(index)).map(([at, words]) => `${at}: nothing late holds "${words}" any more; take it off the list`)
if (refused.length || stale.length) {
  process.stderr.write(`${[...refused, ...stale].join('\n')}\n`)
  process.exit(1)
}
