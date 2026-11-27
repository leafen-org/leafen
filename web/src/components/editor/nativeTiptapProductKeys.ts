import { Extension } from "@tiptap/core";
import { Plugin, PluginKey, Selection, TextSelection, type EditorState } from "@tiptap/pm/state";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { chordLetter, matchesKeyboardShortcut, primaryModifier } from "@/lib/keyboardShortcuts";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { shortcutBlockType } from "./editorInputModel";
import { requestEditorBlockCommand, requestEditorSelectionCommand } from "./editorCommands";
import { requestNativeTiptapEquation } from "./nativeTiptapInlineUiEvents";
import { moveBlocksByKeys } from "./syncedCopyMove";
import { getEditable, placeCaret } from "./focus";
import { caretUpToTitle, caretUpToTitleComment } from "./titleBodyCaret";
import { insertEditorFiles, type FollowLater } from "./mediaBlockUpload";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import { projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import type { EditorView } from "@tiptap/pm/view";
import { pastedSyncedBlock } from "./syncedBlockClipboard";
import { escapeLayerOver, topLayerKind } from "@/lib/overlayLayer";
import { lineArrowStep } from "@/lib/readingDirection";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { requestTableCellChoice } from "./useSimpleTableCellSelection";
import { selectionStopsBefore } from "./nativeTiptapWidgetSelection";
import { spanBlockSelection } from "./blockSelectionKeyboard";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/** The line's words ⌘A chose, while they stay the selection. */
const lineChosenKey = new PluginKey<{ from: number; to: number } | null>("nativeLineChosen");

/**
 * Whether ⌘A chose the words the selection holds, which Notion's ⌘C then
 * copies as their line; all of a line's words dragged over copy as words
 * (seen 2026-11-11: "- bullet two" and "bullet two").
 */
export function lineChosenByKey(state: EditorState) {
  const chosen = lineChosenKey.getState(state);
  return Boolean(chosen && state.selection.from === chosen.from && state.selection.to === chosen.to);
}

export function nativeTiptapProductKeys(options: {
  adapter(): NativeEditorGraphAdapter;
  run(change: () => void): void;
  /** The block whose content the editor shows, which moved blocks stay in. */
  rootId?: string | null;
  /** Chooses a block, where it is drawn (`at`, an occurrence: blockOccurrences), or all of them. */
  selectBlocks?(blockId: string | null, at?: string): void;
  chooseBlocks?(ids: readonly string[], places?: ReadonlyMap<string, string>): void;
  /** Chooses what a paste brought; copied blocks (`copied`) shown (pastedBlocksShown). */
  choosePasted?(ids: readonly string[], copied?: boolean): void;
  /** A later change joined to an earlier step (FollowLater): a paste's files taken as they finish uploading. */
  follow?: FollowLater;
}) {
  /**
   * Escape on words chosen across lines chooses those lines' blocks, the
   * caret and the words gone, as Notion's (seen 2026-11-09: dragged down or
   * up, or Shift+↓, over two or three lines; measured once 2026-11-15 in
   * Chrome 1440 with no script before the key: Shift+↓ over two lines, then
   * Escape, chose both blocks and a letter typed then went nowhere); a popup
   * over them closes first.
   */
  function escapeSeveralLines(view: EditorView, event: KeyboardEvent) {
    const { selection } = view.state;
    if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey || !options.chooseBlocks
      || !(selection instanceof TextSelection) || selection.empty) return false;
    if (escapeLayerOver(view.dom, event)) return true;
    const places = new Map(nativeTiptapTextFields(projectedBlocks(view.state.doc))
      .filter(field => field.contentFrom < selection.to && field.contentFrom + field.node.content.size > selection.from)
      .map(field => [field.block.nativeId, field.block.id] as const));
    if (places.size < 2) return false;
    view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, selection.head)));
    view.dom.blur(); options.chooseBlocks([...places.keys()], places);
    return true;
  }
  /**
   * Shift+↓ on a selection's last line before a block that stops words
   * being chosen (a code block, a picture, a file, a bookmark, an inline
   * database, KEY_STOPS) takes it to
   * the line's end, then chooses the lines it runs over as blocks, the next
   * Shift+↓ choosing on (useBlockSelectionKeys), as Notion's (seen
   * 2026-11-11: from a quote's "qu|ote six" before a code block, "ote six",
   * then the quote, then the code with it); Shift+↑ so upward (measured once
   * 2026-11-15 in Chrome 1440 with no script before the keys: from a line
   * after a code block, the words to its start, then the line as a block,
   * then the code block with it).
   */
  function shiftPastStop(view: EditorView, event: KeyboardEvent) {
    const { selection, doc } = view.state, down = event.key === "ArrowDown";
    if (!event.shiftKey || event.metaKey || event.ctrlKey || event.altKey || !(down || event.key === "ArrowUp") || !options.chooseBlocks
      || !(selection instanceof TextSelection) || selection.$head.parent.type.spec.code || !view.endOfTextblock(down ? "down" : "up")) return false;
    const { $head } = selection;
    // With no text past the stop, the page's end that way.
    const next = Selection.findFrom(doc.resolve(down ? $head.after() : $head.before()), down ? 1 : -1, true)?.$head
      ?? doc.resolve(down ? doc.content.size : 0);
    if (!selectionStopsBefore(doc, selection.$anchor, next)) return false;
    const edge = down ? $head.end() : $head.start();
    if ($head.pos !== edge) {
      view.dispatch(view.state.tr.setSelection(TextSelection.create(doc, selection.anchor, edge)).scrollIntoView());
      return true;
    }
    const places = new Map(nativeTiptapTextFields(projectedBlocks(doc))
      .filter(field => field.contentFrom <= selection.to && field.contentFrom + field.node.content.size >= selection.from)
      .map(field => [field.block.nativeId, field.block.id] as const));
    if (!places.size) return false;
    view.dispatch(view.state.tr.setSelection(TextSelection.create(doc, selection.head)));
    view.dom.blur(); options.chooseBlocks([...places.keys()], places);
    return true;
  }
  /**
   * A Shift+press in another block's text, with the caret or words in one
   * block, chooses the blocks from that one to the pressed one, the caret
   * and the words gone, as Notion's (seen 2026-11-11: no words, no format
   * bar). In the same block it extends the words, and a drag across blocks
   * stays words; a press beside a block's text is BlockFrame's.
   */
  function shiftPressAcross(view: EditorView, event: MouseEvent) {
    const { selection, doc } = view.state;
    if (!event.shiftKey || event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey || !options.chooseBlocks
      || !(selection instanceof TextSelection) || !(event.target instanceof Element) || !event.target.closest("[data-rt-editable]")) return false;
    const fields = nativeTiptapTextFields(projectedBlocks(doc));
    const at = (position: number) => fields.find(field => position >= field.contentFrom && position <= field.contentFrom + field.node.content.size);
    const from = at(selection.anchor), head = at(selection.head);
    const hit = view.posAtCoords({ left: event.clientX, top: event.clientY });
    const to = hit ? at(hit.pos) : undefined;
    if (!from || !to || head?.block.id !== from.block.id || to.block.id === from.block.id) return false;
    // From where each is drawn: a line a synced copy draws again, there (blockOccurrences).
    const adapter = options.adapter();
    const span = spanBlockSelection(adapter.graph, from.block.id, to.block.id, options.rootId ?? null, adapter.pageId);
    if (span.selection.size < 2) return false;
    event.preventDefault();
    view.dispatch(view.state.tr.setSelection(TextSelection.create(doc, selection.head)));
    view.dom.blur(); options.chooseBlocks([...span.selection], span.places);
    return true;
  }
  /**
   * ⌘⌥0–9 on words chosen across lines turn every line the words touch, the
   * words staying chosen for a text type, as Notion's (measured 2026-11-20 in
   * Chrome at 1440 in two tabs five hours apart, words dragged over two or
   * three lines, no script before the keys, one letter typed after: ⌘⌥2,
   * ⌘⌥4 and ⌘⌥5 turned them all and the letter took the words' place; ⌘⌥9
   * left pages, nothing chosen, the letter going nowhere — leaveTurnedLine;
   * ⌘⌥8 left code blocks and the letter went nowhere either, so the editor
   * lets go of the words here).
   */
  function turnLinesUnderWords(view: EditorView, type: NonNullable<ReturnType<typeof shortcutBlockType>>) {
    const { selection } = view.state;
    const ids = [...new Set(nativeTiptapTextFields(projectedBlocks(view.state.doc))
      .filter(field => field.field === "text" && field.contentFrom < selection.to && field.contentFrom + field.node.content.size > selection.from)
      .map(field => field.block.nativeId))];
    if (!ids.length) return false;
    const adapter = options.adapter();
    options.run(() => adapter.changeType(ids, type));
    if (type === "code" && ids.length > 1) view.dom.blur();
    return true;
  }
  return Extension.create({ name: "nativeProductKeys", priority: 2000,
    addProseMirrorPlugins() {
      return [new Plugin({ key: lineChosenKey,
        state: {
          init: () => null,
          apply: (tr, chosen: { from: number; to: number } | null) => {
            if (tr.getMeta(lineChosenKey)) return { from: tr.selection.from, to: tr.selection.to };
            if (!chosen) return null;
            const from = tr.mapping.map(chosen.from), to = tr.mapping.map(chosen.to);
            return tr.selection.from === from && tr.selection.to === to ? { from, to } : null;
          },
        },
        props: { handleDOMEvents: { mousedown: shiftPressAcross }, handleKeyDown: (view, event) => {
        if (event.defaultPrevented || imeComposing(view) || !this.editor.isEditable) return false;
        // A key on the heels of a click or a caret key acts where the caret is now.
        takeDomSelection(view);
        if (shiftPastStop(view, event)) return true;
        const target = nativeTiptapSelectionContext(view.state);
        if (!target) {
          // Words chosen across blocks take a comment from ⌘⇧M too, as Notion's (seen 2026-11-09).
          if (!view.state.selection.empty && matchesKeyboardShortcut("addComment", event)) return requestEditorSelectionCommand("comment");
          const turned = shortcutBlockType(event);
          if (turned) return turnLinesUnderWords(view, turned);
          return event.key === "Escape" && escapeSeveralLines(view, event);
        }
        const adapter = options.adapter(), blockId = target.block.nativeId;
        // A line shown again in a synced copy on its original's page: the copy it is shown in.
        const shownIn = target.block.projectionParentId !== target.block.parentId ? target.block.projectionParentId : null;
        const type = shortcutBlockType(event);
        if (type && target.field === "text") {
          // ⌘⌥9 leaves the page block in place, unopened, as the block menu's
          // "전환 → 페이지" and the "/" menu's "페이지 · 전환" do (Notion's, seen
          // 2026-11-18 in Chrome at 1440 in two tabs); where the caret goes
          // then is leaveTurnedLine's (caretLineTurnedTextless.ts).
          options.run(() => adapter.changeType([blockId], type));
          return true;
        }
        if (primaryModifier(event) && !event.altKey && !event.shiftKey && chordLetter(event) === "a") {
          const from = target.contentFrom, to = from + target.node.content.size;
          if (view.state.selection.from === from && view.state.selection.to === to && options.selectBlocks) {
            view.dom.blur(); options.selectBlocks(null);
          } else view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, from, to)).setMeta(lineChosenKey, true));
          return true;
        }
        // A popup over the text that Escape closes takes it first; a discussion's card opened from its text stays (escapeLayerOver).
        if (event.key === "Escape" && escapeLayerOver(view.dom, event)) return true;
        if (event.key === "Escape" && !(event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) && options.selectBlocks) {
          // In code or any caption the first goes to its start, the second chooses the block, as Notion's (seen 2026-11-04,
          // 11-05; 2026-11-06 for picture, bookmark, video and embed captions, the caret put anywhere by a press, by typing
          // after the caption command from the menu, the toolbar or ⌘⌥M, after a mention, or round chosen words).
          // In a simple table's cell so does the first; the second chooses the cell (useSimpleTableCellSelection
          // climbs on from there to its row and the table, as Notion's, seen 2026-11-07).
          // Measured 2026-11-15 in Chrome 1440 with no script before the keys, a letter typed after: code's and
          // a cell's first Escape put it before their text, a second Escape in a row chose the code block; in a
          // cell End, Escape, Escape chose the cell, the letter then going to its end (three times, two tabs an
          // hour and a half apart — the row chosen once before was not seen again). Escape at code's start just
          // after a Backspace split, chose the block three times and kept the caret four, no condition found
          // (not a fresh page, not the pause between the keys), so it stays the second Escape's rule.
          const { selection } = view.state;
          const cell = typeof target.field === "number" ? target.field : null;
          const stepped = selection.$from.parent.type.spec.code || target.field === "caption" || cell !== null;
          if (stepped && !(selection.empty && selection.from === target.contentFrom)) {
            view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, target.contentFrom)));
            return true;
          }
          view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, view.state.selection.head)));
          if (cell !== null) { requestTableCellChoice(blockId, cell); return true; }
          // Chosen where its line is drawn: in a synced copy, there.
          view.dom.blur(); options.selectBlocks(blockId, target.block.id); return true;
        }
        const command = matchesKeyboardShortcut("addComment", event) ? "comment"
          : matchesKeyboardShortcut("blockColor", event) ? "color"
            : matchesKeyboardShortcut("addLink", event) ? "link"
              : matchesKeyboardShortcut("inlineEquation", event) ? "equation"
                : matchesKeyboardShortcut("clearFormatting", event) ? "clear" : null;
        // With a caret, ⌘⇧H colors its block with the color last used, and
        // ⌘⇧E makes an equation there, as Notion's (seen 2026-11-11); code holds none.
        if (command === "color" && view.state.selection.empty) { requestEditorBlockCommand(blockId, "color"); return true; }
        // ⌘⇧M with a caret comments on its block, its text all marked, as the
        // block's own "댓글" does and Notion's (seen 2026-11-11); in a simple
        // table's cell, on its table, as Notion's (seen 2026-11-22 in Chrome at
        // 1440: the field by the table's top, the thread the table's).
        if (command === "comment" && view.state.selection.empty) {
          requestEditorBlockCommand(typeof target.field === "number" ? target.block.parentId ?? blockId : blockId, "comment"); return true;
        }
        if (command === "equation" && view.state.selection.empty) {
          if (!adapter.readOnly && !view.state.selection.$from.parent.type.spec.code) requestNativeTiptapEquation({ view }, view.state.selection.from, view.state.selection.from);
          return true;
        }
        if (command) return requestEditorSelectionCommand(command);
        if (matchesKeyboardShortcut("copyBlockLink", event)) { requestEditorBlockCommand(blockId, "copyLink"); return true; }
        // ⌘/ with a menu open at the caret ("/", "@", "[[", ":", "+") does nothing, the menu staying and the next letter
        // its filter, as Notion's (2026-11-26 in Chrome at 1440, two tabs at two times, judged by the screen and one letter
        // after the key with nothing run in the page before it).
        if (matchesKeyboardShortcut("openBlockMenu", event) && topLayerKind() === "caret-menu") return true;
        // In a caption or a simple table's cell ⌘/ opens its block's own
        // menu — the picture's, the table's — as Notion's do (seen 2026-11-07,
        // 2026-11-13: in a cell, the table's menu).
        // A cell's line is a row of its table: the menu is the table's.
        if ((target.field === "caption" || typeof target.field === "number") && matchesKeyboardShortcut("openBlockMenu", event)) {
          requestEditorBlockCommand(typeof target.field === "number" ? target.block.parentId ?? blockId : blockId, "menu"); return true;
        }
        // ⌘⇧P and ⌘⇧↑↓ in a caption move its block too, the block then
        // chosen and the caret gone, as Notion's picture and code captions
        // do (seen 2026-11-09); ⌘D there is the caret's (useBlockSelectionKeys).
        if (target.field === "caption") {
          if (matchesKeyboardShortcut("moveBlock", event)) { requestEditorBlockCommand(blockId, "move"); return true; }
          const step = matchesKeyboardShortcut("moveBlockUp", event) ? "up" : matchesKeyboardShortcut("moveBlockDown", event) ? "down" : null;
          if (!step) return false;
          options.run(() => moveBlocksByKeys(options.adapter(), new Set([blockId]), step, options.rootId, { shownIn }));
          view.dom.blur(); options.selectBlocks?.(blockId, target.block.id); return true;
        }
        if (target.field !== "text") return false;
        // At the start of the page's first line, with nothing before it to
        // go to, the arrow back goes on to the end of the page's title, as
        // Notion's — ←, or → in a line read from the right (lineArrowStep); ↑ on
        // its first line, to the title's last line above the caret
        // (titleBodyCaret, seen 2026-11-13), or to the comment field under
        // the title when the page shows one (seen 2026-11-14).
        // Backspace on an empty first line takes the line away too (seen
        // 2026-11-02: a database page's emptied body showed its template
        // prompt again); on one with words it puts them in the title
        // (nativeTiptapMergeKeys).
        const { selection } = view.state;
        const plain = !(event.metaKey || event.ctrlKey || event.altKey || event.shiftKey);
        const emptyLine = event.key === "Backspace" && selection.$from.depth === 2 && target.block.container.attrs.kind === "paragraph"
          && target.node.content.size === 0 && !adapter.graph.blocks.some(block => block.parentId === blockId);
        const back = (event.key === "ArrowLeft" || event.key === "ArrowRight") && selection.empty && selection.$from.parentOffset === 0
          && lineArrowStep(event.key, view.domAtPos(selection.from).node.parentElement, selection.$from.parent.textContent) < 0;
        if (plain && selection.empty && (event.key === "ArrowUp" || back || emptyLine)) {
          const atStart = event.key === "ArrowUp" ? view.endOfTextblock("up") : selection.$from.parentOffset === 0;
          const title = atStart && !Selection.findFrom(view.state.doc.resolve(target.contentFrom - 1), -1)
            ? getEditable(`title:${adapter.pageId}`) : undefined;
          if (title && emptyLine) options.run(() => adapter.remove([blockId]));
          if (title && event.key === "ArrowUp" && (caretUpToTitleComment(title, adapter.pageId)
            || caretUpToTitle(title, view.coordsAtPos(selection.from).left))) return true;
          if (title && placeCaret(title, "end")) return true;
        }
        // While typing, the line's block keys do what they do to a selected
        // block, as Notion's: its menu, moving it up or down with the caret
        // kept in it, and Cmd/Ctrl+Enter checking a to-do or opening a toggle
        // (the editor before Tiptap did; 2026-10-29), and "Move to" taking the
        // block, with its window at the block (seen 2026-11-05; a page's title
        // and no caret move the page).
        if (matchesKeyboardShortcut("openBlockMenu", event)) { requestEditorBlockCommand(blockId, "menu"); return true; }
        if (matchesKeyboardShortcut("moveBlock", event)) { requestEditorBlockCommand(blockId, "move"); return true; }
        // ⌘⌥M in a code block's text opens its caption, the caret at its end,
        // as its menu's "캡션" does and Notion's (seen 2026-11-13).
        if (matchesKeyboardShortcut("toggleCaption", event) && selection.$from.parent.type.spec.code) {
          requestEditorBlockCommand(blockId, "caption"); return true;
        }
        const direction = matchesKeyboardShortcut("moveBlockUp", event) ? "up" : matchesKeyboardShortcut("moveBlockDown", event) ? "down" : null;
        if (direction) {
          options.run(() => moveBlocksByKeys(adapter, new Set([blockId]), direction, options.rootId, { shownIn, caret: target.start }));
          return true;
        }
        const block = adapter.graph.blocks.find(candidate => candidate.id === blockId);
        if (block && fieldEnter(event) && primaryModifier(event) && !event.shiftKey && !event.altKey) {
          if (block.type === "to_do") { options.run(() => adapter.updateBlock(block, { checked: block.content.checked !== true })); return true; }
          if (TOGGLE_BLOCK_TYPES.has(block.type as never)) {
            options.run(() => adapter.setToggleCollapsed(block, block.content.collapsed !== true)); return true;
          }
        }
        return false;
      },
      // "Copy and sync" pastes a copy of the synced block and copied files
      // (an image) become file blocks unless rich text comes with them — the
      // editor before Tiptap did both (2026-10-25: neither did anything). They
      // take an empty paragraph's place or go after the line, chosen, as
      // Notion's do (seen 2026-11-03).
      handlePaste: (view, event) => {
        if (!this.editor.isEditable || !event.clipboardData) return false;
        const target = nativeTiptapSelectionContext(view.state);
        if (!target || target.field !== "text" || view.state.selection.$from.parent.type.spec.code) return false;
        const adapter = options.adapter(), blockId = target.block.nativeId, data = event.clipboardData;
        const empty = target.block.container.attrs.kind === "paragraph" && target.node.content.size === 0;
        const insert = (made: () => readonly string[], copied = false) => {
          let ids: readonly string[] = [];
          options.run(() => {
            ids = made();
            if (ids.length && empty) adapter.remove([blockId]);
          });
          if (ids.length) options.choosePasted?.(ids, copied);
          return true;
        };
        const synced = pastedSyncedBlock(data);
        if (synced) return insert(() => {
          const created = adapter.insertAfter(blockId, "synced_block", { syncedBlockId: synced.blockId, syncedPageId: synced.pageId });
          return created ? [created.id] : [];
        }, true);
        const files = Array.from(data.files ?? []);
        if (files.length && (!data.types.includes("text/html") || !data.getData("text/plain").trim())) {
          return insert(() => insertEditorFiles(adapter, blockId, files, "pasted", options.follow));
        }
        return false;
      } } })];
    },
  });
}
