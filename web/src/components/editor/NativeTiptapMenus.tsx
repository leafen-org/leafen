import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { Fragment, useContext, useEffect, useRef, useState } from "react";
import { TextSelection, type Transaction } from "@tiptap/pm/state";
import { ReplaceStep } from "@tiptap/pm/transform";
import { useProductPages } from "@/components/product/productPagesContext";
import { InviteAndMentionPanel, inviteMentionDoneText } from "@/components/page/InviteAndMentionPanel";
import { popoverRectAt } from "@/lib/popoverPlacement";
import { makeAnchoredSubPage, makePageUnder } from "./anchoredSubPage";
import type { GraphBlockType } from "@/contentGraph";
import type { TextSpan } from "@/lib/types";
import { BlockMoveToDialog } from "./BlockMoveToDialog";
import { TextBlockMenus, type TextInlineMenu } from "./TextBlockMenus";
import type { SlashMenuAnchor, SlashPageIn } from "./SlashMenu";
import { pageInUnavailable } from "./pageInPickerModel";
import { i18next } from "@/i18n";
import type { MentionItem } from "./blockPickerModel";
import type { BlockDef, InlineSlashKind } from "./blocks";
import { blockItemText } from "./blockItemLabels";
import { requestEditorBlockCommand } from "./editorCommands";
import { mentionInsertion } from "./mentionInsertion";
import { invitedPersonMentions, offerMentionInvite } from "./mentionInvite";
import type { MentionPick } from "./MentionMenu";
import { writeTodoMention } from "./todoTaskMention";
import { todoMenuTask } from "./todoTask";
import { useOptionalProductToast } from "@/lib/productToast";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { useSuggestingMode } from "./suggestingMode"; import { pickSlashRowWhileSuggesting } from "./suggestedSlashRows"; import { mapMenuRange } from "./nativeTiptapMenuRange";
import { slashMenuTrigger, mentionMenuTrigger, pageLinkMenuTrigger, plusPageLinkTrigger, triggerOpensAfter, typedTrigger } from "./textBlockInputModel";
import { putInOnePiece } from "./keyboardMode"; import { triggerFor } from "./triggerCharacters"; import { editorHoldsPageFocus } from "./editorHoldsPageFocus";
import { nativeProjectionMeta, nativeTiptapRangeSelection, resolveNativeTiptapSelection, type NativeRelativeSelection } from "./nativeTiptapBinding";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { nativeOffsetAt, nativeRichTextToFragment, pmOffsetAt } from "./nativeTiptapInline";
import { nativeTiptapSelectionContext } from "./nativeTiptapSelectionContext";
import { NativeTiptapReactContext, useNativeTiptapAdapter } from "./nativeTiptapReactContext";
import { placeSlashBlock } from "./slashBlockPlacement"; import { applyBlockColor } from "./blockHandleModel";
import { turnBlocksIntoPageIn } from "./blockMoveHistory";
import { turnBlocksInto } from "./blockTurnInto";
import { requestNativeTiptapEquation } from "./nativeTiptapInlineUiEvents";
import { registerMentionQuery, showMentionQuery, withoutMentionQuery } from "./mentionQuery";
import { SLASH_INLINE_REQUEST, type SlashInlineRequest } from "./slashInlineEvents";
import { PASTED_BLOCKS_MENU_REQUEST, PASTED_URL_MENU_REQUEST, type PastedBlocksMenuRequest, type PastedUrlMenuRequest } from "./textBlockClipboardModel";
import { PastedLinkMenu, type PastedLinkPlace } from "./PastedLinkMenu";
import { pastedLinkField, pastedLinkPlace } from "./pastedLinkPlace";
import { PastedBlocksMenu } from "./PastedBlocksMenu";
import { projectedBlocks } from "./nativeTiptapBinding";
import { createPortal } from "react-dom";
import { NATIVE_PHONE_MENU, type NativePhoneMenuRequest } from "./nativePhoneMenuEvents";
import { nativeTiptapCaretCoords, nativeTiptapHeldMenuAnchor, nativeTiptapLineMenuAnchor, nativeTiptapMenuAnchor } from "./nativeTiptapMenuAnchor";
import { pressLeftForField, SLASH_LINE_REQUEST, takeSlashLine } from "./slashLineRequest";
import { syncedBlockMayGoIn } from "./syncedBlockModel";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { pushRoute } from "@/lib/router";
import { pageHref } from "@/lib/navigation";
import { readsRightToLeft } from "@/lib/readingDirection";
import { compositionEnds, imeComposing } from "@/lib/fieldKeyBoundary";
import { giveFocusBack } from "@/lib/focusGiveBack";

/** Toggle headings a "· 전환" item closes on the line it turns (Notion's, seen 2026-11-06). */
const CLOSED_WHEN_TURNED: ReadonlySet<string> = new Set(["toggle_heading_1", "toggle_heading_2", "toggle_heading_3"]);

// A held menu stays while a window it opened (adding a workspace emoji) has the focus.
/**
 * A typed "+" opens the page link menu in a synced block's lines as in any
 * other, as Notion's (seen 2026-11-12 in an original: after "ab ", at a
 * line's start and after a space on an empty line).
 */
function plusTriggerAt(text: string, caret: number) {
  return plusPageLinkTrigger(text, caret);
}

// The text an input method committed, read as typed (NativeTiptapMenus).
const COMMITTED = "nativeMenuCommitted";

// A line menu ("+") takes its whole line as its filter.
type Menu = { value: TextInlineMenu; occurrenceId: string; blockId: string; from: number; to: number; explicit?: boolean; held?: boolean;
  line?: boolean };
type PageRequest = { range: Menu; title: string; expected: string; relative?: NativeRelativeSelection };

/**
 * The text typed on an empty page, which the typing inserts as the page's
 * first line: one whole new line holding only that text.
 */
function firstLineTyped(tr: Transaction): string | null {
  // Only a page with no words yet makes its first line around what is typed;
  // a line put back whole (⇧Tab, a block moved) is no typing, so its words
  // open no menu, as Notion's (seen 2026-11-08).
  if (tr.before.textContent) return null;
  for (const step of tr.steps) {
    const line = step instanceof ReplaceStep && step.slice.content.childCount === 1 ? step.slice.content.firstChild : null;
    if (line?.type.name !== "nativeBlock" || line.childCount !== 1) continue;
    const field = line.firstChild!;
    if (field.childCount === 1 && field.firstChild!.isText) return field.firstChild!.text ?? null;
  }
  return null;
}

/** Menus retain product policies and change only the engine's native range. */
export function NativeTiptapMenus() {
  // The menus read the page's adapter itself, as before node views kept a live one.
  const live = useContext(NativeTiptapReactContext)!;
  const context = { ...live, adapter: useNativeTiptapAdapter(live.adapter) };
  const latest = useRef(context); latest.current = context;
  const { controller, adapter } = context;
  const editor = controller.editor;
  // Suggesting, the page takes no writes but "@" and "/" still open their menus, and a
  // pick is suggested, as Notion's (2026-10-19; "/" 2026-11-18, suggestedSlashRows.ts).
  const suggesting = useSuggestingMode();
  const toast = useOptionalProductToast();
  const pageGraph = useOpenPageGraphStore();
  const suggestingRef = useRef(suggesting); suggestingRef.current = suggesting;
  // On a phone a typed "/" or ":" stays a letter and opens no menu, as
  // Notion's phone page (seen 2026-11-13 in iPhone Safari at 402 with its
  // soft keyboard, "/" and ":sm" on an empty line); its blocks come from the
  // tools' "+" (NativePhoneToolbar). "@" and "[[" open their lists there.
  const { phone } = useSidebarLayout(true);
  const phoneRef = useRef(phone); phoneRef.current = phone;
  const [menu, setMenuState] = useState<Menu | null>(null);
  const menuRef = useRef(menu); menuRef.current = menu;
  // Every editor transaction with nothing to show closes the menu: once it is
  // closed (the last value given was none) that queues no update.
  const givenMenu = useRef<Menu | null>(null);
  function setMenu(next: Menu | null) {
    if (!next && !givenMenu.current) return;
    givenMenu.current = next; setMenuState(next);
  }
  const [addPage, setAddPage] = useState<PageRequest | null>(null);
  // "초대" opens "초대와 멘션" by the "@", the name typed in its field (InviteAndMentionPanel);
  // the "@" and what was typed after it become whom it invited (mentionInvited).
  const [inviting, setInviting] = useState<{ name: string; anchor: SlashMenuAnchor; request: PageRequest } | null>(null);
  const pageRequest = useRef(addPage); pageRequest.current = addPage;
  const productPages = useProductPages();
  const [pasted, setPasted] = useState<{ blockId: string; url: string; range: Menu; place: PastedLinkPlace } | null>(null);
  // The last pasted address's place, kept where it is as the page changes,
  // also while a choice made in its menu reads what it needs.
  const pastedRange = useRef<Menu | null>(null);
  const [pastedBlocks, setPastedBlocks] = useState<{ request: PastedBlocksMenuRequest; anchor: SlashMenuAnchor } | null>(null);
  // Blocks pasted here from another page offer what else they can be, under
  // the last of them at its right (PastedBlocksMenu).
  useEffect(() => {
    function onPastedBlocks(event: Event) {
      const request = (event as CustomEvent<PastedBlocksMenuRequest>).detail;
      const current = latest.current.adapter;
      if (!request || current.readOnly || !current.isCurrent() || request.source.pageId === current.pageId) return;
      window.requestAnimationFrame(() => {
        const { graph } = latest.current.adapter;
        if (editor.isDestroyed || !request.blockIds.every(id => graph.blocks.some(block => block.id === id))) return;
        // Synced blocks come as synced copies already, with nothing to choose (Notion's, seen 2026-11-06).
        if (request.blockIds.every(id => graph.blocks.find(block => block.id === id)?.type === "synced_block")) return;
        const last = editor.view.dom.querySelector(`[data-block-id="${CSS.escape(request.blockIds.at(-1)!)}"]`);
        // A table by its own edges, narrower or wider than the text as it may
        // run, the menu then reaching past its left (Notion's, measured 2026-11-06).
        const table = last?.getAttribute("data-block-type") === "table" ? last.querySelector('[role="table"]') : null;
        const rect = (table ?? last)?.getBoundingClientRect();
        if (!rect) return;
        // 320 wide, its right edge on the blocks' and 4px under them (Notion's, measured 2026-11-05),
        // a database's under its title row (Notion's, measured 2026-11-06).
        // Where the screen reads from the right, its left edge on theirs: its
        // start (`left`, PastedBlocksMenu) is then 320 right of that.
        const left = readsRightToLeft(last) ? (table ? rect.left + 320 : Math.min(rect.right, rect.left + 320))
          : table ? rect.right - 320 : Math.max(rect.left, rect.right - 320);
        const titleRow = last?.getAttribute("data-block-type") === "inline_database" ? last.querySelector("[data-title-row]") : null;
        const bottom = titleRow?.getBoundingClientRect().bottom ?? rect.bottom;
        setPastedBlocks({ request, anchor: { left, top: bottom, bottom } });
      });
    }
    window.addEventListener(PASTED_BLOCKS_MENU_REQUEST, onPastedBlocks);
    return () => window.removeEventListener(PASTED_BLOCKS_MENU_REQUEST, onPastedBlocks);
  }, [editor]);
  const inlineRef = useRef(inline); inlineRef.current = inline;
  useEffect(() => {
    const usable = () => !latest.current.adapter.readOnly && latest.current.adapter.isCurrent() && !editor.isDestroyed && editor.isFocused;
    function onInline(event: Event) {
      const detail = (event as CustomEvent<SlashInlineRequest>).detail, target = nativeTiptapSelectionContext(editor.state);
      if (!usable() || !target || target.block.nativeId !== detail?.blockId || target.field !== "text") return;
      menuRef.current = { blockId: detail.blockId, occurrenceId: target.block.id, from: editor.state.selection.from,
        to: editor.state.selection.to, value: { kind: "slash", query: "", anchor: nativeTiptapMenuAnchor(editor, editor.state.selection.head) } };
      inlineRef.current(detail.kind);
    }
    function onPasted(event: Event) {
      const detail = (event as CustomEvent<PastedUrlMenuRequest>).detail;
      const field = detail && usable() ? pastedLinkField(editor, detail.from, detail.to, detail.text ?? detail.url) : null;
      if (!field || field.block.nativeId !== detail.blockId) return;
      // The menu stands under the pasted link's end, as Notion's (seen 2026-10-29).
      const range: Menu = { blockId: detail.blockId, occurrenceId: field.block.id, from: detail.from, to: detail.to,
        value: { kind: "pasted-url", query: detail.url, anchor: nativeTiptapMenuAnchor(editor, detail.to) } };
      pastedRange.current = range;
      setPasted({ blockId: detail.blockId, url: detail.url, range,
        place: pastedLinkPlace(editor, range, detail.text ?? detail.url, spans => !latest.current.adapter.readOnly && replace(range, spans)) });
    }
    function onPhone(event: Event) {
      const detail = (event as CustomEvent<NativePhoneMenuRequest>).detail;
      const target = nativeTiptapSelectionContext(editor.state);
      if (detail?.owner !== editor.view.dom || !usable() || !target || target.field !== "text") return;
      const position = editor.state.selection.head;
      const next: Menu = { blockId: target.block.nativeId, occurrenceId: target.block.id, from: position, to: position, explicit: true,
        value: { kind: "slash", query: detail.query, lineCommands: false, anchor: nativeTiptapMenuAnchor(editor, position) } };
      menuRef.current = next; setMenu(next);
    }
    function onLine() {
      const target = nativeTiptapSelectionContext(editor.state);
      if (usable() && target?.field === "text" && takeSlashLine(target.block.nativeId)) openLine(target);
    }
    window.addEventListener(SLASH_INLINE_REQUEST, onInline); window.addEventListener(PASTED_URL_MENU_REQUEST, onPasted);
    window.addEventListener(NATIVE_PHONE_MENU, onPhone); window.addEventListener(SLASH_LINE_REQUEST, onLine);
    return () => { window.removeEventListener(SLASH_INLINE_REQUEST, onInline); window.removeEventListener(PASTED_URL_MENU_REQUEST, onPasted);
      window.removeEventListener(NATIVE_PHONE_MENU, onPhone); window.removeEventListener(SLASH_LINE_REQUEST, onLine); };
  }, [editor]);

  useEffect(() => {
    // Typing on an empty page makes its first line around what is typed; if
    // the line gets its identity only in the projection after, what was typed
    // waits for that to open a menu, as on any other line.
    let typedFirstLine: string | null = null;
    function observe({ transaction: tr }: { transaction: Transaction }) {
      for (const range of [menuRef.current, pageRequest.current?.range, pastedRange.current]) {
        if (range) mapMenuRange(tr, range);
      }
      const local = !tr.getMeta(nativeProjectionMeta) && !tr.getMeta("uiEvent");
      const firstLine = !local ? typedFirstLine : null;
      typedFirstLine = null;
      if ((latest.current.adapter.readOnly && !suggestingRef.current.active) || !latest.current.adapter.isCurrent() || (!editorHoldsPageFocus(editor) && !menuRef.current?.explicit && !menuRef.current?.held) ||
        !editor.isEditable) { setMenu(null); return; }
      const target = nativeTiptapSelectionContext(editor.state);
      // An IME composing after the "/", "@" or ":" (Korean on a phone's
      // keyboard) keeps its menu, filtered by what it has written so far.
      // While composing only the "+" menu opens, whose opener is the "+" and
      // the letter after it, which a Korean or Japanese IME composes ("+가"):
      // an Enter that ends the composition then picks its first row, as the
      // "@" menu's does and as Notion's makes the new page (seen 2026-11-08);
      // the others open at the commit (committed, below).
      if (imeComposing(editor.view) && !menuRef.current && !(target && plusTriggerAt(target.text, target.start))) return;
      if (!target?.block.id || !editor.state.selection.empty || target.node.type.name === "nativeCode") {
        if (local && !target?.block.id) typedFirstLine = firstLineTyped(tr);
        setMenu(null); return;
      }
      const previous = menuRef.current?.occurrenceId === target.block.id ? menuRef.current : null;
      // A "+" line's words are its menu's filter; the line asked for opens
      // when the caret comes to it.
      if (previous?.line) { openLine(target); return; }
      if (!previous && target.field === "text" && takeSlashLine(target.block.nativeId)) { openLine(target); return; }
      // Explicit phone menus have no trigger in the document. Their cancel
      // button may take focus without ending or rewriting that range.
      if (previous?.explicit || previous?.held) return;
      // Only a local typed trigger starts a menu. Paste/API/remote projection
      // can update an already-open query, but cannot resurrect a closed one.
      const typedStep = local ? tr.steps.find((step): step is ReplaceStep =>
        step instanceof ReplaceStep && step.slice.content.childCount === 1 && !!step.slice.content.firstChild?.isText) : undefined;
      const typed = typedStep?.slice.content.firstChild?.text ?? tr.getMeta(COMMITTED) ?? (local ? firstLineTyped(tr) : firstLine) ?? "";
      const char = typed ? target.text[target.start - 1] : "";
      // What was typed starts a menu by typedTrigger: text put in at once by
      // its first letter, keys that landed together (the page busy, as
      // straight after Enter makes a block) each in turn, the rest the query.
      const started = !previous && typed ? typedTrigger(target.text, target.start, typed, putInOnePiece(typed)) : null;
      const existing = previous ? { start: nativeOffsetAt(target.node.content,
        Math.max(0, previous.from - target.contentFrom)) } : started ? { start: started.start } : null;
      const letterOnly = (trigger: string) => phoneRef.current && (trigger === "/" || trigger === ":");
      const opening = (trigger: string) => !previous && !letterOnly(trigger) && started?.char === trigger;
      let kind: TextInlineMenu["kind"] | undefined;
      let trigger: { start: number; end: number; query: string } | null = null;
      if (target.field === "text" && (previous?.value.kind === "slash" || opening("/"))) {
        kind = "slash"; trigger = slashMenuTrigger(existing, target.text, target.start, char === "/");
      } else if (previous?.value.kind === "page-link" || opening("[[") || opening("+")) {
        kind = "page-link"; trigger = pageLinkMenuTrigger(existing, target.text, target.start, char === "[");
      } else if (previous?.value.kind === "mention" || opening("@")) {
        kind = "mention"; trigger = mentionMenuTrigger(existing, target.text, target.start, char === "@");
      } else if (previous?.value.kind === "emoji" || opening(":")) {
        // ":" opens at the first letter after it, or with the letters that
        // arrived with it; ":" alone opens none.
        const start = existing?.start ?? target.text.lastIndexOf(":", target.start - 1);
        const query = target.text.slice(start + 1, target.start);
        // Once open, its words may run on past a space, as Notion's (":thumbs up", seen 2026-11-05).
        if (target.text[start] === ":" && (previous ? target.start > start : query !== "" && !/\s/u.test(query))
          && (previous || triggerOpensAfter(":", target.text, start))) {
          kind = "emoji"; trigger = { start, end: target.start, query };
        }
      }
      if (!kind || !trigger || suggestingRef.current.active && kind !== "mention" && kind !== "slash") { setMenu(null); return; }
      const from = target.contentFrom + pmOffsetAt(target.node.content, trigger.start);
      // A slash query grows after its opener; keep the menu at the slash. A
      // page link's menu stands by its "[[" or "+", as Notion's (seen 2026-11-07),
      // an emoji's 3px past its ":" however long its words (seen 2026-11-08),
      // and an "@"'s by the caret right after its "@", where its query grows
      // (Notion's, seen 2026-11-12: at 570.4 with "xqzv" typed as without).
      const anchor = nativeTiptapMenuAnchor(editor, kind === "mention" ? from + 1 : from, kind === "emoji" ? 3 : 0, kind === "mention", previous?.value.kind === kind ? previous.value.anchor : undefined);
      const next: Menu = { blockId: target.block.nativeId, occurrenceId: target.block.id,
        from,
        to: editor.state.selection.head,
        value: { kind, query: trigger.query, trigger, anchor, only: previous?.value.only, newPageFirst: kind === "page-link" && triggerFor(target.text[trigger.start]) === "+", lineEmpty: target.text.length === trigger.end - trigger.start } };
      menuRef.current = next; setMenu(next);
    }
    // An input method's commit ("＠" or "／" from a Japanese keyboard, a phone keyboard's "@there" ended by a space)
    // opens its menu as typing does, as Notion's (its opener reads the text an insertion puts in, 2026-11-18): once the
    // editor has the commit and before a next composition, started in the same key press, holds menus back (compositionEnds).
    let commit = "";
    const commitEnds = compositionEnds(() => editor.isDestroyed ? null : editor.view, () => {
      if (commit && typedFirstLine === null) observe({ transaction: editor.state.tr.setMeta(COMMITTED, commit) }); });
    const committed = (event: CompositionEvent) => { commit = event.data; commitEnds.ended(); };
    const dom = editor.view.dom;
    editor.on("transaction", observe); dom.addEventListener("compositionend", committed);
    return () => { editor.off("transaction", observe); dom.removeEventListener("compositionend", committed); commitEnds.stop(); };
  }, [editor]);
  useEffect(() => { if (adapter.readOnly && !suggesting.active) { setMenu(null); setPasted(null); } }, [adapter.readOnly, suggesting.active]);
  // A typed "@" or "/" and its query sit in a pale pill while their menu is open.
  useEffect(() => registerMentionQuery(editor), [editor]);
  // A "+" line's "/" menu says what typing does there, as Notion's (seen 2026-11-04).
  useEffect(() => {
    if (!menu?.line) return undefined;
    const at = editor.view.domAtPos(menu.from).node;
    const field = (at instanceof Element ? at : at.parentElement)?.closest("[data-rt-editable]");
    field?.setAttribute("data-slash-filter", blockItemText("block.slashFilter"));
    return () => field?.removeAttribute("data-slash-filter");
  }, [editor, menu?.line, menu?.from]);
  useEffect(() => {
    // A "+" line's words, its filter, sit in the pill too (Notion's, seen 2026-11-11); its empty line's chip is its placeholder's.
    const typed = (menu?.value.kind === "mention" || menu?.value.kind === "slash") && (!menu.explicit || menu.line);
    const placeholder = menu?.value.kind === "slash" && !menu.line && !menu.value.query ? blockItemText("block.slashQuery") : undefined;
    const show = () => showMentionQuery(editor, typed && menu ? { from: menu.from, to: menu.to, placeholder } : null);
    if (!imeComposing(editor.view)) { show(); return; }
    // Redrawing the pill over a syllable being composed would break it: the pill catches up once the
    // composition ends, before a next one starts in the same key press (macOS Chrome's Korean).
    const end = compositionEnds(() => editor.isDestroyed ? null : editor.view, show), ended = () => end.ended();
    const dom = editor.view.dom;
    dom.addEventListener("compositionend", ended, { once: true });
    return () => { dom.removeEventListener("compositionend", ended); end.stop(); };
  }, [editor, menu]);

  function replace(range: Menu, spans: readonly TextSpan[], scroll = true) {
    const current = latest.current;
    if (!current.adapter.isCurrent() || current.adapter.readOnly || editor.isDestroyed) return false;
    const from = Math.max(0, range.from), to = Math.max(from, range.to);
    // Both endpoints must stay in the same native field and occurrence.
    const field = nativeTiptapTextFields(projectedBlocks(editor.state.doc)).find(field =>
      field.block.id === range.occurrenceId && from >= field.contentFrom && to <= field.contentFrom + field.node.content.size);
    if (!field) return false;
    const content = nativeRichTextToFragment(spans, editor.schema);
    const tr = editor.state.tr.replaceWith(from, to, content);
    tr.setSelection(TextSelection.create(tr.doc, from + content.size));
    editor.view.dispatch(scroll ? withoutMentionQuery(tr).scrollIntoView() : withoutMentionQuery(tr)); editor.view.focus();
    return true;
  }
  function openLine(target: NonNullable<ReturnType<typeof nativeTiptapSelectionContext>>) {
    const next: Menu = { blockId: target.block.nativeId, occurrenceId: target.block.id, explicit: true, line: true,
      from: target.contentFrom, to: target.contentFrom + target.node.content.size,
      // The "+" menu has nothing to turn, act on or color (decided 2026-09-30); it stands at the line's block.
      value: { kind: "slash", query: target.text, lineCommands: false, anchor: nativeTiptapLineMenuAnchor(editor, target.contentFrom) } };
    menuRef.current = next; setMenu(next);
  }
  function close(refocus = false) {
    const explicit = menuRef.current?.explicit;
    menuRef.current = null; setMenu(null);
    // A window opened as it closes keeps the focus, as Notion's ("+", Escape and ⌘⇧O at once: 2026-11-26, two tabs), and so does a field a press beside it went on to (pressLeftForField).
    if (refocus && explicit) giveFocusBack({ give: () => editor.view.focus(), unlessOpened: true, when: () => !editor.isDestroyed && latest.current.adapter.isCurrent() && !latest.current.adapter.readOnly && !pressLeftForField(editor.view.dom) });
  }
  function insertEmoji(emoji: string) { const range = menuRef.current; close(); if (range) replace(range, [{ text: emoji }]); }
  function holdMenu(held: boolean) { if (menuRef.current) menuRef.current.held = held; }
  // A workspace emoji goes in as its picture, named ":name:" as text.
  function insertCustomEmoji(emoji: WorkspaceCustomEmoji) {
    const range = menuRef.current; close();
    if (range) replace(range, [{ text: `:${emoji.name}:`, mention: "custom_emoji", customEmojiId: emoji.id }]);
  }
  // A person, page or date picked while suggesting is suggested in place of
  // the "@" text the person's own suggested letters made, a date with its
  // reminder, as Notion's "리마인더 받기" (seen 2026-11-18).
  function suggestMention(range: Menu, item: MentionItem) {
    const insertion = item.kind === "person" || item.kind === "page" || item.kind === "date" ? mentionInsertion(item) : null;
    if (!insertion) return;
    const field = nativeTiptapTextFields(projectedBlocks(editor.state.doc)).find(field =>
      field.block.id === range.occurrenceId && range.from >= field.contentFrom && range.to <= field.contentFrom + field.node.content.size);
    if (!field) return;
    controller.suggestMention(field.block.nativeId, nativeOffsetAt(field.node.content, range.from - field.contentFrom),
      nativeOffsetAt(field.node.content, range.to - field.contentFrom), insertion);
  }
  function pickMention(item: MentionItem, how?: MentionPick) {
    const range = menuRef.current; close(); if (!range) return;
    if (suggestingRef.current.active) { suggestMention(range, item); return; }

    const relative = adapter.collaborationDocument ? nativeTiptapRangeSelection(editor.state, adapter.collaborationDocument, range.from, range.to) : undefined;
    const expected = editor.state.doc.textBetween(range.from, range.to, "\n", node => node.attrs.text ?? "");
    if (item.kind === "invite") {
      setInviting({ name: item.name, anchor: range.value.anchor ?? nativeTiptapMenuAnchor(editor, range.from), request: { range, title: item.name, expected, relative } });
      return;
    }
    if (item.kind === "add_page") { setAddPage({ range, title: item.title, expected, relative }); return; }
    if (item.kind === "sub_page") {
      const request = { range, title: item.title, expected, relative }; pageRequest.current = request;
      if (makeLinePage(request)) return;
      void makePage(adapter.pageId, request).then(() => latest.current.adapter.graphSync?.reread())
        .catch(() => latest.current.adapter.moveContext?.onStatus?.(blockItemText("mention.addPageFailed"), "error"));
      return;
    }
    const insertion = mentionInsertion(item);
    // A to-do's "@" assigns it or sets its due date along with the mention, unless ⇧↵ (todoTaskMention.ts).
    const todo = how?.mentionOnly ? undefined : latest.current.adapter.graph.blocks.find(block => block.id === range.blockId && block.type === "to_do");
    const wrote = todo && (item.kind === "date" || item.kind === "person")
      ? writeTodoMention({ adapter: latest.current.adapter, run: controller.run, block: todo, item, write: spans => replace(range, spans) })
      : insertion ? replace(range, [{ text: insertion.text, ...insertion.attributes }, ...(insertion.spaced ? [{ text: " " }] : [])]) : false;
    // Someone the page doesn't open to is offered an invite (mentionInvite.ts).
    if (wrote && item.kind === "person" && adapter.session && toast) {
      void offerMentionInvite({ session: adapter.session, pageId: adapter.pageId, userId: item.userId, name: item.label, notify: toast.notify });
    }
  }
  // A sub-page made from a line's words, a table row's cell or a caption
  // hangs on that block, as Notion's (user decision 2026-11-11; seen
  // 2026-11-13: its parent is the line, the row or the captioned block):
  // its mention takes the words' place at once (makeAnchoredSubPage). False
  // when the field can't hold it here, for makePage to make it.
  function makeLinePage(request: PageRequest) {
    const current = latest.current.adapter, move = current.moveContext, { range: asked } = request;
    const line = nativeTiptapTextFields(projectedBlocks(editor.state.doc)).find(field => field.block.id === asked.occurrenceId
      && asked.from >= field.contentFrom && asked.to <= field.contentFrom + field.node.content.size);
    if (!move || current.readOnly || !current.isCurrent() || !line || line.block.nativeId !== asked.blockId) return false;
    return makeAnchoredSubPage({ move, outbox: productPages?.mutationOutbox, parentId: current.pageId, workspaceId: current.workspaceId,
      title: request.title, anchor: asked.blockId, field: line.field !== "text", write: (page) => {
        const range = requestRange(request);
        return Boolean(range && replace(range, [{ text: page.title || untitledPageDisplayTitle(page), mention: "page", pageId: page.id }, { text: " " }]));
      } });
  }
  // A page made under a page picked ("@" "페이지 추가") shows in the press's
  // own frame, its mention in with it (makePageUnder).
  async function makePage(destination: string, request: PageRequest) {
    const current = latest.current.adapter, move = current.moveContext;
    if (!move || current.readOnly || !current.isCurrent()) return;
    const page = await makePageUnder({ move, outbox: productPages?.mutationOutbox, parentId: destination, workspaceId: current.workspaceId,
      title: request.title,
      // Its block comes to its parent's body once the server made it: this body is read again then.
      made: () => void latest.current.adapter.graphSync?.reread() });
    if (!current.isCurrent() || current.signal.aborted || latest.current.adapter.readOnly || editor.isDestroyed) return;
    const range = requestRange(request);
    if (!range || !replace(range, [{ text: page.title || request.title || untitledPageDisplayTitle(page), mention: "page", pageId: page.id }, { text: " " }])) {
      move.onStatus?.(blockItemText("mention.pageCreatedWithoutMention"), "default");
    }
    // An untitled page opens to be named, as Notion's (seen 2026-10-10).
    if (!request.title.trim()) pushRoute(pageHref(page.id));
  }
  // Where a request's typed words are now, null once they changed or went.
  function requestRange(request: PageRequest): Menu | null {
    const range = { ...request.range }, shared = latest.current.adapter.collaborationDocument;
    if (request.relative && shared) {
      const position = resolveNativeTiptapSelection(editor.state.doc, shared, request.relative);
      if (!position) return null;
      range.from = Math.min(position.anchor, position.head); range.to = Math.max(position.anchor, position.head);
    }
    return range.from <= range.to && range.to <= editor.state.doc.content.size
      && editor.state.doc.textBetween(range.from, range.to, "\n", node => node.attrs.text ?? "") === request.expected ? range : null;
  }
  // Whom an "@" invited is mentioned where the "@" was, as Notion's says
  // "〈주소〉님을 초대했습니다." and mentions them (seen 2026-11-12, a database cell's).
  async function mentionInvited(request: PageRequest, emails: string[]) {
    const current = latest.current.adapter;
    toast?.notify(inviteMentionDoneText(emails), "success");
    if (!current.session) return;
    const words = await invitedPersonMentions(current.session, current.workspaceId, emails).catch(() => {
      toast?.notify(i18next.t("topBar:inviteMention.notPlaced"), "error");
      return [];
    });
    if (!words.length || !current.isCurrent() || current.signal.aborted || latest.current.adapter.readOnly || editor.isDestroyed) return;
    const range = requestRange(request);
    if (range) replace(range, words);
  }
  function inline(kind: InlineSlashKind) {
    const range = menuRef.current; close(); if (!range) return;
    // The typed "/…" goes, a step of its own, and the equation is made where
    // it stood, as Notion's (seen 2026-11-11: Undo of a done one takes the
    // equation away, the next brings the "/…" back).
    if (kind === "equation") { if (replace(range, [])) requestNativeTiptapEquation(editor, range.from, range.from); return; }
    const mention = ["person", "page", "date"].includes(kind) ? kind as "person" | "page" | "date" : undefined, held = nativeTiptapCaretCoords(editor, editor.state.selection.head).left;
    if (!replace(range, [{ text: mention ? "@" : ":" }])) return;
    const anchor = nativeTiptapHeldMenuAnchor(editor, mention ? range.from + 1 : range.from, mention ? 0 : 3, Boolean(mention), held);
    const next: Menu = { ...range, line: false, explicit: range.explicit && !range.line, to: range.from + 1, value: { kind: mention ? "mention" : "emoji", query: "", anchor,
      trigger: { start: 0, end: 1 }, only: mention } }; // A "+" line's choice leaves a typed "@" or ":", no longer the line's filter.
    menuRef.current = next; setMenu(next);
  }
  function pickBlock(definition: BlockDef) {
    const range = menuRef.current; close(); if (!range) return;
    if (suggestingRef.current.active) { pickSlashRowWhileSuggesting(definition, range, controller.suggestSlash, controller.suggestRemoval); return; }
    const current = latest.current.adapter, block = current.graph.blocks.find(block => block.id === range.blockId);
    if (!block) return;
    // The search's words go as a step of their own: ⌘Z takes the block back, then the words come back (Notion's, seen 2026-11-05).
    // They go unscrolled: the block made scrolls to its own caret, and one with none (a new database) leaves the page where its start panel put it.
    let cleared = false;
    controller.run(() => { cleared = replace(range, [], false); });
    if (!cleared) return;
    controller.run(() => {
      if (definition.action === "turn_into") {
        turnBlocksInto(current, [block.id], {
          key: definition.id ?? definition.type, type: definition.type as GraphBlockType, columns: definition.columnCount,
        });
        // The line turned into toggle heading 1–3 is closed, so Enter goes on
        // after it; into toggle heading 4 or a toggle list it stays open
        // (Notion's "· 전환", seen 2026-11-06).
        if (CLOSED_WHEN_TURNED.has(definition.type)) current.setToggleCollapsed(block, true);
      } else if (definition.action === "set_color") applyBlockColor(current, [block.id], definition.colorToken ?? "default");
      else if (definition.action === "import") productPages?.openImport?.({ pageId: current.pageId,
        title: productPages.pages.find(page => page.id === current.pageId)?.title ?? "", format: definition.importFormat });
      else if (definition.action) {
        const command = { copy_link: "copyLink", duplicate: "duplicate", move_to: "move", delete: "delete", present: "present" } as const;
        const key = definition.action as keyof typeof command;
        if (command[key]) requestEditorBlockCommand(block.id, command[key]);
      } else placeSlashBlock(current, block, definition, current.text(block).trim() === "");
    });
  }
  // "페이지 위치:" puts the line's block in a page the person picks, as a
  // page there, leaving its link here (blockTurnInto.ts), as the block menu's.
  async function putLineInPage(destinationPageId: string) {
    const range = menuRef.current; close(); if (!range) return;
    const current = latest.current.adapter, move = current.moveContext;
    const block = current.graph.blocks.find(candidate => candidate.id === range.blockId);
    if (!block || !move) return;
    let cleared = false;
    controller.run(() => { cleared = replace(range, []); });
    if (!cleared) return;
    try {
      const caretAtTitle = await turnBlocksIntoPageIn(current, [block.id], false, destinationPageId, pageGraph);
      void current.graphSync?.reread();
      caretAtTitle?.();
    } catch {
      move.onStatus?.(i18next.t("blockHandle:turnPageIn.failed"), "error");
    }
  }
  const pageIn: SlashPageIn | undefined = adapter.moveContext && !adapter.readOnly && !suggesting.active ? {
    pages: adapter.moveContext.pages, session: adapter.moveContext.session, workspaceId: adapter.workspaceId, sourcePageId: adapter.pageId,
    unavailable: pageInUnavailable(adapter.moveContext.pages, adapter.pageId, i18next.t("blockHandle:turnPageIn.unavailable")),
    onHold: holdMenu, onChoose: pageId => void putLineInPage(pageId),
  } : undefined;
  const menuTodo = menu ? adapter.graph.blocks.find((block) => block.id === menu.blockId && block.type === "to_do") : undefined;
  // The touch tools' "+" and picture open the phone's "블록 삽입" sheet, in
  // the middle of a wider screen, as Notion's iPad (seen 2026-11-13).
  const MenuLayer = menu?.explicit && !menu.line ? TouchSheetLayout : Fragment;
  // A pasted link's menu closed by Escape or "URL" gives the page back its caret,
  // after the link; what Notion's does there is measured in PastedLinkMenu (keepUrl).
  return <>
    {pasted && adapter.graph.blocks.find(block => block.id === pasted.blockId) && createPortal(<PastedLinkMenu adapter={adapter}
      block={adapter.graph.blocks.find(block => block.id === pasted.blockId)!} url={pasted.url} run={controller.run} anchor={pasted.range.value.anchor}
      place={pasted.place} onClose={refocus => { setPasted(null); if (refocus && !editor.isDestroyed) editor.view.focus(); }} />, document.body)}
    {pastedBlocks && createPortal(<PastedBlocksMenu adapter={adapter} request={pastedBlocks.request} anchor={pastedBlocks.anchor}
      run={controller.run} onClose={() => setPastedBlocks(null)} />, document.body)}
    <MenuLayer><TextBlockMenus blockId={menu?.blockId ?? ""} pageId={adapter.pageId} workspaceId={adapter.workspaceId} menu={menu?.value ?? null}
      mentionSnapshot={adapter.readOnly && !suggesting.active ? null : adapter.mentionSnapshot} newPages={Boolean(adapter.moveContext) && !suggesting.active}
      invite={Boolean(adapter.moveContext && adapter.session) && !suggesting.active} session={adapter.session}
      onClose={() => close(true)} onPickEmoji={insertEmoji}
      customEmoji={adapter.workspaceId && !adapter.readOnly ? { workspaceId: adapter.workspaceId, onPick: insertCustomEmoji, onHold: holdMenu } : undefined}
      onPickMention={pickMention} onPickBlock={pickBlock} onInline={inline} pageIn={pageIn} suggesting={suggesting.active}
      currentType={menu ? adapter.graph.blocks.find((block) => block.id === menu.blockId)?.type : undefined}
      syncedBlocks={!menu || syncedBlockMayGoIn(adapter.graph, adapter.graph.blocks.find((block) => block.id === menu.blockId)?.parentId ?? null)}
      task={menuTodo && !suggesting.active ? todoMenuTask(menuTodo.content) : undefined} /></MenuLayer>
    {addPage && adapter.moveContext && <BlockMoveToDialog blockId={addPage.range.blockId} sourcePageId={adapter.pageId}
      workspaceId={adapter.workspaceId} pages={adapter.moveContext.pages} session={adapter.moveContext.session}
      title={blockItemText("mention.addPageTitle")} placeholder={blockItemText("mention.addPagePlaceholder")}
      sourcePage="always" doneMessage={null} failedMessage={blockItemText("mention.addPageFailed")}
      onChooseDestination={destination => makePage(destination, addPage)} onClose={() => setAddPage(null)}
      onMoved={() => void adapter.graphSync?.reread()} onPageChange={adapter.moveContext.onPageChange}
      onStatus={adapter.moveContext.onStatus} />}
    {inviting && adapter.session ? <InviteAndMentionPanel session={adapter.session} pageId={adapter.pageId} invite={inviting.name}
      anchor={() => popoverRectAt(inviting.anchor)} byLine onBack={() => setInviting(null)} onClose={() => setInviting(null)}
      onInvited={emails => void mentionInvited(inviting.request, emails)} /> : null}
  </>;
}
