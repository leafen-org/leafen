import { startTransition, useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type SetStateAction } from "react";
import { afterNextPaint } from "@/lib/afterPress";
import { useOptionalProductToast } from "@/lib/productToast";
import { createNativeTiptapController } from "./nativeTiptapController";
import { nativeTiptapNodeViews } from "./nativeTiptapNodeViews";
import { createLatestGraph, NativeTiptapAdapterContext, nativeTiptapGraphIndex, NativeTiptapReactContext } from "./nativeTiptapReactContext";
import { adapterDrawnFieldsBesideGraph, createAdapterStore, createNativeTiptapUiStore, liveAdapter, liveSelection } from "./nativeTiptapLiveState";
import { editorGoing, NativeEditorContent } from "./NativeEditorContent";
import { registerNativeTiptapFocus } from "./nativeTiptapFocus";
import { NativeTiptapLinkPill } from "./NativeTiptapLinkPill";
import { NativeTiptapLinkMentionTurn } from "./NativeTiptapLinkMentionTurn";
import { NativeTiptapMenus } from "./NativeTiptapMenus";
import { NativeTiptapInlineUi } from "./NativeTiptapInlineUi";
import { NativeTiptapCommentMarkPress } from "./NativeTiptapCommentMarkPress";
import { NativeTiptapSelectionToolbar } from "./NativeTiptapSelectionToolbar";
import { NativePhoneToolbar } from "./NativePhoneToolbar";
import { EditorDropLayer } from "./EditorDropLayer";
import { EditorSurface } from "./EditorSurface";
import { SyncedEmptyContent } from "./SyncedEmptyContent";
import { PageStarter } from "./PageStarter";
import { openTogglesAround, useBlockAnchorScroll } from "./useBlockAnchorScroll";
import { useBlockSelectionKeys } from "./useBlockSelectionKeys";
import { allBlockSelection } from "./blockSelectionKeyboard";
import { editorDescendantIds } from "./editorBlockIndex";
import { PagePresenceFaces } from "./PagePresenceFaces";
import { trackPagePresenceCaret } from "@/lib/pagePresenceCaret";
import { useKeptChoice } from "@/lib/selectionMemory";
import { listenForBlockSelection } from "./editorCommands";
import { besideEditor, dropTextRangeIn, editorOwnsKeyboard, editorRootOf, notePointedEditor } from "./editorScope";
import type { EditorProps, EditorSelection } from "./editorRuntimeTypes";
import styles from "./NativeTiptap.module.css";
import { PageButtonExecutionScope } from "./pageButtonExecutionState";
import { i18next } from "@/i18n";
import { chordLetter, matchesKeyboardShortcut, primaryModifier } from "@/lib/keyboardShortcuts";
import { isTextField } from "@/lib/menuKeyboard";
import { useSuggestingMode } from "./suggestingMode";
import { useSuggestedRemovalKeys } from "./useSuggestedRemovalKeys";
import { usePublicRichtextLinks } from "./publicRichtext";
import { richtextLinkHref } from "./richtextPageLinks";
import { textLinkHref } from "./richtextUrlPolicy";
import { recordCommittedNativeEditorCommand } from "./nativeEditorUndoOwner";
import { nativeEditorStepsBegun } from "./nativeEditorUndoScopes";
import { listenForPageChoose, listenForPageGraphChange, listenForPageHistoryStep, listenForPageUndo } from "./pageUndo";
import { listenForBodyStartPaste, listenForBodyStartSplit } from "./pageBodyStart";
import { selectAfterHistoryStep } from "./editorBlockDuplicate";
import { usePeekSetsChoiceAside } from "./peekChosenBlocks";
import { pageBodyReady } from "@/lib/pageTitleFocus";
import { rehearseBlockMenu } from "./blockMenuRehearsal";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { offerDroppedBlockMove } from "./blockMoveHistory";
import { keepPageGraphForCutRows } from "./cutPagePaste";
import { inlineDatabaseDrawable } from "@/lib/inlineDatabaseFullPage";
import { keepChoicePlaces, placeChoice } from "./blockOccurrences";

/** The same Tiptap surface serves pages, row bodies and synced originals. */
function EditorBody({ adapter, loading, rootBlockId = null, onlyBlockId = null, staticReadOnlySemantics, showPageStarter, starter }: EditorProps) {
  const rootRef = useRef<HTMLDivElement>(null), rootId = useId();
  const loadingRef = useRef(loading); loadingRef.current = loading;
  // Node views draw from these by what each shows (nativeTiptapLiveState.ts).
  const [ui] = useState(createNativeTiptapUiStore);
  const selection = useSyncExternalStore(ui.subscribe, () => ui.get().selection);
  const setSelection = useCallback((next: SetStateAction<EditorSelection>) => ui.set("selection", next), [ui]);
  const setColumnPreview = useCallback((next: SetStateAction<ReadonlyMap<string, number>>) => ui.set("columnPreview", next), [ui]);
  const setActiveTabs = useCallback((next: SetStateAction<ReadonlyMap<string, string>>) => ui.set("activeTabs", next), [ui]);
  const [selectionNow] = useState(() => liveSelection(ui));
  const selectionRef = useRef(selection); selectionRef.current = selection;
  const latest = useRef(adapter); latest.current = adapter;
  const [adapterNow] = useState(() => liveAdapter(latest));
  const [adapterStore] = useState(() => createAdapterStore(adapter));
  adapterStore.keep(adapter);
  useLayoutEffect(() => adapterStore.tell(), [adapter, adapterStore]);
  const drawn = useRef<{ fields: readonly unknown[]; revision: number }>({ fields: [], revision: 0 });
  const fields = adapterDrawnFieldsBesideGraph(adapter);
  if (fields.length !== drawn.current.fields.length || fields.some((field, index) => !Object.is(field, drawn.current.fields[index]))) {
    drawn.current = { fields, revision: drawn.current.revision + 1 };
  }
  const adapterRevision = drawn.current.revision;
  const graphIndex = useMemo(() => nativeTiptapGraphIndex(adapter.graph.blocks), [adapter.graph.blocks]);
  // A view whose own block changed draws at once from the newest graph
  // (useNativeTiptapNode); one showing something else of the blocks around
  // it, past the frame that shows the change, in a transition a new press
  // cuts (the press answers on the next frame: a long page's paste drew all
  // its views first, and then all of them again past that frame).
  const [latestGraph] = useState(() => createLatestGraph(graphIndex));
  latestGraph.keep(graphIndex);
  useLayoutEffect(() => latestGraph.tell(), [graphIndex, latestGraph]);
  const aroundPending = useRef<(() => void) | null>(null);
  useEffect(() => {
    aroundPending.current ??= afterNextPaint(() => {
      aroundPending.current = null;
      startTransition(() => latestGraph.tellAround());
    });
  }, [graphIndex, latestGraph]);
  useEffect(() => () => { aroundPending.current?.(); aroundPending.current = null; }, []);
  const links = usePublicRichtextLinks(), linksRef = useRef(links); linksRef.current = links;
  const suggesting = useSuggestingMode();
  const modeRef = useRef(suggesting); modeRef.current = suggesting;
  const toast = useOptionalProductToast();
  const [controller, setController] = useState<ReturnType<typeof createNativeTiptapController> | null>(null);
  const resource = useRef<{ active: boolean; core: ReturnType<typeof createNativeTiptapController> } | null>(null);
  useLayoutEffect(() => {
    let held = resource.current;
    if (!held) {
      const lease = { active: true };
      // The page's adapter as it stands each time it is read: one kept through
      // several changes reads the graph they made. A copy of it kept the graph
      // it was taken with, and a paste with databases put each line of it in
      // that graph, so the pasting tab drew them out of order (2026-11-08).
      const adapterLeased = liveAdapter(latest, { isCurrent: () => lease.active && latest.current.isCurrent() });
      const core = createNativeTiptapController(() => adapterLeased, {
        rootId: rootBlockId, onlyBlockId, nodeViews: nativeTiptapNodeViews(),
        linkHref: href => richtextLinkHref(textLinkHref(href), linksRef.current ?? undefined),
        suggesting: () => modeRef.current,
        selectBlocks: (id, at) => {
          const current = latest.current;
          const chosen = id ? placeChoice(new Set([id]), at ? [[id, at]] : undefined) : allBlockSelection(current.graph, rootBlockId);
          setSelection(current.fixedBody ? new Set([...chosen].filter(id => current.graph.blocks.find(block => block.id === id)?.parentId)) : chosen);
        },
        chosenBlocks: () => ui.get().selection,
        chooseBlocks: (ids, places) => {
          if (ids.length) notePointedEditor(rootRef.current);
          setSelection(current => ids.length || current.size ? placeChoice(new Set(ids), places) : current);
        },
        onPasteRefused: () => {
          // Why nothing was pasted and what to try instead, from the editor's own messages.
          const message = String(i18next.t("editor:pasteRefused"));
          if (toast) toast.notify(message, "error"); else latest.current.moveContext?.onStatus?.(message, "error");
        },
      });
      held = Object.assign(lease, { core }); resource.current = held;
    }
    held.active = true; setController(held.core);
    editorGoing(held.core.editor, false);
    return () => {
      held.active = false;
      // Its content unmounts after this: the editor goes with it.
      editorGoing(held.core.editor, true);
      // A StrictMode setup can reacquire this committed view before the
      // microtask; a real unmount disposes every native observer/handler.
      queueMicrotask(() => { if (!held.active) { held.core.dispose(); if (resource.current === held) resource.current = null; } });
    };
    // Scope changes remount EditorBody through its native identity key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useLayoutEffect(() => {
    if (!controller) return;
    let current = true;
    // React NodeViewRenderer flushes a newly created view synchronously.
    // Projection runs after this React commit, outside its layout lifecycle.
    queueMicrotask(() => { if (current) controller.sync(); });
    return () => { current = false; };
  }, [adapter, controller, suggesting.active]);
  useLayoutEffect(() => rootRef.current && controller ? registerNativeTiptapFocus(rootRef.current, controller) : undefined, [controller]);
  // A page's body that can take the caret says so before the frame it first
  // shows in is painted, its lines and their views drawn by then: what was
  // pressed or typed for it while it opened goes there (pageBodyReady). In
  // the commit itself its lines' views weren't all drawn yet, and a press
  // took a line two below the one pressed.
  useLayoutEffect(() => {
    if (!controller || loading || adapter.readOnly || rootBlockId !== null) return;
    const frame = window.requestAnimationFrame(() => pageBodyReady());
    return () => window.cancelAnimationFrame(frame);
  }, [adapter.readOnly, controller, loading, rootBlockId]);
  // Its lines' block menu is drawn once out of sight after the start, so its
  // first press is like a later one (blockMenuRehearsal.tsx). A page read
  // without an account has no handles.
  const handles = !adapter.readOnly || adapter.session !== null;
  useEffect(() => { if (handles) rehearseBlockMenu(); }, [handles]);
  useEffect(() => controller && rootBlockId === null && !onlyBlockId && !adapter.fixedBody
    ? listenForBodyStartPaste(adapter.pageId, () => rootRef.current, (blocks) => {
      if (loadingRef.current) return false;
      const made = controller.pasteAtStart(blocks);
      if (!made) return false;
      // Chosen with no caret, as Notion's (seen 2026-11-04), and this editor's
      // keys act on them, whichever other editor shows.
      if (made.length) {
        if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
        notePointedEditor(rootRef.current);
        setSelection(new Set(made));
      }
      return true;
    }) : undefined,
  [adapter.fixedBody, adapter.pageId, controller, onlyBlockId, rootBlockId, setSelection]);
  useEffect(() => controller && rootBlockId === null && !onlyBlockId && !adapter.fixedBody
    ? listenForBodyStartSplit(adapter.pageId, () => rootRef.current, (text, title) => !loadingRef.current && controller.startFromTitle(text, title))
    : undefined, [adapter.fixedBody, adapter.pageId, controller, onlyBlockId, rootBlockId]);
  useEffect(() => rootBlockId === null && !onlyBlockId && !adapter.readOnly && controller && rootRef.current
    ? trackPagePresenceCaret(adapter.pageId, rootRef.current) : undefined,
  [adapter.pageId, adapter.readOnly, controller, onlyBlockId, rootBlockId]);
  // Its blocks dragged onto a page in the sidebar move as its move dialog moves them.
  const pageGraph = useOpenPageGraphStore();
  const movesBlocks = Boolean(adapter.moveContext) && !adapter.readOnly;
  useEffect(() => rootBlockId === null && !onlyBlockId && movesBlocks ? offerDroppedBlockMove(adapterNow, pageGraph) : undefined,
    [adapter.pageId, adapterNow, movesBlocks, onlyBlockId, pageGraph, rootBlockId]);
  // A cut database item pasted here shows its ⌘Z and ⌘⇧Z on the page as drawn (cutPagePaste.ts).
  useEffect(() => rootBlockId === null && !onlyBlockId ? keepPageGraphForCutRows(adapter.pageId, pageGraph) : undefined, [adapter.pageId, onlyBlockId, pageGraph, rootBlockId]);
  useBlockAnchorScroll(adapter.pageId, adapter.graph.blocks.length, rootBlockId === null,
    (blockId) => openTogglesAround(adapter.graph.blocks, blockId, (toggle) => adapter.setToggleCollapsed(toggle, false)),
    // The block a fixed body is — a full-page database's, a database opened alone — is the page (BlockFrame's `fixed`).
    (blockId) => blockId === onlyBlockId || latest.current.fixedBody === true
      && latest.current.graph.blocks.some((block) => block.id === blockId && !block.parentId));
  useBlockSelectionKeys({ adapter, rootRef, nested: rootBlockId !== null, rootBlockId, selectionRef, setSelection, engine: controller ?? undefined });
  useSuggestedRemovalKeys({ adapter, rootRef, nested: rootBlockId !== null, rootBlockId, selectionRef, setSelection, engine: controller ?? undefined, suggesting: suggesting.active });
  const chosenNow = useCallback(() => ui.get().selection, [ui]);
  usePeekSetsChoiceAside(rootRef, chosenNow, setSelection, id => latest.current.graph.blocks.some(block => block.id === id));
  // Blocks chosen while the text has no focus leave no text range behind, as
  // Notion's: a syllable typed then went into the line the caret had left
  // (2026-11-05). A press that chooses a block drops it once it lands (BlockFrame).
  // It goes once the frame after the choice is painted: reading the range
  // laid out the page the choice had just changed, again in its frame
  // (2026-11-15: some 2.5 ms of a ⌘V with a picture chosen in a 1,031-block
  // page at CPU ×2). A syllable started meanwhile drops it itself
  // (useBlockSelectionKeys).
  useEffect(() => {
    let waiting: (() => void) | null = null;
    const textFocused = () => {
      const active = document.activeElement;
      return active instanceof HTMLElement && active.isContentEditable && Boolean(rootRef.current?.contains(active));
    };
    const stop = ui.subscribe(() => {
      if (waiting || !ui.get().selection.size || textFocused()) return;
      waiting = afterNextPaint(() => {
        waiting = null;
        if (ui.get().selection.size && !textFocused()) dropTextRangeIn(rootRef.current);
      });
    });
    return () => { stop(); waiting?.(); };
  }, [ui]);
  useEffect(() => {
    setSelection(current => keepChoicePlaces(current, new Set([...current].filter(id => adapter.graph.blocks.some(block => block.id === id)))));
  }, [adapter.graph.blocks]);
  // A page's chosen blocks come back when the tab reloads, and with the way back or forward, as Notion's (selectionMemory.ts).
  useKeptChoice(rootBlockId === null && !onlyBlockId ? `blocks:${adapter.pageId}` : null, selection, setSelection,
    id => latest.current.graph.blocks.some(block => block.id === id), () => !loadingRef.current && latest.current.graph.blocks.length > 0, true);
  // A block made without text is selected, as Escape selects one.
  // A block made in a synced copy on its original's page is chosen there (requestEditorBlockSelection's `at`).
  useEffect(() => listenForBlockSelection((blockId, owner, keepChosen, at) => {
    if (owner ? owner !== rootRef.current : rootBlockId !== null) return false;
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    setSelection((current) => keepChosen && current.has(blockId) ? current : placeChoice(new Set([blockId]), at ? [[blockId, at]] : undefined));
    return true;
  }), [rootBlockId, setSelection]);
  useEffect(() => {
    function pointed(event: PointerEvent) {
      notePointedEditor(event.target);
      const owner = editorRootOf(event.target);
      // A press on a block's words lets go of the chosen blocks, as Notion's —
      // a right one too, which then opens the text menu on the word, not the
      // block menu (seen 2026-11-13); a code block's colored copy at rest is
      // its words too (it hands the caret to the code under it).
      const text = event.target instanceof Element && event.target.closest("[data-rt-editable], [data-code-highlight]");
      // So does a plain press on the page around it (besideEditor).
      const beside = event.button === 0 && !event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey
        && besideEditor(event.target, rootRef.current);
      if (owner && (owner !== rootRef.current || text) || beside) setSelection(current => current.size ? new Set() : current);
    }
    // A caret put in a field there by the keys lets go of them too.
    function focused(event: FocusEvent) {
      const target = event.target;
      if (target instanceof HTMLElement && (target.isContentEditable || target.matches("input, textarea"))
        && besideEditor(target, rootRef.current)) setSelection(current => current.size ? new Set() : current);
    }
    function undo(event: KeyboardEvent) {
      if (event.defaultPrevented || latest.current.readOnly
        || !editorOwnsKeyboard(rootRef.current, rootBlockId !== null) || !primaryModifier(event)) return;
      // A field typed in keeps its own; a checkbox just pressed does not —
      // its change is the page's step, as Notion's (seen 2026-11-13).
      const active = document.activeElement;
      if (active instanceof HTMLElement && (active.isContentEditable || isTextField(active)
        || active.closest('select,[role="dialog"]:not([data-page-peek],[data-row-peek-panel]),[role="menu"],[role="textbox"]:not([data-native-editor-content])'))) return;
      const key = chordLetter(event);
      if (key !== "z" && key !== "y") return;
      event.preventDefault();
      const forward = event.shiftKey || key === "y";
      selectAfterHistoryStep(() => latest.current, setSelection, forward, () => {
        if (forward) controller?.editor.commands.redo(); else controller?.editor.commands.undo();
      });
    }
    document.addEventListener("pointerdown", pointed, true); document.addEventListener("keydown", undo);
    document.addEventListener("focusin", focused);
    // The page menu's "실행 취소" reaches the page's own editor, not a synced block's.
    // Taken, it says when the step began: at once, or once those pressed before it have gone (nativeEditorUndoOwner).
    const stopPageUndo = rootBlockId === null ? listenForPageUndo(adapter.pageId, (forward) => {
      if (latest.current.readOnly || !controller) return false;
      if (forward) controller.editor.commands.redo(); else controller.editor.commands.undo();
      const { session, workspaceId, pageId } = latest.current;
      return nativeEditorStepsBegun(`${workspaceId}/${pageId}/${session?.user.id ?? ""}`);
    }) : undefined;
    const stopPageChanges = rootBlockId === null ? listenForPageGraphChange(adapter.pageId,
      ({ before, after, operations }) => latest.current.localGraphChanged(before, after, operations)) : undefined;
    // Blocks chosen from elsewhere: an editor of one block's content, as a
    // synced copy's, takes them when they are all in it.
    const stopPageChoose = listenForPageChoose(adapter.pageId, (ids) => {
      const inside = rootBlockId === null ? null : editorDescendantIds(latest.current.graph, [rootBlockId]);
      if (!inside || ids.every((id) => id !== rootBlockId && inside.has(id))) setSelection(new Set(ids));
    });
    const stopHistorySteps = rootBlockId === null ? listenForPageHistoryStep(adapter.pageId, (key, step) => {
      const { session, workspaceId, pageId } = latest.current;
      if (session) recordCommittedNativeEditorCommand(`${workspaceId}/${pageId}/${session.user.id}`, key, step);
    }) : undefined;
    return () => {
      document.removeEventListener("pointerdown", pointed, true); document.removeEventListener("keydown", undo); document.removeEventListener("focusin", focused); stopPageUndo?.(); stopPageChanges?.(); stopPageChoose?.(); stopHistorySteps?.();
    };
  }, [adapter.pageId, controller, rootBlockId, setSelection]);
  // A new value draws every node view again: for a change to what they draw
  // from the adapter beside its graph.
  const context = useMemo(() => controller ? { adapter: adapterNow, latestGraph, controller, selection: selectionNow,
    setSelection, setColumnPreview, setActiveTabs, staticReadOnlySemantics, ui } : null,
  // eslint-disable-next-line react-hooks/exhaustive-deps
  [adapterNow, adapterRevision, controller, latestGraph, selectionNow, setActiveTabs, setColumnPreview, setSelection, staticReadOnlySemantics, ui]);
  if (!controller || !context) return null;
  const empty = controller.editor.state.doc.childCount === 0;
  return <NativeTiptapAdapterContext.Provider value={adapterStore}><NativeTiptapReactContext.Provider value={context}>
    <div ref={rootRef} tabIndex={-1} className={styles.surface} data-editor-root={rootId} data-editor-page={adapter.pageId}
      data-editor-generation={adapter.generation} data-editor-nested={rootBlockId !== null ? "true" : undefined}
      onKeyDown={(event) => {
        // ⌘⇧⌥X turns suggesting on or off, as Notion's "편집 내용 제안".
        if (event.defaultPrevented || !suggesting.onChange || !matchesKeyboardShortcut("suggestEdits", event)) return;
        event.preventDefault();
        suggesting.onChange(!suggesting.active);
      }}>
      {rootBlockId === null && !onlyBlockId ? <PagePresenceFaces pageId={adapter.pageId} rootRef={rootRef} /> : null}
      <EditorDropLayer adapter={adapter} history={controller} selection={selection} setSelection={setSelection}>
        <NativeEditorContent editor={controller.editor} />
        {rootBlockId !== null && empty ? <SyncedEmptyContent adapter={adapter} originalId={rootBlockId} at={rootBlockId} /> : null}
      </EditorDropLayer>
      <NativeTiptapMenus /><NativeTiptapInlineUi /><NativeTiptapCommentMarkPress /><NativeTiptapSelectionToolbar /><NativeTiptapLinkPill /><NativeTiptapLinkMentionTurn />
      {/* The page's own tools stand under its title's caret too, but on an empty page of its own, whose starter is Notion's
          template bar there; a database's page and a template's have them empty too, as Notion's (NativePhoneToolbar). */}
      <NativePhoneToolbar title={rootBlockId === null && !onlyBlockId && !(showPageStarter && starter)} />
    </div>
  </NativeTiptapReactContext.Provider></NativeTiptapAdapterContext.Provider>;
}

export function Editor(props: EditorProps) {
  const { adapter, loading, showPageStarter, pagePlaceholderBlockId, onAddDatabase, starter, starterHint, onAddParagraph, rootBlockId = null } = props;
  const identity = `${adapter.pageId}/${adapter.generation}/${adapter.collaborationDocument?.guid ?? "static"}/${rootBlockId}/${props.onlyBlockId ?? ""}`;
  // A page only read is drawn from its graph alone when no document comes for
  // it (a public page's); one whose document is still on its way waits for it,
  // as an editable page does, and is drawn once (2026-11-11: a reader's page
  // and every database in it were drawn again when the document came, the
  // presses made meanwhile lost and each database read again).
  const body = adapter.collaborationDocument || (adapter.readOnly && !loading) ? <EditorBody key={identity} {...props} /> : null;
  const scopedBody = <PageButtonExecutionScope identity={`${adapter.pageId}/${adapter.generation}`} blocks={adapter.graph.blocks}>{body}</PageButtonExecutionScope>;
  if (rootBlockId !== null) return scopedBody;
  // The starter owns the chooser until its selection settles; writing
  // returns through the existing blank-line focus owner.
  const pageStarter = !adapter.readOnly ? <PageStarter key={`${adapter.pageId}/${adapter.generation}`} adapter={adapter} visible={showPageStarter}
    placeholderBlockId={pagePlaceholderBlockId ?? null} onAddDatabase={onAddDatabase} actions={starter} hint={starterHint}
    onText={() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); onAddParagraph(); }} /> : null;
  // An inline database opened alone, drawn from what its graph holds of it,
  // shows no loading lines over it while the page holding it is still read
  // (one it moved to, drawn as it was: lib/inlineDatabaseMovedShown.ts).
  const drawnAlone = Boolean(body && props.onlyBlockId && inlineDatabaseDrawable(adapter.graph, props.onlyBlockId));
  return <EditorSurface pageId={adapter.pageId} loading={loading && !drawnAlone} unavailable={props.unavailable} populated={adapter.graph.blocks.length > 0} starter={pageStarter} starterVisible={showPageStarter && !adapter.readOnly}
    starterHint={Boolean(starterHint)}>
    {props.unavailable ? null : scopedBody}
  </EditorSurface>;
}
