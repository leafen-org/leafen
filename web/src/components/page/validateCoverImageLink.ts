/** A submitted image link must load as an image before it replaces a cover.
 * This reads directly in the browser, never through the server's credentials. */
export function validateCoverImageLink(url: string, signal: AbortSignal) {
  return new Promise<boolean>(resolve => {
    const image = new Image();
    image.referrerPolicy = "no-referrer";
    let settled = false;
    const finish = (valid: boolean) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      signal.removeEventListener("abort", cancel);
      image.onload = null; image.onerror = null;
      image.src = "";
      resolve(valid);
    };
    const cancel = () => finish(false);
    const timer = window.setTimeout(cancel, 7000);
    image.onload = () => finish(image.naturalWidth > 0 && image.naturalHeight > 0);
    image.onerror = cancel;
    signal.addEventListener("abort", cancel, { once: true });
    if (signal.aborted) cancel(); else image.src = url;
  });
}
