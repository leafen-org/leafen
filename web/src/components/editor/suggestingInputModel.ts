import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { blockSpec, isBlockType, TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { editorBlockNeighbors } from "./editorGraphModel";

/**
 * What suggesting-mode input suggests for a block's text, read from the text
 * as the page's collaborative document holds it (UTF-16 offsets).
 */

/** The blocks whose text takes suggestions, as the suggestion route allows. */
export const SUGGESTING_BLOCK_TYPES: ReadonlySet<string> = new Set([
  "paragraph", "heading_1", "heading_2", "heading_3", "heading_4",
  "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4",
  "bulleted_list_item", "numbered_list_item", "to_do", "toggle", "quote", "callout", "code",
]);

/**
 * The block Enter at a block's end suggests next, as Notion's does: a list
 * goes on as a list and anything else as a paragraph, an open toggle takes
 * a first child paragraph and a closed one a toggle after it. Code blocks
 * suggest none.
 */
export function suggestedNextBlock(type: string, collapsed: boolean): { type: string; firstChild: boolean } | null {
  if (!SUGGESTING_BLOCK_TYPES.has(type) || type === "code" || !isBlockType(type)) return null;
  if (TOGGLE_BLOCK_TYPES.has(type)) return collapsed ? { type, firstChild: false } : { type: "paragraph", firstChild: true };
  return { type: blockSpec(type).continuesOnEnter ? type : "paragraph", firstChild: false };
}

/**
 * The block Enter before a block's end splits off, after the break or, at
 * the start of the text, before the block: a list goes on as a list and
 * anything else as a paragraph, as the editor's own Enter does. Code blocks
 * take no suggested break.
 */
export function suggestedSplitBlock(type: string): string | null {
  if (!SUGGESTING_BLOCK_TYPES.has(type) || type === "code" || !isBlockType(type)) return null;
  return blockSpec(type).continuesOnEnter ? type : "paragraph";
}

/**
 * A block suggested right after this one, or as its first child, shows as
 * a suggested line break at its end, as does a suggested break that split
 * the text after it off — unless the line ends in text that suggestion
 * suggested in (`endsIn`, the suggestion of the line's last text): the
 * break is then part of that text and Notion marks it with nothing (seen
 * 2026-11-15, two ways: "…gh" then Enter, and Enter in the middle of one's
 * own suggested text, draw no "⏎"; an empty line between still has one).
 * A block a menu picked, alone in its suggestion ("+" or "/" after words),
 * comes with no break (page_suggestion_picked.go).
 */
export function suggestedBreakAfter(graph: ContentGraph, block: GraphBlock, endsIn: string | null = null) {
  return suggestedBreakOf(graph, block, endsIn) !== null;
}

/** The suggestion whose break a line ends in (suggestedBreakAfter), and whether it is the line's own split; null for none. */
export function suggestedBreakOf(graph: ContentGraph, block: GraphBlock, endsIn: string | null = null) {
  const suggestion = (value: unknown) => typeof value === "string" && value !== "" && value !== endsIn ? value : null;
  const { next, firstChild } = editorBlockNeighbors(graph, block);
  const split = suggestion(block.content.splitSuggestionId);
  if (split) return { id: split, split: true };
  const after = (next?.content.suggestionAlone !== true ? suggestion(next?.content.suggestionId) : null)
    ?? suggestion(firstChild?.content.suggestionId);
  return after ? { id: after, split: false } : null;
}

/** The suggestion a line's last text was suggested in under, read from its annotation, or null. */
export function lineEndingSuggestion(annotation: string | undefined): string | null {
  if (!annotation) return null;
  try {
    const value = JSON.parse(annotation) as { suggestion?: unknown; suggestionId?: unknown };
    return value.suggestion === "insert" && typeof value.suggestionId === "string" ? value.suggestionId : null;
  } catch { return null; }
}

/**
 * Whether a break at an offset in a block's text is one the suggestion route
 * takes: one with no suggested text after it, or one in or right after text
 * suggested in whose suggestion holds all the suggested text after it — the
 * break goes on with that suggestion, as Notion's does (seen 2026-11-15, two
 * ways: Enter in the middle of one's own "abcd" leaves "ab" and a new line
 * "cd", one card "추가: “ab”" / "추가: “cd”", no "⏎"). Whose suggestion it is
 * the route checks.
 */
export function suggestedBreakTaken(runs: readonly SuggestingTextRun[], offset: number) {
  const before = runs.find((run) => run.start < offset && offset <= run.end);
  const goesOn = before?.attributes.suggestion === "insert" ? before.attributes.suggestionId : undefined;
  return runs.every((run) => run.end <= offset || !run.attributes.suggestion
    || (goesOn !== undefined && run.attributes.suggestion === "insert" && run.attributes.suggestionId === goesOn));
}

/** A run of a block's text with one set of attributes, at its UTF-16 range. */
export type SuggestingTextRun = {
  start: number;
  end: number;
  attributes: Readonly<Record<string, unknown>>;
};

type DeltaItem = { insert?: unknown; attributes?: Record<string, unknown> };

export function suggestingTextRuns(delta: readonly DeltaItem[]): SuggestingTextRun[] {
  const runs: SuggestingTextRun[] = [];
  let position = 0;
  for (const item of delta) {
    const length = typeof item.insert === "string" ? item.insert.length : item.insert ? 1 : 0;
    if (length === 0) continue;
    runs.push({ start: position, end: position + length, attributes: item.attributes ?? {} });
    position += length;
  }
  return runs;
}

function runAt(runs: readonly SuggestingTextRun[], index: number) {
  return runs.find((run) => run.start <= index && index < run.end);
}

function suggestedAway(runs: readonly SuggestingTextRun[], index: number) {
  return runAt(runs, index)?.attributes.suggestion === "delete";
}

function highSurrogate(code: number) {
  return code >= 0xd800 && code <= 0xdbff;
}

function lowSurrogate(code: number) {
  return code >= 0xdc00 && code <= 0xdfff;
}

/** A suggested deletion's range and where the caret goes after it. */
export type SuggestedDeletion = { edit: { offset: number; length: number } | null; caret: number };

/**
 * Backspace suggests the character before the caret away, past text already
 * suggested away, and leaves the caret before it.
 */
export function backspaceSuggestion(
  text: string, runs: readonly SuggestingTextRun[], caret: number,
): SuggestedDeletion {
  let at = Math.min(Math.max(caret, 0), text.length);
  while (at > 0 && suggestedAway(runs, at - 1)) at -= 1;
  if (at === 0) return { edit: null, caret: at };
  const width = at > 1 && lowSurrogate(text.charCodeAt(at - 1)) && highSurrogate(text.charCodeAt(at - 2)) ? 2 : 1;
  return { edit: { offset: at - width, length: width }, caret: at - width };
}

/**
 * Delete suggests the character after the caret away, past text already
 * suggested away, and leaves the caret after it.
 */
export function forwardDeleteSuggestion(
  text: string, runs: readonly SuggestingTextRun[], caret: number,
): SuggestedDeletion {
  let at = Math.min(Math.max(caret, 0), text.length);
  while (at < text.length && suggestedAway(runs, at)) at += 1;
  if (at === text.length) return { edit: null, caret: at };
  const width = at + 1 < text.length && highSurrogate(text.charCodeAt(at)) && lowSurrogate(text.charCodeAt(at + 1)) ? 2 : 1;
  return { edit: { offset: at, length: width }, caret: at + width };
}

const TYPED_MARKS = ["bold", "italic", "underline", "strikethrough", "code"] as const;

/** Typed text takes the formatting of the character before it, but not its link or mention. */
export function typedMarks(runs: readonly SuggestingTextRun[], offset: number): Record<string, boolean | string> {
  const before = offset > 0 ? runAt(runs, offset - 1) : undefined;
  if (!before) return {};
  const marks: Record<string, boolean | string> = {};
  for (const mark of TYPED_MARKS) {
    if (before.attributes[mark] === true) marks[mark] = true;
  }
  if (typeof before.attributes.color === "string" && before.attributes.color) marks.color = before.attributes.color;
  return marks;
}

/** A formatting shortcut over a range turns its mark on, or off when all of the range has it. */
export function formatSuggestionMarks(
  runs: readonly SuggestingTextRun[], start: number, end: number, mark: string,
): Record<string, boolean> {
  const covered = runs.filter((run) => run.start < end && start < run.end);
  const all = covered.length > 0 && covered.every((run) => run.attributes[mark] === true);
  return { [mark]: !all };
}
