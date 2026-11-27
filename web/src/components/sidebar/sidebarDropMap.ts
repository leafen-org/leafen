import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import { dragCopies } from "@/lib/dragCopyKey";
import { dropTargetByPointer, underDrag } from "@/lib/pointerDrag";
import { pageTreeDropIntent, treePageDragId, type PageTreeDropIntent } from "./pageTreeModel";
import { sidebarListEdges } from "./sidebarListEdges";
import { styles } from "./sidebarStyles";

type DragData = Record<string | symbol, unknown>;

/** What a place of the sidebar that takes a page does with it: a page's row, or a section's or a teamspace's head. */
export type SidebarDropTaker = {
  accepts(data: DragData, intent: PageTreeDropIntent, copy: boolean): boolean;
  over(data: DragData, intent: PageTreeDropIntent, copy: boolean): void;
  leave(): void;
  drop(data: DragData, intent: PageTreeDropIntent, copy: boolean): void;
};

/**
 * Where a drag over the sidebar's pages lands: a page's row with where on it
 * (a head takes it to its list's top, its intent unread), and whether that is
 * where the page already is.
 */
export type SidebarDropSpot = { element: Element; intent: PageTreeDropIntent; ownPlace: boolean };

// The rows (PageTreeItem) and the heads (usePageSectionDrop) that take a page, by element.
const takers = new WeakMap<Element, () => SidebarDropTaker | null>();

/** Lets the drop map send a drag to `element`'s taker; the returned function takes it back. */
export function registerDropTaker(element: Element, taker: () => SidebarDropTaker | null) {
  takers.set(element, taker);
  return () => { if (takers.get(element) === taker) takers.delete(element); };
}

/** Whether the place a spot names takes the drag there. */
export function spotTaken(spot: SidebarDropSpot, data: DragData, copy: boolean) {
  return takers.get(spot.element)?.()?.accepts(data, spot.intent, copy) ?? false;
}

// A favorite's row dragged: among the favorites it only reorders them.
const FAVORITE_ROW = "leafen-favorite-row";
export const isFavoriteRowDrag = (data: DragData) => data[FAVORITE_ROW] === true;
export const favoriteRowData = (data: DragData) => ({ ...data, [FAVORITE_ROW]: true });

const PAGE_ROW = "[data-page-tree-item]";
const TOP_ROW = `${PAGE_ROW}[aria-level="1"]`;
// A list of pages in the sidebar: the favorites, the shared and the private pages, a teamspace (in the
// teamspaces section or as a section of its own).
const PAGE_LIST = "[data-sidebar-favorites-section], [data-sidebar-shared-section], [data-sidebar-private-section], "
  + "[data-sidebar-teamspace], [data-sidebar-teamspace-section]";
const TEAMSPACE_HEAD = "[data-sidebar-teamspace-head]";
// A list's own parts that are no page's row: its "+" rows and its "하위 페이지 없음", which stand for its end.
const LIST_ENDS = "[data-sidebar-teamspace-add-row], [data-sidebar-add-private-page], [data-sidebar-teamspace-empty]";
const classSelector = (key: string) => (styles[key] ?? "").split(" ").filter(Boolean).map((name) => `.${CSS.escape(name)}`).join("");
const within = (element: Element, key: string) => { const selector = classSelector(key); return Boolean(selector && element.closest(selector)); };

// The row before or after a row among its siblings, past an open row's pages.
function siblingRow(row: Element, step: "previousElementSibling" | "nextElementSibling"): Element | null {
  let at = row[step];
  if (at?.matches('[role="group"]')) at = at[step];
  return at?.matches(PAGE_ROW) ? at : null;
}
const pageOf = (row: Element | null) => row?.getAttribute("data-tree-page-id") ?? null;

// Before or after a row as its line shows: before the row right after the dragged page, where that page
// already is, lands inside the row instead; after the row right before it shows but lands nowhere, as Notion's
// (seen 2026-11-27: the row lit inside and the page went into it; the line, and no move and no question).
function edgeSpot(row: Element, intent: PageTreeDropIntent, dragged: string): SidebarDropSpot {
  if (intent === "before" && pageOf(siblingRow(row, "previousElementSibling")) === dragged) return { element: row, intent: "inside", ownPlace: false };
  return { element: row, intent, ownPlace: intent === "after" && pageOf(siblingRow(row, "nextElementSibling")) === dragged };
}

/**
 * Where a page dropped on a row lands, as Notion's sidebar: the row's top
 * before it, its middle inside it, its bottom after it (edgeSpot), but the
 * bottom of an open row puts it first among the pages under it — its line
 * where the first one's top is —, or inside an open row with none. A
 * favorite's row dragged keeps the halves as they are (`plain`).
 */
function rowSpot(row: Element, clientY: number, dragged: string, plain: boolean): SidebarDropSpot {
  const rect = row.getBoundingClientRect();
  const intent = pageTreeDropIntent(clientY, rect.top, rect.height);
  if (plain) return { element: row, intent, ownPlace: false };
  if (intent === "after" && row.getAttribute("aria-expanded") === "true") {
    const group = row.nextElementSibling;
    const first = group?.matches('[role="group"]') ? [...group.children].find((child) => child.matches(PAGE_ROW)) : undefined;
    return first ? edgeSpot(first, "before", dragged) : { element: row, intent: "inside", ownPlace: false };
  }
  return edgeSpot(row, intent, dragged);
}

// A list's head: its section's own drop, a teamspace's name, or else its first page's top.
function headSpot(list: Element, dragged: string): SidebarDropSpot | null {
  if (takers.has(list)) return { element: list, intent: "before", ownPlace: false };
  const head = list.querySelector(TEAMSPACE_HEAD);
  if (head && takers.has(head)) return { element: head, intent: "before", ownPlace: false };
  const first = list.querySelector(TOP_ROW);
  return first ? edgeSpot(first, "before", dragged) : null;
}

// A list's top from above its heading: its first page's top, its head where it shows none.
function topSpot(list: Element, dragged: string) {
  const first = list.querySelector(TOP_ROW);
  return first ? edgeSpot(first, "before", dragged) : headSpot(list, dragged);
}

// A list's end: after its last top page, the pages under it shown or not; its head where it shows none.
function endSpot(list: Element, dragged: string): SidebarDropSpot | null {
  const rows = list.querySelectorAll(TOP_ROW);
  return rows.length ? edgeSpot(rows[rows.length - 1], "after", dragged) : headSpot(list, dragged);
}

// The lists a block of the sidebar holds: itself, or those in it (the teamspaces section's teamspaces).
const listsIn = (block: Element) => (block.matches(PAGE_LIST) ? [block] : [...block.querySelectorAll(PAGE_LIST)]);

/**
 * The list a point off every row belongs to, among the sidebar's sections
 * (`blocks`): in a list, its end; under a list, down to the next section,
 * that list's end — under the last, past "사이드바 사용자 지정" too —; under
 * a section of something else (recents) or above every list, the next
 * list's top, as Notion's (seen 2026-11-27 in Chrome, three tabs: under a
 * teamspace's "+ 새로 추가" down to "개인 페이지" its last page's line, and
 * "팀스페이스" itself the favorites' last line, or the line under the
 * teamspace's name with recents above it; once the 8px over "개인 페이지"
 * drew the private pages' first line, seen once and not taken).
 */
function betweenLists(blocks: readonly Element[], clientY: number, dragged: string): SidebarDropSpot | null {
  const boxes = blocks.map((block) => ({ block, rect: block.getBoundingClientRect() }));
  const next = boxes.findIndex(({ rect }) => rect.top > clientY);
  const above = next < 0 ? boxes[boxes.length - 1] : boxes[next - 1];
  const below = next < 0 ? undefined : boxes[next];
  if (above && clientY < above.rect.bottom) {
    const inner = listsIn(above.block);
    if (inner.length === 1 && inner[0] === above.block) return endSpot(above.block, dragged);
    return inner.length ? betweenLists(inner, clientY, dragged) : null;
  }
  return listAfter(above?.block, below?.block, dragged);
}

// The space between two blocks of the sidebar: the upper one's end where it holds pages, else the lower one's top.
function listAfter(upper: Element | undefined, lower: Element | undefined, dragged: string) {
  const last = upper ? listsIn(upper).at(-1) : undefined;
  if (last) return endSpot(last, dragged);
  const first = lower ? listsIn(lower)[0] : undefined;
  return first ? topSpot(first, dragged) : null;
}

// The space between rows, or under an open page that has none ("하위 페이지 없음"): the row below's top,
// at any depth; under the last row, that row's end; inside the page that has none.
function gapSpot(container: Element, clientY: number, dragged: string): SidebarDropSpot | null {
  const rows = [...container.querySelectorAll(PAGE_ROW)];
  if (!rows.length) {
    const owner = container.matches('[role="group"]') ? container.previousElementSibling : null;
    return owner?.matches(PAGE_ROW) ? { element: owner, intent: "inside", ownPlace: false } : null;
  }
  const lower = rows.find((row) => row.getBoundingClientRect().top >= clientY);
  const level = [...container.children].filter((child) => child.matches(PAGE_ROW));
  return lower ? edgeSpot(lower, "before", dragged) : level.length ? edgeSpot(level[level.length - 1], "after", dragged) : null;
}

/**
 * Where a page dragged over the sidebar's list lands, decided here for every
 * place of it at once, as Notion's sidebar (seen 2026-11-27 in Chrome, three
 * tabs at three times, synthetic pages: the private pages by last edit, a
 * teamspace folded and open, the favorites): a page's row by its parts
 * (rowSpot); a list's heading or its "모두 보기" its head; the space between
 * rows and an open page's "하위 페이지 없음" by gapSpot; a list's "+" rows,
 * its "하위 페이지 없음", the space around the lists and the teamspaces'
 * heading by betweenLists. Null off the pages (recents, views, the
 * shortcuts) and for anything but a page.
 */
export function sidebarDropSpot(list: Element, clientX: number, clientY: number, data: DragData): SidebarDropSpot | null {
  const under = underDrag(clientX, clientY);
  const dragged = treePageDragId(data);
  if (!under || !list.contains(under) || dragged === null) return null;
  const row = under.closest(PAGE_ROW);
  if (row) return rowSpot(row, clientY, dragged, isFavoriteRowDrag(data));
  const head = under.closest(TEAMSPACE_HEAD);
  if (head) return { element: head, intent: "before", ownPlace: false };
  const listed = under.closest(PAGE_LIST);
  if (listed && (within(under, "sectionLabel") || under.closest("[data-sidebar-show-all]"))) return headSpot(listed, dragged);
  // A section's heading that takes nothing itself (the teamspaces') is the space above its lists.
  const section = under.closest("section");
  if (section && within(under, "sectionLabel") && listsIn(section).length) {
    const upper = section.previousElementSibling;
    return listAfter(upper?.matches("section") ? upper : undefined, section, dragged);
  }
  if (under.matches('[role="tree"], [role="group"]')) return gapSpot(under, clientY, dragged);
  if (listed) {
    if (under !== listed && !under.closest(LIST_ENDS) && !under.matches(classSelector("rootDropArea") || "*:not(*)")) return null;
    const first = listed.querySelector(TOP_ROW);
    return first && clientY < first.getBoundingClientRect().top ? edgeSpot(first, "before", dragged) : endSpot(listed, dragged);
  }
  // Off the lists: in a section of something else (recents, views) or the shortcuts, nothing.
  const block = [...list.children].find((child) => child.contains(under));
  if (block && (within(block, "shortcutList") || (block.matches("section") && !listsIn(block).length))) return null;
  return betweenLists([...list.children].filter((child) => child.matches("section")), clientY, dragged);
}

/** The drop spot of a drag over the sidebar's list `element` is in, now. */
export function dropSpotAt(element: Element, input: { clientX: number; clientY: number }, data: DragData) {
  const list = element.closest("[data-sidebar-drop-list]");
  return list ? sidebarDropSpot(list, input.clientX, input.clientY, data) : null;
}

/**
 * The sidebar's list taking a page dragged where it means another place than
 * the one under the pointer (sidebarDropSpot): that place's row or head
 * hears it as if the pointer were on it, lit, and takes the drop where the
 * last move chose (a release without a move keeps it, though the list moved
 * under it since); where the page already is it lights and moves nothing. A
 * row or a head under the pointer that means itself hears it itself.
 */
export function sidebarDropList(list: HTMLElement) {
  list.setAttribute("data-sidebar-drop-list", "");
  let shown: { spot: SidebarDropSpot; taker: SidebarDropTaker } | null = null;
  const forwarded = (input: { clientX: number; clientY: number }, data: DragData) => {
    const spot = sidebarDropSpot(list, input.clientX, input.clientY, data);
    const under = underDrag(input.clientX, input.clientY);
    if (!spot || (under && spot.element.contains(under))) return null;
    const taker = takers.get(spot.element)?.();
    return taker ? { spot, taker } : null;
  };
  const leave = () => { shown?.taker.leave(); shown = null; };
  const own = (dropTargets: readonly { element: Element }[]) => dropTargets[0]?.element === list;
  const unregister = dropTargetByPointer({
    element: list,
    canDrop: ({ source, input }) => {
      const at = forwarded(input, source.data);
      return Boolean(at?.taker.accepts(source.data, at.spot.intent, dragCopies(input)));
    },
    onDrag: ({ source, location }) => {
      const at = own(location.current.dropTargets) ? forwarded(location.current.input, source.data) : null;
      if (shown && shown.spot.element !== at?.spot.element) leave();
      if (!at) return;
      shown = at;
      at.taker.over(source.data, at.spot.intent, dragCopies(location.current.input));
    },
    onDragLeave: leave,
    onDrop: ({ source, location }) => {
      const at = own(location.current.dropTargets) ? shown : null;
      leave();
      if (at && !at.spot.ownPlace) at.taker.drop(source.data, at.spot.intent, dragCopies(location.current.input));
    },
  });
  return () => { leave(); unregister(); list.removeAttribute("data-sidebar-drop-list"); };
}

/** The sidebar's list scrolling and fading at its edges under a drag (sidebarListEdges) and taking pages by its drop map. */
export function sidebarListWithDrops(list: HTMLElement, fades: boolean) {
  return combine(sidebarListEdges(list, fades), sidebarDropList(list));
}
