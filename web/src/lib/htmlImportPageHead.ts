import { importMarkPart } from "./htmlImportCallouts";
import type { ImportReferenceMap } from "./importReferences";

/**
 * An exported page's icon and cover, which Notion's HTML export and Leafen's
 * write at the head of the page's header (seen 2026-11-14): the cover a
 * picture of the "page-cover-image" class moved by its object-position, the
 * icon in a "page-header-icon" box — an emoji drawn from its attribute, a
 * built-in icon a picture marked a static icon that the article names by its
 * path in Notion's icon set (data-notion-page-icon="/icons/rocket_purple.svg"),
 * or a picture, linked from the web or kept in the export as the file it was
 * uploaded as. Brought back they are the page's, not lines of its body: the
 * header loses them (withoutPageHead), and a line opening the page's Markdown
 * names them (pageHeadLine), which the importer gives the page while it has
 * none (workspace_import_page_head.go).
 */

const PAGE_HEAD = "article.page > header";
const BUILT_IN_PATH = /^\/icons\/[a-z0-9-]+_[a-z]+\.svg$/;
const BUILT_IN_ADDRESS = /^https:\/\/(?:app\.notion\.com|(?:www\.)?notion\.so)(\/icons\/[a-z0-9-]+_[a-z]+\.svg)$/;

/** The page line an exported page's header gives, "" for none. */
export function pageHeadLine(html: string, references?: ImportReferenceMap) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const header = document.querySelector(PAGE_HEAD);
  if (!header) return "";
  const box = header.querySelector(":scope > .page-header-icon");
  const icon = box ? headerIcon(box, header.parentElement!, references) : "";
  const picture = header.querySelector(":scope > img.page-cover-image");
  const cover = picture ? pictureName(picture, references) : "";
  if (!icon && !cover) return "";
  return `[](leafen-page-head:${importMarkPart(icon)},${importMarkPart(cover)},${coverPlace(picture)})`;
}

/** The document without its page's icon and cover, which pageHeadLine names. */
export function withoutPageHead(document: Document) {
  document.querySelectorAll(`${PAGE_HEAD} > .page-header-icon, ${PAGE_HEAD} > img.page-cover-image`).forEach((part) => part.remove());
}

function headerIcon(box: Element, article: Element, references?: ImportReferenceMap) {
  const emoji = box.querySelector("[data-emoji]")?.getAttribute("data-emoji")?.trim();
  if (emoji) return emoji;
  const picture = box.querySelector("img");
  if (!picture) return (box.textContent ?? "").trim();
  if (picture.classList.contains("notion-static-icon")) {
    const named = article.getAttribute("data-notion-page-icon")?.trim() ?? "";
    if (BUILT_IN_PATH.test(named)) return named;
    return BUILT_IN_ADDRESS.exec(picture.getAttribute("src")?.trim() ?? "")?.[1] ?? "";
  }
  return pictureName(picture, references);
}

/** A picture's name: one the import brings, else its web address. */
function pictureName(picture: Element, references?: ImportReferenceMap) {
  const source = picture.getAttribute("src")?.trim() ?? "";
  const kept = references?.get(source);
  if (kept && kept.kind !== "page") return `leafen-file:${kept.fileId}`;
  return /^https?:\/\/\S+$/i.test(source) ? source : "";
}

/** Where a cover is moved to, 0–100 from its top (object-position: center 10.73%); 50 for none. */
function coverPlace(picture: Element | null) {
  const place = /object-position\s*:\s*\S+\s+(-?\d+(?:\.\d+)?)%/i.exec(picture?.getAttribute("style") ?? "");
  return place ? Math.min(100, Math.max(0, Math.round(Number(place[1])))) : 50;
}

/** The pictures an exported page's header shows, which pageHeadLine names as the import brings them. */
export function pageHeadHrefs(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  return Array.from(document.querySelectorAll(`${PAGE_HEAD} > .page-header-icon img[src], ${PAGE_HEAD} > img.page-cover-image[src]`))
    .map((picture) => picture.getAttribute("src")!.trim());
}
