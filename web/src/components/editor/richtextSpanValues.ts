import type { TextSpan } from "@/lib/types";
import { TEXT_SPAN_FIELDS } from "@/lib/modelTypes/content";
import { assertRichtextSpanCount, consumeRichtextOutput, type RichtextBounds, type RichtextBudget } from "./richtextBudget";

const COLOR_TOKENS = new Set([
  "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red",
  "default_background",
  "gray_background", "brown_background", "orange_background", "yellow_background",
  "green_background", "blue_background", "purple_background", "pink_background",
  "red_background",
]);

// Neighbours join only when every field but the text is the same and neither
// is a mention or an equation (joins).
const MARK_KEYS = TEXT_SPAN_FIELDS;

function sameMarks(a: TextSpan, b: TextSpan) {
  return MARK_KEYS.every((key) => a[key] === b[key]);
}

/** A mention or an equation is a piece of its own: two that read alike stay two, as Notion keeps them. */
function joins(a: TextSpan, b: TextSpan) {
  return !a.mention && !a.equation && !a.template && !a.emptyPiece && !b.mention && !b.equation && !b.template && !b.emptyPiece
    && sameMarks(a, b);
}

export function safeColorTokenValue(color: string | undefined) {
  return color && COLOR_TOKENS.has(color) ? color : undefined;
}

export function safeCommentIdValue(commentId: string | undefined) {
  // The threads marked on this text, separated by spaces.
  if (!commentId || !/^[A-Za-z0-9_-]+( [A-Za-z0-9_-]+)*$/.test(commentId)) return undefined;
  return commentId;
}

/** A database template's "when duplicated" mention: today, now or me. */
export function templateMentionValue(value: string | undefined) {
  return value === "today" || value === "now" || value === "me" ? value : undefined;
}

/** Spans with neighbours that read alike joined, within a caller's budget. */
export function coalesceWithin(
  spans: TextSpan[],
  bounds: RichtextBounds,
  budget: RichtextBudget,
) {
  const output: TextSpan[] = [];
  for (const span of spans) {
    // An empty piece stays, as one with no characters (TextSpan.emptyPiece).
    if (span.text === "" && !span.emptyPiece) continue;
    const last = output[output.length - 1];
    if (last && joins(last, span)) last.text += span.text;
    else output.push({ ...span });
  }
  assertRichtextSpanCount(output.length, bounds);
  for (const span of output) consumeRichtextOutput(span.text, bounds, budget);
  return output;
}
