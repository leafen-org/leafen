import { afterNextPaint } from "@/lib/afterPress";
import { pageTitleJoin, rowTitleCaretAtEnd, rowTitleUndo } from "@/lib/inlineDatabaseRequests";
import { databaseRedoCaret } from "./databaseRedoCaret";
import { focusEditableNextFrame } from "./focus"; import { bodyUnseen } from "./pageBodyFrame"; import { showPastedBlocks } from "./pastedBlocksShown";
import { newId } from "@/lib/ids";
import { linesTitleMade } from "./pageUndo";
import { keepFreshDatabasesThroughStep } from "@/lib/freshDatabases";
import { invalidateNativeDomSelection } from "@/lib/tiptapDomSelection";
import { TiptapReadOnlyMark } from "@/lib/tiptapReadOnlyMark";
import { Editor, Extension, type Extensions, type NodeViewRenderer } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import { ReplaceStep } from "@tiptap/pm/transform";
import type { Mark, Node } from "@tiptap/pm/model";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { createNativeEditorUndoOwner } from "./nativeEditorUndoOwner";
import {
  createNativeTiptapBinding, nativeProjectionMeta, nativeTiptapSelection,
  projectedBlocks, restoreNativeTiptapSelection, type NativeRelativeSelection,
} from "./nativeTiptapBinding";
import { nativeTiptapBlocks, type NativeProjectionCache } from "./nativeTiptapGraph";
import { nativeTiptapGraphChange, nativeTiptapStructureChanged } from "./nativeTiptapGraphChange";
import { NativeTiptapKeys } from "./nativeTiptapKeys";
import { AppleTouchPlainText } from "./appleTouchPlainText";
import { CompositionEndKey } from "./compositionEndKey";
import { NativeTiptapWidgetSelection } from "./nativeTiptapWidgetSelection";
import { NativeTiptapDomRead } from "./nativeTiptapDomRead";
import { NativeTiptapCaretScroll } from "./nativeTiptapCaretScroll";
import { typedUndoStart, typingOverLines } from "./nativeTiptapUndoStart";
import { NativeTiptapCodeColors } from "./nativeTiptapCodeColors";
import { NativeTiptapCaretLine } from "./nativeTiptapCaretLine";
import { NativeTiptapChosenAtoms } from "./nativeTiptapChosenAtoms";
import { NativeTiptapFieldKeys } from "./nativeTiptapFieldKeys";
import { NativeTiptapSyncedCopyBackspace } from "./syncedCopyBackspace";
import { NativeTiptapStructureKeys } from "./nativeTiptapStructureKeys";
import { NativeTiptapEngineKeymap, NativeTiptapMergeKeys } from "./nativeTiptapMergeKeys";
import { isEditorBlockDrag } from "./editorDragModel";
import { externalDragHasFiles, FILE_DROP_BOUNDS } from "./fileDrop";
import { PAGE_DRAG_TYPE } from "@/components/dndTypes";
import { NativeTiptapClipboard, pasteAtDocStart } from "./nativeTiptapClipboard";
import { NativeTiptapCopiedWords } from "./nativeTiptapCopiedWords";
import type { PastedBlock } from "./markdownPaste";
import type { TitleSplit } from "./pageBodyStart";
import { dispatchPastedBlocksMenu, type PastedBlocksMenuRequest } from "./textBlockClipboardModel";
import { newNativeTiptapAttrs } from "./nativeTiptapIdentity";
import { copiedLines, copyPastedDatabases, pasteBlocksAfter, pastedSpot, pastePlace, pastesCut, pastesServerBlocks, placePastedLines, standsAt } from "./blockClipboard";
import { blockItemText } from "./blockItemLabels";
import { nativeTiptapExtensions } from "./nativeTiptapSchema";
import { reconcileNativeTiptapProjection } from "./nativeTiptapProjection";
import { NativeTiptapCopyIndicator, requestEditorCopyIndicators } from "./nativeTiptapCopyIndicator";
import { NativeTiptapCommentPending } from "./nativeTiptapCommentPending";
import { NativeTiptapLinkTarget } from "./nativeTiptapLinkTarget";
import { NativeTiptapEquationDraft } from "./nativeTiptapEquationDraft";
import { NativeTiptapQuoteHolder } from "./nativeTiptapQuoteHolder";
import { NativeTiptapBlockRuns } from "./nativeTiptapBlockRuns";
import { nativeTiptapTextFields } from "./nativeTiptapTextFields";
import { NativeTiptapInputRules } from "./nativeTiptapInputRules";
import { nativeTiptapProductKeys } from "./nativeTiptapProductKeys";
import { createNativeTiptapSuggestions } from "./nativeTiptapSuggestions";
import { NodeSelection, Selection, TextSelection } from "@tiptap/pm/state";
import { pmOffsetAt } from "./nativeTiptapInline";
import type { SuggestingMode } from "./suggestingMode";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { actedSince, CARET_SCROLL_MARGIN, CARET_SCROLL_THRESHOLD, keepCaretClear } from "./nativeTiptapCaretClear";

/**
 * What a person holds when an action begins or ends: a caret, blocks chosen
 * with none, or nothing. Undo and Redo give it back, as Notion's do (seen
 * 2026-11-04): blocks taken away chosen come back chosen, and the Redo that
 * takes them away again leaves nothing chosen and no caret.
 */
type Held = { text?: NativeRelativeSelection; caret: boolean; chosen: readonly string[]; marks?: readonly Mark[] | null };

/** One engine view belongs to one page generation and collaborative document. */
export function createNativeTiptapController(
  getAdapter: () => NativeEditorGraphAdapter,
  options: { rootId?: string | null; onlyBlockId?: string | null; extensions?: Extensions; nodeViews?: Readonly<Record<string, NodeViewRenderer>>;
    suggesting?(): SuggestingMode; selectBlocks?(blockId: string | null, at?: string): void; onPasteRefused?(error: RangeError): void;
    chosenBlocks?(): ReadonlySet<string>; chooseBlocks?(ids: readonly string[], places?: ReadonlyMap<string, string>): void;
    linkHref?(href: string): string } = {},
) {
  const initial = getAdapter();
  const document = initial.collaborationDocument;
  if (!document && !initial.readOnly) throw new Error("A writable native editor requires its collaborative document.");
  const rootId = options.onlyBlockId ? initial.graph.blocks.find(block => block.id === options.onlyBlockId)?.parentId ?? null : options.rootId ?? null;
  let disposed = false;
  // The blocks as last projected, made again only where they changed (nativeTiptapBlocks).
  const projected: NativeProjectionCache = new Map();
  // Where a press landed that came from a field in the page, until its mouseup.
  let pressFromField: { left: number; top: number } | null = null;
  const isCurrent = () => {
    const adapter = getAdapter();
    return !disposed && adapter.isCurrent() && !adapter.signal.aborted
      && adapter.pageId === initial.pageId && adapter.generation === initial.generation
      && adapter.collaborationDocument === document;
  };
  const canWrite = () => !!document && isCurrent() && !getAdapter().readOnly && owner?.isCurrent() === true;
  const suggestions = createNativeTiptapSuggestions({ adapter: getAdapter, mode: () => options.suggesting?.(), current: isCurrent,
    settled(blockId, caret, moved) {
      if (!isCurrent()) return;
      binding?.flushDeferred();
      project(nativeTiptapBlocks(getAdapter().graph, document, editor.schema, rootId, initial.pageId, options.onlyBlockId, projected));
      const current = projectedBlocks(editor.state.doc).find(block => block.nativeId === blockId && block.node.isTextblock);
      if (!moved && editor.isFocused && current && caret !== null) editor.view.dispatch(editor.state.tr
        .setSelection(TextSelection.create(editor.state.doc, current.contentFrom + pmOffsetAt(current.node.content, caret)))
        .setMeta(nativeProjectionMeta, true).setMeta("addToHistory", false));
    },
  });
  const canInput = () => canWrite() || !!document && suggestions.active();
  const held = (state = editor.state): Held => {
    const chosen = options.chosenBlocks?.();
    const blocks = chosen?.size ? new Set(getAdapter().graph.blocks.map(block => block.id)) : null;
    return { caret: editor.view.hasFocus(), chosen: blocks ? [...chosen!].filter(id => blocks.has(id)) : [],
      text: document ? binding?.selection(state) ?? nativeTiptapSelection(state, document) : undefined, marks: state.storedMarks };
  };
  // Taking the chosen blocks away leaves nothing in hand.
  const heldAfter = (before: Held, state?: typeof editor.state): Held => {
    const after = held(state);
    return before.chosen.length && !after.chosen.length && !after.caret ? { caret: false, chosen: [] } : after;
  };
  let owner: ReturnType<typeof createNativeEditorUndoOwner<Held>> | null = null;
  function acquireHistory() {
    if (!document || owner || getAdapter().readOnly) return;
    owner = createNativeEditorUndoOwner<Held>(getAdapter, saved => {
      sync();
      if (!saved || !isCurrent() || databaseRedo.placed()) return;
      if (saved.text) editor.view.dispatch(restoreNativeTiptapSelection(editor.state.tr, document, saved.text, saved.marks)
        .setMeta(nativeProjectionMeta, true).setMeta("addToHistory", false));
      options.chooseBlocks?.(saved.chosen);
      if (saved.chosen.length || !saved.text) letGoOfText();
      else if (!caretInDatabaseBack() && saved.caret) editor.view.focus();
    }, () => held(), (forward) => { historyStep(forward); });
  }
  const databaseRedo = databaseRedoCaret(() => getAdapter().graph, initial.pageId, ids => options.chooseBlocks?.(ids));
  /**
   * A database a history step leaves chosen leaves the caret in the page, at
   * the start of the line before it, not in its title nor the whole of it
   * chosen with the focus nowhere; with no line before or after it, at the
   * start of the page's title.
   */
  function caretInDatabaseBack() {
    const { selection, doc } = editor.state;
    if (!(selection instanceof NodeSelection) || !String(selection.$from.parent.attrs.kind).includes("database")) return false;
    const before = Selection.findFrom(doc.resolve(selection.from), -1, true);
    const near = before ?? Selection.findFrom(doc.resolve(selection.to), 1, true);
    if (!near) {
      const title = pageTitleJoin(initial.pageId);
      if (!title?.caretAtStart) return false;
      title.caretAtStart();
      return true;
    }
    const at = before ? before.$from.start() : near.from;
    editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(doc, at)).setMeta("addToHistory", false));
    editor.view.focus();
    return true;
  }
  /**
   * A paste with blocks the server makes (copied databases): what the page
   * makes itself goes in at once, in order, chosen, each of the server's
   * holding its place with "불러오는 중..." until it comes there, one step of
   * the page's history with them, as Notion's (seen 2026-11-08); one ⌘Z
   * before it came takes it away as it comes. Waiting for them all first
   * showed nothing for seconds, then each at the page's end (2026-11-08).
   */
  function pasteDatabases(blocks: readonly PastedBlock[], afterId: string | null, emptyLine: boolean, source?: PastedBlocksMenuRequest["source"] | null) {
    if (!canWrite()) return;
    const status = getAdapter().moveContext?.onStatus, place = pastePlace(getAdapter(), afterId, emptyLine);
    let placed: (string | null)[] = blocks.map(() => null), gone = false;
    const waiting = new Map<number, string>(blocks.flatMap((block, index) => pastesServerBlocks([block]) ? [[index, ""]] : [])), indicators = newId();
    // Each one still to come holds its place after the block it will follow, till it is drawn there.
    const holdPlaces = () => requestEditorCopyIndicators(indicators, gone ? [] : [...waiting].flatMap(([index, until]) => {
      const after = pastedSpot(getAdapter(), place, placed, index).afterId;
      return after ? [{ after, ...(until ? { until } : {}) }] : [];
    }));
    const stands = (id: string | null): id is string => !!id && getAdapter().graph.blocks.some((block) => block.id === id);
    const made = () => placed.filter(stands);
    const after = () => ({ caret: false, chosen: made() });
    // The empty line pasted on goes once a block takes its place.
    const takePlace = () => { if (stands(place.replacing) && made().length) getAdapter().remove([place.replacing]); };
    const start = held();
    let step = owner?.run(() => { placed = placePastedLines(getAdapter(), place, blocks); takePlace(); }, true, start, after) ?? null;
    let chosen = made();
    if (chosen.length) choosePasted(chosen, true);
    holdPlaces();
    const arrived = (index: number, id: string | null) => {
      waiting.delete(index);
      if (!canWrite()) return;
      sync();
      const put = () => {
        const spot = pastedSpot(getAdapter(), place, placed, index), adapter = getAdapter();
        if (id) {
          owner?.adoptServerBlocks([id]);
          if (!standsAt(adapter.graph, id, spot)) adapter.move(id, spot.parentId, spot.afterId);
          placed[index] = id;
        } else placed[index] = pasteBlocksAfter(adapter, spot.afterId, [blocks[index]])[0] ?? null;
        takePlace();
      };
      const taken = step ? owner?.follow(step, put, after) : owner?.run(put, true, start, after);
      if (taken === null && step) {
        // Undone before it came: it goes, as the rest of the paste did.
        gone = true;
        holdPlaces();
        // It goes from the page, and so from the server's, once the page has it.
        const goes = (tries: number) => {
          if (stands(id)) owner?.aside(() => getAdapter().remove([id]));
          else if (tries > 0 && canWrite()) window.setTimeout(() => goes(tries - 1), 100);
        };
        if (id) goes(50);
        return;
      }
      step = taken ?? step;
      holdPlaces();
      // Still chosen as they were left, the paste's blocks are chosen with it.
      const now = options.chosenBlocks?.();
      if (!chosen.length || now?.size === chosen.length && chosen.every((one) => now.has(one))) choosePasted(chosen = made());
    };
    const spot = (index: number, id: string | null) => {
      if (id) {
        waiting.set(index, id);
        holdPlaces();
      }
      return pastedSpot(getAdapter(), place, placed, index);
    };
    // Undone before they all came, the paste starts no more of them.
    void copyPastedDatabases(getAdapter, blocks, spot, arrived, () => !gone && canWrite() && (!step || owner?.holds(step) !== false))
      .then(() => {
        // From another page they offer to sync with or link to where they came from, as other blocks do (Notion's, seen 2026-11-06);
        // cut, nothing is left there, and Notion offers nothing (seen 2026-11-07).
        if (!gone && made().length && source && !pastesCut(blocks)) dispatchPastedBlocksMenu({ blockIds: made(), source });
      }).catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === "AbortError")) status?.(blockItemText("database.duplicateFailed"), "error");
      }).finally(() => requestEditorCopyIndicators(indicators, []));
  }
  /**
   * Chosen blocks leave no caret and no chosen words showing, as Notion's
   * (seen 2026-11-04). The text loses the focus at once; its range, which
   * shows nothing out of focus, goes once the frame is painted: taken away
   * at the press, it laid out the page the paste had just changed (2026-11-15:
   * a 1,031-block page at CPU ×2). A syllable started meanwhile over the
   * chosen blocks drops it itself (useBlockSelectionKeys).
   */
  function letGoOfText() {
    editor.view.dom.blur();
    afterNextPaint(() => {
      if (editor.isDestroyed || editor.view.hasFocus()) return;
      const shown = window.getSelection();
      if (shown?.anchorNode && editor.view.dom.contains(shown.anchorNode)) shown.removeAllRanges();
    });
  }
  /** Pasted blocks are chosen with no caret, copied blocks shown (pastedBlocksShown), and a Redo chooses them again. */
  function choosePasted(ids: readonly string[], copied = false) {
    if (copied) showPastedBlocks(editor.view.dom, ids, editor.view.hasFocus()); options.chooseBlocks?.(ids);
    letGoOfText();
    owner?.noteAfter({ caret: false, chosen: ids });
  }
  // A read-only view of the same document observes text without replacing
  // the writable view's history owner. Permission regrant acquires it in sync.
  acquireHistory();
  const typedOver = typingOverLines<NonNullable<ReturnType<ReturnType<typeof createNativeEditorUndoOwner<Held>>["run"]>>>();
  const binding = document ? createNativeTiptapBinding({
    document, canWrite: canInput, deferProjection: suggestions.deferProjection,
    changeStructure(before, after) {
      getAdapter().changeLayout(graph => nativeTiptapGraphChange(graph, before, after, rootId));
    },
    captureLocal(before, after, change, transaction) {
      if (suggestions.active()) return;
      const structural = nativeTiptapStructureChanged(projectedBlocks(before.doc), projectedBlocks(after.doc))
        || !!transaction.getMeta("uiEvent") || !!transaction.getMeta("nativeInputRule") || transaction.steps.some(step => !(step instanceof ReplaceStep));
      const activeField = (state: typeof before) => nativeTiptapTextFields(projectedBlocks(state.doc)).find(field =>
        field.field !== "text" && state.selection.from >= field.contentFrom && state.selection.to <= field.contentFrom + field.node.content.size);
      const typing = transaction.steps.length > 0 && transaction.steps.every(step => {
        if (!(step instanceof ReplaceStep)) return false;
        let textOnly = true;
        step.slice.content.descendants(node => { if (!node.isText) textOnly = false; });
        return textOnly;
      });
      const startState = typedUndoStart(before, transaction, typing);
      const first = activeField(startState), last = activeField(after);
      const capture = typing && !transaction.getMeta("uiEvent") && first && last
        && first.block.id === last.block.id && first.field === last.field ? {
          key: `${first.block.nativeId}/${first.field}`,
          from: startState.selection.from - first.contentFrom, to: after.selection.to - last.contentFrom, time: transaction.time,
        } : undefined;
      const start = held(startState), joined = typedOver.joins(transaction, typing);
      let ran = false;
      const once = () => { ran = true; change(); };
      if (joined) owner?.follow(joined, once, () => heldAfter(start, after));
      if (!ran) typedOver.made(owner?.run(once, structural, start, () => heldAfter(start, after), capture), before, transaction);
    },
    undo: historyStep,
  }) : null;
  /** ⌘Z or ⌘⇧Z in the page: whether it took a step (or one waits its turn). */
  function historyStep(forward: boolean, apply?: boolean) {
    if (suggestions.active() || !isCurrent()) return false;
    const titleLines = apply === false ? [] : linesTitleMade(initial.pageId, getAdapter().graph);
    if (apply !== false) keepFreshDatabasesThroughStep();
    databaseRedo.stepping(forward && apply !== false);
    if (owner?.undo(forward, apply)) {
      // The line a title's Enter made going, the caret goes back to the title; coming back, to it, as Notion's (seen 2026-11-05); in a body kept unseen, it stays in the title (pageBodyFrame).
      const stands = (id: string) => getAdapter().graph.blocks.some(block => block.id === id);
      if (!forward && titleLines.some(id => !stands(id))) {
        const title = pageTitleJoin(initial.pageId);
        if (title?.caretAtEnd) title.caretAtEnd(); else if (!bodyUnseen(editor.view.dom)) rowTitleCaretAtEnd(initial.pageId);
      }
      const back = forward && apply !== false ? linesTitleMade(initial.pageId, getAdapter().graph).find(id => !titleLines.includes(id)) : undefined;
      if (back && !bodyUnseen(editor.view.dom)) focusEditableNextFrame(back, "start");
      return true;
    }
    // With the body's Undos used up, the next undoes what was typed in the
    // page's title, its caret going there, as Notion's (seen 2026-11-04).
    return !forward && apply !== false && (!!pageTitleJoin(initial.pageId)?.undoTyped?.() || rowTitleUndo(initial.pageId));
  }
  const access = Extension.create({
    name: "nativeEditorAccess",
    addProseMirrorPlugins: () => [new Plugin({
      filterTransaction: tr => !tr.docChanged || tr.getMeta(nativeProjectionMeta) || canInput(),
      props: { handleDOMEvents: {
        // Native block/file drops are owned by the enclosing product drop
        // layer, and a sidebar page is not text. Consume PM's default
        // UUID/plain-text insertion while leaving the event available to
        // that native graph/history owner.
        drop: (_view, event) => !!event.dataTransfer && (isEditorBlockDrag(event.dataTransfer)
          || event.dataTransfer.types.includes(PAGE_DRAG_TYPE)
          || externalDragHasFiles(Array.from(event.dataTransfer.types), FILE_DROP_BOUNDS)),
        // A native drag source (a block handle, a tab) belongs to its drop
        // targets; the engine must not start a text drag of its own from it.
        dragstart: (_view, event) => event.target instanceof Element && !!event.target.closest("[data-native-drag-source]"),
        // PM skips editing keymaps when editable=false. Consume browser Undo
        // here so a readonly selection cannot undo another editable DOM.
        keydown: (_view, event) => {
          if (canInput() || !primaryModifier(event) || !["z", "y"].includes(chordLetter(event))) return false;
          event.preventDefault();
          return true;
        },
      } },
    })],
  });
  const editor = new Editor({
    // Tiptap's core keys come from NativeTiptapEngineKeymap, with the Mac's
    // meanings, first as Tiptap's own come; copied words' text for other apps
    // from NativeTiptapCopiedWords.
    enableCoreExtensions: { keymap: false, clipboardTextSerializer: false },
    extensions: [NativeTiptapEngineKeymap, TiptapReadOnlyMark, ...nativeTiptapExtensions(options.nodeViews, options.linkHref), NativeTiptapKeys, NativeTiptapWidgetSelection, AppleTouchPlainText, CompositionEndKey, NativeTiptapDomRead, NativeTiptapCaretScroll, NativeTiptapCodeColors, NativeTiptapCaretLine, NativeTiptapChosenAtoms, NativeTiptapCopyIndicator, NativeTiptapCommentPending, NativeTiptapLinkTarget, NativeTiptapEquationDraft, NativeTiptapQuoteHolder, NativeTiptapBlockRuns, NativeTiptapFieldKeys, NativeTiptapSyncedCopyBackspace.configure({ adapter: getAdapter, rootId }), NativeTiptapStructureKeys, NativeTiptapMergeKeys.configure({ pageId: () => initial.pageId, undo: () => { owner?.undo(false); },
        removeWith: (blockId, change) => heldRun(() => { change(); getAdapter().remove([blockId]); }), withNextUndo: step => owner?.withNext(step) }),
      NativeTiptapInputRules.configure({ enabled: () => !suggestions.active() }),
      NativeTiptapClipboard.configure({ ...(options.onPasteRefused ? { onRefused: options.onPasteRefused } : {}), onBlocksPasted: choosePasted, onDatabasesPasted: pasteDatabases }),
      NativeTiptapCopiedWords.configure({ linesCopy: lines => copiedLines(getAdapter(), lines) }), access, suggestions.extension, nativeTiptapProductKeys({ adapter: getAdapter, selectBlocks: options.selectBlocks, chooseBlocks: options.chooseBlocks, choosePasted, run: heldRun, follow: (change, step) => canWrite() ? owner?.followOrRun(change, step) ?? null : null, rootId }),
      ...(binding ? [binding.extension] : []), ...(options.extensions ?? [])],
    content: "", editable: canInput(), injectCSS: false, enableInputRules: ["nativeTypedRules"],
    editorProps: {
      attributes: { role: "textbox", "aria-multiline": "true", "data-native-editor-content": "true", tabindex: "0" },
      // The caret keeps clear of the page's top and bottom edges, as Notion's:
      // within about 35px of one it scrolls to about 74px from it, its line
      // then 64px clear (seen 2026-11-06, clicked, arrowed to or typed in),
      // so a menu opened there ("/", "@") has the room Notion's has.
      scrollThreshold: { top: CARET_SCROLL_THRESHOLD, bottom: CARET_SCROLL_THRESHOLD, left: 0, right: 0 },
      scrollMargin: { top: CARET_SCROLL_MARGIN, bottom: CARET_SCROLL_MARGIN, left: 5, right: 5 },
      handleDOMEvents: {
        // Composing over chosen lines is one Undo with the lines it takes out (typingOverLines).
        compositionstart: (view) => { typedOver.start(view); return false; },
        // A letter typed over chosen code, or over more than one line, goes
        // in through the engine (nativeTiptapDomRead's typesOverLines).
        // A press from a field in the page into its text, kept for its mouseup.
        mousedown: (view, event) => {
          const active = view.dom.ownerDocument.activeElement;
          pressFromField = active instanceof HTMLElement && active !== view.dom && view.dom.contains(active)
            && active.matches("input, textarea, select") && !(event.target instanceof Element && active.contains(event.target))
            ? { left: event.clientX, top: event.clientY } : null;
          return false;
        },
        // A click places the caret without scrolling it into view; it then
        // keeps its distance too, measured where the browser put it (the
        // engine reads that a moment later).
        mouseup: (view, event) => {
          const press = pressFromField;
          pressFromField = null;
          // A click in a part the text doesn't run through (a database's
          // table, a widget) places no caret: the page stays where it is
          // rather than rolling back to the caret left in the text (seen
          // 2026-11-07: a files cell's menu far down a page sent it to the top).
          const island = event.target instanceof Element ? event.target.closest('[contenteditable="false"]') : null;
          if (island && island !== view.dom && view.dom.contains(island)) return false;
          const overtaken = press ? actedSince(view.dom.ownerDocument) : null;
          window.requestAnimationFrame(() => {
            const acted = overtaken?.() ?? false;
            if (view.isDestroyed || !view.hasFocus()) return;
            // A press from a field in the page (a database's title) into its text puts the caret
            // where pressed, as Notion's (2026-11-15, worker Chrome, no script before the press:
            // from a database's title into a line's words, the letter typed next went in where
            // pressed, in both): Chrome put it on an empty line far below, and the page
            // jumped there, the engine holding the database chosen (2026-11-06). A key or another
            // press before this frame left the caret where the person meant it, and the place
            // pressed may hold another line by now: then it stays (2026-11-09: a to-do's box
            // checked, a press on a line, ⌘← and Enter at once, and what was typed next went into
            // the empty line Enter had put where the pressed line was).
            const hit = press && !acted && view.dom.ownerDocument.getSelection()?.isCollapsed ? view.posAtCoords(press) : null;
            if (hit && view.state.doc.resolve(hit.pos).parent.inlineContent) {
              view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, hit.pos)));
            }
            keepCaretClear(view.dom);
          });
          return false;
        },
        // Aria announces virtual option focus with synthetic blur/focus while
        // the real caret remains here. Only real DOM focus loss ends this
        // editor's native lease; normal engine focus/input handling stays intact.
        blur: (view, event) => !event.isTrusted && view.dom.ownerDocument.activeElement === view.dom,
      },
    },
  });
  const invalidateSelection = () => invalidateNativeDomSelection(editor.view.root);
  editor.on("beforeTransaction", invalidateSelection); editor.on("focus", invalidateSelection); editor.on("blur", invalidateSelection);
  let previousGraph = initial.graph;
  let previousSuggesting = suggestions.active();
  project(nativeTiptapBlocks(previousGraph, document, editor.schema, rootId, initial.pageId, options.onlyBlockId, projected));

  function heldRun(change: () => void) {
    const start = held();
    owner?.run(change, true, start, () => { sync(); return heldAfter(start); });
  }

  function project(blocks: readonly Node[]) {
    if (binding) { binding.projectStructure(blocks); return; }
    const tr = reconcileNativeTiptapProjection(editor.state.tr, blocks)
      .setMeta(nativeProjectionMeta, true).setMeta("addToHistory", false);
    if (tr.docChanged) editor.view.dispatch(tr);
  }

  function sync() {
    if (!isCurrent()) return;
    const adapter = getAdapter();
    const modeChanged = previousSuggesting !== suggestions.active();
    previousSuggesting = suggestions.active();
    acquireHistory();
    owner?.observeAdapter();
    suggestions.syncMode();
    if (!canInput()) binding?.cancelComposition();
    if (editor.isEditable !== canInput()) editor.setEditable(canInput());
    if (!suggestions.active()) binding?.flushDeferred();
    if (previousGraph === adapter.graph && !modeChanged) return;
    previousGraph = adapter.graph;
    project(nativeTiptapBlocks(previousGraph, document, editor.schema, rootId, initial.pageId, options.onlyBlockId, projected));
  }

  return {
    editor,
    sync,
    suggestFormat: suggestions.format,
    suggestMention: suggestions.mention, suggestSlash: suggestions.slash, suggestRemoval: suggestions.remove, suggestLineBelow: suggestions.lineBelow,
    /** Menus/layout commands enter the same causal native history as typing. */
    run(change: () => void) {
      if (canWrite()) heldRun(change);
    },
    choosePasted, follow: (change: () => void, step: unknown) => canWrite() ? owner?.followOrRun(change, step) ?? null : null,
    pasteDatabases,
    /** Blocks the server made here are one step of the page's history, and are chosen. */
    adoptServerBlocks(ids: readonly string[]) {
      // A ⌘Z may have taken them away as they came (copyOnServer's oneStep).
      if (!canWrite() || !ids.some((id) => getAdapter().graph.blocks.some((block) => block.id === id))) return;
      sync();
      owner?.run(() => owner?.adoptServerBlocks(ids, { caret: false, chosen: ids }), true, held(), () => ({ caret: false, chosen: ids }));
      options.chooseBlocks?.(ids);
      letGoOfText();
    },
    /** A paste the page's title hands over: the blocks it put in, null when the body can't take it yet. */
    pasteAtStart(blocks: readonly PastedBlock[]) {
      if (!canWrite()) return null;
      sync();
      try {
        const { tr, blockIds } = pasteAtDocStart(editor.state, blocks);
        editor.view.dispatch(tr);
        return blockIds;
      } catch (error) {
        if (!(error instanceof RangeError)) throw error;
        options.onPasteRefused?.(error);
        return [];
      }
    },
    /** A title's Enter in its text: the words after the caret start a new first line, the caret at its start, one Undo with the title's change (pageBodyStart). */
    startFromTitle(text: string, title: TitleSplit) {
      if (!canWrite()) return false;
      sync();
      const { schema } = editor.state, attrs = newNativeTiptapAttrs("paragraph");
      const tr = editor.state.tr.insert(0, schema.nodes.nativeBlock.create(attrs,
        schema.nodes.nativeText.create(null, text ? schema.text(text) : null)));
      // Undone, the caret goes back where the title was split; done again, to
      // the line's start (Notion's, measured 2026-11-15 in Chrome at 1440 with
      // no script in the page: after Enter inside the title, ⌘Z and ⌘⇧Z, a
      // typed "z" went before the new line's first letter).
      owner?.withNext((forward) => {
        const shown = pageTitleJoin(initial.pageId);
        if (shown?.title === (forward ? title.before : title.after)) shown.set(forward ? title.after : title.before, forward ? undefined : title.after.length);
        if (forward) focusEditableNextFrame(attrs.id, "start");
      });
      editor.view.dispatch(tr.setSelection(TextSelection.create(tr.doc, 2)).scrollIntoView());
      editor.view.focus();
      return true;
    },
    dispose() { disposed = true; suggestions.dispose(); owner?.dispose(); editor.destroy(); },
  };
}
