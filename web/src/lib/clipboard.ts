import { clipboardHtmlFragment } from "./clipboardHtml";
import { copyText as copyPlainText } from "./copyText";

export const LEAFEN_BLOCKS_MIME = "application/x-leafen-blocks";
export const LEAFEN_TABLE_ROWS_MIME = "application/x-leafen-table-rows";
/** The page editor's own HTML of words copied over lines, which a paste in a page takes back as they were. */
export const LEAFEN_WORDS_MIME = "application/x-leafen-words";
const CLIPBOARD_MIME_ATTRIBUTE = "data-leafen-clipboard-mime";
const CLIPBOARD_PAYLOAD_ATTRIBUTE = "data-leafen-clipboard-payload";

export type ClipboardBounds = {
  maxTextCharacters: number;
  maxHtmlCharacters: number;
  maxEntries: number;
  maxEntryCharacters: number;
  maxTotalEntryCharacters: number;
};

type ClipboardEntry = [mime: string, data: string];

function validBound(value: number) {
  return Number.isSafeInteger(value) && value >= 0;
}

function validBounds(bounds: ClipboardBounds) {
  return validBound(bounds.maxTextCharacters)
    && validBound(bounds.maxHtmlCharacters)
    && validBound(bounds.maxEntries)
    && validBound(bounds.maxEntryCharacters)
    && validBound(bounds.maxTotalEntryCharacters);
}

function readHtmlClipboardData(
  data: DataTransfer,
  mime: string,
  bounds: ClipboardBounds,
) {
  if (typeof DOMParser === "undefined") return "";
  const rawHtml = data.getData("text/html");
  if (rawHtml.length > bounds.maxHtmlCharacters) return "";
  if (!rawHtml) return "";
  const document = clipboardHtmlFragment(rawHtml, bounds.maxHtmlCharacters);
  let matchedEntries = 0;
  for (const element of document.querySelectorAll<HTMLElement>(
    `[${CLIPBOARD_MIME_ATTRIBUTE}]`,
  )) {
    matchedEntries += 1;
    if (matchedEntries > bounds.maxEntries) return "";
    if (element.getAttribute(CLIPBOARD_MIME_ATTRIBUTE) !== mime) continue;
    const payload = element.getAttribute(CLIPBOARD_PAYLOAD_ATTRIBUTE) ?? "";
    return payload.length <= bounds.maxEntryCharacters ? payload : "";
  }
  return "";
}

export function readTextWithMime(
  data: DataTransfer,
  mime: string,
  bounds: ClipboardBounds,
) {
  if (!validBounds(bounds) || mime.length > bounds.maxEntryCharacters) return "";
  const html = readHtmlClipboardData(data, mime, bounds);
  if (html) return html;
  const value = data.getData(mime);
  return value.length <= bounds.maxEntryCharacters ? value : "";
}

// Within the bounds, plain text goes the one way the screens copy (copyText.ts).
export async function copyText(text: string, bounds: ClipboardBounds): Promise<boolean> {
  if (!validBounds(bounds) || !text || text.length > bounds.maxTextCharacters) return false;
  return copyPlainText(text);
}

function copyTextWithMimeEvent(text: string, entries: readonly ClipboardEntry[]) {
  if (typeof document === "undefined") return false;
  let wroteData = false;
  const onCopy = (event: ClipboardEvent) => {
    const clipboardData = event.clipboardData;
    if (!clipboardData) return;
    event.preventDefault();
    clipboardData.setData("text/plain", text);
    for (const [mime, data] of entries) {
      if (data) clipboardData.setData(mime, data);
    }
    wroteData = true;
  };
  document.addEventListener("copy", onCopy);
  try {
    return document.execCommand("copy") && wroteData;
  } finally {
    document.removeEventListener("copy", onCopy);
  }
}

function escapeHtmlAttribute(value: string) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function embedClipboardData(
  html: string,
  mime: string,
  data: string,
  bounds: ClipboardBounds,
) {
  if (!html || !mime || !data) return html;
  if (html.length > bounds.maxHtmlCharacters) return null;
  const markerIndex = html.indexOf("data-leafen-copy-id=");
  if (markerIndex < 0) return html;
  const tagEnd = html.indexOf(">", markerIndex);
  if (tagEnd < 0) return html;
  const attributes =
    ` ${CLIPBOARD_MIME_ATTRIBUTE}="${escapeHtmlAttribute(mime)}"` +
    ` ${CLIPBOARD_PAYLOAD_ATTRIBUTE}="${escapeHtmlAttribute(data)}"`;
  if (html.length + attributes.length > bounds.maxHtmlCharacters) return null;
  return `${html.slice(0, tagEnd)}${attributes}${html.slice(tagEnd)}`;
}

function admitClipboardEntries(
  mime: string,
  data: string,
  extraEntries: Record<string, string>,
  bounds: ClipboardBounds,
): ClipboardEntry[] | null {
  if (!validBounds(bounds)) return null;
  const entries: ClipboardEntry[] = [];
  const indexes = new Map<string, number>();
  let totalCharacters = 0;

  const admit = (key: string, value: string) => {
    if (
      key.length > bounds.maxEntryCharacters
      || value.length > bounds.maxEntryCharacters
      || (key === "text/html" && value.length > bounds.maxHtmlCharacters)
    ) return false;
    const existingIndex = indexes.get(key);
    const previousCharacters = existingIndex === undefined
      ? 0
      : entries[existingIndex][0].length + entries[existingIndex][1].length;
    const nextCharacters = totalCharacters - previousCharacters + key.length + value.length;
    if (!Number.isSafeInteger(nextCharacters)) return false;
    totalCharacters = nextCharacters;
    if (totalCharacters > bounds.maxTotalEntryCharacters) return false;
    if (existingIndex !== undefined) {
      entries[existingIndex] = [key, value];
      return true;
    }
    if (entries.length >= bounds.maxEntries) return false;
    indexes.set(key, entries.length);
    entries.push([key, value]);
    return true;
  };

  for (const key in extraEntries) {
    if (!Object.prototype.hasOwnProperty.call(extraEntries, key)) continue;
    if (!admit(key, extraEntries[key])) return null;
  }
  if (!admit(mime, data)) return null;

  const htmlIndex = indexes.get("text/html");
  if (htmlIndex !== undefined && data) {
    const embedded = embedClipboardData(entries[htmlIndex][1], mime, data, bounds);
    if (embedded === null || !admit("text/html", embedded)) return null;
  }
  return entries;
}

export async function copyTextWithMime(
  text: string,
  mime: string,
  data: string,
  extraEntries: Record<string, string>,
  bounds: ClipboardBounds,
): Promise<boolean> {
  if (text.length > bounds.maxTextCharacters) return false;
  const entries = admitClipboardEntries(mime, data, extraEntries, bounds);
  if (!entries || (!text && !entries.some(([, value]) => Boolean(value)))) return false;
  // The copy event inside the key press or click writes every entry at once,
  // as Notion's copy does. Chrome on macOS never settles an asynchronous write
  // that carries a "web " custom format (seen 2026-11-01, Chrome 147 and 156)
  // and a stuck write holds back the later ones, so the asynchronous fallback
  // writes only the text and the HTML that embeds the payload.
  if (copyTextWithMimeEvent(text, entries)) return true;
  if (
    typeof navigator !== "undefined" &&
    navigator.clipboard?.write &&
    typeof ClipboardItem !== "undefined" &&
    typeof Blob !== "undefined"
  ) {
    const html = entries.find(([entryMime]) => entryMime === "text/html")?.[1];
    if (html) {
      try {
        await navigator.clipboard.write([
          new ClipboardItem({
            "text/plain": new Blob([text], { type: "text/plain" }),
            "text/html": new Blob([html], { type: "text/html" }),
          }),
        ]);
        return true;
      } catch {
        // A plain-text fallback below still replaces any older clipboard data.
      }
    }
  }
  return copyText(text, bounds);
}

export async function copyTextWithBlocks(
  text: string,
  blocksJson: string,
  html: string | undefined,
  bounds: ClipboardBounds,
): Promise<boolean> {
  return copyTextWithMime(
    text,
    LEAFEN_BLOCKS_MIME,
    blocksJson,
    html ? { "text/html": html } : {},
    bounds,
  );
}

/** Native block selections are copied through a clipboard event: the
 * browser's own Copy, or the one ⌘C raises through `copyTextWithMime`. */
export function writeTextWithBlocks(target: DataTransfer, text: string, blocksJson: string,
  html: string | undefined, bounds: ClipboardBounds) {
  return writeTextWithMime(target, text, LEAFEN_BLOCKS_MIME, blocksJson, html, bounds);
}

/** Writes text, HTML and Leafen's own data, which the HTML also carries, in a clipboard event. */
export function writeTextWithMime(target: DataTransfer, text: string, mime: string, data: string,
  html: string | undefined, bounds: ClipboardBounds) {
  if (text.length > bounds.maxTextCharacters) return false;
  const entries = admitClipboardEntries(mime, data, html ? { "text/html": html } : {}, bounds);
  if (!entries || (!text && !entries.some(([, value]) => Boolean(value)))) return false;
  try {
    target.setData("text/plain", text);
    for (const [type, value] of entries) if (value) target.setData(type, value);
    return true;
  } catch { return false; }
}
