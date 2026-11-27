type Side = "before" | "after";
type Input = { clientX: number; clientY: number };
type Saved = { transform: string; transition: string; zIndex: string; position: string };

/** One direction a list runs in: what its rows' places, the pointer and the scroll are read along. */
type Along = {
  start: "top" | "left"; end: "bottom" | "right"; client: "clientY" | "clientX"; translate: "translateY" | "translateX";
  scroll: "scrollTop" | "scrollLeft"; overflow: "overflowY" | "overflowX";
  scrollSize: "scrollHeight" | "scrollWidth"; clientSize: "clientHeight" | "clientWidth";
};
const VERTICAL: Along = { start: "top", end: "bottom", client: "clientY", translate: "translateY", scroll: "scrollTop",
  overflow: "overflowY", scrollSize: "scrollHeight", clientSize: "clientHeight" };
const HORIZONTAL: Along = { start: "left", end: "right", client: "clientX", translate: "translateX", scroll: "scrollLeft",
  overflow: "overflowX", scrollSize: "scrollWidth", clientSize: "clientWidth" };
const span = (rect: DOMRect, along: Along) => ({ start: rect[along.start], end: rect[along.end], size: rect[along.end] - rect[along.start] });

/** How long the other rows take to make way, as Notion's menu lists (200ms ease, seen 2026-11-11). */
const MAKE_WAY = "transform 200ms ease";

/**
 * A list's rows moved live while one is dragged, as Notion's menu lists move
 * theirs (seen 2026-11-11 in the relation picker, the files editor and view
 * settings › 속성) and a table's column heads along the head (seen
 * 2026-11-11: the head dragged goes with the pointer, the heads it passes
 * slide over by its width in 200ms, the columns under them stay): the
 * dragged row goes with the pointer along the list and the rows it passes
 * slide over by its place. A row is passed once the dragged row's middle
 * crosses into it. Where the rows stood at the start stays the measure, so a
 * row sliding away doesn't change where the dragged one lands; a list
 * scrolled during the drag counts its scroll. Along a line read from the
 * right, the row passed toward the left lands after it.
 */
export function startLiveReorder(source: string, rows: { id: string; element: HTMLElement }[], start: Input,
  axis: "vertical" | "horizontal" = "vertical") {
  const along = axis === "vertical" ? VERTICAL : HORIZONTAL;
  const placed = rows.filter(({ element }) => element.isConnected)
    .map(({ id, element }) => ({ id, element, home: span(element.getBoundingClientRect(), along) }))
    .filter(({ home }) => home.size > 0)
    .sort((a, b) => a.home.start - b.home.start);
  const from = placed.findIndex(({ id }) => id === source);
  if (from < 0) return null;
  const scroller = scrollingAncestor(placed[from].element, along);
  const scrolled = () => (scroller?.[along.scroll] ?? 0);
  const scrollStart = scrolled();
  // Read from the right, a row further left comes later in the list.
  const reversed = axis === "horizontal" && getComputedStyle(placed[from].element).direction === "rtl";
  const saved = new Map<HTMLElement, Saved>(placed.map(({ element }) => [element, {
    transform: element.style.transform, transition: element.style.transition,
    zIndex: element.style.zIndex, position: element.style.position,
  }]));
  const dragged = placed[from].element;
  if (getComputedStyle(dragged).position === "static") dragged.style.position = "relative";
  dragged.style.zIndex = "1";
  dragged.style.transition = "none";
  // The dragged row's place: what the rows after it slide back by, or the
  // rows before it on by, gaps between rows included.
  const after = placed[from + 1] ? placed[from + 1].home.start - placed[from].home.start : 0;
  const before = placed[from - 1] ? placed[from].home.end - placed[from - 1].home.end : 0;
  let at = from;

  return {
    place(input: Input) {
      const moved = input[along.client] - start[along.client] + (scrolled() - scrollStart);
      const middle = (placed[from].home.start + placed[from].home.end) / 2 + moved;
      at = from;
      for (let index = from + 1; index < placed.length && middle > placed[index].home.start; index += 1) at = index;
      if (at === from) for (let index = from - 1; index >= 0 && middle < placed[index].home.end; index -= 1) at = index;
      dragged.style.transform = `${along.translate}(${moved}px)`;
      placed.forEach(({ element }, index) => {
        if (index === from) return;
        const shift = index > from && index <= at ? -after : index < from && index >= at ? before : 0;
        element.style.transition = MAKE_WAY;
        element.style.transform = shift ? `${along.translate}(${shift}px)` : "";
      });
    },
    /** Where the dragged row lands: the row it took the place of, and on which side. */
    landing(): { target: string; side: Side } | null {
      return at === from ? null : { target: placed[at].id, side: (at > from) !== reversed ? "after" : "before" };
    },
    /** Every row back where the list draws it, at once. */
    end() {
      for (const [element, style] of saved) {
        element.style.transition = "none";
        element.style.transform = style.transform;
        element.style.zIndex = style.zIndex;
        element.style.position = style.position;
      }
      window.requestAnimationFrame(() => { for (const [element, style] of saved) element.style.transition = style.transition; });
    },
  };
}

export type LiveReorder = NonNullable<ReturnType<typeof startLiveReorder>>;

/** The box that scrolls a row's list along its direction, if any. */
export function listScroller(element: HTMLElement, axis: "vertical" | "horizontal") {
  return scrollingAncestor(element, axis === "vertical" ? VERTICAL : HORIZONTAL);
}

function scrollingAncestor(element: HTMLElement, along: Along) {
  for (let parent = element.parentElement; parent; parent = parent.parentElement) {
    const overflow = getComputedStyle(parent)[along.overflow];
    if ((overflow === "auto" || overflow === "scroll") && parent[along.scrollSize] > parent[along.clientSize]) return parent;
  }
  return null;
}
