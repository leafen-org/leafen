import { htmlBlockColor, notionColorName } from "./htmlImportColors";
import type { ImportReferenceMap } from "./importReferences";

/**
 * A callout in an imported HTML document, as an export draws one: Notion's
 * an aside marked data-notion-callout (its icon, color and box, seen
 * 2026-11-13), which Notion's HTML import reads as a callout in any
 * document, and inside an export (Notion's or Leafen's, whose page is an
 * article holding the page's body) a box of the "callout" class — Notion's
 * figure of earlier exports, Leafen's figure holding a .callout-icon and a
 * .callout-body. Its icon is the emoji, the built-in icon's /icons/ path or
 * the picture's web address the export names, or a picture kept in the
 * export itself as the file it was uploaded as (leafen-file:, references);
 * none leaves it without one. Its color is the box's (htmlImportColors.ts).
 */
export type HtmlCallout = { icon: string; color: string; body: Node[] };

export function htmlCallout(element: Element, exported: boolean, references?: ImportReferenceMap): HtmlCallout | null {
  if (!element.hasAttribute("data-notion-callout") && !(exported && element.classList.contains("callout"))) return null;
  const leafenBody = element.querySelector(":scope > .callout-body");
  const iconBox = Array.from(element.children).find((part) => part.matches(".callout-icon") || part.querySelector(":scope > .icon"));
  const iconOf = (box: Element) => {
    const holder = box.matches(".callout-icon") ? box : box.querySelector(":scope > .icon")!;
    const picture = holder.matches("img") ? holder : holder.querySelector("img");
    const source = picture?.getAttribute("src")?.trim() ?? "";
    const kept = picture ? references?.get(source) : undefined;
    if (kept && kept.kind === "image") return `leafen-file:${kept.fileId}`;
    if (picture) return /^https?:\/\/\S+$/i.test(source) ? source : "";
    return (holder.getAttribute("data-emoji") ?? holder.textContent ?? "").trim();
  };
  const icon = element.getAttribute("data-notion-callout-icon")?.trim() || (iconBox ? iconOf(iconBox) : "");
  const named = element.getAttribute("data-notion-callout-background");
  const color = named === null ? htmlBlockColor(element) : notionColorName(named);
  // Notion's box holds its icon's box, then its words' (the last box).
  const box = leafenBody ?? Array.from(element.children).filter((part) => part.tagName === "DIV" && part !== iconBox).pop();
  const body = box ? Array.from(box.childNodes) : Array.from(element.childNodes).filter((part) => part !== iconBox);
  return { icon, color, body };
}

/** Each part of a mark's address, written so Markdown reads the address whole. */
export function importMarkPart(value: string) {
  return encodeURIComponent(value).replace(/[!'()*]/g, (character) => `%${character.charCodeAt(0).toString(16).toUpperCase()}`);
}

/**
 * The mark opening the list item the importer reads as the callout: its
 * color and its icon (workspace_import_markdown_marks.go).
 */
export function calloutMark(callout: HtmlCallout) {
  return `[](leafen-callout:${importMarkPart(callout.color)},${importMarkPart(callout.icon)})`;
}
