// The ring a link of a page's text takes from a finger's long press
// (textLongPress.ts), as the Notion Android app rings it (seen 2026-11-25 on
// a Galaxy A90, seven times, the text written in or not): the keys'
// focus ring round each line of the link's words (#2383e2 2px, 2px out, its
// corners about 6 round outside), staying with the words as the page
// scrolls, until the next press or key. The text is not a field the focus
// can rest on there, so the ring is drawn over it rather than by the focus.

let shown: { link: HTMLAnchorElement; rings: HTMLElement[]; stop: () => void } | null = null;

/** Takes the ring off, wherever it is. */
export function unringPressedLink() {
  const was = shown;
  shown = null;
  was?.stop();
  for (const ring of was?.rings ?? []) ring.remove();
}

/** Rings `link` until the next press or key, or until it leaves the page. */
export function ringPressedLink(link: HTMLAnchorElement) {
  unringPressedLink();
  const doc = link.ownerDocument, rings: HTMLElement[] = [];
  const place = () => {
    if (!link.isConnected) { unringPressedLink(); return; }
    const lines = [...link.getClientRects()].filter(line => line.width > 0 && line.height > 0);
    while (rings.length > lines.length) rings.pop()?.remove();
    lines.forEach((line, index) => {
      let ring = rings[index];
      if (!ring) {
        ring = doc.createElement("div");
        ring.dataset.pressedLinkRing = "";
        ring.setAttribute("aria-hidden", "true");
        ring.style.cssText = "position:fixed;pointer-events:none;z-index:1;border-radius:2px;box-shadow:var(--keyboard-focus-ring)";
        doc.body.append(ring);
        rings.push(ring);
      }
      Object.assign(ring.style, { left: `${line.left}px`, top: `${line.top}px`, width: `${line.width}px`, height: `${line.height}px` });
    });
  };
  const away = () => unringPressedLink();
  doc.addEventListener("scroll", place, { capture: true, passive: true });
  window.addEventListener("resize", place);
  doc.addEventListener("pointerdown", away, true);
  doc.addEventListener("keydown", away, true);
  const stop = () => {
    doc.removeEventListener("scroll", place, { capture: true });
    window.removeEventListener("resize", place);
    doc.removeEventListener("pointerdown", away, true);
    doc.removeEventListener("keydown", away, true);
  };
  shown = { link, rings, stop };
  place();
}
