import { monitorForElements } from "@atlaskit/pragmatic-drag-and-drop/element/adapter";

// The band at each end that scrolls, as a share of the list's height, and the
// speed in it, in the list's heights a second, as Notion's: one rule for its
// sidebar and its page, the band and the speed growing with the list (a row
// or block held still for a second at each distance from the list's foot,
// and from its top the same, with no wind-up). Seen 2026-11-16, 1440 wide,
// dark: the sidebar's 469px list — 50 from its foot 0, 40 93px/s, 30 203, 20
// 472, 15 667, 10 855, 5 1140; a page's 679px pane (723 window) — 70 0, 60 90,
// 50 199, 40 389, 30 743, 20 1055, 10 1571, 5 1756; the same page's 479px pane
// (523 window) — 50 0, 40 97, 30 262, 20 580, 10 1002, 5 1259. One curve
// fits all three within about a tenth.
const BAND_SHARE = 0.103;
const SLOWEST = 0.09;
const SPEED = 2.96;
const CURVE = 2.1;
// A frame's step is capped, so a frame held up doesn't jump the list.
const LONGEST_FRAME = 50;

/** How fast a pointer this far from an edge of a list this tall scrolls it, in px a second. */
export function edgeScrollSpeed(fromEdge: number, height: number) {
  const band = height * BAND_SHARE;
  if (band <= 0 || fromEdge >= band || fromEdge < 0) return 0;
  const into = (band - fromEdge) / band;
  return height * (SLOWEST + SPEED * into ** CURVE);
}

/**
 * A drag held near a list's or a page's top or foot scrolls it: slowly where
 * the band starts, faster towards the edge (edgeScrollSpeed). The pointer
 * counts only over it. It ends as the drag does. Returns its cleanup.
 */
export function edgeAutoScroll(element: HTMLElement) {
  let pointer: { x: number; y: number } | null = null;
  let frame = 0, last = 0, carried = 0;
  const speedNow = () => {
    if (!pointer) return 0;
    const box = element.getBoundingClientRect();
    if (pointer.x < box.left || pointer.x > box.right) return 0;
    const down = edgeScrollSpeed(box.bottom - pointer.y, box.height);
    if (down > 0) return element.scrollTop + element.clientHeight < element.scrollHeight ? down : 0;
    const up = edgeScrollSpeed(pointer.y - box.top, box.height);
    return up > 0 && element.scrollTop > 0 ? -up : 0;
  };
  const step = (now: number) => {
    frame = 0;
    const speed = speedNow();
    if (!speed) { last = 0; carried = 0; return; }
    const elapsed = last ? Math.min(LONGEST_FRAME, now - last) : 1000 / 60;
    last = now;
    carried += speed * elapsed / 1000;
    const whole = Math.trunc(carried);
    if (whole) { element.scrollTop += whole; carried -= whole; }
    frame = window.requestAnimationFrame(step);
  };
  const follow = (input: { clientX: number; clientY: number }) => {
    pointer = { x: input.clientX, y: input.clientY };
    if (!frame && speedNow()) frame = window.requestAnimationFrame(step);
  };
  const stop = () => {
    pointer = null;
    if (frame) window.cancelAnimationFrame(frame);
    frame = 0; last = 0; carried = 0;
  };
  const unmonitor = monitorForElements({
    onDragStart: ({ location }) => follow(location.current.input),
    onDrag: ({ location }) => follow(location.current.input),
    onDrop: stop,
  });
  return () => { stop(); unmonitor(); };
}
