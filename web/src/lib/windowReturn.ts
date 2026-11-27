// What the app shows but doesn't follow live — the people, a page's site,
// its comments, the unread dots — is read again when the person comes back
// to the app, for what changed while they were away.

// A return within this of the last read reads nothing more.
const RETURN_REST_MS = 30_000;

/**
 * Calls `read` when the app comes back: its tab back in view or its window
 * back to the front. A return reads once, though a window shown and focused
 * says so twice, and not within half a minute of the last read or of the
 * start of following, so resizing a window or moving between windows reads
 * nothing more (each such return read every list again, a round of about ten
 * requests).
 */
export function onWindowReturn(read: () => void): () => void {
  if (typeof window === "undefined" || typeof document === "undefined") return () => {};
  let lastRead = Date.now();
  const back = () => {
    if (document.visibilityState !== "visible") return;
    const now = Date.now();
    if (now - lastRead < RETURN_REST_MS) return;
    lastRead = now;
    read();
  };
  document.addEventListener("visibilitychange", back);
  window.addEventListener("focus", back);
  return () => {
    document.removeEventListener("visibilitychange", back);
    window.removeEventListener("focus", back);
  };
}
