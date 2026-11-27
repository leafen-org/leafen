import { readImportText } from "./importText";

export type MarkdownImportFile = Pick<File, "name" | "size" | "text" | "type" | "arrayBuffer">;

/**
 * A file's name as a page's title: without its extension, or the ID an export
 * adds after the title, a Notion export's 32 characters, a Leafen export's 15
 * of letters and digits both or its data source's UUID, so a page exported and brought back keeps its title.
 * In an export (`exported`) any 15 letters and digits are the ID, capitals
 * too: a row or database a CSV made has them (2026-11-07).
 */
export function titleFromFileName(name: string, untitled: string, exported = false) {
  return name.replace(/\.[^.]+$/, "").replace(/\s+[0-9a-f]{32}$/i, "")
    .replace(/\s+[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i, "")
    .replace(exported ? /\s+[a-z0-9]{15}$/i : /\s+(?=[a-z]*\d)(?=\d*[a-z])[a-z0-9]{15}$/, "").trim() || untitled;
}

/**
 * Whether names are an export's, each ending in its page's ID: a Notion
 * export's 32 characters or UUID, or a Leafen export's 15, which may then be
 * letters alone (2026-11-05: about one page in 130 kept its ID as words).
 */
export function exportedFileNames(names: readonly string[]) {
  return names.length > 0 && names.every(namedWithExportId)
    && names.some((name) => /\s(?:[0-9a-f]{32}|[0-9a-f-]{36}|(?=[a-z]*\d)(?=\d*[a-z])[a-z0-9]{15})$/i.test(fileStem(name)));
}

/** Whether a file's name ends in an ID as an export names its pages (exportedFileNames). */
export function namedWithExportId(name: string) {
  return /\s(?:[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[a-z0-9]{15})$/i.test(fileStem(name));
}

function fileStem(name: string) {
  return (name.split("/").pop() ?? name).replace(/\.[^.]+$/, "");
}

export function pageMarkdownImportFormat(
  file: Pick<MarkdownImportFile, "name" | "type">,
): "csv" | "markdown" {
  return /\.csv$/i.test(file.name)
    || file.type === "text/csv"
    || file.type === "application/csv"
    ? "csv"
    : "markdown";
}

export function utf8ByteLength(value: string) {
  return new TextEncoder().encode(value).byteLength;
}

export async function readBoundedMarkdownImportText(
  file: Pick<MarkdownImportFile, "size" | "arrayBuffer">,
  maxBytes: number,
): Promise<{ ok: true; text: string } | { ok: false; reason: "size" }> {
  if (!Number.isSafeInteger(maxBytes) || maxBytes < 0) {
    throw new RangeError("Markdown import requires a caller-owned nonnegative safe-integer byte bound.");
  }
  if (!Number.isSafeInteger(file.size) || file.size < 0 || file.size > maxBytes) {
    return { ok: false, reason: "size" };
  }
  const text = await readImportText(file);
  return utf8ByteLength(text) <= maxBytes
    ? { ok: true, text }
    : { ok: false, reason: "size" };
}
