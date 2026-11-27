/**
 * The rows a drag of `source` carries among a view's rows in their order
 * (`order`, a row shown twice once): the chosen ones under its own parent
 * when it is chosen itself, in that order, as Notion's views carry their
 * chosen rows (seen 2026-11-24 in Chrome at 1685, dpr 1, two tabs: a
 * table's, a list's, a gallery's, a board's and a timeline's — two chosen
 * rows apart dragged by one's handle stood together where it was dropped,
 * in the order they were shown, chosen); else it alone, as an unchosen row
 * dragged among chosen ones went alone, and it alone was chosen after.
 */
export function chosenCarried(order: readonly { id: string; parentId?: string | null }[], chosen: ReadonlySet<string>, source: string): string[] {
  if (!chosen.has(source)) return [source];
  const parentId = order.find(({ id }) => id === source)?.parentId ?? null;
  const ids = order.filter(({ id, parentId: parent }) => chosen.has(id) && (parent ?? null) === parentId).map(({ id }) => id);
  return ids.includes(source) ? [...new Set(ids)] : [source];
}
