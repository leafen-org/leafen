/**
 * Lists made from another by replacing some of its items in place — a
 * database's rows once another person's edit changed one of them — or by
 * taking some out and putting some in (spliceItems): a view's rows once
 * that edit moved one under its sorts, filters or groups. What is worked
 * out from a list of 100,000 rows item by item — their pages, their place
 * in the view, the table's lines — is worked out again only for the items
 * changed, from what was worked out for the list they changed (2026-11-11:
 * each edit that arrived on a table of 100,000 rows took 1.0–1.15 s of
 * React work, every row's projections made anew; 270–360 ms when it moved
 * the row).
 */

type Replacement = { from: readonly unknown[]; at: readonly number[] };
// Items taken out of `from` at `removed` (its places) and put in at `added`
// (the list's places), both in order; the other items keep their order.
type Splice = { from: readonly unknown[]; removed: readonly number[]; added: readonly number[] };
/** Where items stand in a list by their IDs. */
export type Places = { get(id: string): number | undefined };

const replacements = new WeakMap<readonly unknown[], Replacement>();
const splices = new WeakMap<readonly unknown[], Splice>();
const placesKept = new WeakMap<readonly unknown[], Places & { depth?: number }>();

// More items replaced than this, or a list made in more steps than this
// from one worked out, is worked out whole.
const MOST_REPLACED = 1_000;
const MOST_STEPS = 32;

/** `list` with the items at the places given replaced, kept as made from it. */
export function replaceItems<T>(list: readonly T[], replaced: ReadonlyMap<number, T>): T[] {
  const next = list.slice();
  for (const [index, item] of replaced) next[index] = item;
  replacements.set(next, { from: list, at: [...replaced.keys()] });
  return next;
}

/** The list `list` was made from by replacing the items at `at`, if it was. */
export function itemsReplacedFrom<T>(list: readonly T[]): { from: readonly T[]; at: readonly number[] } | undefined {
  return replacements.get(list) as { from: readonly T[]; at: readonly number[] } | undefined;
}

/** Keeps `list`, made elsewhere, as `from` with the items at `at` replaced. */
export function noteItemsReplaced<T>(list: readonly T[], from: readonly T[], at: readonly number[]) {
  if (list !== from && list.length === from.length && at.length <= MOST_REPLACED) replacements.set(list, { from, at });
}

/**
 * Each item's place in `list` by its ID, `idOf` naming it (one list is
 * always named alike); a list made by replacing items with others of the
 * same IDs keeps the places of the one it was made from, and one spliced
 * from it reads them through its places (splicedPlaces).
 */
export function placesById<T>(list: readonly T[], idOf: (item: T) => string): Places {
  let places = placesKept.get(list);
  if (places) return places;
  const made = replacements.get(list), spliced = splices.get(list);
  const before = made ? placesKept.get(made.from) : spliced && placesKept.get(spliced.from);
  if (made && before && made.at.every((index) => idOf(list[index]) === idOf(made.from[index] as T))) {
    places = before;
  } else if (spliced && before && (before.depth ?? 0) < MOST_STEPS) {
    places = splicedPlaces(before, spliced, list, idOf);
  } else {
    const fresh = new Map<string, number>();
    list.forEach((item, index) => fresh.set(idOf(item), index));
    places = fresh;
  }
  placesKept.set(list, places);
  return places;
}

// The places in a list spliced from another, read through the other's: an
// item put in stands where it was put, one taken out nowhere, any other
// where it stood less the items taken out before it and with those put in
// before it — a lookup of the items changed, not a map of every item made
// again (each made after up to MOST_STEPS splices, then made whole).
function splicedPlaces<T>(before: Places & { depth?: number }, spliced: Splice, list: readonly T[], idOf: (item: T) => string) {
  const put = new Map(spliced.added.map((index) => [idOf(list[index]), index]));
  const gone = new Set(spliced.removed.map((index) => idOf(spliced.from[index] as T)));
  return {
    depth: (before.depth ?? 0) + 1,
    get(id: string) {
      const added = put.get(id);
      if (added !== undefined || gone.has(id)) return added;
      const was = before.get(id);
      if (was === undefined) return undefined;
      let low = 0, high = spliced.removed.length;
      while (low < high) {
        const middle = (low + high) >>> 1;
        if (spliced.removed[middle] < was) low = middle + 1;
        else high = middle;
      }
      return restPlace(was - low, spliced.added);
    },
  };
}

// The place in a list of the item at `at` among those not at `skipped` (its places, in order).
function restPlace(at: number, skipped: readonly number[]) {
  let place = at;
  for (const index of skipped) {
    if (index <= place) place += 1;
    else break;
  }
  return place;
}

/**
 * `list` with the items at `removed` (its places, in order) taken out and
 * the items of `added` put in at theirs in the list made (in order), kept
 * as made from it; taken out and put in at the same places, it is `list`
 * with them replaced (replaceItems).
 */
export function spliceItems<T>(list: readonly T[], removed: readonly number[], added: readonly (readonly [number, T])[]): T[] {
  if (removed.length === added.length && removed.every((index, at) => index === added[at][0])) return replaceItems(list, new Map(added));
  const next = new Array<T>(list.length - removed.length + added.length);
  let from = 0, gone = 0, put = 0;
  for (let index = 0; index < next.length; index += 1) {
    if (put < added.length && added[put][0] === index) {
      next[index] = added[put++][1];
      continue;
    }
    while (gone < removed.length && removed[gone] === from) {
      from += 1;
      gone += 1;
    }
    next[index] = list[from++];
  }
  if (removed.length + added.length <= MOST_REPLACED) splices.set(next, { from: list, removed, added: added.map(([index]) => index) });
  return next;
}

/**
 * `list` less the items at `removed` (its places), with each of `coming`
 * put after the rest's items that come before it (`precedes`, the rest
 * being in that order), kept as made from it (spliceItems).
 */
export function moveItems<T>(list: readonly T[], removed: readonly number[], coming: readonly T[], precedes: (item: T, other: T) => boolean): T[] {
  const gone = [...removed].sort((left, right) => left - right);
  const rest = list.length - gone.length;
  const placed = coming.map((item) => {
    let low = 0, high = rest;
    while (low < high) {
      const middle = (low + high) >>> 1;
      if (precedes(list[restPlace(middle, gone)], item)) low = middle + 1;
      else high = middle;
    }
    return { item, at: low };
  });
  placed.sort((left, right) => left.at - right.at || (precedes(left.item, right.item) ? -1 : precedes(right.item, left.item) ? 1 : 0));
  return spliceItems(list, gone, placed.map(({ item, at }, index) => [at + index, item] as const));
}

/**
 * `list` less the items `gone` says and with `coming` put in where `precedes`
 * orders them among the rest (the rest being in that order), kept as made
 * from it (spliceItems); `list` itself when nothing goes or comes. Rows a
 * press sends to the trash, brings back or copies are taken out and put in
 * so (lib/rowsShownAtPress.ts, lib/copiesOnTheirWay.ts): made as a new list,
 * every row's page, cells and related pages were made again for them
 * (2026-11-14: a copy of one row drew 1,701 components of a rollup table).
 */
export function itemsWithout<T>(list: readonly T[], gone: (item: T) => boolean, coming: readonly T[] = [],
  precedes?: (item: T, other: T) => boolean): T[] {
  const removed: number[] = [];
  list.forEach((item, index) => { if (gone(item)) removed.push(index); });
  if (!removed.length && !coming.length) return list as T[];
  return coming.length && precedes ? moveItems(list, removed, coming, precedes) : spliceItems(list, removed, []);
}

/**
 * The items `make` makes for the empty places of `list`, by place, each at a
 * number between the places (`position`) of the items around it: items put
 * in among others ordered by such numbers leave the others' as they were —
 * a database's pages, whose places order them, so a page after a row copied
 * or brought back is not made again for its new place (2026-11-14).
 */
export function madeBetween<T extends { position: number }>(list: readonly (T | null)[], make: (index: number, position: number) => T): Map<number, T> {
  const made = new Map<number, T>();
  for (let start = 0; start < list.length; start += 1) {
    if (list[start]) continue;
    let end = start;
    while (end < list.length && !list[end]) end += 1;
    const run = end - start;
    const low = start > 0 ? list[start - 1]!.position : end < list.length ? list[end]!.position - run - 1 : -1;
    const high = end < list.length ? list[end]!.position : low + run + 1;
    for (let index = start; index < end; index += 1) made.set(index, make(index, low + ((high - low) * (index - start + 1)) / (run + 1)));
    start = end;
  }
  return made;
}

/** The list `list` was spliced from (spliceItems), with the places taken out and put in, if it was. */
export function itemsSplicedFrom<T>(list: readonly T[]): { from: readonly T[]; removed: readonly number[]; added: readonly number[] } | undefined {
  return splices.get(list) as { from: readonly T[]; removed: readonly number[]; added: readonly number[] } | undefined;
}

/**
 * What was kept in `kept` for the list `list` was spliced from
 * (spliceItems), with the places taken out of it and put in.
 */
export function splicedBefore<T, Value>(
  list: readonly T[],
  kept: WeakMap<readonly T[], Value>,
): { value: Value; from: readonly T[]; removed: readonly number[]; added: readonly number[] } | undefined {
  const spliced = splices.get(list);
  const value = spliced && kept.get(spliced.from as readonly T[]);
  return spliced && value !== undefined ? { value, from: spliced.from as readonly T[], removed: spliced.removed, added: spliced.added } : undefined;
}

/**
 * What was worked out, and kept in `kept`, for the list `list` was made from
 * by replacing items — or for one before it — and the places replaced
 * since; none when no such list was worked out or too much changed since.
 */
export function workedOutBefore<T, Value>(
  list: readonly T[],
  kept: WeakMap<readonly T[], Value>,
): { value: Value; from: readonly T[]; at: readonly number[] } | undefined {
  let current: readonly unknown[] = list;
  const at = new Set<number>();
  for (let step = 0; step < MOST_STEPS; step += 1) {
    const made = replacements.get(current);
    if (!made) return undefined;
    for (const index of made.at) at.add(index);
    if (at.size > MOST_REPLACED) return undefined;
    current = made.from;
    const value = kept.get(current as readonly T[]);
    if (value !== undefined) return { value, from: current as readonly T[], at: [...at] };
  }
  return undefined;
}

/**
 * `derive` of each item of `list`, in its order — what it makes of an item
 * reads the item alone, not its place: from the list worked out for the one
 * it was made from, with only the items changed since (replaced, or put in
 * when spliced) derived again, when there is one.
 */
export function mapItems<T, U>(
  list: readonly T[],
  kept: WeakMap<readonly T[], readonly U[]>,
  derive: (item: T) => U,
): readonly U[] {
  let derived = kept.get(list);
  if (derived) return derived;
  const before = workedOutBefore(list, kept);
  const spliced = before ? undefined : splicedBefore(list, kept);
  derived = before ? replaceItems(before.value, new Map(before.at.map((index) => [index, derive(list[index])])))
    : spliced ? spliceItems(spliced.value, spliced.removed, spliced.added.map((index) => [index, derive(list[index])] as const))
    : list.map((item) => derive(item));
  kept.set(list, derived);
  return derived;
}

/**
 * The items of `list` that `keep` keeps, in its order — `list` itself when
 * it keeps them all: from those kept of the list it was made from, with the
 * items replaced, taken out or put in since kept, dropped or put at their
 * places alone (`keep` says the same of an item each time it is asked).
 */
export function keptItems<T>(
  list: readonly T[],
  kept: WeakMap<readonly T[], readonly T[]>,
  keep: (item: T) => boolean,
  idOf: (item: T) => string,
): readonly T[] {
  let shown = kept.get(list);
  if (shown) return shown;
  const before = workedOutBefore(list, kept);
  const spliced = before ? undefined : splicedBefore(list, kept);
  if (before) shown = keptAsBefore(list, before, keep, idOf);
  else if (spliced) shown = keptAsSpliced(list, spliced, keep, idOf);
  if (!shown) shown = list.every(keep) ? list : list.filter(keep);
  kept.set(list, shown);
  return shown;
}

// The kept items with those that come put at their places in `list` and
// those at `gone` (their places among the kept) taken out.
function keptMoved<T>(shown: readonly T[], gone: readonly number[], come: readonly T[], list: readonly T[], idOf: (item: T) => string) {
  if (!gone.length && !come.length) return shown;
  const order = come.length ? placesById(list, idOf) : undefined;
  return moveItems(shown, gone, come, (item, other) => (order!.get(idOf(item)) ?? -1) < (order!.get(idOf(other)) ?? -1));
}

function keptAsBefore<T>(
  list: readonly T[],
  before: { value: readonly T[]; from: readonly T[]; at: readonly number[] },
  keep: (item: T) => boolean,
  idOf: (item: T) => string,
): readonly T[] | undefined {
  const all = before.value === before.from;
  const replaced = new Map<number, T>(), gone: number[] = [], come: T[] = [];
  for (const index of before.at) {
    const was = before.from[index], item = list[index];
    if (idOf(was) !== idOf(item)) return undefined;
    const wasKept = keep(was), isKept = keep(item);
    if (!wasKept) {
      if (isKept) come.push(item);
      continue;
    }
    if (all && isKept) continue;
    const place = all ? index : placesById(before.value, idOf).get(idOf(item));
    if (place === undefined) return undefined;
    if (isKept) replaced.set(place, item);
    else gone.push(place);
  }
  // All kept before, the list itself has those still kept in their places.
  if (all) return keptMoved(list, gone, [], list, idOf);
  return keptMoved(replaced.size ? replaceItems(before.value, replaced) : before.value, gone, come, list, idOf);
}

function keptAsSpliced<T>(
  list: readonly T[],
  spliced: { value: readonly T[]; from: readonly T[]; removed: readonly number[]; added: readonly number[] },
  keep: (item: T) => boolean,
  idOf: (item: T) => string,
): readonly T[] | undefined {
  const out = spliced.removed.filter((index) => keep(spliced.from[index]));
  const come = spliced.added.filter((index) => keep(list[index]));
  if (!out.length && !come.length) return spliced.value;
  if (spliced.value === spliced.from && come.length === spliced.added.length) return list;
  const places = placesById(spliced.value, idOf), gone: number[] = [];
  for (const index of out) {
    const place = places.get(idOf(spliced.from[index]));
    if (place === undefined || spliced.value[place] !== spliced.from[index]) return undefined;
    gone.push(place);
  }
  return keptMoved(spliced.value, gone, come.map((index) => list[index]), list, idOf);
}
