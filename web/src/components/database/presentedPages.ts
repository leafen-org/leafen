import type { GraphProperty, GraphRow } from "@/contentGraph";
import { lookupById, lookupList, lookupOver } from "@/lib/keptLookup";
import { madeBetween, placesById, replaceItems, spliceItems, splicedBefore, workedOutBefore } from "@/lib/rowsReplaced";
import type { Page } from "@/lib/types";
import { galleryPresentationPage, type GalleryPagePresentation } from "./galleryViewModel";

/**
 * A card view's rows as pages by ID — a board's, a list's, a gallery's, a
 * calendar's, a timeline's — made for each list of rows once, and for a
 * list with rows changed in place again for those rows alone
 * (lib/rowsReplaced.ts), each other row keeping its page (2026-11-11: each
 * edit that arrived on a view of 100,000 rows presented every row anew, and
 * a list view each time it drew).
 */

/** The rows by ID, kept for each list of rows and following it (lib/keptLookup.ts). */
export function rowsById(rows: readonly GraphRow[]): Readonly<Record<string, GraphRow>> {
  return lookupById(rows);
}

const rowIdsKept = new WeakMap<readonly GraphRow[], ReadonlySet<string>>();

/**
 * The rows' IDs, kept for each list of rows: a list made from another by
 * replacing rows, or by moving them, with the same IDs keeps its set.
 */
export function rowIds(rows: readonly GraphRow[]): ReadonlySet<string> {
  let ids = rowIdsKept.get(rows);
  if (!ids) {
    const replaced = workedOutBefore(rows, rowIdsKept), spliced = replaced ? undefined : splicedBefore(rows, rowIdsKept);
    const taken = spliced && new Set(spliced.removed.map((index) => spliced.from[index].id));
    const moved = spliced && taken && spliced.removed.length === spliced.added.length && spliced.added.every((index) => taken.has(rows[index].id));
    ids = replaced && replaced.at.every((index) => replaced.from[index].id === rows[index].id) ? replaced.value
      : moved ? spliced.value : new Set(rows.map((row) => row.id));
    rowIdsKept.set(rows, ids);
  }
  return ids;
}

const idsJoined = new WeakMap<ReadonlySet<string>, WeakMap<ReadonlySet<string>, ReadonlySet<string>>>();

/** `ids` with `more`, kept for the pair: `more` itself when it holds them all. */
export function idsWith(ids: ReadonlySet<string>, more: ReadonlySet<string> | undefined): ReadonlySet<string> {
  if (!more?.size) return ids;
  let byMore = idsJoined.get(ids);
  if (!byMore) idsJoined.set(ids, byMore = new WeakMap());
  let both = byMore.get(more);
  if (!both) {
    let all = true;
    for (const id of ids) if (!more.has(id)) { all = false; break; }
    byMore.set(more, both = all ? more : new Set([...ids, ...more]));
  }
  return both;
}

type Presentation = GalleryPagePresentation & { structuralPlaceholder?: boolean; isLocked?: boolean };
type PageSnapshot = {
  /** Every row the view has read, by ID; not read when its rows are all of them (rowsAreAll). */
  pagesById?: Readonly<Record<string, GraphRow>>;
  /** Whether its rows are every row the view has read: a board's, a timeline's. */
  rowsAreAll?: boolean;
  rows: readonly GraphRow[];
  workspaceId: string;
  pagePresentationById: Readonly<Record<string, Presentation | undefined>>;
  pageContentPreviews?: Readonly<Record<string, { revision: number } | undefined>>;
};
/** What a view adds to each page made: its content preview's revision, its own finish; one object for each kind of view, which its pages are kept by (pagesKept). */
export type PagePresenting = { previews?: boolean; finish?: (page: Page, presentation: Presentation | undefined) => void };

type Kept = { inputs: readonly unknown[]; pages: readonly Page[]; byId: Readonly<Record<string, Page>> };
type PagesKept = WeakMap<readonly GraphRow[], Kept>;
// Each kind of view keeps the pages it makes apart (its `presenting`): the
// views of one database on a page read the same rows, and a board, a list
// and a calendar each make their pages their own way; kept as one, each made
// every row's page again over the others', and every card of each drew
// again for a row made in the table beside them (2026-11-27: a table's
// "+ 새 페이지" with its board, list, gallery, calendar and timeline on the
// page drew some 6,900 parts for 50 rows, 290 ms on the NAS and CPU ×2).
const pagesKept = new WeakMap<PagePresenting, PagesKept>(), rowPagesKept: PagesKept = new WeakMap();
function pagesKeptFor(presenting: PagePresenting) {
  let store = pagesKept.get(presenting);
  if (!store) pagesKept.set(presenting, store = new WeakMap());
  return store;
}
// The rows each list of pages made by keptPages was made from.
const pagesMadeFrom = new WeakMap<readonly Page[], readonly GraphRow[]>();
const someKeys = <Value extends object>(record: Value | undefined) => {
  for (const key in record) if (Object.hasOwn(record, key)) return record;
  return undefined;
};

// The pages of rows taken out of the list and put in it (a row made, a copy,
// rows to the trash and back): those put in made, each between the places
// of the pages around it (madeBetween), the others kept, so a list drawn
// again draws the rows put in alone (2026-11-14: every row of a list was
// made again for the one made, and every row after a copy for its place).
function splicedPages(spliced: { value: Kept; removed: readonly number[]; added: readonly number[] }, source: readonly GraphRow[],
  pageOf: (row: GraphRow, position: number) => Page) {
  const moved = spliceItems<Page | null>(spliced.value.pages, spliced.removed, spliced.added.map((index) => [index, null] as const));
  if (moved.some((page, index) => page && page.id !== source[index].id)) return source.map(pageOf);
  const made = madeBetween(moved, (index, position) => pageOf(source[index], position));
  return spliceItems(spliced.value.pages, spliced.removed, spliced.added.map((index) => [index, made.get(index)!] as const));
}

// Each row's page (`pageOf`) in the rows' order and by ID, kept for the
// rows with what made them (`inputs`): a list with rows changed in place has
// their pages alone made again.
function keptPages(store: PagesKept, source: readonly GraphRow[], inputs: readonly unknown[], pageOf: (row: GraphRow, position: number) => Page) {
  const same = (kept: Kept) => kept.inputs.every((input, index) => input === inputs[index]);
  let kept = store.get(source);
  if (!kept || !same(kept)) {
    const before = workedOutBefore(source, store);
    const spliced = before ? undefined : splicedBefore(source, store);
    // A row changed in place takes its page's place (splicedPages).
    const pages = before && same(before.value)
      ? replaceItems(before.value.pages, new Map(before.at.map((index) => [index, pageOf(source[index], before.value.pages[index]?.position ?? index)])))
      : spliced && same(spliced.value) ? splicedPages(spliced, source, pageOf)
      : source.map(pageOf);
    kept = { inputs, pages, byId: lookupById(pages) };
    pagesMadeFrom.set(pages, source);
    store.set(source, kept);
  }
  return kept;
}

/**
 * The pages of the rows a view has read (its snapshot's pages by ID, then
 * its rows not among them), each where it stands among them, as its cards
 * read them; kept for the rows the record was made from (rowsById).
 */
export function presentedPagesById(snapshot: PageSnapshot, titleProperty: GraphProperty | undefined, presenting: PagePresenting): Record<string, Page> {
  const presentation = someKeys(snapshot.pagePresentationById), previews = presenting.previews ? someKeys(snapshot.pageContentPreviews) : undefined;
  const pageOf = (row: GraphRow, position: number) => {
    const shown = presentation?.[row.id];
    const page = galleryPresentationPage({ row, titleProperty, workspaceId: snapshot.workspaceId, previewRevision: previews?.[row.id]?.revision, presentation: shown, position });
    presenting.finish?.(page, shown);
    return page;
  };
  const inputs = [titleProperty, snapshot.workspaceId, presentation, previews, presenting.finish];
  if (snapshot.rowsAreAll) return keptPages(pagesKeptFor(presenting), snapshot.rows, inputs, pageOf).byId as Record<string, Page>;
  const record = snapshot.pagesById ?? {}, source = lookupList<GraphRow>(record);
  // Rows shown that the record doesn't hold as they are follow its own, as they always have.
  if (!source || !rowsHeld(snapshot.rows, source, record)) {
    const combined = new Map([...Object.values(record), ...snapshot.rows].map((row) => [row.id, row]));
    return Object.fromEntries([...combined.values()].map((row, index) => [row.id, pageOf(row, index)]));
  }
  return keptPages(pagesKeptFor(presenting), source, inputs, pageOf).byId as Record<string, Page>;
}

// Whether a record holds each of the rows as it is, kept for the rows and
// the record's list: rows made from those checked by replacing items, or by
// taking some out and putting some in (a row moved under the view's sorts),
// and a list made from the record's before by replacing items, are checked
// again for the items replaced or put in alone.
const rowsChecked = new WeakMap<readonly GraphRow[], { source: readonly GraphRow[]; held: boolean }>();
const rowId = (row: GraphRow) => row.id;

function rowsHeld(rows: readonly GraphRow[], source: readonly GraphRow[], record: Readonly<Record<string, GraphRow>>) {
  const checked = rowsChecked.get(rows);
  if (checked?.source === source) return checked.held;
  const replaced = workedOutBefore(rows, rowsChecked), spliced = replaced ? undefined : splicedBefore(rows, rowsChecked);
  const before = replaced ? { value: replaced.value, changed: replaced.at } : spliced ? { value: spliced.value, changed: spliced.added }
    : checked && { value: checked, changed: [] as readonly number[] };
  const sourceBefore = !before?.value.held ? undefined
    : before.value.source === source ? { at: [] as number[] } : workedOutBefore(source, new WeakMap([[before.value.source, true]]));
  const held = before && sourceBefore
    ? before.changed.every((index) => record[rows[index].id] === rows[index])
      && sourceBefore.at.every((index) => {
        const place = placesById(rows, rowId).get(source[index].id);
        return place === undefined || rows[place] === source[index];
      })
    : rows.every((row) => record[row.id] === row);
  rowsChecked.set(rows, { source, held });
  return held;
}

/**
 * The pages of `rows` in their order, without reading each, when `pagesById`
 * (presentedPagesById's) was made from those rows: a view shows the rows it
 * read as they are, a board every one.
 */
export function rowPagesInOrder(rows: readonly GraphRow[], pagesById: object): readonly Page[] | undefined {
  const pages = lookupList<Page>(pagesById);
  return pages && pagesMadeFrom.get(pages) === rows ? pages : undefined;
}

/** The rows' pages in their order and by ID, each where it stands among them, kept as presentedPagesById's are. */
export function presentedRowPages(rows: readonly GraphRow[], titleProperty: GraphProperty | undefined, workspaceId: string) {
  return keptPages(rowPagesKept, rows, [titleProperty, workspaceId], (row, position) => galleryPresentationPage({ row, titleProperty, workspaceId, position }));
}

/**
 * `pages` over `base` (the related pages a view's cells read), kept for the
 * pair, reading through both when either follows its list (lib/keptLookup.ts).
 */
export function pagesOver<Base extends Readonly<Record<string, unknown>>>(base: Base, pages: Readonly<Record<string, Page>>) {
  return lookupOver(base, pages) as Base & Record<string, Page>;
}
