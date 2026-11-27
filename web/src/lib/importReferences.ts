import { escapeImportMarkdown, importParagraphText } from "./importMarkdownText";

/**
 * References between documents imported together. A document's relative
 * link to another imported document becomes a page mention, and its
 * relative image or file becomes an uploaded workspace file: the importer
 * reads [text](leafen-page:<page ID>) as a mention, a line holding only
 * ![alt](leafen-file:<file ID>) or [name](leafen-file:<file ID>) as an image
 * or file block, and a line holding only [title](leafen-subpage:<page ID>) as
 * the place of the document's own sub-page.
 */

export type ImportReference =
  // `sub`: the document's own sub-page, which a line naming it alone puts there.
  | { kind: "page"; pageId: string; sub?: boolean }
  | { kind: "image" | "file"; fileId: string; name: string };

export type ImportReferenceMap = ReadonlyMap<string, ImportReference>;

export type ImportAssetUploader = (name: string, bytes: Uint8Array | Blob) => Promise<ImportReference | null>;

/** The names of a document's files that weren't brought in (not readable,
 * or refused on upload), for its import to report. */
export type ImportLeftOut = Set<string>;

const CONTENT_TYPES: Record<string, string> = {
  png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", bmp: "image/bmp",
  avif: "image/avif", heic: "image/heic", pdf: "application/pdf", mp4: "video/mp4", mov: "video/quicktime",
  webm: "video/webm", mp3: "audio/mpeg", wav: "audio/wav", m4a: "audio/mp4", zip: "application/zip",
  csv: "text/csv", txt: "text/plain", docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
};

export function importAssetContentType(name: string) {
  return CONTENT_TYPES[name.split(".").pop()?.toLowerCase() ?? ""] ?? "application/octet-stream";
}

/** A link written relative to the document, not a web, mail, data or in-page address. */
export function isRelativeImportHref(href: string) {
  const value = href.trim();
  return Boolean(value) && !/^[a-z][a-z0-9+.-]*:/i.test(value) && !value.startsWith("#") && !value.startsWith("//");
}

/** Resolves a relative link against a directory inside an archive. */
export function resolveArchivePath(directory: string, href: string): string | null {
  let path = href.trim().split("#")[0]!.split("?")[0]!;
  try {
    path = decodeURIComponent(path);
  } catch {
    // A malformed escape is read as written.
  }
  const parts = path.startsWith("/") ? [] : directory.split("/").filter(Boolean);
  for (const part of path.split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      if (parts.length === 0) return null;
      parts.pop();
    } else parts.push(part);
  }
  return parts.length ? parts.join("/") : null;
}

/** A page reference's address: a mention, or the place of the document's own sub-page. */
export function pageReference(reference: Extract<ImportReference, { kind: "page" }>) {
  return `${reference.sub ? "leafen-subpage" : "leafen-page"}:${reference.pageId}`;
}

/** The importer's line for an uploaded image or file; an image's `words` show under it and are its alt text. */
export function importFileLine(reference: Extract<ImportReference, { fileId: string }>, text: string, words = "") {
  const label = escapeImportMarkdown(text.trim() || reference.name);
  if (reference.kind !== "image") return `[${label}](leafen-file:${reference.fileId})`;
  const title = words.trim() ? ` "${words.trim().replace(/["\\]/g, "\\$&")}"` : "";
  return `![${label}](leafen-file:${reference.fileId}${title})`;
}

/**
 * A Markdown link or image: its words, and its address, which may hold
 * parentheses in pairs, as CommonMark reads it — an export names a second
 * page or database of a title "Name (1)" and writes its address with them as
 * they are (2026-11-13: such a link stayed as words and its database went
 * to the top of its page).
 */
const LINK = String.raw`(!?)\[((?:\\.|[^\]\\])*)\]\(\s*<?((?:[^()\s>]|\([^()\s>]*\))+)>?(?:\s+"[^"]*")?\s*\)`;

const LONE_REFERENCE = new RegExp(`^${LINK}$`);

const MARKDOWN_LINK = new RegExp(LINK, "g");

function markdownLines(markdown: string) {
  let fenced = false;
  return markdown.split("\n").map((line) => {
    const fence = /^\s*```/.test(line);
    const code = fenced || fence;
    if (fence) fenced = !fenced;
    return { line, code };
  });
}

/** The relative links and images a Markdown document refers to. */
export function markdownReferenceHrefs(markdown: string) {
  const hrefs = new Set<string>();
  for (const { line, code } of markdownLines(markdown)) {
    if (code) continue;
    for (const match of line.matchAll(MARKDOWN_LINK)) {
      if (isRelativeImportHref(match[3]!)) hrefs.add(match[3]!);
    }
  }
  return [...hrefs];
}

/**
 * Rewrites a Markdown document's relative links: another document becomes a
 * page mention, and an image or file moves to its own line after the text.
 */
export function rewriteMarkdownReferences(markdown: string, references: ImportReferenceMap) {
  if (references.size === 0) return markdown;
  return markdownLines(markdown).map(({ line, code }) => {
    if (code) return line;
    // A line holding one image or one file alone, as Notion's Markdown import
    // reads it (seen 2026-11-07): an image keeps its words as a paragraph
    // above it and under it as its caption, and a file is one block named as
    // the file, its link's words left out.
    const lone = LONE_REFERENCE.exec(line.trim());
    const reference = lone ? references.get(lone[3]!) : undefined;
    if (lone && reference && reference.kind !== "page" && Boolean(lone[1]) === (reference.kind === "image")) {
      const leading = /^\s*/.exec(line)?.[0] ?? "";
      if (reference.kind === "file") return `${leading}${importFileLine(reference, "")}`;
      const words = lone[2]!.replace(/\\(.)/g, "$1").trim();
      const image = `${leading}${importFileLine(reference, words, words)}`;
      return words ? `${leading}${importParagraphText(words)}\n\n${image}` : image;
    }
    const after: string[] = [];
    const rewritten = line.replace(MARKDOWN_LINK, (whole, image: string, text: string, href: string) => {
      const reference = references.get(href);
      if (!reference) return whole;
      if (reference.kind === "page") return image ? whole : `[${text}](${pageReference(reference)})`;
      after.push(importFileLine(reference, text.replace(/\\(.)/g, "$1")));
      return image ? "" : text;
    });
    if (after.length === 0) return rewritten;
    const leading = /^\s*/.exec(line)?.[0] ?? "";
    // A list item's files sit under the item.
    const indent = /^\s*(?:[-*+]|\d+[.)])\s/.test(line) ? `${leading}  ` : leading;
    const text = `${leading}${rewritten.slice(leading.length).replace(/ {2,}/g, " ")}`;
    return [text.trim() ? text : "", ...after.map((entry) => `${indent}${entry}`)]
      .filter((part, index) => index > 0 || part).join("\n\n");
  }).join("\n");
}
