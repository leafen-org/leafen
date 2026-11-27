/**
 * Where a property stands in the order its database's API lists them, by
 * their public IDs, as the server marks each one it reads (publicOrder); one
 * not marked yet stands after them.
 */
export function publicOrderOf(property: object) {
  const place = (property as { publicOrder?: unknown }).publicOrder;
  return typeof place === "number" ? place : Number.MAX_SAFE_INTEGER;
}

/**
 * Orders properties as their API lists them: the order Notion picks a view's
 * first property of a kind in — a new or API-made board's group, a
 * calendar's or timeline's date, a chart's X axis, a new list's shown
 * property — and the fallback of a view whose property went (seen
 * 2026-11-12: selects made 선택1, 선택2, 선택3 with IDs "qUdm", "@_mA",
 * "Z_j|" grouped a new board by 선택2 and, 선택2 deleted, by 선택3; the dates
 * alike), neither their names' nor their columns' order. Those marked alike
 * keep the order given.
 */
export function inPublicOrder<T extends object>(properties: readonly T[]): T[] {
  return [...properties].sort((left, right) => publicOrderOf(left) - publicOrderOf(right));
}
