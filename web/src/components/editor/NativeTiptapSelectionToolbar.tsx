import { LINK_WINDOW_CHOICE, requestNativeTiptapLinkEdit } from "./nativeTiptapInlineUiEvents";
import { normalizePastedLink } from "./editorInputModel";
import { memo, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { createPortal, flushSync } from "react-dom";
import type { BlockType } from "@/lib/types";
import { copyText } from "@/lib/copyText";
import { desktopTextMenuUp, watchDesktopTextMenu } from "@/lib/desktopTextMenu";
import { SelectionToolbarView, type SelectionToolbarPage } from "./SelectionToolbarView";
import { INACTIVE_MARKS, type SelectionToolbarMark } from "./selectionToolbarModel";
import { storedEditorColor } from "./colorMemory";
import { colorChosenWords, commentOnChosenWords, reactToChosenWords } from "./chosenWordsActions";
import { NativeTiptapReactContext, useNativeTiptapAdapter } from "./nativeTiptapReactContext";
import { nativeTiptapSelectionContext, nativeTiptapSelectionIntoTable } from "./nativeTiptapSelectionContext";
import { projectedBlocks } from "./nativeTiptapBinding";
import { chosenTextAway, chosenTextPlace, liveBox, liveChosenTextBox } from "./selectionToolbarPlace";
import { useTextReactionStarter } from "./TextReactions";
import { chosenCommentText } from "./nativeTiptapChosenComment";
import { openBlockToolMenu, useEditorSelectionCommand, type EditorSelectionCommand } from "./editorCommands";
import { requestNativeTiptapEquation } from "./nativeTiptapInlineUiEvents";
import { markLinkWindowTarget } from "./nativeTiptapLinkTarget";
import { useSuggestingMode } from "./suggestingMode";
import type { BlockPageInAnchor } from "./BlockActionMenu";
import { turnBlocksIntoPageIn } from "./blockMoveHistory";
import { turnBlocksInto } from "./blockTurnInto";
import { PageInPicker } from "./PageInPicker";
import { pageInUnavailable } from "./pageInPickerModel";
import { useOptionalProductToast } from "@/lib/productToast";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { TextSelection, type EditorState, type Selection, type Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { editorHoldsPageFocus } from "./editorHoldsPageFocus";
import { useShareKeyOpened } from "./blockMenuShareKey";
import { wordsChosenByPerson } from "./inputMethodChoice";

const markNames = { bold: "bold", italic: "italic", underline: "underline", strikethrough: "strike", code: "code", link: "link" };

/** `rect` is read at the words each time (liveChosenTextBox); `at` is where they were as it was drawn. */
type Shown = { rect: DOMRect; at: DOMRect; page: SelectionToolbarPage; state: EditorState };
/** The drawn toolbar's answer to a command, and a command waiting for it to draw. */
type Commands = { run: ((command: EditorSelectionCommand) => void) | null; waiting: EditorSelectionCommand | null };

/** The element the chosen words start in, inside the editor and the drawing of their block. */
function chosenWordsElement(view: EditorView): Element {
  const { node } = view.domAtPos(view.state.selection.from);
  return (node instanceof Element ? node : node.parentElement) ?? view.dom;
}

/**
 * What ⌘K's link window opens on: the chosen words, but in a picture's caption
 * the caption's box, whose middle the window takes, as Notion's (measured
 * 2026-11-14 and 2026-11-15 in Chrome at 1440: a 704-wide picture's caption
 * word at 497.5 opened a window centred on 847.5, the caption's middle; in a
 * bookmark's, code block's, video's or file's caption, a simple table's cell or
 * a synced block, it stood under the words). Read where they are each time,
 * as the panel's place (liveChosenTextBox), it stays at them as the page scrolls.
 */
function linkWindowAnchor(view: EditorView, state: EditorState): DOMRect | null {
  const { from, to, $from } = state.selection;
  const inPicture = $from.parent.type.name === "nativeCaption" && $from.node($from.depth - 1).attrs.kind === "image";
  const measure = () => {
    const head = view.domAtPos(from), tail = view.domAtPos(to);
    const caption = inPicture && (head.node instanceof Element ? head.node : head.node.parentElement)?.closest("[data-native-caption]");
    if (caption) return caption.getBoundingClientRect();
    const range = document.createRange();
    range.setStart(head.node, head.offset); range.setEnd(tail.node, tail.offset);
    return range.getBoundingClientRect();
  };
  try { return liveBox(measure, measure()); } catch { return null; }
}

/** Whether the selection holds any character: text, or an inline part (a mention, an equation) as one. */
function chosenCharacters(state: EditorState) {
  return state.doc.textBetween(state.selection.from, state.selection.to, "", node => node.isInline ? "￼" : "") !== "";
}

/**
 * Whether the selection takes the text toolbar: text a person chose
 * (wordsChosenByPerson — not an input method's clause), not a block's own
 * part, nor the break between two lines alone — a right press beside a
 * line's words chooses it, as does a drag from a line's end to the next
 * one's start — which draws none, as Notion's (seen 2026-11-13).
 */
function drawsForWords(view: EditorView) {
  const { state } = view;
  return wordsChosenByPerson(view) && state.selection instanceof TextSelection && chosenCharacters(state);
}

/**
 * Watches the editor for chosen text and draws the toolbar only while there
 * is some: typing with nothing chosen sets nothing here, and a shown toolbar
 * draws again only when the text, the selection or its place changed.
 * It answers the keys for chosen text (⌘⇧M, ⌘K, ⌘⇧H, …) whenever its editor
 * has them, whether or not the toolbar has drawn yet: keys right on the heels
 * of the choosing (a double-click or ⇧Home, then ⌘⇧M) wait for it to draw
 * and act there, rather than falling through to the text.
 */
export const NativeTiptapSelectionToolbar = memo(function NativeTiptapSelectionToolbar() {
  const live = useContext(NativeTiptapReactContext)!;
  const editor = live.controller.editor;
  // A touch screen's chosen words take the line of tools under them
  // (PhoneSelectionTools), where the page can be written in.
  const { touch } = useSidebarLayout(true);
  const suggestingMode = useSuggestingMode();
  const lineTools = useRef(false);
  lineTools.current = touch && !suggestingMode.active;
  const [shown, setShown] = useState<Shown | null>(null);
  const shownRef = useRef(shown);
  const toolbarRef = useRef<HTMLDivElement>(null);
  const dismissedSelection = useRef<Selection | null>(null);
  const commands = useRef<Commands>({ run: null, waiting: null });
  // The first panel since the editor opened, while it shows, and after it.
  const firstPanel = useRef<"none" | "showing" | "done">("none");
  const show = useCallback((next: Shown | null) => {
    if (!next) commands.current.waiting = null;
    if (!next && !shownRef.current) return;
    if (next && firstPanel.current === "none") firstPanel.current = "showing";
    else if (!next && firstPanel.current === "showing") firstPanel.current = "done";
    shownRef.current = next; setShown(next);
  }, []);
  // Its place is read at its words each time (liveChosenTextBox): the page
  // scrolled, it is placed again where they are.
  const countsRunOn = () => firstPanel.current === "done";
  const shownFor = (state: EditorState, { rect, page } = chosenTextPlace(editor.view, state, countsRunOn())): Shown =>
    ({ rect: liveChosenTextBox(editor.view, rect, countsRunOn), at: rect, page, state });
  // A touch screen's chosen words in a page that can be written take the line of tools instead.
  const drawsFor = () => drawsForWords(editor.view) && !(lineTools.current && !live.adapter.readOnly);
  useEditorSelectionCommand(command => {
    if (editor.isDestroyed || (!editorHoldsPageFocus(editor) && !toolbarRef.current?.contains(document.activeElement))) return false;
    if (commands.current.run) { commands.current.run(command); return true; }
    // Words chosen with no toolbar drawn for them yet, or one put away, draw
    // it now and act there: the key is never dropped to the text or the page
    // (2026-11-14: a ⌘K lost, the address typed next went over the words).
    if (!shownRef.current) {
      if (!drawsFor()) return false;
      dismissedSelection.current = null;
      show(shownFor(editor.state));
    }
    commands.current.waiting = command;
    return true;
  });
  useEffect(() => {
    function update(event?: { transaction?: Transaction }) {
      // PM dispatches its blur transaction while DOM focus is still between
      // owners. The blur listener reads the settled owner after that move.
      if (event?.transaction?.getMeta("blur")) return;
      const state = editor.state, selection = state.selection;
      // A link window's choosing of its words draws no toolbar until it closes.
      const linkChoice = event?.transaction?.getMeta(LINK_WINDOW_CHOICE);
      if (linkChoice === "quiet" && !shownRef.current) { dismissedSelection.current = selection; return; }
      if (linkChoice === "done") dismissedSelection.current = null;
      // The words a right press chose in the Mac app draw none while its menu is up, nor after
      // while they stay chosen, as Notion's app's (desktopTextMenu.ts).
      if (desktopTextMenuUp()) { dismissedSelection.current = selection; show(null); return; }
      if (dismissedSelection.current?.eq(selection)) return;
      dismissedSelection.current = null;
      // "페이지 위치:"'s window, which takes the keys, is the toolbar's too, as
      // is a link's window opened from it (NativeTiptapLinkPill), which
      // leaves it shown, as Notion's (seen 2026-11-09).
      const linkWindow = document.activeElement?.closest("[data-link-edit-window]");
      if (linkWindow && !shownRef.current) return;
      // Another tab or window in front leaves it up, as the text menus (editorHoldsPageFocus).
      if ((!editorHoldsPageFocus(editor) && !toolbarRef.current?.contains(document.activeElement)
        && !document.activeElement?.closest("[data-page-in-picker]") && !linkWindow) || !drawsFor()) {
        // A later selection opens a fresh toolbar, never a stale child menu:
        // the menus go with the toolbar's body.
        show(null); return;
      }
      // By the chosen lines whichever way they were chosen, as Notion's
      // (seen 2026-10-31: Shift+Home puts it after the line, not before).
      const place = chosenTextPlace(editor.view, state, firstPanel.current === "done"), { rect, page } = place;
      const drawn = shownRef.current;
      if (drawn && drawn.state.doc === state.doc && drawn.state.selection.eq(selection) && drawn.state.storedMarks === state.storedMarks
        && drawn.at.x === rect.x && drawn.at.y === rect.y && drawn.at.width === rect.width && drawn.at.height === rect.height
        && drawn.page.column === page.column && drawn.page.right === page.right && drawn.page.top === page.top) return;
      show(shownFor(state, place));
    }
    const blur = () => queueMicrotask(() => update());
    editor.on("transaction", update); editor.on("focus", update); editor.on("blur", blur);
    const stopMenu = watchDesktopTextMenu(() => update());
    return () => { editor.off("transaction", update); editor.off("focus", update); editor.off("blur", blur); stopMenu(); };
  }, [editor, show]);
  // Out of sight, still open, while its words are scrolled out of the window (chosenTextAway).
  const [away, setAway] = useState(false);
  useLayoutEffect(() => {
    if (!shown) { setAway(false); return undefined; }
    const check = () => setAway(chosenTextAway(shown.rect));
    check();
    document.addEventListener("scroll", check, { capture: true, passive: true }); window.addEventListener("resize", check);
    return () => { document.removeEventListener("scroll", check, { capture: true }); window.removeEventListener("resize", check); };
  }, [shown]);
  if (!shown) return null;
  return <ShownSelectionToolbar rect={shown.rect} page={shown.page} hide={() => show(null)} toolbarRef={toolbarRef}
    dismissedSelection={dismissedSelection} commands={commands} away={away} />;
});

/** Formatting uses PM commands; native binding records the resulting delta. */
function ShownSelectionToolbar({ rect, page, hide, toolbarRef, dismissedSelection, commands, away }: {
  rect: DOMRect;
  away: boolean;
  page: SelectionToolbarPage;
  hide: () => void;
  toolbarRef: RefObject<HTMLDivElement | null>;
  dismissedSelection: RefObject<Selection | null>;
  commands: RefObject<Commands>;
}) {
  const { adapter: live, controller } = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live);
  const editor = controller.editor;
  const suggesting = useSuggestingMode();
  const reaction = useTextReactionStarter();
  const { t } = useTranslation("selectionToolbar");
  const toast = useOptionalProductToast();
  const pageGraph = useOpenPageGraphStore();
  const [linkAtText, setLinkAtText] = useState<DOMRect | null>(null);
  const [colorsOpen, setColorsOpen] = useState(false);
  const [turnOpen, setTurnOpen] = useState(false);
  const [linkOpen, setLinkOpen] = useState(false);
  const [linkValue, setLinkValue] = useState("");
  const [hadLinkOnOpen, setHadLinkOnOpen] = useState(false);
  const [linkCopied, setLinkCopied] = useState(false);
  const [pageInPlace, setPageInPlace] = useState<BlockPageInAnchor | null>(null);
  // The window goes with the turn list, so a list opened later starts without it.
  if (!turnOpen && pageInPlace) setPageInPlace(null);
  const colorMenuRef = useRef<HTMLDivElement>(null);
  const turnMenuRef = useRef<HTMLDivElement>(null), toolbarLinkMenuRef = useRef<HTMLDivElement>(null);
  const colorButtonRef = useRef<HTMLButtonElement>(null), turnButtonRef = useRef<HTMLButtonElement>(null);
  const linkButtonRef = useRef<HTMLButtonElement>(null), linkInputRef = useRef<HTMLInputElement>(null);
  const selectionOwnerRef = useRef<HTMLElement>(editor.view.dom);
  const target = nativeTiptapSelectionContext(editor.state) ?? nativeTiptapSelectionIntoTable(editor.state);
  const block = target && adapter.graph.blocks.find(block => block.id === target.block.nativeId);
  const ids = [...new Set(projectedBlocks(editor.state.doc).filter(block => block.node.isTextblock
    && block.contentFrom < editor.state.selection.to && block.contentFrom + block.node.content.size > editor.state.selection.from)
    .map(block => block.nativeId))];
  // Words chosen across blocks: their types' row, Notion's (seen 2026-11-09),
  // named by the first block's type, or "여러 유형" for blocks of several types.
  const several = !target && ids.length > 1;
  const firstBlock = several ? adapter.graph.blocks.find(block => block.id === ids[0]) : undefined;
  const mixed = several && ids.some(id => adapter.graph.blocks.find(block => block.id === id)?.type !== firstBlock?.type);
  const writable = !adapter.readOnly && !suggesting.active;
  // The words a comment or a reaction is left on: a block's, a caption's, a
  // table cell's or across blocks, each taking both in the last row, as
  // Notion's (seen 2026-11-09; a cell's 2026-11-22).
  const chosen = chosenCommentText(editor.state);
  const activeMarks = { ...INACTIVE_MARKS };
  for (const key of Object.keys(markNames) as SelectionToolbarMark[]) activeMarks[key] = editor.isActive(markNames[key]);
  const activeColor = editor.getAttributes("nativeColor").stringValue ?? "default";
  const activeBackground = editor.getAttributes("nativeBackground").stringValue ?? "default_background";
  // The text takes the focus back with its words still chosen, as Notion's after
  // its link field's Enter (measured 2026-11-15 in Chrome, no script in the page:
  // the toolbar stayed and the next letter typed took the words' place).
  function refocus() { if (!editor.isDestroyed) editor.view.focus(); }
  // ⌘⇧O over the toolbar or its turn list lets the words go, the caret left at their end, and the toolbar closes as
  // the share opens, as Notion's (2026-11-26, Chrome 1440, two tabs at two times, judged by the screen and one letter
  // after the keys: the share's field took it; after its Escape nothing was chosen and no toolbar came back). The
  // letter then lands at that caret, as after any share the key opened (Notion's goes to the page's start).
  // Over the colors menu the share opens over it and the toolbar, which stay (not yet compared with Notion's).
  useShareKeyOpened(true, (over) => {
    if (over !== "toolbar" || colorsOpen || editor.isDestroyed) return;
    editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(editor.state.doc, editor.state.selection.to)));
    // From the turn list the text takes the focus, which the share gives back to as it closes.
    refocus(); hide();
  });
  // The window goes before the text takes the focus back: still open, its
  // focus scope pulled the focus back into its field, which the window took
  // away with it, the caret landing at the page's start (2026-11-15, once the
  // field took the focus as the window was drawn).
  function closeLink(restore = false) {
    if (!restore) { setLinkOpen(false); setLinkAtText(null); return; }
    flushSync(() => { setLinkOpen(false); setLinkAtText(null); });
    refocus();
  }
  // Nothing stays selected after either, so the bar doesn't come back.
  function comment() { if (commentOnChosenWords(editor, adapter.pageId)) hide(); }
  function react(button: HTMLElement) { if (reaction && reactToChosenWords(editor, adapter.pageId, reaction, button)) hide(); }
  function apply(mark: SelectionToolbarMark, fromKeys = false) {
    if (mark === "link") {
      const href = editor.getAttributes("link").href ?? "";
      // A link already there opens its own window, as its "편집" does: from
      // the link button, under it; from ⌘K, under the words. The toolbar
      // stays, as Notion's (seen 2026-11-09).
      if (href) { requestNativeTiptapLinkEdit(editor, fromKeys ? null : linkButtonRef.current); return; }
      setHadLinkOnOpen(Boolean(href)); setLinkValue(href); setLinkCopied(false);
      setColorsOpen(false); setTurnOpen(false); setLinkOpen(true); return;
    }
    if (suggesting.active) controller.suggestFormat({ [mark]: !activeMarks[mark] });
    // The words stay chosen with the text's focus (Notion's "B", measured 2026-11-15
    // in Chrome as above: the toolbar stayed, the next letter took the words' place).
    else if (writable) editor.chain().focus().toggleMark(markNames[mark]).run();
  }
  // As Notion's, the menu stays open and the text keeps its chosen words (measured
  // 2026-11-15 in Chrome as above: after a color the next letter took their place).
  function applyColor(token: string) {
    colorChosenWords(editor, token, { suggest: suggesting.active ? controller.suggestFormat : undefined, writable });
  }
  function clearMarks() {
    if (!writable) return;
    const chain = editor.chain().focus();
    // Links stay, as Notion's "형식 초기화" keeps them (seen 2026-10-30).
    for (const name of [...Object.values(markNames).filter((name) => name !== "link"), "nativeColor", "nativeBackground"]) chain.unsetMark(name);
    chain.run();
  }
  // The watcher's keys for the chosen text act here, one that came before
  // this drew too, before it is painted.
  function onCommand(command: EditorSelectionCommand) {
    if (command === "comment") comment();
    else if (command === "color") {
      // ⌘⇧H colors the words with the color last used, as Notion's; before any, it opens the colors.
      const last = storedEditorColor();
      if (last) applyColor(last); else setColorsOpen(true);
    }
    else if (command === "clear") clearMarks();
    else if (command === "equation") {
      // The same editor the toolbar's equation button opens, where it may.
      // Code holds only characters (Notion's turns the words to a broken glyph, seen 2026-11-05).
      if (writable && target && !suggesting.active && !target.node.type.spec.code) requestNativeTiptapEquation(editor, editor.state.selection.from, editor.state.selection.to);
    }
    else {
      setLinkAtText(command === "link" ? linkWindowAnchor(editor.view, editor.state) : null);
      apply(command, true);
    }
  }
  // The words the link window links are its target while it is open (nativeTiptapLinkTarget).
  useLayoutEffect(() => {
    if (!linkOpen) return undefined;
    const { from, to } = editor.state.selection;
    markLinkWindowTarget(editor.view, { from, to });
    return () => markLinkWindowTarget(editor.view, null);
  }, [editor, linkOpen]);
  const latestCommand = useRef(onCommand);
  latestCommand.current = onCommand;
  useLayoutEffect(() => {
    const own = commands.current;
    own.run = (command) => latestCommand.current(command);
    const waiting = own.waiting;
    own.waiting = null;
    if (waiting) own.run(waiting);
    return () => { own.run = null; };
  }, [commands]);
  async function putInPage(destinationPageId: string) {
    const context = adapter.moveContext;
    hide();
    if (!context || !writable) return;
    try {
      const caretAtTitle = await turnBlocksIntoPageIn(adapter, ids, false, destinationPageId, pageGraph);
      void adapter.graphSync?.reread();
      caretAtTitle?.();
    } catch {
      toast?.notify(t("blockHandle:turnPageIn.failed"), "error");
    }
  }
  const pageIn = turnOpen && pageInPlace && adapter.moveContext ? (
    <PageInPicker row={pageInPlace.row} menu={pageInPlace.menu} pages={adapter.moveContext.pages}
      returnFocusRef={pageInPlace.returnFocusRef} session={adapter.moveContext.session} workspaceId={adapter.workspaceId}
      sourcePageId={adapter.pageId} unavailable={pageInUnavailable(adapter.moveContext.pages, adapter.pageId, t("blockHandle:turnPageIn.unavailable"))}
      onChoose={(pageId) => void putInPage(pageId)}
      onClose={(leaveList) => { setPageInPlace(null); if (leaveList) pageInPlace.onLeaveList(); }} />
  ) : null;
  // Code takes no inline code: its button is off there, as Notion's (seen
  // 2026-11-07), and in a code block's caption (seen 2026-11-09, not in a
  // picture's). Words across blocks take no link (seen 2026-11-09).
  const { $from } = editor.state.selection;
  const offMarks = { code: $from.parent.type.name === "nativeCode"
    || ($from.parent.type.name === "nativeCaption" && $from.node($from.depth - 1).attrs.kind === "code"), link: several };
  return createPortal(<div style={{ display: "contents", visibility: away ? "hidden" : undefined }}><SelectionToolbarView rect={rect} page={page} activeMarks={activeMarks} offMarks={offMarks} activeColor={activeColor} activeBackground={activeBackground}
    selectionOwnerRef={selectionOwnerRef} linkReturnFocusRef={selectionOwnerRef} selectLinkOnOpen
    dismissToolbar={() => {
      dismissedSelection.current = editor.state.selection;
      hide();
    }}
    activeType={(block ?? firstBlock)?.type as BlockType ?? null} commentOnly={adapter.readOnly && !suggesting.active} apply={mark => apply(mark)}
    severalBlocks={several ? { count: ids.length, mixed } : undefined} focusText={refocus}
    // A caption's or a table cell's "⋯", and the equation and "⋯" across blocks, are greyed, as Notion's (seen 2026-11-09; a cell's 2026-11-22).
    inertMore={several || target?.field === "caption" || typeof target?.field === "number"} inertEquation={several}
    applyColor={applyColor} clearFormatting={clearMarks} linkPages={adapter.session ? { session: adapter.session, workspaceId: adapter.workspaceId } : undefined}
    applyLinkValue={(given) => {
      const href = given ?? normalizePastedLink(linkValue);
      // A typed text that is no address links nothing; the picker stays.
      if (!href && linkValue.trim()) return;
      if (suggesting.active) { controller.suggestFormat({ link: href }); closeLink(true); return; }
      if (!writable) return;
      if (href) editor.chain().focus().setMark("link", { href }).run();
      else editor.chain().focus().unsetMark("link").run();
      closeLink(true);
    }} closeColors={() => setColorsOpen(false)}
    closeTurn={() => setTurnOpen(false)} closeLinkEditor={closeLink}
    colorsOpen={colorsOpen} setColorsOpen={setColorsOpen} turnOpen={turnOpen} setTurnOpen={setTurnOpen}
    toolbarRef={toolbarRef} colorButtonRef={colorButtonRef} colorMenuRef={colorMenuRef}
    turnButtonRef={turnButtonRef} turnMenuRef={turnMenuRef} linkButtonRef={linkButtonRef}
    toolbarLinkMenuRef={toolbarLinkMenuRef} linkInputRef={linkInputRef} linkOpen={linkOpen} linkAtText={linkAtText} linkValue={linkValue}
    setLinkValue={setLinkValue} hadLinkOnOpen={hadLinkOnOpen} linkCopied={linkCopied} currentLinkUrl={normalizePastedLink(linkValue)}
    copyCurrentLink={async () => { if (await copyText(normalizePastedLink(linkValue))) setLinkCopied(true); }}
    openCurrentLink={() => { const href = normalizePastedLink(linkValue); if (href) window.open(href, "_blank", "noopener,noreferrer"); }}
    removeLink={() => { if (suggesting.active) controller.suggestFormat({ link: "" }); else if (writable) editor.chain().focus().unsetMark("link").run(); closeLink(true); }}
    commentSelection={comment} blockActions={Boolean(target?.field === "text")} commentAction={Boolean(chosen)}
    reactToSelection={reaction && chosen ? react : undefined}
    // The blocks turned, the words stay chosen with the text's focus and the
    // panel goes, as Notion's (measured 2026-11-16 in Chrome, no script in the
    // page: a word chosen, and words across two lines, turned into 제목 2 and
    // 제목 3 from the panel's list — the panel gone, the next letter took the
    // words' place); a new choosing draws it again.
    turnIntoAvailable={writable && Boolean(ids.length)} turnInto={choice => {
      if (writable) controller.run(() => { turnBlocksInto(adapter, ids, choice); });
      dismissedSelection.current = editor.state.selection;
      hide(); refocus();
    }} onPageIn={adapter.moveContext && writable ? setPageInPlace : undefined}
    // Its "⋯" opens the block's menu where it was pressed and chooses the
    // block, chosen still once Escape closes the menu, as a block's own tools'
    // "⋯" (openBlockToolMenu), not under the block's box as ⌘/ does (Notion's,
    // seen 2026-11-18 in Chrome at 1440: the menu's top left at the pointer,
    // over it where only that has room, kept 12px inside the window). The bar
    // is drawn outside the editor: the chosen words say which editor and which
    // drawing of the block (a synced copy's) it is.
    openBlockMenu={block ? (event) => openBlockToolMenu(block.id, event, chosenWordsElement(editor.view)) : undefined}
    makeEquation={writable && target ? () => requestNativeTiptapEquation(editor, editor.state.selection.from, editor.state.selection.to) : undefined}
    suggesting={suggesting.onChange && !suggesting.inPeek ? { active: suggesting.active, onChange: suggesting.onChange } : undefined} />{pageIn}</div>, document.body);
}
