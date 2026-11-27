import { afterNextPaint } from "./afterPress";

/**
 * What opening a page sets off that its screen doesn't show: the visit kept
 * for the library, the page left told, the view counted, who viewed it
 * before, whether it is published as a site. It waits until the page is
 * drawn and its own reads — its body, comments and backlinks — have gone
 * first: sent with them, it took the browser's few connections to the
 * server and the server's time from them (2026-11-12, a NAS-class server
 * 100 ms away: eleven requests as a page opened, its body's read waiting a
 * round trip for a connection).
 */
export const PAGE_SIDE_WORK_AFTER_MS = 800;

/** Runs `run` once the page opening now has been drawn and its own reads sent; returns how to call it off. */
export function afterPageShown(run: () => void): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const cancelPaint = afterNextPaint(() => {
    timer = setTimeout(run, PAGE_SIDE_WORK_AFTER_MS);
  });
  return () => {
    cancelPaint();
    clearTimeout(timer);
  };
}
