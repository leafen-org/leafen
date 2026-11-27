import { placesById } from "./rowsReplaced";

/**
 * Records of a list's items by ID — a view's rows, their pages — that
 * follow the list: from LOOKUP_FROM items on, a record reads through the
 * list's places (lib/rowsReplaced.ts placesById), which a list made from
 * another by replacing items with others of the same IDs keeps, so the
 * record of such a list is made without going over its items (2026-11-11:
 * each edit that arrived on a view of 100,000 rows made four records of
 * 100,000 keys again, and a fifth of them laid over another, 5–10 ms
 * each). It reads, lists and spreads as the record of its items would; a
 * shorter list's record is that record.
 */
const LOOKUP_FROM = 1_000;

type Keyed = { readonly id: string };
type Lookup<T> = Readonly<Record<string, T>>;

const lookups = new WeakMap<readonly Keyed[], Lookup<Keyed>>();
const listsOf = new WeakMap<object, readonly Keyed[]>();
// Each lookup's keys in its list's order, kept for its places: a list with
// items replaced by others of the same IDs keeps them.
const keysKept = new WeakMap<object, readonly string[]>();
const followed = new WeakSet<object>();
const idOf = (item: Keyed) => item.id;
const own = (record: object, key: PropertyKey) => Object.prototype.hasOwnProperty.call(record, key);

/** The list's items by ID, kept for the list; the last of the same ID wins, as in a record. */
export function lookupById<T extends Keyed>(list: readonly T[]): Lookup<T> {
  let lookup = lookups.get(list);
  if (!lookup) {
    lookup = list.length < LOOKUP_FROM ? recordOf(list) : listLookup(list);
    lookups.set(list, lookup);
    listsOf.set(lookup, list);
  }
  return lookup as Lookup<T>;
}

/** The list a lookup was made from (lookupById), in its order. */
export function lookupList<T extends Keyed>(lookup: object): readonly T[] | undefined {
  return listsOf.get(lookup) as readonly T[] | undefined;
}

function recordOf(list: readonly Keyed[]) {
  const made: Record<string, Keyed> = {};
  for (const item of list) made[item.id] = item;
  return made;
}

function listLookup(list: readonly Keyed[]): Lookup<Keyed> {
  const places = placesById(list, idOf);
  const item = (key: PropertyKey) => {
    const at = typeof key === "string" ? places.get(key) : undefined;
    return at === undefined ? undefined : list[at];
  };
  const keys = () => {
    let kept = keysKept.get(places);
    if (!kept) keysKept.set(places, kept = [...new Set(list.map(idOf))]);
    return kept as string[];
  };
  return following({
    get: (target, key, receiver) => item(key) ?? Reflect.get(target, key, receiver),
    has: (target, key) => item(key) !== undefined || Reflect.has(target, key),
    ownKeys: keys,
    getOwnPropertyDescriptor: (target, key) => {
      const value = item(key);
      return value === undefined ? Reflect.getOwnPropertyDescriptor(target, key) : { value, writable: true, enumerable: true, configurable: true };
    },
  });
}

function following<T>(handler: ProxyHandler<Record<string, T>>): Lookup<T> {
  const lookup = new Proxy({} as Record<string, T>, handler);
  followed.add(lookup);
  return lookup;
}

const overs = new WeakMap<object, WeakMap<object, Lookup<unknown>>>();

/**
 * `over`'s items where it has them and `under`'s elsewhere, as `{ ...under,
 * ...over }` is, kept for the pair: over a lookup that follows its list
 * (lookupById) it reads through both, made without going over them.
 */
export function lookupOver<Under, Over>(under: Lookup<Under>, over: Lookup<Over>): Lookup<Under | Over> {
  let byOver = overs.get(under);
  if (!byOver) overs.set(under, byOver = new WeakMap());
  let both = byOver.get(over) as Lookup<Under | Over> | undefined;
  if (!both) {
    both = followed.has(under) || followed.has(over) ? overLookup(under, over) : { ...under, ...over };
    byOver.set(over, both);
  }
  return both;
}

function overLookup<Under, Over>(under: Lookup<Under>, over: Lookup<Over>): Lookup<Under | Over> {
  let keys: string[] | undefined;
  const from = (key: PropertyKey) => own(over, key) ? over : own(under, key) ? under : undefined;
  return following<Under | Over>({
    get: (target, key, receiver) => {
      if (typeof key !== "string") return Reflect.get(target, key, receiver);
      const top = over[key];
      return top !== undefined || own(over, key) ? top : under[key];
    },
    has: (target, key) => key in over || key in under || Reflect.has(target, key),
    ownKeys: () => {
      if (!keys) {
        const below = Reflect.ownKeys(under).filter((key): key is string => typeof key === "string");
        keys = [...below, ...Reflect.ownKeys(over).filter((key): key is string => typeof key === "string" && !own(under, key))];
      }
      return keys;
    },
    getOwnPropertyDescriptor: (target, key) => {
      const record = from(key);
      return record ? { value: (record as Record<PropertyKey, unknown>)[key], writable: true, enumerable: true, configurable: true }
        : Reflect.getOwnPropertyDescriptor(target, key);
    },
  });
}
