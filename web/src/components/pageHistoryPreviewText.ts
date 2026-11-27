import { formatFileSize } from "@/components/database/files";
import type { TextSpan } from "@/lib/types";
import type { PageHistoryBlock } from "@/pageHistoryWire.generated";

function contentValue(content: Record<string, unknown>, key: string) {
  const value = content[key];
  return typeof value === "string" ? value.trim() : "";
}

// The blocks that hold a file, shown by its name (2026-10-16: they read as
// empty blocks).
export const FILE_BLOCK_TYPES = new Set(["file", "image", "pdf", "video", "audio"]);

function spansText(value: unknown) {
  return Array.isArray(value)
    ? value.map((span) => typeof span === "object" && span !== null && "text" in span ? String(span.text ?? "") : "").join("").trim()
    : "";
}

/** A simple table row's cells, each as its text. */
export function tableRowPreviewCells(block: PageHistoryBlock): string[] {
  const cells = block.content.cells;
  return Array.isArray(cells) ? cells.map(spansText) : [];
}

/** A version's block as one line of text for its preview. */
export function blockPreviewText(block: PageHistoryBlock) {
  const rich = block.content.rich;
  if (Array.isArray(rich)) {
    const text = rich
      .map((span) => typeof span === "object" && span !== null && "text" in span
        ? String(span.text ?? "")
        : "")
      .join("")
      .trim();
    if (text) return text;
  }
  if (block.plainText.trim()) return block.plainText.trim();
  if (FILE_BLOCK_TYPES.has(block.type)) {
    const name = contentValue(block.content, "name") || spansText(block.content.caption);
    if (name) return name;
    const url = contentValue(block.content, "url");
    if (url) return decodeURIComponent(url.split(/[?#]/u)[0].split("/").filter(Boolean).at(-1) ?? url);
  }
  // An HTML block's uploaded file and an app's block go by their stored name.
  for (const key of ["expression", "buttonLabel", "childPageTitle", "fileName", "name", "url"]) {
    const value = contentValue(block.content, key);
    if (value) return value;
  }
  const table = block.content.table;
  if (Array.isArray(table)) {
    return table.flat().map(String).map((cell) => cell.trim()).filter(Boolean).slice(0, 8).join(" · ");
  }
  return "";
}

/** A file block's size as its row on the page reads it, or "" without one. */
export function fileBlockSize(block: PageHistoryBlock) {
  if (!FILE_BLOCK_TYPES.has(block.type)) return "";
  const size = block.content.size;
  return typeof size === "number" && Number.isFinite(size) && size >= 0 ? formatFileSize(size) : "";
}

/** A version's block's text with its formatting, mentions and equations, where the history kept them. */
export function blockPreviewSpans(block: PageHistoryBlock): TextSpan[] | null {
  const rich = block.content.rich;
  if (!Array.isArray(rich) || rich.length === 0) return null;
  const spans = rich.filter((span): span is TextSpan =>
    typeof span === "object" && span !== null && typeof (span as TextSpan).text === "string");
  return spans.length === rich.length ? spans : null;
}

/**
 * The spans from one place to another of the trimmed text the preview reads.
 * A mention or an equation is one piece: it goes whole to the part it starts in.
 */
export function previewSpansBetween(spans: readonly TextSpan[], from: number, to: number): TextSpan[] {
  const text = spans.map((span) => span.text).join("");
  const start = text.length - text.trimStart().length;
  const low = start + from, high = start + to;
  const result: TextSpan[] = [];
  let offset = 0;
  for (const span of spans) {
    const length = span.text.length;
    if (span.mention || span.equation !== undefined) {
      if (offset >= low && offset < high) result.push(span);
    } else {
      const cut = Math.max(offset, low), end = Math.min(offset + length, high);
      if (end > cut) result.push({ ...span, text: span.text.slice(cut - offset, end - offset) });
    }
    offset += length;
  }
  return result;
}
