import { Extension } from "@tiptap/core";
import type { Editor } from "@tiptap/core";
import { Plugin, EditorState, TextSelection, type Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import * as Y from "yjs";
import { ReplaceStep } from "@tiptap/pm/transform";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { newId } from "@/lib/ids";
import { shortcutTextMark } from "./editorInputModel";
import { nativeProjectionMeta, projectedBlocks } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { requestSlashLine } from "./slashLineRequest";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import { createNativeSuggestedDraft } from "./nativeTiptapSuggestedDraft";
import { drawSuggestedBreak } from "./nativeTiptapSuggestedBreak";
import { BLOCK_TEXT_ROOT_PREFIX } from "@/lib/blockTextEditing";
import { SuggestingInputQueue } from "./suggestingInputQueue";
import { SUGGESTING_BLOCK_TYPES, suggestedBreakTaken, suggestedNextBlock, suggestedSplitBlock, suggestingTextRuns } from "./suggestingInputModel";
import type { SuggestingMode } from "./suggestingMode";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { compositionEnds, fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// The list blocks whose empty item Enter ends (unlist).
const LIST_KINDS: ReadonlySet<string> = new Set(["bulleted_list_item", "numbered_list_item", "to_do"]);

/** Suggested input is a native API intent, never a canonical editor write. */
export function createNativeTiptapSuggestions(options: {
  adapter(): NativeEditorGraphAdapter;
  mode(): SuggestingMode | undefined;
  current(): boolean;
  settled(blockId: string, caret: number | null, moved: boolean): void;
}) {
  const queues = new Map<string, SuggestingInputQueue>();
  const drafts = new Map<string, ReturnType<typeof createNativeSuggestedDraft>>();
  // Lines a break drew before the page holds their blocks (drawSuggestedBreak), by
  // their blocks' ids, with whether each came: until then the page is not drawn again over them.
  const drawn = new Map<string, Promise<boolean>>();
  // Lines drawn for blocks a menu picked (suggestBreak's `picked`), until the page holds them.
  const pickedLines = new Set<string>();
  // A drawn line refused while the caret was in it: the caret goes back to where Enter was pressed.
  let returnTo: { blockId: string; offset: number } | null = null;
  let proposals = 0;
  const handled = new WeakSet<Transaction>();
  let editor: Editor;
  let composition: EditorState | null = null;
  let compositionPending = false;
  let moved = false, disposed = false, frame = 0;
  // A composition's words are proposed once it ends, before a next one starts
  // (fieldKeyBoundary.ts): macOS Chrome's Korean starts the next syllable in
  // the same key press, and proposing a frame later let go of that one, its
  // words left out of the suggestion. `ended` is the state the composition began on.
  let ended: EditorState | null = null;
  const compositionEnd = compositionEnds(() => disposed || !editor || editor.isDestroyed ? null : editor.view, () => {
    const before = ended;
    if (!before) return;
    ended = null; composition = null;
    if (!active()) return;
    compositionPending = true; propose(before, editor.state);
    const selected = target(before);
    if (selected && !queue(selected.block.nativeId).busy && !drawn.size) {
      compositionPending = false; options.settled(selected.block.nativeId, null, true);
    }
  });
  const active = () => !disposed && options.current() && options.mode()?.active === true
    && !!options.adapter().collaborationDocument;
  function target(state: EditorState) {
    const value = nativeTiptapSelectionContext(state);
    return value?.field === "text" && SUGGESTING_BLOCK_TYPES.has(value.block.container.attrs.kind) ? value : null;
  }
  function queue(blockId: string) {
    let value = queues.get(blockId);
    if (value) return value;
    const document = options.adapter().collaborationDocument!;
    value = new SuggestingInputQueue({ document, blockId, after: drawn.get(blockId),
      suggest: async edit => {
        if (!active() || options.adapter().collaborationDocument !== document) throw new Error("Suggestion scope ended.");
        return await options.mode()!.suggest(edit);
      },
      settled: caret => {
        if (!disposed && options.current()) {
          compositionPending = false;
          if (idle()) {
            const back = returnTo; returnTo = null; drafts.clear();
            if (back) options.settled(back.blockId, back.offset, false); else options.settled(blockId, caret, moved);
          }
        }
      },
      // The suggested block reaches the editor with a later graph than the
      // reread's: the caret goes into it once it is there, as Notion's does
      // (2026-10-16: it stayed at the line's end and what was typed next
      // joined the line above). A line drawn for it already holds the caret
      // and what was typed since: the page is drawn again once nothing waits.
      blockSuggested: (caretBlockId, newBlockId) => {
        const adapter = options.adapter();
        void adapter.graphSync?.reread().then(() => {
          let tries = 60;
          const place = () => {
            if (!active()) return;
            const there = options.adapter().graph.blocks.some(block => block.id === newBlockId);
            if (!there && --tries > 0) { frame = requestAnimationFrame(place); return; }
            if (drawn.delete(newBlockId)) { settleDrawn(); return; }
            if (there) options.settled(caretBlockId, 0, moved);
          };
          place();
        });
      },
    });
    queues.set(blockId, value); return value;
  }
  const idle = () => !composition && !proposals && !drawn.size && ![...queues.values()].some(queue => queue.busy);
  // Once nothing waits, the page is drawn again with the caret where it is.
  function settleDrawn() {
    if (!idle() || !editor) return;
    const here = target(editor.state), back = returnTo;
    returnTo = null; drafts.clear();
    if (back) options.settled(back.blockId, back.offset, false);
    else options.settled(here?.block.nativeId ?? "", here ? here.start : null, moved);
  }
  /** Enter's new line drawn at once (drawSuggestedBreak), its block suggested after what was asked before it. */
  function drawBreak(
    view: EditorView, selected: NonNullable<ReturnType<typeof target>>, came: Promise<boolean>, newBlockId: string,
    next: { type: string; firstChild: boolean }, splitType: string | null,
  ) {
    const document = options.adapter().collaborationDocument!;
    // Suggested text after a break in the line keeps it whole — such a break is refused, so none is drawn —
    // unless it is the suggestion the break goes on with (suggestedBreakTaken).
    const runs = suggestingTextRuns(document.getText(BLOCK_TEXT_ROOT_PREFIX + selected.block.nativeId).toDelta());
    if (selected.start > 0 && selected.start < selected.text.length && !suggestedBreakTaken(runs, selected.start)) return;
    const shown = drawSuggestedBreak(view.state, selected, newBlockId, next, splitType);
    if (!shown) return;
    const text = document.getText(BLOCK_TEXT_ROOT_PREFIX + newBlockId);
    const origin = { blockId: selected.block.nativeId, offset: selected.start };
    drawn.set(newBlockId, came);
    drafts.set(newBlockId, createNativeSuggestedDraft(text, { length: shown.length, ready: came }));
    if (shown.cutAt !== null) draft(selected.block.nativeId).cut(shown.cutAt);
    // Refused, the line goes as the page is drawn again.
    void came.then(landed => {
      if (landed || !drawn.delete(newBlockId)) return;
      if (target(editor.state)?.block.nativeId === newBlockId) returnTo = origin;
      settleDrawn();
    });
    view.dispatch(shown.tr);
  }
  /**
   * A block suggested from the caret's line as Enter suggests it, its line
   * drawn at once; one a menu picked (`picked`) is a suggestion of its own,
   * no break before it (page_suggestion_picked.go).
   */
  function suggestBreak(view: EditorView, selected: NonNullable<ReturnType<typeof target>>, next: { type: string; firstChild: boolean }, split: string | null,
    picked = false, id = newId()) {
    const intent = queue(selected.block.nativeId), selection = draft(selected.block.nativeId).select(selected.start, selected.end);
    // A break at the text's start stays at its start while letters go in before the text there.
    const at = selected.start === 0 && selected.end === 0 && selected.text.length > 0
      ? { ...selection, start: lineStart(selected.block.nativeId), end: lineStart(selected.block.nativeId) } : selection;
    if (picked) pickedLines.add(id);
    drawBreak(view, selected, intent.newBlock(at, next.type, id, next.firstChild, split, picked), id, next, split);
    moved = false;
  }
  // An empty list item, to-do or numbered item suggested in (Enter's or a "/" row's).
  const unlists = (selected: NonNullable<ReturnType<typeof target>>) => selected.text === "" && selected.block.id === selected.block.nativeId
    && LIST_KINDS.has(selected.block.container.attrs.kind) && (drawn.has(selected.block.nativeId)
      || !!options.adapter().graph.blocks.find(block => block.id === selected.block.nativeId)?.content.suggestionId);
  /**
   * Enter on an empty suggested list item ends the list, as Notion's
   * (measured 2026-11-18 in Chrome at 1440, two tabs: a numbered, a bulleted
   * and a to-do item Enter suggested, and a numbered item picked from "/";
   * judged by the letter typed after): the item turns to text where it
   * stands, within the suggestion it is in, its card as it was, and the caret
   * stays in it. The line turns at once (a projection's transaction); someone
   * else's suggested item, refused, goes on as a list (`refused`).
   */
  function unlist(view: EditorView, selected: NonNullable<ReturnType<typeof target>>, refused: (here: NonNullable<ReturnType<typeof target>>) => void) {
    const blockId = selected.block.nativeId, at = selected.contentFrom - 2, node = view.state.doc.nodeAt(at);
    if (node?.type.name === "nativeBlock") view.dispatch(view.state.tr.setNodeMarkup(at, undefined, { ...node.attrs, kind: "paragraph", props: {} })
      .setMeta(nativeProjectionMeta, true).setMeta("addToHistory", false));
    void queue(blockId).turn("paragraph", true).then(came => {
      const here = came || !active() ? null : target(view.state);
      if (here?.block.nativeId === blockId) refused(here);
    });
  }
  /**
   * The "+" beside a block's handle while suggesting, as Notion's (measured
   * 2026-11-18 in Chrome at 1440, the owner and a guest with comment access,
   * two tabs each): a text block of its own is suggested right after the
   * block ("추가: 블록", no "⏎" on the block), its line drawn at once with the
   * caret in it, and the "/" menu opens on it, its words the menu's filter.
   */
  function lineBelow(blockId: string) {
    if (!active()) return false;
    const view = editor.view;
    const field = nativeTiptapTextFields(projectedBlocks(view.state.doc))
      .find(field => field.block.id === blockId && field.block.nativeId === blockId && field.field === "text");
    if (!field) return false;
    view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, field.contentFrom + field.node.content.size)));
    view.focus();
    const here = target(view.state);
    if (!here || here.block.nativeId !== blockId || !suggestedNextBlock(here.block.container.attrs.kind, true)) return false;
    const id = newId();
    requestSlashLine(id);
    suggestBreak(view, here, { type: "paragraph", firstChild: false }, null, true, id);
    return true;
  }
  /**
   * A row picked from "/" while suggesting, as Notion's (seen 2026-11-18 in
   * Chrome at 1440, a page and a database page's side peek): the typed "/…"
   * goes, and a block's kind picked turns the line into it where that leaves
   * the line empty and it is the person's own suggested block (the line
   * Enter suggested: "/" "제목1" there is a suggested heading in its place,
   * a suggestion of its own after the break's), and otherwise suggests a block of the kind after the line, the caret in
   * it ("beta line /글머리": a suggested list item under the line), a block a
   * menu picked: no break before it, its card "추가: 블록" (seen 2026-11-18).
   */
  function slash(from: number, to: number, blockType?: string) {
    if (!active()) return false;
    const view = editor.view;
    view.dispatch(view.state.tr.delete(from, to));
    // Once the words' going is proposed, which the transaction's own microtask does.
    if (blockType) queueMicrotask(() => {
      const line = active() ? target(view.state) : null;
      if (!line) return;
      const blockId = line.block.nativeId;
      const after = () => {
        const now = active() ? target(view.state) : null;
        if (!now || now.block.nativeId !== blockId) return;
        const end = now.contentFrom + now.node.content.size;
        if (!view.state.selection.empty || view.state.selection.from !== end) view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, end)));
        const here = target(view.state);
        if (here) suggestBreak(view, here, { type: blockType, firstChild: false }, null, true);
      };
      const graphed = options.adapter().graph.blocks.find(block => block.id === blockId)?.content;
      const suggested = drawn.has(blockId) || !!graphed?.suggestionId;
      moved = false;
      // Someone else's suggested line is refused the turn and gets the block after it. A
      // block a menu picked turns within its suggestion, its card staying "추가: 블록" (seen 2026-11-18).
      if (line.text === "" && suggested) void queue(blockId).turn(blockType, pickedLines.has(blockId) || graphed?.suggestionAlone === true)
        .then(came => { if (!came) after(); });
      else after();
    });
    return true;
  }
  /**
   * Suggests blocks away, one suggestion for them all, as Notion's "/"
   * "삭제", a block's "삭제" and Backspace over chosen blocks do while
   * suggesting (page_suggestion_removals.go); after what was asked before in
   * the first one's text, the "/…" a "/" row took away among it.
   */
  function remove(blockIds: readonly string[]) {
    if (!active() || blockIds.length === 0) return false;
    queueMicrotask(() => { if (active()) queue(blockIds[0]).remove(blockIds.slice(1)); });
    moved = false;
    return true;
  }
  function propose(before: EditorState, after: EditorState) {
    const first = target(before), last = target(after);
    if (!first || !last || first.block.id !== last.block.id) return;
    if (first.text === last.text) return;
    moved = false;
    draft(first.block.nativeId).change(queue(first.block.nativeId), first.text, last.text, { start: first.start, end: first.end, caret: last.start });
  }
  const lineStart = (blockId: string) => Y.createRelativePositionFromTypeIndex(
    options.adapter().collaborationDocument!.getText(BLOCK_TEXT_ROOT_PREFIX + blockId), 0, -1);
  function draft(blockId: string) {
    let value = drafts.get(blockId);
    if (!value) { value = createNativeSuggestedDraft(options.adapter().collaborationDocument!.getText(BLOCK_TEXT_ROOT_PREFIX + blockId)); drafts.set(blockId, value); }
    return value;
  }

  function format(marks: Record<string, boolean | string>) {
    if (!active()) return false;
    const selected = target(editor.state);
    if (!selected || selected.start === selected.end) return false;
    const intent = queue(selected.block.nativeId);
    intent.formatWith(draft(selected.block.nativeId).select(selected.start, selected.end), marks); moved = false; return true;
  }
  /**
   * Suggests a mention "@" picked in place of the text it was picked with
   * ("@que…", the person's own suggested letters), and the space a picked
   * mention has after it, in the same suggestion, the caret after the space
   * (2026-10-19: "@" stayed a letter). Notion writes the mention and its
   * space into the page at once (seen 2026-11-18 in Chrome, a page and a
   * database row's side peek: a date, a reminder, a page and a person each
   * in the public API with " " after, the card only "추가: “ ”" for the
   * space typed before "@", the mention left on rejecting it); here it is
   * suggested, as all a person does while suggesting is. Offsets are the block's text.
   */
  function mention(blockId: string, start: number, end: number, insertion: { text: string; attributes: Record<string, string>; spaced?: true }) {
    if (!active()) return false;
    const intent = queue(blockId), written = intent.insert(insertion.text, draft(blockId).select(start, end), false, insertion.attributes);
    if (insertion.spaced) {
      // Right after the mention's last character, none when it was refused: the space goes nowhere then.
      const after = written.then(chars => chars.at(-1)?.end ?? null);
      intent.insert(" ", { start: after, end: after, collapsed: true }, false, {});
    }
    moved = false;
    return true;
  }
  const afterState = (state: EditorState, tr: Transaction) => EditorState.create({ schema: state.schema,
    doc: tr.doc, selection: tr.selection, storedMarks: tr.storedMarks });
  const extension = Extension.create({ name: "nativeSuggestions", priority: 5000,
    onCreate() { editor = this.editor; },
    addProseMirrorPlugins() {
      editor = this.editor;
      return [new Plugin({
        filterTransaction: (tr, state) => {
          if (!active() || !tr.docChanged || tr.getMeta(nativeProjectionMeta)) return true;
          const after = afterState(state, tr), first = target(state), last = target(after);
          if (!first || !last || first.block.id !== last.block.id
            || first.block.container.attrs.kind !== last.block.container.attrs.kind
            || !tr.steps.every(step => step instanceof ReplaceStep && step.from >= first.contentFrom
              && step.to <= first.contentFrom + first.node.content.size)) return false;
          if (composition || imeComposing(editor.view)) {
            return true;
          }
          if (!handled.has(tr)) {
            handled.add(tr);
            if (first.text !== last.text) {
              proposals++;
              queueMicrotask(() => { try { if (active()) propose(state, after); } finally { proposals--; } });
            }
          }
          // PM owns the transient pending input. Its native API queue resolves
          // each future character to the acknowledgement that creates it.
          return first.text !== last.text;
        },
        props: {
          handleKeyDown: (view, event) => {
            if (!active()) return false;
            if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End", "PageUp", "PageDown"].includes(event.key)) moved = true;
            if (imeComposing(view) || imeComposing(event)) return false;
            if (event.key === "Tab" || primaryModifier(event) && ["z", "y"].includes(chordLetter(event))) return true;
            const selected = target(view.state);
            if (!selected) return false;
            const intent = queue(selected.block.nativeId), selection = draft(selected.block.nativeId).select(selected.start, selected.end);
            const mark = shortcutTextMark(event);
            if (mark) { intent.format(selection, mark); moved = false; return true; }
            if (fieldEnter(event) && event.shiftKey) {
              view.dispatch(view.state.tr.replaceSelectionWith(view.state.schema.nodes.hardBreak.create()));
              moved = false; return true;
            }
            if (fieldEnter(event) && !event.shiftKey) {
              const kind = selected.block.container.attrs.kind;
              const next = suggestedNextBlock(kind, selected.block.container.attrs.props.collapsed === true);
              if (unlists(selected)) unlist(view, selected, here => { if (next) suggestBreak(view, here, next, suggestedSplitBlock(kind)); });
              else if (next) suggestBreak(view, selected, next, suggestedSplitBlock(kind));
              moved = false; return true;
            }
            return false;
          },
          handlePaste: (view, event) => {
            if (!active()) return false;
            // The whole text: a suggestion longer than the server keeps is refused when sent, never cut.
            const selected = target(view.state), text = (event.clipboardData?.getData("text/plain") ?? "").replace(/\r\n?/gu, "\n");
            if (selected && text) {
              // Lines pasted at the end of a line that has words go into a text block after it, its lines kept in the
              // one block, where Notion's puts them (seen 2026-11-18, a page and a database page: "p1⏎p2⏎p3" one
              // paragraph under the line). Notion writes that block at once, suggesting mode or not; here it is
              // suggested, as all a person does while suggesting is (a page is never written from suggesting mode).
              if (text.includes("\n") && selected.start === selected.end && selected.start === selected.text.length && selected.text.length > 0
                && selected.block.id === selected.block.nativeId && suggestedNextBlock(selected.block.container.attrs.kind, true)) {
                suggestBreak(view, selected, { type: "paragraph", firstChild: false }, null);
              }
              const content = view.state.schema.text(text);
              view.dispatch(view.state.tr.replaceSelectionWith(content, false).setMeta("uiEvent", "paste"));
              moved = false;
            }
            return true;
          },
          handleDrop: () => active(),
          handleDOMEvents: {
            pointerdown: () => { moved = true; return false; },
            compositionstart: () => { if (active()) composition = editor.state; return false; },
            compositionend: () => {
              if (composition) { ended = composition; compositionEnd.ended(); }
              return false;
            },
          },
        },
      })];
    },
  });
  return { extension, active, format, mention, slash, remove, lineBelow, deferProjection: () => active() && (!!composition || compositionPending || proposals > 0 || drawn.size > 0 || [...queues.values()].some(queue => queue.busy)),
    syncMode() {
      if (active()) return;
      composition = null; ended = null; compositionPending = false; drafts.clear(); drawn.clear(); pickedLines.clear();
      if (frame) cancelAnimationFrame(frame);
      frame = 0; compositionEnd.stop();
    },
    dispose() { disposed = true; if (frame) cancelAnimationFrame(frame); compositionEnd.stop(); queues.clear(); drafts.clear(); drawn.clear(); pickedLines.clear(); } };
}
