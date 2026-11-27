/*
 * The block a page is read at stays where it stands as the page's width
 * changes — a side peek opening or closing beside it, the sidebar shown or
 * hidden, the window made wider or narrower — as Notion's (seen 2026-11-14
 * at 1440×723: a row's side peek opened from an inline database in the
 * middle of a long page and closed by Escape left the line above the
 * database at 175 → 175 → 175 and the one under it at 466 → 481 → 466, the
 * 15 between being the database's own sideways scrollbar while the page was
 * narrow). The browser's own scroll anchoring stands down whenever a width
 * rule changes over the block it holds, as the peek's room does
 * (PageView.module.css), and the page lost its place by 300px and more.
 *
 * The block is the deepest one at the top of the page's window, looked for
 * at an idle moment after the page scrolls; meanwhile the block held is read
 * where the scroll took it. Looked for in the frame after each scroll, it
 * hit-tested the whole page after every Enter that rolled the caret down,
 * before the next key (2026-11-14: 5.7 ms at CPU ×2 on a long page of
 * pictures). As a press starts (before what it opens changes the width)
 * where it stands is read again, without looking for it anew while it is
 * still in the window: looking hit-tested the whole page at each key and
 * press, two at a ⌘V (2026-11-14: 17 ms of a ⌘V and 5.5 of an Enter on a
 * long page of pictures at CPU ×2). When the width of the page or of its
 * column changes, the page scrolls so that block stands where it stood,
 * before that frame is painted.
 */

// How far under the window's top the block is looked for: the first point
// may fall between blocks or on the page's margin.
const READ_DEPTHS = [8, 40, 96, 200];
// How long a scroll's look waits for an idle moment at most, and where the
// browser can't say when one comes, how long after the last scroll it looks.
const LOOK_WAIT_MS = 1_000;
const LOOK_AFTER_SCROLL_MS = 200;
// Keys that are only modifiers.
const LONE_MODIFIERS = new Set(["Shift", "Control", "Alt", "Meta"]);

type Place = { block: Element; top: number };

function blockAtTop(scroller: HTMLElement): Place | null {
  // A page at its top stays there, as the browser's own anchoring leaves a
  // page at its top (and Notion's with it): nothing to hold, and no point of
  // it hit-tested.
  if (scroller.scrollTop <= 0) return null;
  const box = scroller.getBoundingClientRect();
  if (box.width <= 0 || box.height <= 0) return null;
  const x = box.left + box.width / 2;
  for (const depth of READ_DEPTHS) {
    if (depth >= box.height) break;
    const hit = document.elementFromPoint(x, box.top + depth);
    const block = hit && scroller.contains(hit) ? hit.closest("[data-block-id]") : null;
    if (block && scroller.contains(block)) return { block, top: block.getBoundingClientRect().top };
  }
  return null;
}

/** Keeps the page's reading place across changes of its width; returns how to stop. */
export function keepPlaceAcrossWidths(scroller: HTMLElement): () => void {
  let place: Place | null = null;
  let frame = 0;
  let look: { idle: number } | { timer: number } | null = null;
  // The scroll this keeper set, which reads no new place.
  let set: number | null = null;
  // Whether the place read last still stands: nothing scrolled, changed size or was typed since.
  let known = false;
  const read = () => { place = blockAtTop(scroller); known = true; };
  const readNext = () => { frame = 0; read(); };
  const readIdle = () => { look = null; read(); };
  const lookSoon = () => {
    if (look && "timer" in look) window.clearTimeout(look.timer);
    else if (look) return;
    look = typeof window.requestIdleCallback === "function"
      ? { idle: window.requestIdleCallback(readIdle, { timeout: LOOK_WAIT_MS }) }
      : { timer: window.setTimeout(readIdle, LOOK_AFTER_SCROLL_MS) };
  };
  // A press: the block held, where it stands now, while it is in the window;
  // none found at the last read, none looked for again until the page moves
  // (2026-11-14: four points hit-tested at each press on a page with no block
  // at its top, some 20 ms of a press at CPU ×2). A key may move what it
  // stands over: the next press reads again. A modifier held alone opens,
  // closes and moves nothing, and reads nothing (2026-11-15: the ⌘ of a ⌘V
  // laid out a 1,031-block page again, 3.3 ms at CPU ×2).
  const readAtPress = (event: Event) => {
    if (event instanceof KeyboardEvent && LONE_MODIFIERS.has(event.key)) return;
    try { pressRead(); } finally { if (event.type === "keydown") known = false; }
  };
  const pressRead = () => {
    if (known && !place) return;
    const block = place?.block;
    if (block?.isConnected && scroller.contains(block)) {
      const box = scroller.getBoundingClientRect(), rect = block.getBoundingClientRect();
      if (rect.bottom > box.top && rect.top < box.bottom) { place = { block, top: rect.top }; return; }
    }
    read();
  };
  const onScroll = () => {
    const own = set !== null && Math.abs(scroller.scrollTop - set) < 1;
    set = null;
    if (own) return;
    known = false;
    // The block held, where the scroll took it, until it is looked for again:
    // its box only, which the scroll leaves laid out.
    if (place?.block.isConnected) place = { block: place.block, top: place.block.getBoundingClientRect().top };
    lookSoon();
  };
  const widths = new WeakMap<Element, number>();
  const sizes = new ResizeObserver((entries) => {
    known = false;
    let changed = false;
    for (const entry of entries) {
      const width = entry.contentRect.width;
      const was = widths.get(entry.target);
      widths.set(entry.target, width);
      if (was !== undefined && Math.abs(was - width) >= 0.5) changed = true;
    }
    if (!changed || !place?.block.isConnected) return;
    const moved = place.block.getBoundingClientRect().top - place.top;
    if (Math.abs(moved) < 0.5) return;
    scroller.scrollTop += moved;
    set = scroller.scrollTop;
  });
  const children = new MutationObserver(() => { for (const child of scroller.children) sizes.observe(child); });
  sizes.observe(scroller);
  for (const child of scroller.children) sizes.observe(child);
  children.observe(scroller, { childList: true });
  scroller.addEventListener("scroll", onScroll, { passive: true });
  // A press reads the place the page stands at as it starts, before what it
  // opens or closes changes the width (a key typed may have moved the block).
  window.addEventListener("pointerdown", readAtPress, { capture: true, passive: true });
  window.addEventListener("keydown", readAtPress, { capture: true, passive: true });
  frame = window.requestAnimationFrame(readNext);
  return () => {
    if (frame) window.cancelAnimationFrame(frame);
    if (look && "idle" in look) window.cancelIdleCallback(look.idle);
    else if (look) window.clearTimeout(look.timer);
    sizes.disconnect();
    children.disconnect();
    scroller.removeEventListener("scroll", onScroll);
    window.removeEventListener("pointerdown", readAtPress, { capture: true });
    window.removeEventListener("keydown", readAtPress, { capture: true });
  };
}
