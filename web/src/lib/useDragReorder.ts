import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { draggable, dropTargetForElements, monitorForElements } from "@atlaskit/pragmatic-drag-and-drop/element/adapter";
import { disableNativeDragPreview } from "@atlaskit/pragmatic-drag-and-drop/element/disable-native-drag-preview";
import { attachClosestEdge, extractClosestEdge } from "@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge";
import { startLineReorder } from "./dragReorderLine";
import { sidePastOwnPlace } from "./dragReorderOwnPlace";
export { rowAfterInRun } from "./dragReorderOwnPlace";
export { chosenCarried } from "./dragReorderCarry";
import { landingAmongCards, landingBesideRows } from "./dragReorderReach";
import { startLiveReorder, type LiveReorder } from "./dragReorderLive";
import { listenPointerReorder, POINTER_REORDER_END } from "./dragReorderPointer";
import { underDrag } from "./pageHoverShield";
import { useKeyedRefs } from "./useKeyedRefs";
import { carryChosen } from "./carriedChoice";

type Side = "before" | "after";
export type DragReorder = ReturnType<typeof useDragReorder>;
const sideOf = (data: Record<string | symbol, unknown>): Side => {
  const edge = extractClosestEdge(data);
  return edge === "bottom" || edge === "right" ? "after" : "before";
};

/**
 * Drag handles that move rows of a list: a row shows a line on the side the
 * dragged row would land, a see-through copy of the row going with the
 * pointer (dragReorderLine), and Alt+↑↓ on a handle moves its row by one. A
 * row may carry others along (`carry`: a view's chosen rows when it is one of
 * them), which go with it as they are drawn and land together, as Notion's
 * views' rows do (seen 2026-11-24 in Chrome at 1685, dpr 1: a table's, a
 * list's, a gallery's, a board's and a timeline's): nothing lands on a row
 * carried, the row right after any of them takes them after itself, and
 * they wear no chosen look while they go (carriedChoice.ts). A
 * `live` list (a menu's list, a page's properties, a table's column heads,
 * as Notion's) instead carries the dragged row itself with the pointer and
 * slides the rows it passes out of its way (dragReorderLive). A mouse's or a
 * pen's drag is followed by its own pointer events (dragReorderPointer), as
 * Notion's are: the browser's own drop was lost at times in WebKit, the
 * desktop app's window. A row without a handle (a tab, a column) is dragged
 * by itself, along `axis`, and a `grip` drags its row too. Pragmatic drag
 * and drop owns a finger's drags, the browser's: their lifetime, drop
 * targets and closest edge; the list keeps which rows may land where and
 * what a move does.
 */
export function useDragReorder(kind: string, move: (sourceId: string, targetId: string, side: Side, carried: readonly string[]) => void,
  { axis = "vertical", live = false, gaps = false, band = false, next, carry, onDragStart, onMove }: {
    axis?: "vertical" | "horizontal";
    /** The rows move live under the drag, as Notion's menu lists and table heads. */
    live?: boolean;
    /** Rows laid out apart in lines (a gallery's cards): a mouse's drag lands
     * beside them too, in a line of them (landingAmongCards); a finger's, over
     * the gap beside a row inside their list, keeps the side of the row the
     * pointer left by, as Notion's gallery shows it between two cards (seen 2026-11-15). */
    gaps?: boolean;
    /** A database view's rows (a table's, a list's): a mouse's drag lands
     * beside them too, by the pointer's height alone (landingBesideRows). */
    band?: boolean;
    /** A view's rows: the row right after the dragged one in its run, which
     * takes it after itself whichever half it is dropped on (sidePastOwnPlace). */
    next?: (sourceId: string) => string | undefined;
    /** The rows a drag of one carries, itself among them, in the list's order; it alone where not given. */
    carry?: (sourceId: string) => readonly string[];
    onDragStart?: (id: string) => void;
    /** A line list's row carried by the pointer, from where it was pressed (a timeline's bar goes with its table row). */
    onMove?: (id: string, input: { clientX: number; clientY: number }, start: { clientX: number; clientY: number }) => void;
  } = {}) {
  const [dragging, setDragging] = useState<string | null>(null);
  const [over, setOver] = useState<{ id: string; side: Side } | null>(null);
  // Bindings live as long as their row element; they read the list's latest
  // rules here rather than rebinding on each render.
  const latest = useRef({ move, next, carry, onDragStart, onMove, live, gaps, band, axis, accepts: new Map<string, (sourceId: string) => boolean>(), enabled: new Map<string, boolean>() });
  latest.current.move = move; latest.current.onDragStart = onDragStart; latest.current.onMove = onMove; latest.current.live = live; latest.current.gaps = gaps;
  latest.current.band = band; latest.current.axis = axis; latest.current.next = next; latest.current.carry = carry;
  // The rows a drag carries and the rows right after them, asked once a drag (its run doesn't change under it).
  const following = useRef<{ source: string; ids: readonly string[]; carried: ReadonlySet<string>; next: ReadonlySet<string> } | null>(null);
  const carriedBy = (source: string) => {
    if (following.current?.source === source) return following.current;
    const asked = latest.current.live ? [] : latest.current.carry?.(source) ?? [];
    const ids = asked.includes(source) ? [...new Set(asked)] : [source];
    const carried = new Set(ids);
    const next = new Set(ids.flatMap((id) => { const after = latest.current.next?.(id); return after && !carried.has(after) ? [after] : []; }));
    return following.current = { source, ids, carried, next };
  };
  // The side of a row a dragged one lands on, past the dragged rows' own places.
  const sideFor = (source: string, target: string, side: Side) => sidePastOwnPlace(side, carriedBy(source).next.has(target));
  // The rows a list that carries rows (`carry`) is dragging, said as its drag starts and let go as it ends, wear no chosen look meanwhile.
  const showing = useRef(false);
  const showCarried = (source: string | null) => {
    if (!latest.current.carry || !source && !showing.current) return;
    showing.current = Boolean(source);
    carryChosen(source ? carriedBy(source).ids : []);
  };
  // Whether a row may take the rows `source` carries: not one of them.
  const takesFrom = (source: string, target: string) => !carriedBy(source).carried.has(target) && (latest.current.accepts.get(target)?.(source) ?? true);
  const handles = useRef(new Map<string, HTMLElement>());
  const grips = useRef(new Map<string, HTMLElement>());
  // Handles drawn outside their rows, and the rows dragged by themselves.
  const outsides = useRef(new Map<string, HTMLElement>());
  const selves = useRef(new Set<string>());
  const rows = useRef(new Map<string, HTMLElement>());
  const moving = useRef<LiveReorder | null>(null);
  // Where the drag last showed its row landing (a line's list), whether a row
  // took the drop, and whether the browser dropped it on one of the rows.
  const drop = useRef<{ active: boolean; shown: { id: string; side: Side } | null; taken: boolean; dropped: boolean }>(
    { active: false, shown: null, taken: false, dropped: false });

  const rowRef = useKeyedRefs<string>((id, element) => {
    rows.current.set(id, element);
    const show = (source: string, data: Record<string | symbol, unknown>) => latest.current.live ? undefined : setOver((current) => {
      const side = sideFor(source, id, sideOf(data));
      drop.current.shown = { id, side };
      return current?.id === id && current.side === side ? current : { id, side };
    });
    const registered = combine(
      () => { if (rows.current.get(id) === element) rows.current.delete(id); },
      draggable({
        element, dragHandle: handles.current.get(id),
        canDrag: () => latest.current.enabled.get(id) === true,
        getInitialData: () => ({ kind, id }),
        // A live list's row is its own preview, carried in the list.
        onGenerateDragPreview: ({ nativeSetDragImage }) => { if (latest.current.live) disableNativeDragPreview({ nativeSetDragImage }); },
        onDragStart: ({ location }) => {
          following.current = null; carriedBy(id);
          setDragging(id); latest.current.onDragStart?.(id);
          if (!latest.current.live) return;
          const { accepts } = latest.current;
          moving.current = startLiveReorder(id, [...rows.current].filter(([rowId]) => rowId === id || (accepts.get(rowId)?.(id) ?? true))
            .map(([rowId, row]) => ({ id: rowId, element: row })), location.initial.input, axis);
        },
      }),
      dropTargetForElements({
        element,
        // A live list's dragged row stays under the pointer, so it takes the drop itself.
        canDrop: ({ source }) => source.data.kind === kind && (source.data.id === id ? latest.current.live
          : takesFrom(String(source.data.id), id)),
        getData: ({ input, element: target }) => attachClosestEdge({ kind, id }, { input, element: target,
          allowedEdges: axis === "vertical" ? ["top", "bottom"] : ["left", "right"] }),
        // Kept, with the side it was left by, while the pointer is still inside the rows' list.
        getIsSticky: ({ input, element: target }) => {
          const list = latest.current.gaps ? target.parentElement?.getBoundingClientRect() : undefined;
          return !!list && input.clientX >= list.left && input.clientX <= list.right && input.clientY >= list.top && input.clientY <= list.bottom;
        },
        onDragEnter: ({ source, self }) => show(String(source.data.id), self.data),
        onDrag: ({ source, self }) => show(String(source.data.id), self.data),
        onDragLeave: () => setOver((current) => current?.id === id ? null : current),
        onDrop: ({ source, self }) => {
          setOver(null);
          drop.current.taken = true;
          const sourceId = String(source.data.id);
          if (!latest.current.live) return latest.current.move(sourceId, id, sideFor(sourceId, id, sideOf(self.data)), carriedBy(sourceId).ids);
          const landing = moving.current?.landing();
          if (landing) latest.current.move(sourceId, landing.target, landing.side, [sourceId]);
        },
      }),
    );
    element.draggable = latest.current.enabled.get(id) === true;
    return registered;
  });

  // A drag ends here even when its row is no longer drawn (a long list
  // windows its rows while the page scrolls under the drag). The browser
  // doesn't reconsider what is under the pointer after a drop until it moves,
  // so the row there is marked `data-drop-hover` until then, for a list to
  // show it hovered at once, as Notion's table heads do (seen 2026-11-07).
  //
  // The browser's own drop on one of the rows moves the dragged row where the
  // list showed it landing even when no row took it: in WebKit (the desktop
  // app's window) a drop let go on the rows at times reached none of them in
  // the drag library, and the list went back (seen 2026-11-12 in the desktop
  // app). A drag the browser didn't drop — let go away from the rows or given
  // up with Escape — moves nothing.
  useEffect(() => {
    const dropped = (event: DragEvent) => {
      const target = event.target;
      if (!drop.current.active || !(target instanceof Node) || ![...rows.current.values()].some((row) => row.contains(target))) return;
      drop.current.dropped = true;
      event.preventDefault();
    };
    // Bound before any drag's own listeners, so it hears the drop first.
    window.addEventListener("drop", dropped, true);
    const stop = monitorForElements({
      canMonitor: ({ source }) => source.data.kind === kind,
      onDragStart: ({ source }) => { drop.current = { active: true, shown: null, taken: false, dropped: false }; following.current = null; showCarried(String(source.data.id)); },
      onDrag: ({ location }) => moving.current?.place(location.current.input),
      onDrop: ({ source, location }) => {
        const { shown, taken, dropped: browserDropped } = drop.current, sourceId = String(source.data.id);
        const live = moving.current?.landing();
        const landing = latest.current.live ? (live ? { id: live.target, side: live.side } : null) : shown;
        if (!taken && browserDropped && landing && landing.id !== sourceId && takesFrom(sourceId, landing.id)) {
          latest.current.move(sourceId, landing.id, landing.side, carriedBy(sourceId).ids);
        }
        drop.current = { active: false, shown: null, taken: false, dropped: false };
        showCarried(null); following.current = null;
        setDragging(null); setOver(null);
        moving.current?.end(); moving.current = null;
        hoverWhereDropped(location.current.input);
      },
    });
    // A live list's rows under a mouse or a pen.
    const stopPointer = listenPointerReorder(() => ({
      axis: latest.current.axis,
      rowPressed(target) {
        const { live } = latest.current;
        for (const [id, row] of rows.current) {
          if (latest.current.enabled.get(id) !== true) continue;
          const handle = handles.current.get(id), grip = grips.current.get(id), outside = outsides.current.get(id);
          if (handle || grip || outside) {
            if (handle?.contains(target) || grip?.contains(target) || outside?.contains(target)) return id;
            continue;
          }
          // A line list's row moves by itself only when it is its own drag source, and not from a field in it
          // (a database's rows sit in a page's editor, in a part of it that takes no typing).
          if (row.contains(target) && (live || selves.current.has(id) && !target.closest("input, textarea, select")
            && !(target instanceof HTMLElement && target.isContentEditable))) return id;
        }
        return null;
      },
      begin(source, start) {
        const row = rows.current.get(source);
        if (!row) return null;
        following.current = null;
        const { accepts } = latest.current, { ids } = carriedBy(source);
        if (latest.current.live) {
          const move = startLiveReorder(source, [...rows.current].filter(([id]) => id === source || (accepts.get(id)?.(source) ?? true))
            .map(([id, element]) => ({ id, element })), start, latest.current.axis);
          return move ? { move, row } : null;
        }
        const others = ids.flatMap((id) => { const other = id === source ? undefined : rows.current.get(id); return other ? [other] : []; });
        return { row, move: startLineReorder({ row, others, start, hit: (input) => { latest.current.onMove?.(source, input, start); return lineLanding(source, input); },
          show: (landing) => setOver(landing ? { id: landing.target, side: landing.side } : null) }) };
      },
      started(source) { showCarried(source); setDragging(source); latest.current.onDragStart?.(source); },
      ended(source, landing, at) {
        if (landing && landing.target !== source && takesFrom(source, landing.target)) {
          latest.current.move(source, landing.target, landing.side, carriedBy(source).ids);
        }
        rows.current.get(source)?.dispatchEvent(new Event(POINTER_REORDER_END, { bubbles: true }));
        showCarried(null); following.current = null;
        setDragging(null); setOver(null);
        hoverWhereDropped(at);
      },
    }));
    return () => { window.removeEventListener("drop", dropped, true); stop(); stopPointer(); showCarried(null); };
  }, [kind]);

  // Where a line list's row dragged by the pointer lands: on the side of the
  // row under it nearer the pointer, whichever part of the row was pressed,
  // as Notion's (seen 2026-11-17); a gallery's cards and a view's rows also
  // beside them (`gaps`, `band` — dragReorderReach). Never on a row it
  // carries, and a view's row right after one takes them after itself (`next`).
  function lineLanding(source: string, input: { clientX: number; clientY: number }) {
    const { axis, gaps, band } = latest.current;
    const takes = (landing: { target: string; side: Side } | null) => landing && landing.target !== source
      && takesFrom(source, landing.target) ? { target: landing.target, side: sideFor(source, landing.target, landing.side) } : null;
    const placed = () => [...rows.current].map(([id, element]) => ({ id, box: element.getBoundingClientRect() }));
    if (gaps) return takes(landingAmongCards(placed(), input));
    const under = underDrag(input.clientX, input.clientY);
    let found: [string, HTMLElement] | undefined;
    if (under) for (const entry of rows.current) if (entry[1].contains(under) && (!found || found[1].contains(entry[1]))) found = entry;
    if (found) {
      const box = found[1].getBoundingClientRect();
      const after = axis === "vertical" ? input.clientY > box.top + box.height / 2 : input.clientX > box.left + box.width / 2;
      return takes({ target: found[0], side: after ? "after" : "before" });
    }
    if (!band) return null;
    const handlesLeft = Math.min(...[...handles.current.values(), ...outsides.current.values()]
      .map((handle) => handle.getBoundingClientRect()).filter((box) => box.width > 0).map((box) => box.left));
    return takes(landingBesideRows(placed(), handlesLeft, input));
  }

  // The browser doesn't reconsider what is under the pointer after a drop
  // until it moves, so the row there is marked `data-drop-hover` until then.
  function hoverWhereDropped({ clientX, clientY }: { clientX: number; clientY: number }) {
    window.requestAnimationFrame(() => {
      const under = document.elementFromPoint(clientX, clientY);
      const row = [...rows.current.values()].find((element) => element.contains(under));
      if (!row) return;
      row.dataset.dropHover = "true";
      const clear = () => { delete row.dataset.dropHover; window.removeEventListener("pointermove", clear); };
      window.addEventListener("pointermove", clear);
    });
  }

  // Pragmatic marks every registered row draggable; a row that may not move
  // stays undraggable so its text still selects, as it registers and as the
  // rules change. Only a row whose rule changed is written: setting the
  // attribute to what it was still made the page work out those rows' styles
  // again, some 3ms for a grouped table's 281 column heads on each of the
  // four renders a new row brings (seen 2026-11-13).
  useLayoutEffect(() => {
    for (const [id, element] of rows.current) {
      const may = latest.current.enabled.get(id) === true;
      if (element.draggable !== may) element.draggable = may;
    }
  });

  const handleRef = useKeyedRefs<string>((id, element) => {
    handles.current.set(id, element);
    return () => { if (handles.current.get(id) === element) handles.current.delete(id); };
  });
  const gripRef = useKeyedRefs<string>((id, element) => {
    grips.current.set(id, element);
    return () => { if (grips.current.get(id) === element) grips.current.delete(id); };
  });
  // A handle drawn outside its row (a table scrolled under its box's edge,
  // TableRowHandleLayer) drags the row itself: the row is what the pointer
  // carries, as from its own handle.
  const outsideHandleRef = useKeyedRefs<string>((id, element) => {
    outsides.current.set(id, element);
    return combine(() => { if (outsides.current.get(id) === element) outsides.current.delete(id); }, draggable({
      element,
      canDrag: () => latest.current.enabled.get(id) === true,
      getInitialData: () => ({ kind, id }),
      onGenerateDragPreview: ({ nativeSetDragImage, location }) => {
        const row = rows.current.get(id);
        if (!row || !nativeSetDragImage) return;
        const box = row.getBoundingClientRect(), { clientX, clientY } = location.current.input;
        nativeSetDragImage(row, clientX - box.left, clientY - box.top);
      },
      onDragStart: () => { setDragging(id); latest.current.onDragStart?.(id); },
    }));
  });

  return {
    /** The row being dragged, if any. */
    dragging,
    /** The rows its drag carries, the dragged one among them, if any. */
    carried: dragging ? following.current?.carried ?? null : null,
    /** Where a line list's dragged row would land, if anywhere. */
    over,
    /** The row's own element ref, for a row that shares its element. */
    elementRef: (id: string) => rowRef(id),
    /** The row's drop target; `accepts` says which dragged rows may land here.
     * `draggable` makes the row itself the drag source when it has no handle. */
    row(id: string, accepts: (sourceId: string) => boolean = () => true, draggable?: boolean) {
      latest.current.accepts.set(id, accepts);
      if (draggable !== undefined) {
        latest.current.enabled.set(id, draggable);
        if (draggable) selves.current.add(id); else selves.current.delete(id);
      }
      return {
        ref: rowRef(id),
        "data-dragging": dragging === id ? (latest.current.live ? "live" : "true") : undefined,
        "data-drop-side": over?.id === id ? over.side : undefined,
        "data-native-drag-source": draggable ? "" : undefined,
      };
    },
    /** More of a live row a mouse's press drags it by besides its handle (a page's property's name, as Notion's). */
    grip: (id: string) => ({ ref: gripRef(id) }),
    /** The row's handle; `neighbours` are the rows before and after it, for
     * Alt+↑↓ (Alt+←→ along a horizontal list). `outside`, the handle is not
     * in the row's element and drags the row from where it is drawn. */
    handle(id: string, enabled: boolean, neighbours: { above?: string; below?: string }, { outside = false } = {}) {
      latest.current.enabled.set(id, enabled);
      const [back, forward] = axis === "vertical" ? ["ArrowUp", "ArrowDown"] : ["ArrowLeft", "ArrowRight"];
      return {
        ref: outside ? outsideHandleRef(id) : handleRef(id),
        "data-native-drag-source": "",
        "aria-keyshortcuts": `Alt+${back} Alt+${forward}`,
        onKeyDown(event: KeyboardEvent<HTMLElement>) {
          if (!enabled || !event.altKey || event.metaKey || event.ctrlKey) return;
          if (event.key !== back && event.key !== forward) return;
          const target = event.key === back ? neighbours.above : neighbours.below;
          event.preventDefault();
          if (target) latest.current.move(id, target, event.key === back ? "before" : "after", [id]);
        },
      };
    },
  };
}
