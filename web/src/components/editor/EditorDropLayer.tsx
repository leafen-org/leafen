import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { dropTargetByPointer, monitorByPointer, underDrag, type ElementDropTargetEventBasePayload } from "@/lib/pointerDrag";
import { getFiles } from "@atlaskit/pragmatic-drag-and-drop/external/file";
import { autoScrollWindowForElements } from "@atlaskit/pragmatic-drag-and-drop-auto-scroll/element";
import { autoScrollForExternal, autoScrollWindowForExternal } from "@atlaskit/pragmatic-drag-and-drop-auto-scroll/external";
import { dropTargetForFiles } from "@/lib/fileDropTarget";
import { edgeAutoScroll } from "@/lib/edgeAutoScroll";
import { dragCopies } from "@/lib/dragCopyKey";
import { useOptionalProductToast } from "@/lib/productToast";
import { blockItemLabels, blockItemText } from "./blockItemLabels";
import { blockLine, placeDroppedBlocks, type BlockDrop } from "./blockDropModel";
import { dropTargetAt, fileDropMode, holderStyle, indicatorStyle, type DropTarget } from "./editorDropTarget";
import { dropOnFileBlock, takesDroppedFile, useEditorFileInsertion, type FileInsertionHistory } from "./mediaBlockUpload";
import { droppedFiles, externalDragHasFiles, FILE_DROP_BOUNDS } from "./fileDrop";
import { editorBlockDragPayload } from "./editorDragModel";
import { editorBlockChildren } from "./editorGraphModel";
import { holdingTyping, OPENED_PAGE_FRAME, PAGE_BODY_PRESS_EVENT, pressedWhileOpening } from "@/lib/pageTitleFocus";
import { afterSoftKeyboard } from "@/lib/softKeyboard";
import { phoneScreen } from "@/lib/screenKind";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { copiesOnServer } from "./nativeEditorGraphAdapter";
import { copyBlocksTo, placeServerCopies, type EditorCopyHistory } from "./editorBlockDuplicate";
import { focusEditable, focusEditableNextFrame, holdTypingForCaret, keepTapCaret } from "./focus";
import { caretBesideLine } from "./caretBesideLine";
import { caretBesideMarginFrame, pressedTableRoom, pressTableRoom, shownEditorLines, type TableRoomPress } from "./blockFramePress";
import { pageBodyFrame } from "./pageBodyFrame";
import { pageFirstLine } from "./pageFirstLine";
import { pageEndLine } from "./pageEndLine";
import { OWN_PRESS } from "./nativeTiptapNodeViews";
import styles from "./editorStyles.generated.module.css";

type Point = { x: number; y: number };
// How far a point is beside a line, and above or below it.
const across = (rect: DOMRect, point: Point) => point.x < rect.left ? rect.left - point.x : point.x > rect.right ? point.x - rect.right : 0;
const up = (rect: DOMRect, point: Point) => point.y < rect.top ? rect.top - point.y : point.y > rect.bottom ? point.y - rect.bottom : 0;
// Text and controls keep their own pointer behaviour; a drag elsewhere selects blocks.
// A handle dragged to resize or move an edge — a table column's, its freeze
// line's, a dashboard widget's — is a control too: a table's chosen cell stays
// chosen through a column's resize, as Notion's (seen 2026-11-08), not left
// for the database block the drag would otherwise select.
const OWN_POINTER = '[data-rt-editable], [data-native-atom], [contenteditable="true"]:not([data-native-editor-content]), input, textarea, select, button, a, [role="textbox"]:not([data-native-editor-content]), [role="dialog"], [role="menu"], [role="separator"]';

// The element that scrolls the editor while files are dragged to its edge.
function scrollerOf(element: HTMLElement): HTMLElement | null {
  for (let node = element.parentElement; node; node = node.parentElement) {
    if (/(auto|scroll)/u.test(window.getComputedStyle(node).overflowY) && node.scrollHeight > node.clientHeight) return node;
  }
  return null;
}

/** The pane the page stands in, whether or not it runs long enough to scroll yet. */
function pagePaneOf(element: HTMLElement): HTMLElement | null {
  for (let node = element.parentElement; node; node = node.parentElement) {
    if (/(auto|scroll)/u.test(window.getComputedStyle(node).overflowY)) return node;
  }
  return null;
}

/**
 * The editor's blocks, which take dragged blocks and files and select the
 * blocks a pointer drags a rectangle over. Dropped blocks move, or copy when
 * Alt is held, and are chosen; a copy is one step of the page's `history`,
 * as a duplicate is (editorBlockDuplicate.ts). A click below the page's
 * blocks goes to its end.
 */
export function EditorDropLayer({
  adapter,
  history,
  selection,
  setSelection,
  children,
}: {
  adapter: NativeEditorGraphAdapter;
  history?: EditorCopyHistory & Partial<FileInsertionHistory>;
  selection: EditorSelection;
  setSelection: (selection: EditorSelection) => void;
  children: ReactNode;
}) {
  const [dropTarget, setDropTarget] = useState<DropTarget | null>(null);
  const [marquee, setMarquee] = useState<{ start: Point; end: Point } | null>(null);
  // Copies made on the server, waiting to go where they were dropped.
  const [placing, setPlacing] = useState<{ originals: string[]; copyIds: string[]; drop: BlockDrop } | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const marqueeCleanup = useRef<(() => void) | null>(null);
  useEffect(() => () => { marqueeCleanup.current?.(); }, [adapter.readOnly, adapter.generation, adapter.signal]);
  const insertFiles = useEditorFileInsertion(adapter, history, setSelection);
  const toast = useOptionalProductToast();

  function notify(message: string, tone: "default" | "success" | "error" = "default", action?: { label: string; onClick: () => void }) {
    if (toast) toast.notify(message, tone, action);
    else adapter.moveContext?.onStatus?.(message, tone);
  }

  // An editor inside one of these blocks takes what happens in it.
  function owns(target: EventTarget | null) {
    return target instanceof Element && target.closest("[data-editor-drop-layer]") === rootRef.current;
  }

  function targetBlock(target: EventTarget | null) {
    return target instanceof HTMLElement ? target.closest<HTMLElement>("[data-block-id]")?.dataset.blockId ?? null : null;
  }

  // What was dropped is chosen, with no notice, as Notion's (seen 2026-11-03).
  function moveBlocks(blockIds: readonly string[], drop: BlockDrop) {
    if (!adapter.changeLayout((graph) => placeDroppedBlocks(graph, blockIds, drop))) {
      notify(blockItemLabels().cantMoveBlockHere);
      return;
    }
    setSelection(new Set(blockIds));
  }

  function copyBlocks(blockIds: readonly string[], drop: BlockDrop) {
    const labels = blockItemLabels();
    // A block is not copied into itself.
    const line = blockLine(adapter.graph, drop.id);
    if ((drop.mode === "inside" || drop.mode === "start") && blockIds.some((id) => line.has(id))) {
      notify(labels.cantCopyBlockHere);
      return;
    }
    // Pages and databases are copied on the server; their copies go where
    // they were dropped once the page has them.
    if (copiesOnServer(adapter.graph, blockIds)) {
      notify(blockItemText("database.duplicating"));
      void adapter.copyOnServer(blockIds).then((copyIds) => {
        if (copyIds.length > 0) setPlacing({ originals: [...blockIds], copyIds, drop });
        else notify(labels.cantCopyBlockHere);
      }).catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === "AbortError")) notify(blockItemText("database.duplicateFailed"), "error");
      });
      return;
    }
    const copies = copyBlocksTo(adapter, blockIds, (graph, copyIds) => placeDroppedBlocks(graph, copyIds, drop), setSelection, history);
    if (copies.length === 0) notify(labels.cantCopyBlockHere);
  }

  useEffect(() => {
    if (!placing || !placing.copyIds.every((id) => adapter.graph.blocks.some((block) => block.id === id))) return;
    setPlacing(null);
    if (!placeServerCopies(adapter, placing.originals, placing.copyIds, (graph, ids) => placeDroppedBlocks(graph, ids, placing.drop), setSelection, history)) {
      notify(blockItemLabels().cantCopyBlockHere);
    }
    // The copies are placed when the page or what waits changes, not on each render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [adapter, placing]);

  // The drags over this layer: blocks dragged in the page, by the pointer (pointerDrag) or the browser, and files from outside;
  // blocks held by the pane's top or foot scroll it as Notion's (lib/edgeAutoScroll.ts). The layer keeps where blocks
  // land and what a drop does. An editor inside one of these blocks takes what is dropped over it.
  const latest = useRef({ adapter, moveBlocks, copyBlocks, insertFiles });
  latest.current = { adapter, moveBlocks, copyBlocks, insertFiles };
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return undefined;
    const innermost = ({ location }: Pick<ElementDropTargetEventBasePayload, "location">) => location.current.dropTargets[0]?.element === root;
    const pointed = ({ location }: Pick<ElementDropTargetEventBasePayload, "location">) =>
      underDrag(location.current.input.clientX, location.current.input.clientY);
    const scroller = scrollerOf(root);
    // The outermost layer scrolls the page; a nested editor's shares it.
    const scrolls = !root.parentElement?.closest("[data-editor-drop-layer]");
    // The page's pane, which takes what is dragged in the margins around the blocks.
    const pane = scrolls ? pagePaneOf(root) : null;
    const inPane = ({ location }: Pick<ElementDropTargetEventBasePayload, "location">) => pane !== null && location.current.dropTargets[0]?.element === pane;
    // Where the dragged blocks land, over the blocks or out in the page's
    // margins beside them, which for a synced block is never inside one
    // (editorDropTarget.ts).
    const blockTarget = (args: Pick<ElementDropTargetEventBasePayload, "location" | "source">) => innermost(args) || inPane(args)
      ? dropTargetAt(root, latest.current.adapter.graph, pointed(args), args.location.current.input.clientX, args.location.current.input.clientY,
        editorBlockDragPayload(args.source.data)?.blockIds ?? []) : null;
    // Dropped blocks move, or copy when Alt is held as they are let go (dragCopyKey).
    const dropBlocks = (args: Pick<ElementDropTargetEventBasePayload, "location" | "source">) => {
      setDropTarget(null);
      const { adapter: current, copyBlocks: copy, moveBlocks: move } = latest.current;
      const payload = editorBlockDragPayload(args.source.data), target = blockTarget(args);
      if (current.readOnly || !payload || !target || payload.pageId !== current.pageId || payload.generation !== current.generation) return;
      (dragCopies(args.location.current.input) ? copy : move)(payload.blockIds, target);
    };
    // Dropped files become blocks before or after the block they land on (fileDropMode), or the file of one holding a file (dropOnFileBlock).
    const fileTarget = (args: Pick<ElementDropTargetEventBasePayload, "location">): DropTarget | null => {
      const element = innermost(args) ? pointed(args)?.closest<HTMLElement>("[data-block-id]") : null;
      const id = element?.dataset.blockId, mode = element ? fileDropMode(element, args.location.current.input.clientY) : "after";
      return element && id && !takesDroppedFile(latest.current.adapter.graph.blocks.find((block) => block.id === id)) ? { id, mode, rect: element.getBoundingClientRect() } : null;
    };
    // A file let go anywhere else in the page's pane lands there too: over the
    // first block above them, at the end below them, as Notion's (seen 2026-11-05:
    // on a new page or under the last block the browser opened the file instead).
    const paneAfter = (clientY: number) => {
      const { adapter: current } = latest.current;
      const blocks = editorBlockChildren(current.graph, null);
      if (blocks.length === 0 || clientY < root.getBoundingClientRect().top) return null;
      return blocks.at(-1)?.id ?? null;
    };
    const paneTarget = (args: Pick<ElementDropTargetEventBasePayload, "location">): DropTarget | null => {
      if (!pane || args.location.current.dropTargets[0]?.element !== pane) return null;
      const id = paneAfter(args.location.current.input.clientY);
      const element = id ? root.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(id)}"]`) : null;
      return element && id ? { id, mode: "after", rect: element.getBoundingClientRect() } : null;
    };
    // While blocks are dragged the page's frames — embeds, videos, HTML —
    // take no pointer, as Notion's covers them (seen 2026-11-08): the drag
    // stays over the page, which finds where the blocks land over them.
    // They are covered in the drag's first event, before the pointer can reach one.
    let covered: { frame: HTMLIFrameElement; was: string }[] = [];
    const uncover = () => {
      for (const { frame, was } of covered) frame.style.pointerEvents = was;
      covered = [];
    };
    const cover = () => {
      if (covered.length) return;
      covered = [...root.querySelectorAll("iframe")].map((frame) => ({ frame, was: frame.style.pointerEvents }));
      for (const { frame } of covered) frame.style.pointerEvents = "none";
    };
    const unbind = combine(
      ...(!scrolls ? [] : [monitorByPointer({
        canMonitor: ({ source }) => editorBlockDragPayload(source.data) !== null,
        onGenerateDragPreview: cover,
        onDragStart: cover,
        onDrop: uncover,
      }), uncover]),
      ...(!pane ? [] : [dropTargetForFiles({
        element: pane,
        canDrop: ({ source }) => !latest.current.adapter.readOnly && externalDragHasFiles(source.types, FILE_DROP_BOUNDS),
        getDropEffect: () => "copy",
        onDragEnter: (args) => setDropTarget(paneTarget(args)),
        onDrag: (args) => setDropTarget((current) => args.location.current.dropTargets[0]?.element === pane ? paneTarget(args) : current),
        onDragLeave: (args) => { if (args.location.current.dropTargets[0]?.element !== root) setDropTarget(null); },
        onDrop: (args) => {
          if (args.location.current.dropTargets[0]?.element !== pane) return;
          setDropTarget(null);
          const files = droppedFiles(getFiles({ source: args.source }));
          if (!latest.current.adapter.readOnly && files.length > 0) latest.current.insertFiles(paneAfter(args.location.current.input.clientY), files);
        },
      })]),
      // Blocks dragged out in the page's margins land as beside the blocks
      // there: columns, or as over the block near its left edge.
      ...(!pane ? [] : [dropTargetByPointer({
        element: pane,
        canDrop: ({ source }) => !latest.current.adapter.readOnly && editorBlockDragPayload(source.data) !== null,
        getDropEffect: ({ input }) => dragCopies(input) ? "copy" : "move",
        onDropTargetChange: (args) => { if (inPane(args)) setDropTarget(blockTarget(args)); },
        onDrag: (args) => { if (inPane(args)) setDropTarget(blockTarget(args)); },
        onDragLeave: () => setDropTarget(null),
        onDrop: (args) => { if (inPane(args)) dropBlocks(args); },
      })]),
      dropTargetByPointer({
        element: root,
        canDrop: ({ source }) => !latest.current.adapter.readOnly && editorBlockDragPayload(source.data) !== null,
        getDropEffect: ({ input }) => dragCopies(input) ? "copy" : "move",
        onDragEnter: (args) => setDropTarget(blockTarget(args)),
        onDrag: (args) => setDropTarget(blockTarget(args)),
        // Out into the page's margin, the pane takes it on.
        onDragLeave: (args) => { if (!inPane(args)) setDropTarget(null); },
        onDrop: dropBlocks,
      }),
      dropTargetForFiles({
        element: root,
        canDrop: ({ source }) => !latest.current.adapter.readOnly && externalDragHasFiles(source.types, FILE_DROP_BOUNDS),
        getDropEffect: () => "copy",
        onDragEnter: (args) => setDropTarget(fileTarget(args)),
        onDrag: (args) => setDropTarget(fileTarget(args)),
        onDragLeave: () => setDropTarget(null),
        onDrop: (args) => {
          setDropTarget(null);
          const files = droppedFiles(getFiles({ source: args.source }));
          if (latest.current.adapter.readOnly || !innermost(args) || files.length === 0) return;
          if (!dropOnFileBlock(latest.current.adapter, targetBlock(pointed(args)), files)) latest.current.insertFiles(fileTarget(args), files);
        },
      }),
      ...(!scrolls ? [] : [pane ? edgeAutoScroll(pane) : autoScrollWindowForElements(),
        scroller ? autoScrollForExternal({ element: scroller }) : autoScrollWindowForExternal()]),
    );
    return unbind;
  }, []);

  // A click below the page's blocks goes to the page's end: the end of its last line where that takes it (pageEndLine:
  // an empty one, or any on a phone), a new line after its last block, or, on a page with none, its first line, made
  // outside its history (pageFirstLine).
  function focusPageEnd(finger = false) {
    if (adapter.readOnly || adapter.fixedBody) return false;
    const last = editorBlockChildren(adapter.graph, null).at(-1), line = pageEndLine(adapter.graph, block => adapter.text(block), finger && phoneScreen());
    if (line) { focusEditable(line, "end"); return true; }
    const created = last ? adapter.insertAfter(last.id, "paragraph") : pageFirstLine(adapter);
    if (!created) return false;
    // The new line has no caret until the page draws it: what is typed before then waits for it (2026-11-05: on a
    // busy page the first letters, or the whole first line, went nowhere). After a finger's tap it waits for the
    // keyboard as well, which the tap raises for what holds the typing: on a phone a caret moved before the keyboard
    // is up lets it go (2026-11-09: the new line had its caret and the tools, but no keyboard).
    holdTypingForCaret();
    if (finger) afterSoftKeyboard(() => focusEditable(created.id, "start"));
    else focusEditableNextFrame(created.id, "start");
    return true;
  }

  // A click in the page's margin beside a line puts the caret on that line —
  // at its end right of it, at its start left of it — as Notion's does
  // (2026-10-16: the caret left the page, and what was typed went nowhere or
  // to where the caret had been).
  function focusLineBeside(point: Point) {
    const nearest = shownLines().filter(({ rect }) => rect.top <= point.y && point.y <= rect.bottom)
      .sort((left, right) => across(left.rect, point) - across(right.rect, point))[0];
    return nearest ? caretBesideLine(nearest.field, point) : false;
  }

  // A press between the page's head and its first block, or beside the head,
  // puts the caret on the body's first line, as Notion's frames all do (seen
  // 2026-11-11 on a full page and in both peeks, 2026-11-12 beside a title
  // and a row's properties; measured once 2026-11-15 in Chrome 1440 on a full
  // page with no script before the press, a letter typed after): at the
  // line's end, or at its start when pressed before the column's start; past
  // a first block that has no line — a
  // divider — at the start of the first line after it.
  function focusFirstLine(point: Point) {
    if (adapter.readOnly || adapter.fixedBody) return false;
    const first = shownLines().sort((upper, lower) => upper.rect.top - lower.rect.top)[0];
    const blockId = first?.field.closest<HTMLElement>("[data-block-id]")?.dataset.blockId;
    if (!first || !blockId) return false;
    const before = getComputedStyle(first.field).direction === "rtl" ? point.x > first.rect.right : point.x < first.rect.left;
    const opening = rootRef.current?.querySelector<HTMLElement>("[data-block-id]")?.contains(first.field) ?? false;
    return focusEditable(blockId, before || !opening ? "start" : "end");
  }

  const shownLines = () => shownEditorLines(rootRef.current);

  // A press made while the page was opening takes the caret once the page's
  // lines have come: on the line drawn where it was pressed, or the nearest,
  // as a press between lines takes; below them, at the page's end. A block
  // drawn there that isn't text — a database, a picture — takes it only in a
  // field of its own there, as a database's cell; else what is typed
  // meanwhile waits for the next press.
  function focusPressed(point: Point) {
    const box = rootRef.current?.getBoundingClientRect();
    if (adapter.readOnly || !box) return;
    const at = document.elementFromPoint(point.x, point.y);
    const island = at?.closest('[contenteditable="false"]');
    if (island && owns(island) && !island.closest(".node-nativeText, .node-nativeCode")) {
      const field = at?.closest<HTMLElement>('[contenteditable="true"]');
      if (field && island.contains(field)) { caretBesideLine(field, point); return; }
      // A database's cell at rest has no field until it is pressed: the
      // press goes to it now, opening it as Notion's first press on a page
      // just opened does, and what was typed meanwhile goes into the field it
      // opens (2026-11-10: the cell stayed shut and the typing was dropped).
      const cell = at?.closest<HTMLElement>('[data-table-cell], [role="gridcell"], [data-timeline-table-cell]');
      if (at instanceof HTMLElement && cell && island.contains(cell)) pressAgain(at, point);
      return;
    }
    if (adapter.fixedBody) return;
    if (point.y > box.bottom) { focusPageEnd(); return; }
    const surface = rootRef.current?.closest<HTMLElement>('[role="region"][data-editor-page]');
    if (point.y < box.top && surface && point.y >= pageBodyFrame(surface).headBottom() && focusFirstLine(point)) return;
    const nearest = shownLines().sort((left, right) =>
      up(left.rect, point) - up(right.rect, point) || across(left.rect, point) - across(right.rect, point))[0];
    if (nearest) caretBesideLine(nearest.field, point);
    else focusPageEnd();
  }

  // The press a page still opening was given, made again where it landed.
  function pressAgain(target: HTMLElement, point: Point) {
    const at = { bubbles: true, cancelable: true, composed: true, clientX: point.x, clientY: point.y, button: 0 };
    target.dispatchEvent(new PointerEvent("pointerdown", { ...at, pointerId: 1, pointerType: "mouse", isPrimary: true, buttons: 1 }));
    target.dispatchEvent(new MouseEvent("mousedown", { ...at, buttons: 1 }));
    target.dispatchEvent(new PointerEvent("pointerup", { ...at, pointerId: 1, pointerType: "mouse", isPrimary: true }));
    target.dispatchEvent(new MouseEvent("mouseup", at));
    target.dispatchEvent(new MouseEvent("click", at));
  }

  // A drag over the blocks selects the ones it touches; a click on the space
  // between them ends the selection, one below them goes to the page's end
  // and one above them, under or beside the page's head, to the body's first
  // line.
  function startMarquee(target: HTMLElement, start: Point, pointerId: number, place: "above" | "beside" | "below" | TableRoomPress = "beside") {
    marqueeCleanup.current?.();
    const onBlock = Boolean(target.closest("[data-block-id]"));
    let moved = false;
    const onMove = (move: PointerEvent) => {
      if (move.pointerId !== pointerId) return;
      // A finger moving past a tap's slop is the page being scrolled: it ends
      // there, choosing no block and putting no caret down, as Notion's phone
      // (2026-11-07: a swipe up from under the body chose the inline database
      // above it before the browser took the pan, or with no page left to pan).
      const touch = move.pointerType === "touch", slop = touch ? 10 : 4;
      if (!moved && Math.abs(move.clientX - start.x) < slop && Math.abs(move.clientY - start.y) < slop) return;
      if (touch) { cleanup(); return; }
      if (!adapter.isCurrent() || adapter.signal.aborted || adapter.readOnly) { cleanup(); setMarquee(null); return; }
      moved = true;
      move.preventDefault();
      // A block selection keeps keyboard ownership through the pointed root,
      // while the engine's focused contenteditable must stop consuming copy,
      // cut and typing as a text selection.
      const active = document.activeElement;
      if (active instanceof HTMLElement && active.hasAttribute("data-native-editor-content")) active.blur();
      rootRef.current?.closest<HTMLElement>("[data-editor-root]")?.focus({ preventScroll: true });
      window.getSelection()?.removeAllRanges();
      const end = { x: move.clientX, y: move.clientY };
      setMarquee({ start, end });
      setSelection(blocksIn(start, end));
    };
    const onUp = (up: PointerEvent) => {
      if (up.pointerId !== pointerId) return;
      cleanup();
      setMarquee(null);
      if (moved || !adapter.isCurrent() || adapter.signal.aborted) return;
      // A page only read lets go of its chosen blocks wherever it is pressed, and
      // has no caret to put down (2026-11-06: a block chosen from its handle on a
      // locked page stayed chosen until the page was left).
      if (!onBlock || typeof place === "object" || adapter.readOnly) setSelection(new Set());
      // A finger's caret put down here keeps its place through the browser's own tap (keepTapCaret); a table's room has its own (pressTableRoom).
      const placed = !adapter.readOnly && (typeof place === "object" ? pressTableRoom(place, shownLines(), adapter.pageId, start.x)
        : place === "below" ? focusPageEnd(up.pointerType === "touch") : place === "above" ? focusFirstLine(start)
        : !onBlock && (focusLineBeside(start) || (up.pointerType !== "touch" && caretBesideMarginFrame(rootRef.current, start, adapter.pageId))));
      if (placed && up.pointerType !== "mouse") keepTapCaret();
    };
    const onCancel = () => { cleanup(); setMarquee(null); };
    const cleanup = () => {
      window.removeEventListener("pointermove", onMove); window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      if (marqueeCleanup.current === cleanup) marqueeCleanup.current = null;
    };
    marqueeCleanup.current = cleanup;
    window.addEventListener("pointermove", onMove); window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
  }

  // A press in an inline database is the database's — its cards and rows
  // dragged, its cells chosen — and starts no drag over the page's blocks, as
  // Notion's (seen 2026-11-12: a card dragged on a board, or a drag from a
  // gallery's empty room out past it, left the database block unchosen;
  // 2026-11-11: the card's first move, before the browser's drag began, chose
  // the whole database block). It lets go of the page's chosen blocks, as
  // Notion's (seen 2026-11-12: a row's "열기", a cell, the title or the search
  // did; a to-do's box or a toggle's arrow keeps them, here too).
  function onPointerDown(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || !owns(event.target)) return;
    const target = event.target as HTMLElement;
    if (target.closest("[data-inline-database-wrapper]")) {
      if (selection.size) setSelection(new Set());
      roomPressTakesNoFocus(target, adapter.fixedBody ? null : caretKeptBy(target), !adapter.fixedBody && !target.closest("[data-view-type]"));
    } else if (!target.closest(OWN_POINTER)) {
      // A mouse's press in a simple table's room off its grid is the page's, not the table's: its mousedown reaches neither the engine nor the block (blockFramePress).
      const room = !adapter.readOnly && !adapter.fixedBody && event.pointerType !== "touch" ? pressedTableRoom(target, event.clientY) : null;
      if (room) event.preventDefault();
      startMarquee(target, { x: event.clientX, y: event.clientY }, event.pointerId, room ?? "beside");
    }
  }

  // A database's room is never one of the page's blocks: a press in it that
  // nothing in it takes — below or beside the table, its head row's empty
  // end — leaves the focus where Notion's does. A full-page database's leaves
  // it nowhere (seen 2026-11-14: the page kept no focus); an inline one's
  // view leaves the page's caret where it was, typing going on there, and its
  // title line or filter bar leaves no caret (seen 2026-11-14). Its block took
  // the focus here, the page's selection on it: Enter there made a row, and
  // what was typed went nowhere until a key ringed the block. A cell being
  // written still ends and stays chosen (useCellChosenAfterPressAway).
  // The page's caret a press in an inline database's view leaves where it
  // was: the page's own field holding the focus and its place as the press began.
  function caretKeptBy(pressed: HTMLElement) {
    const field = document.activeElement;
    // The selection is read last, as reading it lays the page out: a control's press keeps no caret (roomPressTakesNoFocus) and read it inside a table's "+ 새 페이지" (~28 ms at CPU ×2, 2026-11-15).
    if (pressed.closest(OWN_PRESS) || !pressed.closest("[data-view-type]") || !(field instanceof HTMLElement)
      || !field.hasAttribute("data-native-editor-content") || !field.contains(pressed) || !window.getSelection()?.rangeCount) return null;
    return { field, range: window.getSelection()!.getRangeAt(0).cloneRange() };
  }

  function roomPressTakesNoFocus(pressed: HTMLElement, keep: { field: HTMLElement; range: Range } | null, chrome: boolean) {
    const block = pressed.closest<HTMLElement>(".node-nativeWidget");
    if (!block || pressed.closest(OWN_PRESS)) return;
    // The focus moves with the press's mousedown, after a finger's lift: the
    // press is over with its click, or a moment on when it was a drag.
    const taken = (focus: FocusEvent) => {
      if (focus.target !== block) return;
      if (!keep) { block.blur(); return; }
      keep.field.focus({ preventScroll: true });
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(keep.range);
    };
    const done = (event?: Event) => {
      window.clearTimeout(timer);
      document.removeEventListener("focusin", taken, true);
      window.removeEventListener("click", done, true);
      // Its title line, view tabs or filter bar keep none of the page's
      // caret either, the press taking no focus there.
      const field = document.activeElement;
      if (event && chrome && field instanceof HTMLElement && field.hasAttribute("data-native-editor-content") && field.contains(pressed)) field.blur();
    };
    const timer = window.setTimeout(done, 1000);
    document.addEventListener("focusin", taken, true);
    window.addEventListener("click", done, true);
  }

  // The page's editor also starts one from the space around and below its blocks.
  const startMarqueeRef = useRef(startMarquee);
  startMarqueeRef.current = startMarquee;
  const focusPressedRef = useRef(focusPressed);
  focusPressedRef.current = focusPressed;
  // In layout, so a body ready in this frame takes what was pressed for it
  // when it says so (pageBodyReady).
  useLayoutEffect(() => {
    const layer = rootRef.current;
    const surface = layer?.parentElement?.closest("[data-editor-drop-layer]") ? null : layer?.closest<HTMLElement>('[role="region"][data-editor-page]');
    if (!surface) return;
    // And from the empty space of the frame the body stands in, out to its
    // scroller (pageBodyFrame): beside its blocks (2026-10-16: a click there
    // reached nothing), under them, and above them — between the page's head
    // and its first block (2026-11-11: a peek's or a page's focus went to its
    // frame, and what was typed next went nowhere) and beside the head, its
    // title, properties and discussion (2026-11-12: the caret went nowhere,
    // where Notion's goes to the first line with a peek open or not) — a full
    // page's, a peek's and a row page's alike. An empty body's room that
    // starts its writing itself (`data-writes-from-room`, PagePeek and
    // DatabaseRowPage) keeps its press.
    const { margins, shown } = pageBodyFrame(surface);
    function onSurfacePointerDown(event: PointerEvent) {
      // This page is being left for one being made, which takes what is typed;
      // or it is still opening, and the press is the opened page's; or it is unseen.
      if (holdingTyping() || pressedWhileOpening(event) || !shown()) return;
      const target = event.target as HTMLElement;
      const margin = margins.includes(target);
      if (event.button !== 0 || target.hasAttribute("data-writes-from-room")
        || (target !== surface && !margin && !target.closest("[data-editor-tail]"))) return;
      const box = layer?.getBoundingClientRect();
      if (!box) return;
      const place = event.clientY > box.bottom ? "below" : event.clientY >= box.top ? "beside" : "above";
      startMarqueeRef.current(target, { x: event.clientX, y: event.clientY }, event.pointerId, place);
    }
    const listened = [surface, ...margins];
    for (const element of listened) element.addEventListener("pointerdown", onSurfacePointerDown);
    // The page in the frame pressed takes it once its body has been read: the
    // main's, or a row's page opened full, which stands in a frame of its own.
    const toPressed = (event: Event) => {
      if (layer?.closest(OPENED_PAGE_FRAME) && !layer.closest('[aria-busy="true"]') && shown()) focusPressedRef.current((event as CustomEvent<Point>).detail);
    };
    // A page only read has no caret to put down.
    if (!adapter.readOnly) window.addEventListener(PAGE_BODY_PRESS_EVENT, toPressed);
    return () => {
      for (const element of listened) element.removeEventListener("pointerdown", onSurfacePointerDown);
      window.removeEventListener(PAGE_BODY_PRESS_EVENT, toPressed);
    };
  }, [adapter.readOnly]);

  function blocksIn(start: Point, end: Point) {
    const left = Math.min(start.x, end.x);
    const right = Math.max(start.x, end.x);
    const top = Math.min(start.y, end.y);
    const bottom = Math.max(start.y, end.y);
    return new Set([...rootRef.current?.querySelectorAll<HTMLElement>("[data-block-id]") ?? []]
      .filter((element) => {
        // A full-page database's database is its page, which is never selected.
        if (!owns(element) || (adapter.fixedBody && !adapter.graph.blocks.find((block) => block.id === element.dataset.blockId)?.parentId)) {
          return false;
        }
        const rect = element.getBoundingClientRect();
        return rect.left < right && rect.right > left && rect.top < bottom && rect.bottom > top;
      })
      .map((element) => element.dataset.blockId)
      .filter((id): id is string => Boolean(id)));
  }

  return (
    <div ref={rootRef} data-editor-drop-layer data-selection-size={selection.size}
      onPointerDown={onPointerDown}>
      {children}
      {dropTarget?.parent ? <div aria-hidden="true" data-drop-holder style={holderStyle(dropTarget.parent)} /> : null}
      {dropTarget ? <div className={styles.editorFileDropIndicator} data-drop-target={dropTarget.id}
        data-drop-mode={dropTarget.mode} style={indicatorStyle(dropTarget)} /> : null}
      {marquee ? <div className={styles.selectionMarquee} aria-hidden="true" style={{
        position: "fixed",
        left: Math.min(marquee.start.x, marquee.end.x),
        top: Math.min(marquee.start.y, marquee.end.y),
        width: Math.abs(marquee.end.x - marquee.start.x),
        height: Math.abs(marquee.end.y - marquee.start.y),
      }} /> : null}
    </div>
  );
}
