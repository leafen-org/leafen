import { useCallback, useLayoutEffect, useRef, type RefObject } from "react";
import { untilNextPress } from "./afterPress";
import { inlineSign } from "./readingDirection";
import { announceSideRowPeekLayout, withdrawSideRowPeeks } from "./sideRowPeekLayout";

// A side peek's motion, as Notion's (seen 2026-11-12 at 1440×723 by "»",
// Escape and a press on the page beside it alike): it slides out to the
// window's edge in some 190ms, fast then slow (CSS `ease`), at full opacity,
// and is gone at the end, the page widening in the same frames (AppShell).
// It slides in as before (DatabaseRowPreview.module.css), the page narrowing
// with it. Opened whole (⤢), it goes at once and only the page widens; a
// center peek closes with no motion in either. Turned from beside the page to
// its center or back (its view options), it stands at its new place at once,
// only the page behind widening or narrowing (seen 2026-11-12: the center
// window at 233,72 in the first frame it is drawn, the side one at 720).
export const SIDE_PEEK_OPEN_MS = 220;
export const SIDE_PEEK_OPEN_EASING = "cubic-bezier(0.2, 0, 0, 1)";
export const SIDE_PEEK_CLOSE_MS = 190;
export const SIDE_PEEK_CLOSE_EASING = "ease";
// The page takes or gives that room quicker as a peek turns between side and
// center, in some 120ms, fast then slow, as Notion's (seen 2026-11-12: 450
// to 1170 wide and back behind the window standing still).
export const SIDE_PEEK_SWITCH_MS = 120;
let switchedAt = Number.NEGATIVE_INFINITY;

/** Whether a peek turned between side and center just now, once (AppShell). */
export function takePeekSwitch() {
  const just = performance.now() - switchedAt < 1000;
  switchedAt = Number.NEGATIVE_INFINITY;
  return just;
}

/** Whether the window asks for less motion, which leaves a peek's (the stylesheet's own rule too). */
export function lessMotion() {
  return typeof window !== "undefined" && typeof window.matchMedia === "function"
    && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

// The copies of side peeks sliding out now (a row's peek over another's goes with it), the last last.
const leaving: HTMLElement[] = [];

// The side peeks a press closed, sliding out until their page takes them
// away (useSidePeekMotion): the slide, the places the page was told the side
// peeks had (this one's and those under it), and whether it has been asked to
// take them away.
type Going = { slide: Animation; placed: ReturnType<typeof withdrawSideRowPeeks>; closed: boolean };
const going = new WeakMap<HTMLElement, Going>();

// The longest a peek a press closed waits to be taken away before it comes
// back (its close kept it on the screen without drawing it again).
const KEPT_PEEK_MS = 1_000;

/** A peek a press closed that stays on the screen stands as before the press. */
function comeBack(panel: HTMLElement) {
  const was = going.get(panel);
  if (!was) return;
  going.delete(panel);
  was.slide.cancel();
  panel.inert = false;
  placeAgain(was.placed);
}

/** The side peeks a press took the places of that are still on the screen stand beside the page again. */
function placeAgain(placed: Going["placed"]) {
  for (const [owner, place] of placed) {
    if (!(owner instanceof HTMLElement) || owner.isConnected) announceSideRowPeekLayout(owner, place);
  }
}

// The side peek on the screen now and how a press closes it (useSidePeekMotion).
let shown: { panel: HTMLElement; leave: (close: () => void) => void } | null = null;

/**
 * A press outside the side peek that closes it — its row's "닫기" in the
 * table, its "…" menu's trash — closes it as a press in it does
 * (useSidePeekMotion); `close` runs at once when none is on the screen.
 */
export function leaveSidePeek(close: () => void) {
  if (shown?.panel.isConnected) shown.leave(close);
  else close();
}

/**
 * Slides a side peek out however it closes — its "»", Escape, a press
 * beside it, the way back — and gives the next one opened meanwhile the
 * place it has reached: React takes the peek away at once, so a copy of it,
 * held still and unheard, goes in its stead.
 *
 * Answers how a press closes it: in the press's own frame it starts sliding
 * out, unheard, and the page starts taking its room back, and `close` takes
 * it off the screen once it has slid out, or at the next press if that comes
 * first (user decision 2026-11-12: a press answers on the next frame — taken
 * away in the press, its page's editor and the view under it drawn again held
 * that frame, 88–112 ms to the first paint on a NAS-class server with the
 * browser's CPU ×2, 136–168 from its "…" menu's trash, 2026-11-13; taken
 * away once the press was drawn, that work held the slide's frames instead).
 * Its close keeping it on the screen — another item opened in it at that
 * press — it stands there again. One not beside the page, or with no
 * motion, closes at once.
 */
export function useSidePeekMotion(panelRef: RefObject<HTMLElement | null>, side: boolean, itemId: string) {
  const now = useRef({ side, itemId });
  const placed = useRef(side);
  now.current = { side, itemId };
  useLayoutEffect(() => {
    if (placed.current === side) return;
    placed.current = side;
    switchedAt = performance.now();
    if (panelRef.current) placeAtOnce(panelRef.current);
  }, [panelRef, side]);
  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (panel && now.current.side) takeOver(panel);
    else if (panel && performance.now() - centerLeftAt < CENTER_SWAP_MS) standInPlace(panel);
    return () => {
      // One opened whole is the page now: the peek goes at once (Notion's).
      if (panel && now.current.side && !location.pathname.includes(now.current.itemId)) slideOut(panel);
      else if (panel?.dataset.mode === "center") centerLeftAt = performance.now();
    };
  }, [panelRef]);
  const leave = useCallback((close: () => void) => {
    const panel = panelRef.current;
    if (!panel || !now.current.side || panel.dataset.phone === "true" || lessMotion()) {
      close();
      return;
    }
    // A second press before it is taken away (Escape twice) has nothing more to do.
    if (going.has(panel)) return;
    const slide = panel.animate(slideKeyframes(panel), { duration: SIDE_PEEK_CLOSE_MS, easing: SIDE_PEEK_CLOSE_EASING, fill: "forwards" });
    const entry: Going = { slide, placed: withdrawSideRowPeeks(), closed: false };
    going.set(panel, entry);
    panel.inert = true;
    const press = untilNextPress();
    const takeAway = () => {
      if (entry.closed) return;
      entry.closed = true;
      press.removeEventListener("abort", takeAway);
      close();
      // It gone, those it was opened over that stay stand beside the page again.
      window.setTimeout(() => {
        if (panel.isConnected && going.get(panel) === entry) comeBack(panel);
        else if (!panel.isConnected) placeAgain(entry.placed);
      }, KEPT_PEEK_MS);
    };
    press.addEventListener("abort", takeAway, { once: true });
    void slide.finished.then(takeAway, takeAway);
    window.setTimeout(takeAway, SIDE_PEEK_CLOSE_MS + 150);
  }, [panelRef]);
  // Drawn again once it was to be taken away, it is still wanted here.
  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (panel && going.get(panel)?.closed) comeBack(panel);
  });
  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (!side || !panel) return;
    const entry = { panel, leave };
    shown = entry;
    return () => {
      if (shown === entry) shown = null;
    };
  }, [leave, panelRef, side]);
  return leave;
}

/** A side peek sliding out to the window's edge where the screen reads to. */
function slideKeyframes(panel: HTMLElement): Keyframe[] {
  return [{ transform: "translateX(0)" }, { transform: `translateX(${100 * inlineSign(panel)}%)` }];
}

// A center peek that takes another's place at once — a person pressed in it,
// the way back from one — stands there with its shade, as Notion's swaps
// what its window shows (seen 2026-11-12); opened anew it rises as before.
const CENTER_SWAP_MS = 100;
let centerLeftAt = Number.NEGATIVE_INFINITY;

function standInPlace(panel: HTMLElement) {
  centerLeftAt = Number.NEGATIVE_INFINITY;
  for (const part of [panel, panel.previousElementSibling]) {
    if (!(part instanceof HTMLElement) || part.dataset.motionState !== "opening") continue;
    part.dataset.motionState = "open";
    placeAtOnce(part);
  }
}

/** The panel takes its new place without its slide, which runs again from the next frame. */
function placeAtOnce(panel: HTMLElement) {
  const given = panel.style.transition;
  panel.style.transition = "none";
  void panel.offsetWidth;
  window.requestAnimationFrame(() => {
    if (panel.style.transition === "none") panel.style.transition = given;
  });
}

function slideOut(panel: HTMLElement) {
  if (!panel.isConnected || panel.dataset.phone === "true" || lessMotion()) return;
  // One a press closed has slid out, or goes on from where it has slid to.
  const slid = going.get(panel)?.slide;
  going.delete(panel);
  if (slid?.playState === "finished") return;
  const keyframes = slideKeyframes(panel);
  const copy = panel.cloneNode(true) as HTMLElement;
  // What plays or loads again in a copy is left as room of its size.
  const players = panel.querySelectorAll<HTMLElement>("iframe, video, audio, canvas");
  copy.querySelectorAll<HTMLElement>("iframe, video, audio, canvas").forEach((element, index) => {
    const room = document.createElement("div");
    room.style.width = `${players[index]?.offsetWidth ?? 0}px`;
    room.style.height = `${players[index]?.offsetHeight ?? 0}px`;
    element.replaceWith(room);
  });
  for (const name of copy.getAttributeNames()) {
    if (name.startsWith("data-") && !["data-mode", "data-motion-state", "data-stack-columns"].includes(name)) copy.removeAttribute(name);
  }
  for (const name of ["role", "aria-label", "aria-modal", "tabindex", "id"]) copy.removeAttribute(name);
  copy.setAttribute("aria-hidden", "true");
  copy.inert = true;
  copy.dataset.peekLeaving = "";
  // It rests out of the window (the stylesheet's "closing") and slides there
  // from where it stands: an animation, not the stylesheet's transition, which
  // laid the page out again in the frame to start (some 10 ms on a slow device).
  copy.dataset.motionState = "closing";
  copy.style.transition = "none";
  panel.after(copy);
  // It shows the page where it was read to.
  const scrolled = [panel, ...panel.querySelectorAll<HTMLElement>(".nscroll")];
  const copies = [copy, ...copy.querySelectorAll<HTMLElement>(".nscroll")];
  scrolled.forEach((element, index) => {
    if (copies[index] && element.scrollTop) copies[index].scrollTop = element.scrollTop;
  });
  const motion = copy.animate(keyframes, { duration: SIDE_PEEK_CLOSE_MS, easing: SIDE_PEEK_CLOSE_EASING, fill: "forwards" });
  if (slid?.startTime != null) motion.startTime = slid.startTime;
  slid?.cancel();
  leaving.push(copy);
  const done = () => {
    copy.remove();
    if (leaving.includes(copy)) leaving.splice(leaving.indexOf(copy), 1);
  };
  void motion.finished.then(done, done);
  window.setTimeout(done, SIDE_PEEK_CLOSE_MS + 150);
}

/**
 * A side peek opened while another slides out comes in from where that one
 * is, in the time the page takes to narrow again (AppShell), and one that
 * takes another's place at once — another row opened in it — stands where it
 * stood, as Notion's swaps it (seen 2026-11-12).
 */
function takeOver(panel: HTMLElement) {
  const was = leaving.pop();
  if (!was) return;
  const from = new DOMMatrixReadOnly(getComputedStyle(was).transform === "none" ? undefined : getComputedStyle(was).transform).m41;
  was.remove();
  // The stylesheet's slide in waits while this one runs from there.
  panel.style.transition = "none";
  const motion = panel.animate([
    { transform: `translateX(${from}px)`, opacity: 1 },
    { transform: "translateX(0)", opacity: 1 },
  ], { duration: from ? SIDE_PEEK_OPEN_MS : 0, easing: SIDE_PEEK_OPEN_EASING, fill: "forwards" });
  const settle = () => {
    if (panel.dataset.motionState !== "open") return false;
    motion.cancel();
    panel.style.transition = "";
    return true;
  };
  void motion.finished.then(() => {
    if (settle()) return;
    const watch = new MutationObserver(() => { if (settle()) watch.disconnect(); });
    watch.observe(panel, { attributes: true, attributeFilter: ["data-motion-state"] });
  }, () => undefined);
}
