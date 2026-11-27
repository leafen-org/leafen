import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { moveItems, placesById, replaceItems, splicedBefore, workedOutBefore } from "@/lib/rowsReplaced";
import { statusDefaultOptionId } from "@/lib/statusDefaultValue";
import { boardGroupOptions, boardGroupValueIds, type BoardGroupingBounds, type BoardGroupMode, type BoardMainGroupProperty } from "./boardGrouping";
import { boardValueGroups, isBoardValueGroupProperty, type BoardValueGroups } from "./boardValueGrouping";
import { tableKeptGroupKeys } from "./tableGroupSettingsModel";
import { projectBoardColumns, type BoardBucket, type BoardOption } from "./boardViewModel";
import { valueAsRead } from "./valueAsRead";

export type BoardColumns = {
  /** Its groups by what its cards hold, when it is grouped so (boardValueGrouping.ts). */
  valueGroups: BoardValueGroups | null;
  /** Its groups, in their order, without the one for no value. */
  options: BoardOption[];
  /** Its columns, each with its cards in the board's order, the one for no value where it shows. */
  columns: BoardBucket<GraphRow>[];
  /** The keys of the columns a card is in, the same while its columns are followed (bucketKeys). */
  keysOf(row: GraphRow): readonly string[];
};

type Input = {
  property: BoardMainGroupProperty;
  graphProperty: GraphProperty;
  /** Its cards, in its order. */
  rows: readonly GraphRow[];
  /** Its cards' pages, which a person's groups are named from. */
  pages: Page[];
  config: Record<string, unknown>;
  properties: readonly GraphProperty[];
  /** The pages its cells read, which a relation's, a rollup's or a formula's groups read. */
  pagesById: Readonly<Record<string, Page>>;
  /** The properties of the databases its rollups read. */
  propsByDatabaseId?: Readonly<Record<string, readonly DbProperty[]>>;
  currentUserId?: string;
  bounds: BoardGroupingBounds;
  groupMode: BoardGroupMode;
  emptyLabel: string;
};

// The types whose groups a card's own value says, so that a card changed
// moves between its columns alone; others read the pages around it.
const READ_ALONE = new Set(["title", "rich_text", "text", "number", "date", "select", "multi_select", "status", "checkbox", "url", "email",
  "phone_number", "created_time", "last_edited_time"]);
const NONE = "__none";

// A person's columns are the people its cards name, each where it is first
// named (boardGroupOptions): its cards too move between them alone, while
// none of those columns would come, go or stand elsewhere (2026-11-11: each
// edit that arrived on a board of 100,000 cards by a person grouped every
// card again). One added from "+ 신규 그룹" stands whether or not a card names them.
const keptPeople = ({ property, config }: Input) => property.type === "person" ? tableKeptGroupKeys(config) : new Set<string>();
const othersOf = (input: Input) => {
  if (input.property.type !== "person") return undefined;
  const kept = keptPeople(input);
  return (bucket: BoardBucket<GraphRow>) => bucket.key !== NONE && !kept.has(bucket.key);
};

type Made = { inputs: readonly unknown[]; value: BoardColumns; buckets: readonly BoardBucket<GraphRow>[]; keysOf: BoardColumns["keysOf"] };
const kept = new WeakMap<readonly GraphRow[], Made>();

/**
 * A board's columns by its group property, each with its cards: kept for
 * its cards with what made them and, when an edit that arrives changes some
 * cards or moves them under its sorts and filters, followed for those cards
 * alone — each where its columns say, the others where they were
 * (2026-11-11: each edit that arrived on a board of 100,000 cards grouped
 * every card again, and the board did so each time it drew).
 */
export function boardColumns(input: Input): BoardColumns {
  const alone = READ_ALONE.has(input.property.type) || input.property.type === "person";
  const inputs = [input.property, input.graphProperty, input.config, input.properties, input.currentUserId, input.bounds,
    input.groupMode, input.emptyLabel, new Date().toDateString(), alone ? undefined : input.pagesById, alone ? undefined : input.pages,
    alone ? undefined : input.propsByDatabaseId];
  const same = (made: Made | undefined) => !!made && made.inputs.every((value, index) => value === inputs[index]);
  let made = kept.get(input.rows);
  if (!same(made)) {
    made = alone ? followed(input, same, inputs) : undefined;
    made ??= whole(input, inputs);
    kept.set(input.rows, made);
  }
  return made!.value;
}

function membershipOf({ property, bounds, groupMode }: Input, valueGroups: BoardValueGroups | null) {
  return (row: GraphRow) => valueGroups ? valueGroups.keysOf(row)
    : boardGroupValueIds(property, valueAsRead(row, property, row.values[property.id]?.value), bounds, groupMode);
}

// The columns of them all: grouped by its cards' values, the one for no
// value too, which hides while empty as other empty groups do
// (arrangeBoardColumns: unless its empty groups show, as a board's made
// through Notion's API, seen 2026-11-12: "글 없음" empty) and its settings
// list either way — but by when or by whom a card was made or last edited,
// which every card has, only while a card has none (BoardValueGroups
// emptyGroup); by a property's options, always, but for a checkbox's and a
// status's with a default, whose cards that set none sit under it
// (valueAsRead, Notion's, seen 2026-11-08).
function shown(input: Input, valueGroups: BoardValueGroups | null, buckets: readonly BoardBucket<GraphRow>[]) {
  const { property } = input;
  const keepsNone = valueGroups ? valueGroups.emptyGroup : property.type !== "checkbox"
    && !(property.type === "status" && statusDefaultOptionId(property.config as Record<string, unknown>));
  return buckets.filter((bucket) => bucket.key !== NONE || keepsNone || bucket.rows.length > 0);
}

function whole(input: Input, inputs: readonly unknown[]): Made {
  const { property } = input;
  const valueGroups = isBoardValueGroupProperty(property) ? boardValueGroups(input.graphProperty, input.rows, {
    config: input.config, properties: input.properties, pagesById: input.pagesById, propsByDatabaseId: input.propsByDatabaseId,
    currentUserId: input.currentUserId,
  }) : null;
  const options = valueGroups?.options ?? boardGroupOptions(property, input.pages, input.bounds, input.currentUserId, input.groupMode, [...keptPeople(input)]);
  // A status's options are titled by its dotted chips (Notion's, seen 2026-10-24).
  const statusOptions = property.type === "status" && input.groupMode === "option" && !valueGroups;
  const membership = membershipOf(input, valueGroups);
  const buckets = projectBoardColumns({ rows: input.rows, options: statusOptions ? options.map((option) => ({ ...option, statusOption: true })) : options,
    membership, emptyLabel: input.emptyLabel, includeEmpty: true, emptyFirst: valueGroups?.emptyFirst });
  const keysOf = bucketKeys(buckets, membership);
  return { inputs, buckets, keysOf, value: { valueGroups, options, columns: shown(input, valueGroups, buckets), keysOf } };
}

// The columns of cards made from those worked out before by replacing
// some, or by taking some out and putting some in: each card out of the
// columns it was in, each in at its place in the columns it is in now, one
// whose columns stay replaced where it is; none when a column would come or
// go, which only all the cards say.
function followed(input: Input, same: (made: Made | undefined) => boolean, inputs: readonly unknown[]): Made | undefined {
  const others = othersOf(input);
  const made = followedBuckets(input.rows, kept, same, (before) => membershipOf(input, before.value.valueGroups),
    (bucket, before) => bucket.key !== NONE && (!!before.value.valueGroups || bucket.key.startsWith("unknown:") || !!others?.(bucket)));
  if (!made || (others && !firstNamedInOrder(input.rows, made.buckets, others, membershipOf(input, null)))) return undefined;
  const { valueGroups, options } = made.before.value, { keysOf } = made.before;
  return { inputs, buckets: made.buckets, keysOf, value: { valueGroups, options, columns: shown(input, valueGroups, made.buckets), keysOf } };
}

// Whether the columns `others` picks still stand each where its people are
// first named: by the card, then by the person's place among its people.
function firstNamedInOrder(rows: readonly GraphRow[], buckets: readonly BoardBucket<GraphRow>[], others: (bucket: BoardBucket<GraphRow>) => boolean,
  membership: (row: GraphRow) => readonly unknown[]) {
  const order = placesById(rows, (row) => row.id);
  let lastCard = -1, lastPlace = -1;
  for (const bucket of buckets) {
    if (!others(bucket)) continue;
    const first = bucket.rows[0], card = first ? order.get(first.id) ?? -1 : -1;
    const place = first ? membershipValues(membership(first)).indexOf(bucket.key) : -1;
    if (card < 0 || place < 0 || card < lastCard || (card === lastCard && place <= lastPlace)) return false;
    lastCard = card;
    lastPlace = place;
  }
  return true;
}

/** A board's sub-groups over its cards, as boardLanes keeps them. */
export type KeptLanes<Extra> = {
  inputs: readonly unknown[];
  buckets: readonly BoardBucket<GraphRow>[];
  /** The keys of the sub-groups a card is in, the same while they are followed (bucketKeys). */
  keysOf(row: GraphRow): readonly string[];
  /** What made them besides, as their grouping by values (boardValueGroups). */
  extra: Extra;
};
const lanesKept = new WeakMap<readonly GraphRow[], KeptLanes<unknown>>();

/**
 * A board's sub-groups over its cards (`whole`, all grouped), kept for its
 * cards with what made them (`inputs`) and followed for the cards an edit
 * that arrives changes, as its columns are, each card by `membership` of
 * what was kept; a sub-group that only its cards make (`made`) that would
 * come or go has them all grouped again (2026-11-11: a sub-grouped board of
 * 100,000 cards grouped every card into its sub-groups each time it drew).
 */
export function boardLanes<Extra>(rows: readonly GraphRow[], inputs: readonly unknown[], whole: () => {
  buckets: readonly BoardBucket<GraphRow>[]; membership: (row: GraphRow) => readonly unknown[]; extra: Extra;
}, followed?: { membership: (before: KeptLanes<Extra>) => (row: GraphRow) => readonly unknown[]; made: (bucket: BoardBucket<GraphRow>, before: KeptLanes<Extra>) => boolean }): KeptLanes<Extra> {
  const store = lanesKept as WeakMap<readonly GraphRow[], KeptLanes<Extra>>;
  const same = (kept: KeptLanes<Extra> | undefined) => !!kept && kept.inputs.length === inputs.length && kept.inputs.every((value, index) => value === inputs[index]);
  let kept = store.get(rows);
  if (!same(kept)) {
    const moved = followed && followedBuckets(rows, store, same, followed.membership, followed.made);
    if (moved) kept = { inputs, buckets: moved.buckets, keysOf: moved.before.keysOf, extra: moved.before.extra };
    else {
      const made = whole();
      kept = { inputs, buckets: made.buckets, keysOf: bucketKeys(made.buckets, made.membership), extra: made.extra };
    }
    store.set(rows, kept);
  }
  return kept!;
}

// A value's bucket as projectBoardColumns finds it: an option's by its ID,
// then by its name, then one of none of them; the one for no value by none.
function bucketPlaces(buckets: readonly BoardBucket<unknown>[]) {
  const byId = new Map<string, number>(), byName = new Map<string, number>(), byOther = new Map<string, number>();
  let none = -1;
  buckets.forEach((bucket, place) => {
    if (bucket.key === NONE) none = place;
    else if (bucket.key.startsWith("unknown:")) byOther.set(bucket.label, place);
    else {
      byId.set(bucket.key, place);
      byName.set(bucket.label, place);
    }
  });
  return { none, of: (value: string) => byId.get(value) ?? byName.get(value) ?? byOther.get(value) };
}

const membershipValues = (values: readonly unknown[]) => [...new Set(values.map((value) => String(value ?? "").trim()).filter(Boolean))];

/**
 * The keys of the buckets a card is in by `membership`, as
 * projectBoardColumns places it, kept for each card: a value none of them
 * holds goes in the one it would make.
 */
function bucketKeys(buckets: readonly BoardBucket<unknown>[], membership: (row: GraphRow) => readonly unknown[]) {
  const { none, of } = bucketPlaces(buckets);
  const keys = new WeakMap<GraphRow, readonly string[]>();
  return (row: GraphRow) => {
    let found = keys.get(row);
    if (!found) {
      const values = membershipValues(membership(row));
      found = values.length ? values.map((value) => { const place = of(value); return place === undefined ? `unknown:${value}` : buckets[place].key; })
        : [none >= 0 ? buckets[none].key : NONE];
      keys.set(row, found);
    }
    return found;
  };
}

// The buckets of cards made from those kept for the cards these were made
// from (`kept`, as `same` accepts), with the cards changed since moved
// between them by `membership`; none when one would come, or one `made`
// by its cards alone would go. A bucket's cards changed are a list made
// from its cards before (lib/rowsReplaced.ts), so what is kept for them
// follows them too.
function followedBuckets<Kept extends { buckets: readonly BoardBucket<GraphRow>[] }>(rows: readonly GraphRow[], kept: WeakMap<readonly GraphRow[], Kept>,
  same: (made: Kept | undefined) => boolean, membershipOf: (before: Kept) => (row: GraphRow) => readonly unknown[],
  made: (bucket: BoardBucket<GraphRow>, before: Kept) => boolean): { before: Kept; buckets: BoardBucket<GraphRow>[] } | undefined {
  const replaced = workedOutBefore(rows, kept), spliced = replaced ? undefined : splicedBefore(rows, kept);
  const before = replaced?.value ?? spliced?.value;
  if (!before || !same(before)) return undefined;
  const membership = membershipOf(before);
  const { none, of } = bucketPlaces(before.buckets);
  // The columns a card is in, by their places; none when one is not there yet.
  const columnsOf = (row: GraphRow) => {
    const values = membershipValues(membership(row));
    const places = values.length ? values.map(of) : [none];
    return places.every((place) => place !== undefined && place >= 0) && new Set(places).size === places.length ? places as number[] : undefined;
  };
  const out: Array<[GraphRow, number[]]> = [], come: Array<[GraphRow, number[]]> = [], swapped: Array<[GraphRow, number[]]> = [];
  const pairs = replaced ? replaced.at.map((index) => [replaced.from[index], rows[index]] as const)
    : [...spliced!.removed.map((index) => [spliced!.from[index], undefined] as const), ...spliced!.added.map((index) => [undefined, rows[index]] as const)];
  for (const [was, row] of pairs) {
    const from = was && columnsOf(was), to = row && columnsOf(row);
    if ((was && !from) || (row && !to) || (was && row && was.id !== row.id)) return undefined;
    if (from && to && from.length === to.length && from.every((place, at) => place === to[at])) swapped.push([row!, to]);
    else {
      if (from) out.push([was!, from]);
      if (to) come.push([row!, to]);
    }
  }
  // Each bucket changed: its cards replaced in place, taken out, and put in.
  const changes = new Map<number, { swaps: Map<number, GraphRow>; gone: number[]; coming: GraphRow[] }>();
  const change = (place: number) => {
    let found = changes.get(place);
    if (!found) changes.set(place, found = { swaps: new Map(), gone: [], coming: [] });
    return found;
  };
  for (const [row, places] of [...swapped, ...out]) for (const place of places) {
    const at = before.buckets[place].rows.findIndex((card) => card.id === row.id);
    if (at < 0) return undefined;
    if (swapped.some(([card]) => card === row)) change(place).swaps.set(at, row);
    else change(place).gone.push(at);
  }
  for (const [row, places] of come) for (const place of places) change(place).coming.push(row);
  const order = placesById(rows, (row) => row.id);
  const lists = new Map<number, GraphRow[]>();
  for (const [place, { swaps, gone, coming }] of changes) {
    const cards = swaps.size ? replaceItems(before.buckets[place].rows, swaps) : before.buckets[place].rows;
    lists.set(place, gone.length || coming.length
      ? moveItems(cards, gone, coming, (card, other) => (order.get(card.id) ?? -1) < (order.get(other.id) ?? -1)) : cards);
  }
  // A bucket its cards make goes with its last card.
  for (const [place, cards] of lists) if (!cards.length && made(before.buckets[place], before)) return undefined;
  return { before, buckets: before.buckets.map((bucket, place) => lists.has(place) ? { ...bucket, rows: lists.get(place)! } : bucket) };
}

/**
 * A column's first `limit` cards and, after them in its order, those made
 * here (`made`), found by their places rather than every card read each
 * time the column draws (2026-11-11: a board of 100,000 cards went over
 * each column's cards twice whenever it drew).
 */
export function columnCards<Row extends { id: string }>(rows: readonly Row[], limit: number | undefined, made: ReadonlySet<string> | undefined): Row[] {
  if (limit === undefined || rows.length <= limit) return rows as Row[];
  const first = rows.slice(0, limit);
  if (!made?.size) return first;
  const places = placesById(rows, (row) => row.id);
  const later = [...made].map((id) => places.get(id) ?? -1).filter((place) => place >= limit).sort((left, right) => left - right);
  return later.length ? [...first, ...[...new Set(later)].map((place) => rows[place])] : first;
}
