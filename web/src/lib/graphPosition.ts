// A new position is made of the printable characters after the space, which
// sort alike by code unit everywhere and which the server keeps as they are;
// "!" never ends one, so there is always room before it.
const POSITION_LOWEST = 0x21;
const POSITION_BEYOND = 0x7f;
const POSITION_MAX_LENGTH = 200;

/**
 * A position between two neighbours, ordered by code unit as the content is
 * (`compareGraphOrder`): after `before` when there is no `after`, before
 * `after` when there is no `before`. Neighbours that share a place, or come
 * out of order, leave no room between them (two items placed from one list
 * can share one): the new one goes right after `before`, past the other, so
 * the change is made, not refused.
 */
export function graphPositionBetween(before: string | undefined, after: string | undefined) {
  if (!before && !after) return "m";
  const lower = before ?? "";
  let upper = after && lower < after ? after : null;
  let prefix = "";
  for (let index = 0; index < POSITION_MAX_LENGTH; index += 1) {
    const low = Math.max(index < lower.length ? lower.charCodeAt(index) : POSITION_LOWEST, POSITION_LOWEST);
    const high = Math.min(upper !== null && index < upper.length ? upper.charCodeAt(index) : POSITION_BEYOND, POSITION_BEYOND);
    if (high - low > 1) return prefix + String.fromCharCode(Math.floor((low + high) / 2));
    // No character fits here: keep the lower one and look one further.
    const kept = index < lower.length ? lower.charCodeAt(index) : Math.min(POSITION_LOWEST, upper?.charCodeAt(index) ?? POSITION_LOWEST);
    if (upper !== null && (index >= upper.length || kept > upper.charCodeAt(index))) {
      throw new RangeError("Graph row neighbours are not ordered.");
    }
    if (upper !== null && kept < upper.charCodeAt(index)) upper = null;
    prefix += String.fromCharCode(kept);
  }
  throw new RangeError("Graph row positions are too long.");
}
