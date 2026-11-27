import { type GraphRow } from "@/contentGraph";
import { ONE_CHANGE_CHARACTERS } from "@/lib/oneChange";
import type { Page, TextSpan } from "@/lib/types";
import { coalesce, splitSpans, type RichtextBounds } from "../editor/richtext";
import { textSpanColors } from "@/lib/textSpanColors";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";

// A database title or text value keeps its formatting, links, equations and
// mentions as spans beside its plain text, as a block caption does. The
// spans always spell the value; a value written without them is plain.

// A cell holds as much text, in as many pieces, as the server keeps for it
// (2026-10-13: it was held to 100 pieces and 32,768 characters, though the
// cell and the Notion-compatible API take more).
export const CELL_RICHTEXT_BOUNDS = {
  maxSpans: Number.MAX_SAFE_INTEGER,
  maxTextCharacters: ONE_CHANGE_CHARACTERS,
  maxHtmlCharacters: ONE_CHANGE_CHARACTERS,
  maxDomNodes: Number.MAX_SAFE_INTEGER,
  maxDomDepth: 16,
} as const satisfies RichtextBounds;

export type CellMark = "bold" | "italic" | "underline" | "strikethrough" | "code";

/** The spans a graph row keeps for its title and text values. */
export function graphRowPropertySpans(row: GraphRow): Record<string, TextSpan[]> | undefined {
  let spans: Record<string, TextSpan[]> | undefined;
  for (const [propertyId, cell] of Object.entries(row.values)) {
    if ((cell.type !== "title" && cell.type !== "rich_text") || !cell.spans?.length) continue;
    (spans ??= {})[propertyId] = cell.spans as TextSpan[];
  }
  return spans;
}

/** The spans a graph row keeps for its title. */
export function graphRowTitleSpans(row: GraphRow): TextSpan[] | undefined {
  const cell = Object.values(row.values).find((candidate) => candidate.type === "title");
  return cell?.spans?.length ? cell.spans as TextSpan[] : undefined;
}

/** The spans a cell shows for its value, or none when they no longer spell it. */
export function cellSpans(spans: readonly TextSpan[] | undefined, value: unknown): TextSpan[] | undefined {
  if (!spans?.length) return undefined;
  const text = value == null ? "" : String(value);
  return spans.map((span) => span.text).join("") === text ? [...spans] : undefined;
}

/** The spans a row keeps for one property's value. */
export function rowCellSpans(row: Page, propertyId: string, value: unknown) {
  return cellSpans(row.propertySpans?.[propertyId], value);
}

/** A cell's spans, or its plain text as one span. */
export function cellSpansOrText(spans: readonly TextSpan[] | undefined, value: unknown): TextSpan[] {
  const text = value == null ? "" : String(value);
  return cellSpans(spans, text) ?? (text ? [{ text }] : []);
}

function formatted(span: TextSpan) {
  return Object.entries(span).some(([key, value]) => key !== "text" && value !== undefined && value !== false && value !== "");
}

/**
 * What a cell stores for edited spans: its plain text, and the spans when any
 * carry formatting or a thread left on its words (a database cell's comment,
 * as Notion's).
 */
export function cellValueFromSpans(spans: readonly TextSpan[]): { value: string; spans?: TextSpan[] } {
  const kept = spans
    .map(({ unavailable: _unavailable, ...span }) => span)
    .filter((span) => span.text.length > 0);
  const value = kept.map((span) => span.text).join("");
  return kept.some(formatted) ? { value, spans: coalesce(kept, CELL_RICHTEXT_BOUNDS) } : { value };
}

/** An edited value without the spaces around it, as a comment keeps (a title keeps them, titleMerge.ts). */
export function trimCellChange(change: { value: string; spans?: TextSpan[] }): { value: string; spans?: TextSpan[] } {
  const value = change.value.trim();
  if (!change.spans || value === change.value) return { value, ...(change.spans ? { spans: change.spans } : {}) };
  const start = change.value.length - change.value.trimStart().length;
  const { middle } = splitRange(change.spans, start, start + value.length);
  return cellValueFromSpans(middle);
}

function splitRange(spans: readonly TextSpan[], start: number, end: number) {
  const [head, rest] = splitSpans([...spans], start, CELL_RICHTEXT_BOUNDS);
  const [middle, tail] = splitSpans(rest, Math.max(0, end - start), CELL_RICHTEXT_BOUNDS);
  return { head, middle, tail };
}

/** Turns a mark on across a range, or off when the whole range has it. */
export function toggleCellMark(spans: readonly TextSpan[], start: number, end: number, mark: CellMark): TextSpan[] {
  if (end <= start) return [...spans];
  const { head, middle, tail } = splitRange(spans, start, end);
  const on = !middle.every((span) => span[mark] === true);
  const changed = middle.map((span) => {
    const next = { ...span };
    if (on) next[mark] = true;
    else delete next[mark];
    return next;
  });
  return coalesce([...head, ...changed, ...tail], CELL_RICHTEXT_BOUNDS);
}

/** Links the text of a range; a mention or equation keeps what it is. */
export function linkCellRange(spans: readonly TextSpan[], start: number, end: number, link: string): TextSpan[] {
  if (end <= start) return [...spans];
  const { head, middle, tail } = splitRange(spans, start, end);
  const changed = middle.map((span) => span.mention || span.equation ? span : { ...span, link });
  return coalesce([...head, ...changed, ...tail], CELL_RICHTEXT_BOUNDS);
}

/** Takes a range's marks and colors off and keeps its links, as Notion's "형식 초기화" (seen 2026-10-30). */
export function clearCellFormat(spans: readonly TextSpan[], start: number, end: number): TextSpan[] {
  if (end <= start) return [...spans];
  const { head, middle, tail } = splitRange(spans, start, end);
  const changed = middle.map(({ bold: _b, italic: _i, underline: _u, strikethrough: _s, code: _c, color: _color, background: _background, ...span }) => span);
  return coalesce([...head, ...changed, ...tail], CELL_RICHTEXT_BOUNDS);
}

/** Makes a range one inline equation of the given TeX, or takes it out when the TeX is empty. */
export function equationCellRange(spans: readonly TextSpan[], start: number, end: number, tex: string): TextSpan[] {
  const { head, tail } = splitRange(spans, start, end);
  return coalesce([...head, ...(tex ? [{ text: tex, equation: tex }] : []), ...tail], CELL_RICHTEXT_BOUNDS);
}

/** Removes the links of a range's text. */
export function unlinkCellRange(spans: readonly TextSpan[], start: number, end: number): TextSpan[] {
  if (end <= start) return [...spans];
  const { head, middle, tail } = splitRange(spans, start, end);
  const changed = middle.map((span) => {
    if (span.mention || !span.link) return span;
    const { link: _link, ...rest } = span;
    return rest;
  });
  return coalesce([...head, ...changed, ...tail], CELL_RICHTEXT_BOUNDS);
}

/**
 * Colors a range's text or its background, each kept apart as Notion does;
 * a null value takes that kind off.
 */
export function colorCellRange(spans: readonly TextSpan[], start: number, end: number,
  kind: "color" | "background", value: string | null): TextSpan[] {
  if (end <= start) return [...spans];
  const { head, middle, tail } = splitRange(spans, start, end);
  const changed = middle.map((span) => {
    const { color: _color, background: _background, ...rest } = span;
    const colors = { ...textSpanColors(span), [kind]: value ?? undefined };
    return { ...rest, ...(colors.color ? { color: colors.color } : {}), ...(colors.background ? { background: colors.background } : {}) };
  });
  return coalesce([...head, ...changed, ...tail], CELL_RICHTEXT_BOUNDS);
}

export type CellRangeFormat = {
  marks: Record<CellMark | "link", boolean>;
  color: string;
  background: string;
  link: string;
};

/** The formatting a whole range shares, as the selection toolbar shows it. */
export function cellRangeFormat(spans: readonly TextSpan[], start: number, end: number): CellRangeFormat {
  const { middle } = splitRange(spans, start, end);
  const every = (test: (span: TextSpan) => boolean) => middle.length > 0 && middle.every(test);
  const colors = new Set(middle.map((span) => textSpanColors(span).color ?? "default"));
  const backgrounds = new Set(middle.map((span) => textSpanColors(span).background ?? "default_background"));
  const links = new Set(middle.filter((span) => !span.mention).map((span) => span.link ?? ""));
  const link = links.size === 1 ? [...links][0] ?? "" : "";
  return {
    marks: {
      bold: every((span) => span.bold === true), italic: every((span) => span.italic === true),
      underline: every((span) => span.underline === true), strikethrough: every((span) => span.strikethrough === true),
      code: every((span) => span.code === true), link: link !== "",
    },
    color: colors.size === 1 ? [...colors][0] ?? "default" : colors.size === 0 ? "default" : "mixed",
    background: backgrounds.size === 1 ? [...backgrounds][0] ?? "default_background" : backgrounds.size === 0 ? "default_background" : "mixed",
    link,
  };
}

/** A link a cell can keep: a web address, completed as the editor does. */
export function cellLinkValue(value: string) {
  let url = value.trim();
  if (!url || /\s/u.test(url)) return "";
  if (/^(?:[a-z0-9-]+\.)+[a-z]{2,}(?::\d+)?(?:[/?#].*)?$/iu.test(url)) url = `https://${url}`;
  return /^https?:\/\/[^/\s]/iu.test(url) ? url : "";
}

/** Replaces a range with spans: typed text, a line break or a mention. */
export function replaceCellRange(spans: readonly TextSpan[], start: number, end: number, inserted: readonly TextSpan[]): TextSpan[] {
  const { head, tail } = splitRange(spans, start, Math.max(start, end));
  return coalesce([...head, ...inserted, ...tail], CELL_RICHTEXT_BOUNDS);
}

/** The mark a keyboard shortcut toggles, as in Notion. */
export function cellMarkShortcut(event: { key: string; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }): CellMark | null {
  if (!primaryModifier(event) || event.altKey) return null;
  const key = chordLetter(event);
  if (event.shiftKey) return key === "s" || key === "x" ? "strikethrough" : null;
  if (key === "b") return "bold";
  if (key === "i") return "italic";
  if (key === "u") return "underline";
  if (key === "e") return "code";
  return null;
}
