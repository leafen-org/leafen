import { CodeIcon, FileText, PdfRowIcon, SheetRowIcon, SlidesRowIcon, TableIcon } from "@/icons/leafen";
import { documentKindOf } from "@/lib/documentKinds";

/** A file row's kind: a document's, else a code file's or a table's (fileRowKindOf). */
export type FileRowKind = NonNullable<ReturnType<typeof documentKindOf>> | "code" | "table";

const CODE_TYPES = new Set(["application/json", "text/html"]);
const TABLE_TYPES = new Set(["text/csv"]);

/**
 * A file row's kind, uploaded or linked: a document's by its type or name
 * (documentKindOf); else, as Notion's file row marks them (seen 2026-11-12,
 * one file of each type in a dark page), a JSON or HTML file is code and a
 * CSV file a table, by its type or, stored without one, its name; JavaScript,
 * Python, CSS, XML, Markdown, text and the rest are none.
 */
export function fileRowKindOf(name: string, contentType: string): FileRowKind | null {
  const document = documentKindOf(name, contentType);
  if (document) return document;
  const type = contentType.split(";")[0].trim().toLowerCase();
  const typeless = !type || type === "application/octet-stream";
  if (CODE_TYPES.has(type) || typeless && /\.(?:json|html?)$/iu.test(name.trim())) return "code";
  if (TABLE_TYPES.has(type) || typeless && /\.csv$/iu.test(name.trim())) return "table";
  return null;
}

/**
 * A file row's mark by its kind, uploaded or linked: Notion's row marks a PDF
 * with its own "PDF" page (seen 2026-11-05; a linked one too, 2026-11-06), a
 * sheet and slides with theirs, a code file with "</>" and a CSV file with a
 * table (seen 2026-11-12); another document is a page.
 */
export function fileKindIcon(kind: FileRowKind | null, otherwise: typeof FileText) {
  return kind === "xlsx" ? SheetRowIcon : kind === "pptx" ? SlidesRowIcon : kind === "pdf" ? PdfRowIcon
    : kind === "code" ? CodeIcon : kind === "table" ? TableIcon : kind ? FileText : otherwise;
}
