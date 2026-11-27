import { type RefObject, useLayoutEffect } from "react";
import styles from "./DatabaseStickyHeads.module.css";
import { flushHeldStyleWrites, holdStyleWrite, startWithMeasures } from "./heldStyleWrites";
import { watchPageSize } from "../page/pageSizeWatch";
import { afterNextPaint } from "@/lib/afterPress";

const scrolls = (element: Element) => !/^(visible|clip)$/u.test(getComputedStyle(element).overflowX)
  || !/^(visible|clip)$/u.test(getComputedStyle(element).overflowY);

/**
 * A view's heads — a table's column head, a board's column heads, a
 * calendar's month row and weekdays (each marked data-sticky-head) — stay
 * under the top of the page as it scrolls on past them, till their view's
 * end comes up to them, as Notion's do on a database's own page and in a
 * page's text (seen 2026-11-12 at 1440: a table's head at the page's top,
 * 44–80 under a 44px bar; a board's heads 8px under it, 52–92, that room
 * filled with the page; a calendar's month row and weekdays 44–112).
 *
 * A view scrolls sideways in its own box, which keeps CSS sticky from
 * reaching the page's scroller, so the heads slide down in that box by as
 * much as its top has gone past the page's top, the first one `gap` under it:
 * on the page's own scrolling where the browser can (a view timeline), else
 * on each scroll; the view's box is marked data-sticky-heads meanwhile, for
 * the heads' own ground. How far they slide is written on the heads
 * themselves, not on the view's box: a value its rows inherit drew every
 * cell of a table again whenever a row came or went (it changes with the
 * view's height; 2026-11-13, 65 ms a row on a table of 30 rows by 40
 * properties). A view in a box of its own that scrolls (a
 * dashboard's widget), in a callout or on a phone keeps its heads in place
 * (Notion's callout keeps a table's, a board's and a calendar's heads in
 * place where a toggle's, coloured or not, lets them stay at the page's top
 * on its own ground; seen 2026-11-16 at 1440). `shape` is what changes
 * which heads the view has (looked for again when it does). With `own`, the
 * element is the view's box itself, its heads in boxes of their own that
 * scroll sideways under it: a timeline's controls, its dates and its table's
 * head (Notion's, seen 2026-11-12: "» 2026년 11월 … < 오늘 >" and the day
 * numbers 40–108 under the bar, to the end of its rows).
 *
 * Heads marked with a stack (data-sticky-stack) slide together, each stack
 * only till the next one comes up under it, the last till the end of the
 * element they stand in: a grouped table's group head and its column head, as
 * Notion's (seen 2026-11-15 at 1440×723: "▾ Not started" 44–86 and its
 * column head 86–122 under the 44px bar while its rows pass, pushed up
 * together by "In progress" as it comes, which then stays). `rows`, the
 * element whose lines come and go (a windowed table's), has the heads
 * measured again once the frame they came or went in is painted: measured in
 * the task that brought them, the page was worked out anew inside a grouped
 * table's "+ 새 페이지" before its row could show (25–95 ms at CPU ×2,
 * 2026-11-15); a change of the view's size meanwhile is measured with the
 * page's other measures (pageSizeWatch), after its layout.
 */
const HEAD_VALUES = ["--sticky-heads-from", "--sticky-heads-travel", "--sticky-heads-shift"];

type StickyOptions = { gap?: number; shape?: unknown; own?: boolean; rows?: RefObject<HTMLElement | null> };

export function useStickyViewHeads(ref: RefObject<HTMLElement | null>, { gap = 0, shape, own = false, rows }: StickyOptions = {}) {
  // Started with the view's other first measures, the boxes that scroll
  // looked for with them (startWithMeasures).
  useLayoutEffect(() => { const carriedOn = own && carryBoxOver(ref.current); return startWithMeasures(() => {
    // The view's sideways box: the element itself or the nearest one around it.
    let box = ref.current;
    while (!own && box && !box.matches("[data-page-scroll]") && !scrolls(box)) box = box.parentElement;
    let outer = box?.parentElement ?? null;
    while (outer && !scrolls(outer)) outer = outer.parentElement;
    const database = ref.current?.closest("[data-database-view]");
    if (!box || !outer?.matches("[data-page-scroll]") || box.matches("[data-page-scroll]")
      || box.closest('[data-phone="true"], [data-database-view-embedded], [data-block-type="callout"]')) {
      if (database) boxed.set(database, false);
      const drop = carriedOn ? ref.current : null;
      if (drop) holdStyleWrite(() => { drop.classList.remove(styles.box); delete drop.dataset.stickyHeads; });
      return undefined;
    }
    const view = box, page = outer;
    const timeline = typeof CSS !== "undefined" && CSS.supports("animation-timeline: view()");
    // The heads as last measured, each with where its stack starts sliding
    // (from the view's top) and how far it goes.
    let heads: HTMLElement[] = [];
    let runs = new Map<HTMLElement, { from: number; travel: number }>();
    // Where they slide from and how far, written once the frame this measure
    // goes before has painted while none slide yet (the view's top not past
    // the page's): written in it, a timeline's dates, a row of a year of
    // days, were styled again inside its view tab's press (~8 ms at CPU ×2,
    // 2026-11-15). Under the page's top already, they are written in it.
    let later: (() => void) | null = null;
    // A head's slide keeps the start Chrome first aligned it to when its
    // range is written anew with no scroll since (2026-11-26): a grouped
    // table shown, the page then rolled in the same task (its chosen cell
    // brought into view) and its ranges written after that frame, drew its
    // second group's heads slid as far as the first group's, over that
    // group's first rows, till the next scroll. Each head whose range is
    // written has its slide aligned again (play) in the next frame's
    // callbacks, reading the styles that frame works out anyway.
    const rewritten = new Set<HTMLElement>();
    let restart = 0;
    const startAgain = (head: HTMLElement) => {
      if (!timeline) return;
      rewritten.add(head);
      restart ||= requestAnimationFrame(() => {
        restart = 0;
        for (const moved of rewritten) {
          for (const animation of moved.getAnimations()) if (animation.timeline !== document.timeline) animation.play();
        }
        rewritten.clear();
      });
    };
    const unwrite = (from: readonly HTMLElement[]) => {
      for (const head of from) for (const name of HEAD_VALUES) head.style.removeProperty(name);
    };
    const shiftOf = (head: HTMLElement) => parseFloat(getComputedStyle(head).translate.split(" ")[1] ?? "0") || 0;
    const slide = () => {
      const past = page.getBoundingClientRect().top + page.clientTop - view.getBoundingClientRect().top;
      const shifts = heads.map((head) => {
        const run = runs.get(head);
        return run ? `${Math.min(run.travel, Math.max(0, past - run.from))}px` : "0px";
      });
      holdStyleWrite(() => heads.forEach((head, index) => head.style.setProperty("--sticky-heads-shift", shifts[index]!)));
    };
    const measure = () => {
      const found = [...view.querySelectorAll<HTMLElement>("[data-sticky-head]")];
      const gone = heads.filter((head) => !found.includes(head));
      heads = found;
      const top = view.getBoundingClientRect().top;
      // Where each stack's heads lie unslid, from the view's top; heads
      // without a stack are one.
      const stacks = new Map<string, { heads: HTMLElement[]; start: number; end: number; bound: number }>();
      for (const head of heads) {
        const rect = head.getBoundingClientRect(), shift = shiftOf(head);
        const key = head.dataset.stickyStack ?? "";
        const stack = stacks.get(key) ?? { heads: [], start: Infinity, end: -Infinity, bound: view.offsetHeight };
        stack.heads.push(head);
        stack.start = Math.min(stack.start, rect.top - shift - top);
        stack.end = Math.max(stack.end, rect.bottom - shift - top);
        // A stack's last run ends with what holds its heads.
        if (key && head.parentElement) stack.bound = head.parentElement.getBoundingClientRect().bottom - top;
        stacks.set(key, stack);
      }
      // What it measured it writes after the view's other hooks have
      // measured too (heldStyleWrites).
      if (database) boxed.set(database, heads.length > 0);
      if (!heads.length) {
        runs = new Map();
        holdStyleWrite(() => {
          unwrite(gone);
          view.classList.remove(styles.box);
          delete view.dataset.stickyHeads;
        });
        return;
      }
      const ordered = [...stacks.values()].sort((left, right) => left.start - right.start);
      runs = new Map();
      ordered.forEach((stack, index) => {
        const until = ordered[index + 1]?.start ?? stack.bound;
        const run = { from: stack.start - gap, travel: Math.max(0, until - stack.end) };
        for (const head of stack.heads) runs.set(head, run);
      });
      const written = new Map(runs);
      const place = () => {
        for (const [head, run] of written) {
          for (const [name, value] of [["--sticky-heads-from", `${run.from}px`], ["--sticky-heads-travel", `${run.travel}px`]]) {
            if (head.style.getPropertyValue(name) === value) continue;
            head.style.setProperty(name, value);
            startAgain(head);
          }
        }
      };
      const slides = page.getBoundingClientRect().top + page.clientTop - top > Math.min(...[...written.values()].map((run) => run.from));
      later?.();
      later = slides ? null : afterNextPaint(() => { later = null; place(); });
      holdStyleWrite(() => {
        unwrite(gone);
        if (slides) place();
        view.classList.add(styles.box);
        if (view.dataset.stickyHeads !== "") view.dataset.stickyHeads = "";
      });
      if (!timeline) slide();
    };
    measure();
    // As it resizes, with the page's other measures, before their writes
    // (pageSizeWatch), not after them in an observer of its own; as its
    // lines come and go, once the frame they came in is painted.
    const stopView = watchPageSize(view, measure);
    let linesLater: (() => void) | null = null;
    const lines = rows?.current ? new MutationObserver(() => { linesLater ??= afterNextPaint(() => { linesLater = null; measure(); }); }) : null;
    if (lines && rows?.current) lines.observe(rows.current, { childList: true });
    if (!timeline) page.addEventListener("scroll", slide, { passive: true });
    return () => {
      later?.();
      linesLater?.();
      cancelAnimationFrame(restart);
      rewritten.clear();
      stopView();
      lines?.disconnect();
      page.removeEventListener("scroll", slide);
      flushHeldStyleWrites();
      view.classList.remove(styles.box);
      delete view.dataset.stickyHeads;
      unwrite(heads);
    };
  }); }, [ref, gap, shape, own, rows]);
}

// Whether each database's view last kept its heads under the page's top (by its view's section, which its views share).
const boxed = new WeakMap<Element, boolean>();

/**
 * A view that is its own box, put in another's place in a database whose last
 * view kept its heads under the page's top, is marked as keeping them before
 * it is first laid out: marked after it, the view timeline its box names
 * styled the whole view again (a timeline beside its table, ~28 ms at CPU ×2
 * inside its view tab's press, 2026-11-15). Returns whether it marked it.
 */
function carryBoxOver(view: HTMLElement | null) {
  const database = view?.closest("[data-database-view]");
  if (!view || !database || boxed.get(database) !== true || view.classList.contains(styles.box)) return false;
  view.classList.add(styles.box);
  view.dataset.stickyHeads = "";
  return true;
}
