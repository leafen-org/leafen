import createDOMPurify, { type DOMPurify } from "dompurify";

let purifier: DOMPurify | undefined;
// Only these CSS values are consumed by native rich-text conversion, the
// size by a paste's choice of its HTML over its text (pastedHtmlChoice.ts)
// and the font family by Notion's code tokens (richtextHtmlRead.ts).
// Resource, layout and executable CSS never reach PM's temporary clipboard DOM.
const textStyles = ["font-weight", "font-style", "text-decoration", "text-decoration-line",
  "color", "background-color", "display", "white-space", "font-size", "font-family"];

/**
 * Whether pasted words in this font size are big type, as Notion's paste
 * counts it (seen 2026-11-13): its whole number of px or pt 24 and up, or of
 * em or rem over 2 — 24px, 24pt and 3em are, 23.9pt, 18pt, 2.5em, 2rem and
 * "xx-large" not. Big words read bold, all of a block's a heading.
 */
export function bigFontSize(fontSize: string) {
  const size = Number.parseInt(fontSize, 10);
  return /p[xt]/u.test(fontSize) ? size >= 24 : fontSize.includes("em") && size > 2;
}

function clipboardPurifier() {
  if (purifier) return purifier;
  if (typeof window === "undefined") throw new RangeError("HTML clipboard parsing requires a supported browser.");
  const instance = createDOMPurify(window);
  if (!instance.isSupported) throw new RangeError("HTML clipboard sanitization is unavailable.");
  instance.addHook("uponSanitizeAttribute", (node, attribute) => {
    if (attribute.attrName !== "style" || !(node instanceof HTMLElement)) return;
    const style = document.createElement("span").style;
    for (const name of textStyles) {
      const value = node.style.getPropertyValue(name);
      if (value && !/[\\@]|url\s*\(|expression\s*\(|var\s*\(/iu.test(value)) style.setProperty(name, value);
    }
    attribute.attrValue = style.cssText;
    attribute.keepAttr = Boolean(attribute.attrValue);
  });
  purifier = instance;
  return instance;
}

/** Normalize legacy metadata before sanitization. Consumers inspect this
 * detached fragment or serialize it unchanged for PM; no external HTML is
 * attached, concatenated, or rewritten after the security boundary. Native
 * converters still own URL/reference admission and node/depth budgets. */
export function clipboardHtmlFragment(html: string, maxCharacters: number): DocumentFragment {
  if (!Number.isSafeInteger(maxCharacters) || maxCharacters < 0 || html.length > maxCharacters) {
    throw new RangeError("HTML clipboard exceeds its caller-owned bound.");
  }
  return clipboardPurifier().sanitize(html, {
    USE_PROFILES: { html: true }, RETURN_DOM_FRAGMENT: true,
    FORBID_TAGS: ["style", "iframe", "object", "embed", "link", "meta", "base", "audio", "video", "source", "picture"],
    FORBID_ATTR: ["id", "name", "srcset", "form", "autofocus"],
  });
}

export function sanitizedClipboardHtml(html: string, maxCharacters: number) {
  const fragment = clipboardHtmlFragment(html, maxCharacters);
  const container = document.createElement("div");
  container.append(fragment);
  return container.innerHTML;
}

/** PM parses before invoking field handlePaste, even for plain-only fields.
 * A refused rich representation must never fall back to the raw HTML parser.
 * Body admission reports bounds first; field transactions retain their own
 * text budgets and save policy. */
export function clipboardHtmlForEngine(html: string, maxCharacters: number) {
  try { return sanitizedClipboardHtml(html, maxCharacters); }
  catch (error) { if (error instanceof RangeError) return ""; throw error; }
}
