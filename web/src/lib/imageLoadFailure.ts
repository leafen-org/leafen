import { useEffect, useState } from "react";

// Whether a picture couldn't be loaded, for every place that draws one; each
// place draws its own failure as Notion's same place does.

/**
 * The image's address, and whether that address failed to load or the
 * server refused to give one (`refused`, its error: a file gone or damaged).
 */
export function useImageLoadFailure(url: string, refused = 0) {
  const [failedUrl, setFailedUrl] = useState("");
  return { failed: refused > 0 || (url !== "" && failedUrl === url), fail: () => setFailedUrl(url) };
}

/**
 * Whether a picture drawn as a box's background failed to load: a background
 * can't say so, so the same address is asked for as an image, which the
 * browser answers from what it already has.
 */
export function useBackgroundImageFailure(url: string | undefined, refused = 0) {
  const [failedUrl, setFailedUrl] = useState("");
  useEffect(() => {
    if (!url || url.startsWith("linear-gradient")) return;
    let live = true;
    const probe = new Image();
    probe.onerror = () => { if (live) setFailedUrl(url); };
    probe.src = url;
    return () => {
      live = false;
      probe.onerror = null;
    };
  }, [url]);
  return refused > 0 || (Boolean(url) && failedUrl === url);
}
