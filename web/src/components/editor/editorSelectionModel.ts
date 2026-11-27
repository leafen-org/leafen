export function nextBlockSelection(
  current: ReadonlySet<string>,
  blockId: string,
  options: { additive: boolean; range: boolean },
  orderedIds: readonly string[],
) {
  if (options.range && current.size > 0) {
    const anchor = [...current].at(-1);
    const from = orderedIds.indexOf(anchor ?? "");
    const to = orderedIds.indexOf(blockId);
    if (from >= 0 && to >= 0) {
      const [start, end] = from < to ? [from, to] : [to, from];
      return new Set(orderedIds.slice(start, end + 1));
    }
  }
  if (!options.additive) return new Set([blockId]);
  const next = new Set(current);
  if (next.has(blockId)) next.delete(blockId);
  else next.add(blockId);
  return next;
}
