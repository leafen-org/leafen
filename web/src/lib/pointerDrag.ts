import { combine } from "@atlaskit/pragmatic-drag-and-drop/combine";
import {
  draggable, dropTargetForElements, monitorForElements, type ElementDropTargetEventBasePayload, type ElementDropTargetGetFeedbackArgs,
} from "@atlaskit/pragmatic-drag-and-drop/element/adapter";
import { carriedCopy } from "./dragReorderLine";
import { letGoCarried } from "./carriedChoice";
import { DRAG_START, focusBeforePress, pressTaken, takePress } from "./dragReorderPointer";
import { edgeScrollSpeed } from "./edgeAutoScroll";
import { underDrag } from "./pageHoverShield";

export type { ElementDropTargetEventBasePayload };
export { underDrag };
type SourceArgs = Parameters<typeof draggable>[0];
type TargetArgs = Parameters<typeof dropTargetForElements>[0];
type MonitorArgs = Parameters<typeof monitorForElements>[0];
type Input = ElementDropTargetGetFeedbackArgs["input"];
type Payload = ElementDropTargetEventBasePayload;
type TargetRecord = Payload["self"];
type Source = Payload["source"];

// The drag sources and drop targets a mouse's or a pen's drag goes through, by element.
const sources = new Map<Element, SourceArgs>();
const targets = new Map<Element, TargetArgs>();
const monitors = new Set<MonitorArgs>();
// What a source's drag carries with the pointer when not a copy of the element itself.
const carriers = new WeakMap<SourceArgs, () => HTMLElement | readonly HTMLElement[] | null>();
let following = false;

/**
 * A drag source whose mouse or pen drag follows the pointer's own events,
 * as the rows of a list do (dragReorderPointer), and a finger's drag stays
 * the browser's own (Pragmatic drag and drop, which also owns any drag from
 * elsewhere: a page's blocks, files). The browser's drop was lost at times in
 * WebKit, the desktop app's window — let go quickly (seen 2026-11-12) or after
 * the pointer rested still a moment (seen 2026-11-16) — and what was dragged
 * went back. Takes Pragmatic's own arguments; the source and the drop targets
 * hear the same events with the same payloads (no drag preview: a copy of the
 * element goes with the pointer, see-through, dragReorderLine's carriedCopy;
 * `carry` makes another, or several — a board's chosen cards — placed where
 * they start, which the drag moves and takes away as it ends).
 */
export function draggableByPointer(args: SourceArgs, { carry }: { carry?: () => HTMLElement | readonly HTMLElement[] | null } = {}) {
  if (carry) carriers.set(args, carry);
  sources.set(args.element, args);
  if (sources.size === 1) window.addEventListener("pointerdown", pressed, true);
  return combine(draggable(args), () => {
    if (sources.get(args.element) === args) sources.delete(args.element);
    if (!sources.size) window.removeEventListener("pointerdown", pressed, true);
  });
}

/** A drop target for both a pointer's drag (draggableByPointer) and the browser's own. */
export function dropTargetByPointer(args: TargetArgs) {
  targets.set(args.element, args);
  return combine(dropTargetForElements(args), () => { if (targets.get(args.element) === args) targets.delete(args.element); });
}

/** A monitor of both a pointer's drags (draggableByPointer) and the browser's own. */
export function monitorByPointer(args: MonitorArgs) {
  monitors.add(args);
  return combine(monitorForElements(args), () => { monitors.delete(args); });
}

const inputOf = (event: MouseEvent): Input => ({
  altKey: event.altKey, button: event.button, buttons: event.buttons, ctrlKey: event.ctrlKey, metaKey: event.metaKey,
  shiftKey: event.shiftKey, clientX: event.clientX, clientY: event.clientY, pageX: event.pageX, pageY: event.pageY,
});

// Heard on the window as it starts, before the page's own handlers, as a list's rows are (dragReorderPointer).
function pressed(event: PointerEvent) {
  const target = event.target;
  if (following || pressTaken(event) || event.defaultPrevented || event.button !== 0 || event.pointerType === "touch"
    || event.ctrlKey || event.metaKey || !(target instanceof Element) || target.closest("input, textarea, select")
    || (target instanceof HTMLElement && target.isContentEditable)) return;
  // The nearest source the press is in drags, as the browser drags the nearest draggable element; from its handle only.
  let args: SourceArgs | undefined;
  for (let at: Element | null = target; at && !args; at = at.parentElement) args = sources.get(at);
  if (!args || (args.dragHandle && !args.dragHandle.contains(target))) return;
  takePress(event);
  follow(args, event, inputOf(event), focusBeforePress(event));
}

/**
 * The drop targets under the pointer, the nearest first, as Pragmatic finds
 * them (without its stickiness); under an open menu, those on the page under
 * its backdrop (underDrag), as a calendar's "날짜 없음" row is dropped on a day.
 */
function targetsAt(source: Source, input: Input) {
  const found: TargetRecord[] = [];
  for (let at = underDrag(input.clientX, input.clientY); at; at = at.parentElement) {
    const args = targets.get(at);
    if (!args) continue;
    const feedback = { input, source, element: at };
    if (args.canDrop && !args.canDrop(feedback)) continue;
    found.push({ element: at, data: args.getData?.(feedback) ?? {}, dropEffect: args.getDropEffect?.(feedback) ?? "move", isActiveDueToStickiness: false });
  }
  return found;
}

function follow(args: SourceArgs, press: PointerEvent, start: Input, giveFocusBack: () => void) {
  following = true;
  const listening = new AbortController();
  const options = { capture: true, signal: listening.signal };
  const source: Source = { element: args.element, dragHandle: args.dragHandle ?? null, data: {} };
  let last = start, started = false, over = false, copies: readonly HTMLElement[] = [];
  let initial: TargetRecord[] = [], current: TargetRecord[] = [], previous: TargetRecord[] = [];
  // The monitors that took this drag as it started.
  let watching: MonitorArgs[] = [];
  const base = () => ({ source, location: { initial: { input: start, dropTargets: initial },
    current: { input: last, dropTargets: current }, previous: { dropTargets: previous } } });
  const scroller = edgeScroll(() => last, () => place());

  // The targets under the pointer now: those it left hear it, then those it came to.
  const retarget = (next: TargetRecord[]) => {
    const changed = next.length !== current.length || next.some((record, index) => record.element !== current[index].element);
    previous = current;
    current = next;
    if (!changed) return;
    const payload = base(), had = new Set(previous.map((record) => record.element));
    for (const record of previous) {
      const target = targets.get(record.element);
      target?.onDropTargetChange?.({ ...payload, self: record });
      if (!current.some((now) => now.element === record.element)) target?.onDragLeave?.({ ...payload, self: record });
    }
    for (const record of current) {
      if (had.has(record.element)) continue;
      const target = targets.get(record.element);
      target?.onDropTargetChange?.({ ...payload, self: record });
      target?.onDragEnter?.({ ...payload, self: record });
    }
    args.onDropTargetChange?.(payload);
    for (const monitor of watching) monitor.onDropTargetChange?.(payload);
  };
  const place = () => {
    for (const copy of copies) copy.style.transform = `translate(${last.clientX - start.clientX}px, ${last.clientY - start.clientY}px)`;
    retarget(targetsAt(source, last));
    const payload = base();
    for (const record of current) targets.get(record.element)?.onDrag?.({ ...payload, self: record });
    args.onDrag?.(payload);
    for (const monitor of watching) monitor.onDrag?.(payload);
    scroller.follow();
  };
  const begin = () => {
    // A press one of the page's handlers took for itself (a resize border) drags nothing; whether it may
    // drag is asked as it starts, as the browser's drag asked (a sidebar row reads its page's access on hover).
    if (press.defaultPrevented || (args.canDrag && !args.canDrag({ input: last, element: args.element, dragHandle: source.dragHandle }))) return false;
    source.data = args.getInitialData?.({ input: start, element: args.element, dragHandle: source.dragHandle }) ?? {};
    started = true;
    giveFocusBack();
    const carry = carriers.get(args);
    const carried = carry ? carry() : carriedCopy(args.element);
    copies = !carried ? [] : carried instanceof HTMLElement ? [carried] : carried;
    initial = current = targetsAt(source, start);
    const payload = base();
    watching = [...monitors].filter((monitor) => monitor.canMonitor?.({ initial: payload.location.initial, source }) ?? true);
    for (const record of current) targets.get(record.element)?.onDragStart?.({ ...payload, self: record });
    args.onDragStart?.(payload);
    for (const monitor of watching) monitor.onDragStart?.(payload);
    return true;
  };
  const finish = (dropped: boolean) => {
    if (over) return;
    over = true;
    following = false;
    listening.abort();
    scroller.stop();
    if (!started) return;
    for (const copy of copies) copy.remove();
    // Put back (Escape) or lost, it leaves every target and lands on none.
    if (!dropped) retarget([]);
    const payload = base();
    if (dropped) for (const record of current) targets.get(record.element)?.onDrop?.({ ...payload, self: record });
    args.onDrop?.(payload);
    for (const monitor of watching) monitor.onDrop?.(payload);
  };
  // The click a drag's release makes opens nothing (a card, a column's menu).
  const swallowClick = () => {
    const swallow = (event: MouseEvent) => { event.stopImmediatePropagation(); event.preventDefault(); };
    window.addEventListener("click", swallow, { capture: true });
    window.setTimeout(() => window.removeEventListener("click", swallow, { capture: true }), 0);
  };

  // The browser's own drag of the element (draggable for a finger) and a text selection give way while the button is down.
  window.addEventListener("dragstart", (event) => event.preventDefault(), options);
  window.addEventListener("selectstart", (event) => event.preventDefault(), options);
  // The pointer at a point: once it has gone DRAG_START from the press the drag starts, and it goes with it.
  // Whether it is dragging after.
  const moveTo = (event: PointerEvent) => {
    last = inputOf(event);
    if (!started && Math.hypot(last.clientX - start.clientX, last.clientY - start.clientY) < DRAG_START) return false;
    if (!started && !begin()) { finish(false); return false; }
    place();
    return true;
  };
  window.addEventListener("pointermove", (event) => {
    if (event.pointerId === press.pointerId) moveTo(event);
  }, options);
  // The release is the pointer's last move: what it is let go over takes the drop, though no move came
  // before it — a quick hand's moves can all go to the browser's own drag start, which gives way here
  // (2026-11-18: a calendar's "날짜 없음" row dragged onto a day in a few milliseconds and let go at once
  // came with no pointermove between the press and the release, and dated nothing).
  window.addEventListener("pointerup", (event) => {
    if (event.pointerId !== press.pointerId) return;
    if (moveTo(event)) swallowClick();
    finish(true);
  }, options);
  window.addEventListener("pointercancel", (event) => { if (event.pointerId === press.pointerId) finish(false); }, options);
  // The window losing the focus, not the press moving it from a field.
  window.addEventListener("blur", (event) => { if (event.target === window) finish(false); }, options);
  // Escape puts it back, and only that, but a view's chosen cards it carries are let go (carriedChoice.ts); a key
  // held or let go (Option, which copies) counts where the pointer is.
  const keys = (event: KeyboardEvent) => {
    if (!started) return;
    if (event.type === "keydown" && event.key === "Escape") {
      event.preventDefault();
      event.stopImmediatePropagation();
      window.addEventListener("pointerup", swallowClick, { capture: true, once: true });
      letGoCarried();
      return finish(false);
    }
    if (!["Alt", "Shift", "Meta", "Control"].includes(event.key)) return;
    last = { ...last, altKey: event.altKey, shiftKey: event.shiftKey, metaKey: event.metaKey, ctrlKey: event.ctrlKey };
    place();
  };
  window.addEventListener("keydown", keys, options);
  window.addEventListener("keyup", keys, options);
}

// A frame's step is capped, so a frame held up doesn't jump the scroll.
const LONGEST_FRAME = 50;

/**
 * Near the edge of what scrolls under the pointer (under an open menu, the
 * page under its backdrop, underDrag), it scrolls, along either axis, at the
 * speed Notion's sidebar and page scroll under a drag (edgeScrollSpeed —
 * measured along their height; across, the board's columns, not measured
 * against Notion), as the browser scrolled under its own drag. `moved` hears each step.
 */
function edgeScroll(at: () => Input, moved: () => void) {
  let frame = 0, last = 0;
  const carried = { x: 0, y: 0 };
  const scrolls = () => {
    const { clientX: x, clientY: y } = at();
    const found: { element: Element; axis: "x" | "y"; speed: number }[] = [];
    for (const axis of ["y", "x"] as const) {
      for (let element = underDrag(x, y); element; element = element.parentElement) {
        const style = getComputedStyle(element), overflow = axis === "y" ? style.overflowY : style.overflowX;
        const [size, room, scrolled] = axis === "y" ? [element.clientHeight, element.scrollHeight, element.scrollTop]
          : [element.clientWidth, element.scrollWidth, element.scrollLeft];
        if (room <= size || (!/(auto|scroll)/.test(overflow) && element !== document.scrollingElement)) continue;
        const box = element.getBoundingClientRect();
        const [from, to, pointer] = axis === "y" ? [box.top, box.bottom, y] : [box.left, box.right, x];
        const forward = edgeScrollSpeed(to - pointer, to - from), back = edgeScrollSpeed(pointer - from, to - from);
        // Down and right scroll away from where it starts, except across a box read from the right,
        // whose scrollLeft runs down from 0 at its right.
        const far = Math.abs(scrolled) + size >= room - 1, near = Math.abs(scrolled) < 1;
        const [forwardShut, backShut] = axis === "x" && style.direction === "rtl" ? [near, far] : [far, near];
        const speed = forward > 0 && !forwardShut ? forward : back > 0 && !backShut ? -back : 0;
        if (speed) { found.push({ element, axis, speed }); break; }
      }
    }
    return found;
  };
  const step = (now: number) => {
    frame = 0;
    const found = scrolls();
    if (!found.length) { last = 0; carried.x = carried.y = 0; return; }
    const elapsed = last ? Math.min(LONGEST_FRAME, now - last) : 1000 / 60;
    last = now;
    for (const { element, axis, speed } of found) {
      carried[axis] += speed * elapsed / 1000;
      const whole = Math.trunc(carried[axis]);
      if (!whole) continue;
      carried[axis] -= whole;
      if (axis === "y") element.scrollTop += whole; else element.scrollLeft += whole;
    }
    frame = window.requestAnimationFrame(step);
    moved();
  };
  return {
    follow() { if (!frame && scrolls().length) frame = window.requestAnimationFrame(step); },
    stop() { if (frame) window.cancelAnimationFrame(frame); frame = 0; last = 0; },
  };
}
