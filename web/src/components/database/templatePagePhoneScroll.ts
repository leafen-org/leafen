import { useLayoutEffect } from "react";

// The pages' scrollers put past their options already: each once, a scroller made again (the page read) once more.
const placed = new WeakSet<Element>();
// How many frames a page just drawn is waited for to grow tall enough to scroll that far.
const FRAMES = 90;

/**
 * A template's page open whole on a phone stands scrolled past its page
 * options — "아이콘 추가 · 커버 추가" gone under its band, its title near the
 * top — however it was opened (made, edited from its database's templates,
 * copied, or by its address), as Notion's phone (seen 2026-11-25 on a Galaxy
 * A90's Chrome at 411, in two tabs, ten openings: its scroller 44.19 down, the
 * options' foot at the scroller's top). Once a page, as soon as its options
 * are drawn and it is tall enough to go that far, unless a touch has moved it
 * first; scrolled back, it stays where it was put.
 */
export function useTemplatePageScrolledPastOptions(templateId: string, on: boolean, drawnTick: unknown) {
  useLayoutEffect(() => {
    if (!on) return;
    const page = document.querySelector<HTMLElement>(`[data-row-page-row="${CSS.escape(templateId)}"]`);
    const scroller = page?.closest<HTMLElement>("[data-page-scroll]");
    const options = page?.querySelector<HTMLElement>("[data-row-page-header-controls]");
    if (!scroller || !options || placed.has(scroller) || !options.getClientRects().length) return;
    placed.add(scroller);
    let frame = 0, left = FRAMES, ended = false;
    const stop = () => {
      ended = true;
      window.cancelAnimationFrame(frame);
      scroller.removeEventListener("pointerdown", stop, true);
      scroller.removeEventListener("wheel", stop, true);
    };
    const place = () => {
      const wanted = scroller.scrollTop + options.getBoundingClientRect().bottom - scroller.getBoundingClientRect().top;
      scroller.scrollTop = wanted;
      // Its body may come a few frames after it: until then it is too short to go so far.
      if (Math.abs(scroller.scrollTop - wanted) < 1 || !options.isConnected || --left <= 0) stop();
      else frame = window.requestAnimationFrame(place);
    };
    scroller.addEventListener("pointerdown", stop, true);
    scroller.addEventListener("wheel", stop, true);
    place();
    // Called off before it ended — the page drawn again — the next draw tries again.
    return () => { if (!ended) placed.delete(scroller); stop(); };
  }, [on, templateId, drawnTick]);
}
