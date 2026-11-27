import { type RefObject, useLayoutEffect } from "react";

// A media block's caption sits under the picture, player or embed frame at
// its width, as Notion's (2026-10-16: it ran across the whole block from its
// left edge; 2026-11-11: an embed in its 400 box, its caption in the box):
// where the shown media sits in the block goes to the caption as two CSS
// values (EditorImageBlocks.module.css .caption).
export function useCaptionUnder(shownRef: RefObject<HTMLElement | null>, key: unknown, across = false) {
  useLayoutEffect(() => {
    let stop = () => {};
    let frame = 0;
    let tries = 0;
    // The editor puts a block's view in its place after rendering it, so the
    // block may be found a frame or two later.
    const attach = () => {
      const shown = shownRef.current;
      const figure = shown?.closest("figure");
      const lane = figure?.parentElement;
      const host = shown?.closest<HTMLElement>("[data-block-id]");
      if (!shown || !host || !lane) {
        if (shown && tries++ < 20) frame = requestAnimationFrame(attach);
        return;
      }
      const place = () => {
        const around = lane.getBoundingClientRect();
        // Across, it takes the column's own frame, 2px in from the block either
        // side as a caption is (Notion's, seen 2026-11-16: 495.5..1199.5 under a
        // 200px linked picture, its text 2px in and its "ALT" at 1165..1200).
        const box = across ? { left: around.left + 2, width: around.width - 4 } : shown.getBoundingClientRect();
        host.style.setProperty("--media-caption-left", `${Math.round(box.left - around.left)}px`);
        host.style.setProperty("--media-caption-width", `${Math.round(box.width)}px`);
      };
      place();
      // The block's own box too: a picture made narrower moves to the
      // middle as its block narrows, without its own size changing then
      // (2026-11-11: the caption stayed at the block's start).
      const observer = new ResizeObserver(place);
      observer.observe(shown);
      observer.observe(lane);
      if (figure) observer.observe(figure);
      stop = () => {
        observer.disconnect();
        host.style.removeProperty("--media-caption-left");
        host.style.removeProperty("--media-caption-width");
      };
    };
    attach();
    return () => {
      cancelAnimationFrame(frame);
      stop();
    };
  }, [shownRef, key, across]);
}
