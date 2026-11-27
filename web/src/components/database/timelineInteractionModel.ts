export type TimelineInteractionMode = "move" | "resize-start" | "resize-end";

function clamp(value: number, minimum: number, maximum: number) {
  return Math.min(maximum, Math.max(minimum, value));
}

/**
 * Where a bar pressed to move or stretch would land, `deltaDays` columns on,
 * and the columns of the window it is drawn over. Its edges go as far as the
 * pointer, past the window's ends too: a bar running past them keeps its own
 * dates there, the end of one longer than the window stretched an hour on its
 * own end an hour later, as Notion's (seen 2026-11-24 in Chrome at 1685 at the
 * day zoom, pressed by script at the window's edge, where a person scrolling
 * there finds the window drawn again first) — none pulled in to the window's
 * edge, nor a bar moved from past its start brought into it. A stretched end
 * stops at its start, and a start at its end: on its day where the columns are
 * days, meeting it (a moment, `moments`) where they are hours, as a bar it
 * carries does (timelineStretchAlong).
 */
export function timelineInteractionPreview({
  baseEndOffset,
  baseStartOffset,
  deltaDays,
  durationDays,
  maxIndex,
  mode,
  moments = false,
}: {
  baseEndOffset: number;
  baseStartOffset: number;
  deltaDays: number;
  durationDays: number;
  maxIndex: number;
  mode: TimelineInteractionMode;
  moments?: boolean;
}) {
  const meet = moments ? 1 : 0;
  let startOffset = baseStartOffset;
  let endOffset = baseEndOffset;
  if (mode === "move") {
    startOffset = baseStartOffset + deltaDays;
    endOffset = startOffset + durationDays;
  } else if (mode === "resize-start") {
    startOffset = Math.min(baseStartOffset + deltaDays, baseEndOffset + meet);
  } else {
    endOffset = Math.max(baseEndOffset + deltaDays, baseStartOffset - meet);
  }
  const first = clamp(startOffset, 0, maxIndex);
  return {
    endOffset,
    first,
    last: clamp(endOffset, first - meet, maxIndex),
    startOffset,
  };
}

export function projectTimelineDependencyPairs({
  edges,
  positionedRowIds,
}: {
  edges: readonly { predecessorRowId: string; successorRowId: string }[];
  positionedRowIds: readonly string[];
}) {
  const positioned = new Set(positionedRowIds);
  const seen = new Set<string>();
  const pairs: [string, string][] = [];
  for (const edge of edges) {
    const key = `${edge.predecessorRowId}\u0000${edge.successorRowId}`;
    if (
      edge.predecessorRowId === edge.successorRowId
      || !positioned.has(edge.predecessorRowId)
      || !positioned.has(edge.successorRowId)
      || seen.has(key)
    ) continue;
    seen.add(key);
    pairs.push([edge.successorRowId, edge.predecessorRowId]);
  }
  return pairs;
}
