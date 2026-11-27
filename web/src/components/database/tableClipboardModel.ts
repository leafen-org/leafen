import type { GraphRowValue } from "@/contentGraph";
import type { ClipboardBounds } from "@/lib/clipboard";
import { clipboardServer, otherClipboardServer, otherServerSpans, withoutThreadMarks } from "@/lib/clipboardServer";
import type { TextSpan } from "@/lib/types";

export type TableClipboardPayload = {
  databaseId: string;
  rowIds: string[];
  /** Each cell as the table shows it, the plain text any app reads. */
  values: string[][];
  /** The formatting of title and text values, cell by cell, for a paste in Leafen. */
  spans?: (TextSpan[] | null)[][];
  /** The columns' properties and the cells' kept values, for a paste in Leafen into the same column. */
  propertyIds?: string[];
  kept?: (GraphRowValue | null)[][];
};

// Copied cells carry their formatting in Leafen's own clipboard entry; any
// other app reads their plain text.
export const TABLE_CLIPBOARD_BOUNDS = {
  maxTextCharacters: 1_000_000,
  maxHtmlCharacters: 4_000_000,
  maxEntries: 4,
  maxEntryCharacters: 2_000_000,
  maxTotalEntryCharacters: 2_000_000,
} as const satisfies ClipboardBounds;

/** Leafen's own entry of copied cells: their formatting and kept values, and the server whose they are. */
export function tableClipboardEntry(payload: TableClipboardPayload) {
  return JSON.stringify({ version: 2, spans: payload.spans ?? [], propertyIds: payload.propertyIds ?? [], kept: payload.kept ?? [],
    server: clipboardServer() });
}

/**
 * The value a cell copied in Leafen keeps when pasted into the same column
 * of the same server: an option's, a person's or a page's own id rather than
 * its name; its words with no comment or suggested-edit thread
 * (withoutThreadMarks).
 */
export function tableClipboardCellKept(entry: string, row: number, column: number, propertyId: string): GraphRowValue | undefined {
  if (!entry) return undefined;
  try {
    const parsed = JSON.parse(entry) as { version?: number; propertyIds?: unknown; kept?: unknown; server?: unknown };
    if (parsed.version !== 2 || !Array.isArray(parsed.propertyIds) || parsed.propertyIds[column] !== propertyId || !Array.isArray(parsed.kept)
      || otherClipboardServer(parsed.server) !== null) return undefined;
    const cell = (parsed.kept[row] as unknown[] | undefined)?.[column];
    if (!cell || typeof cell !== "object" || typeof (cell as GraphRowValue).type !== "string") return undefined;
    const { spans } = cell as GraphRowValue;
    return Array.isArray(spans) ? { ...cell as GraphRowValue, spans: withoutThreadMarks(spans) } : cell as GraphRowValue;
  } catch {
    return undefined;
  }
}

/**
 * The formatting a pasted cell keeps: spans that still spell its value,
 * naming another server's pages by links to them there (otherServerSpans),
 * with no comment or suggested-edit thread (withoutThreadMarks).
 */
export function tableClipboardCellSpans(entry: string, row: number, column: number, value: string): TextSpan[] | undefined {
  if (!entry) return undefined;
  try {
    const parsed = JSON.parse(entry) as { version?: number; spans?: unknown; server?: unknown };
    const other = otherClipboardServer(parsed.server);
    if ((parsed.version !== 1 && parsed.version !== 2) || !Array.isArray(parsed.spans) || other === undefined) return undefined;
    const cell = (parsed.spans[row] as unknown[] | undefined)?.[column];
    if (!Array.isArray(cell) || cell.length === 0) return undefined;
    const spans = cell.filter((span): span is TextSpan => typeof span === "object" && span !== null &&
      typeof (span as TextSpan).text === "string");
    if (spans.length !== cell.length || spans.map((span) => span.text).join("") !== value) return undefined;
    return other ? otherServerSpans(spans, other) : withoutThreadMarks(spans);
  } catch {
    return undefined;
  }
}

function quoteTsv(value: string) {
  return /[\t\n"]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

export function serializeTableClipboard(payload: TableClipboardPayload) {
  return {
    ...payload,
    text: payload.values.map((row) => row.map(quoteTsv).join("\t")).join("\n"),
  };
}

export function parseTableClipboard(text: string) {
  const rows: string[][] = [[]];
  let value = "";
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (character === '"') {
      if (quoted && text[index + 1] === '"') {
        value += '"';
        index += 1;
      } else {
        quoted = !quoted;
      }
      continue;
    }
    if (!quoted && (character === "\t" || character === "\n" || character === "\r")) {
      rows.at(-1)!.push(value);
      value = "";
      if (character === "\r" && text[index + 1] === "\n") index += 1;
      if (character !== "\t") rows.push([]);
      continue;
    }
    value += character;
  }
  rows.at(-1)!.push(value);
  if (rows.length > 1 && rows.at(-1)?.length === 1 && rows.at(-1)?.[0] === "") rows.pop();
  return rows;
}
