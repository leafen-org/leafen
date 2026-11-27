import type { PlaceSearchResponse } from "@/administratorWire.generated";
import type { ProductStatusSink } from "@/lib/productStatus";
import type { Dispatch, SetStateAction } from "react";
import { i18next } from "@/i18n";
import { refuseViewQueryGrowth } from "@/lib/viewQueryBound";
import {
  type ContentGraph,
  type GraphDataSource,
  type GraphOperation,
  type GraphProperty,
  type GraphRow,
  type GraphValueType,
  type GraphView,
} from "@/contentGraph";
import { distinctGraphPropertyNames, graphPropertiesAfterCommit } from "@/lib/graphFormulaReferences";
import { sendRowValues, type RowValuesAnswer } from "@/lib/rowValuesKept";
import { typedRowValueIn, typedRowValueSent } from "@/lib/rowValuesTyped";
import { isRowValueChanged } from "@/lib/rowTitleSave";
import type { TitleValue } from "@/lib/titleMerge";
import { newId } from "@/lib/ids";
import { rowsWhileTypeChanges, typesKeepTheirCells } from "@/lib/propertyTypeChangeRows";
import { tableKeepsItsShownProperties } from "@/components/database/viewLayoutDefaults";
import { tableConfigAfterAdd } from "@/components/database/tablePropertyMenuModel";
import type { CopiesPlace } from "@/components/database/viewRowOrder";
import {
  graphDataSourceAuthorityPageId, graphOperationsAuthorityPageId, persistGraphOperationRuns, saveRowsShownAtOnce, saveShownAtOnce,
} from "@/lib/nativeGraphAuthority";
import type { InteractiveAutomationExecuteInput, InteractiveAutomationExecuteResponse } from "@/interactiveAutomationWire.generated";
import type { Session } from "@/types";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { api } from "@/api";
import type { SearchOrganizationPeopleInput, SearchOrganizationPeopleResult } from "@/lib/typeaheadSearch";
import { markDatabaseViewShown } from "@/lib/databaseDeletedProperties";
import { propertyDeletion } from "@/lib/relatedPropertyDeletion";
import { rowMoveStep, saveRowMove, stepRowMove, type RowMoveStep } from "@/lib/nativeRowMoveSteps";
import { createTableRowSteps, type RecordRows, type RowStep } from "@/lib/nativeTableRowSteps";
import { sameCell, sameValue } from "@/lib/valueSteps";
import { peopleAboutsOf, peopleOwnValue, peopleRowsWritten, savePeopleAbouts } from "@/components/database/peopleSourceCells";

export type NativeTableProductAdapter = {
 searchPlaces?(query: string, signal?: AbortSignal): Promise<PlaceSearchResponse>;
  /** Makes a row after another, or last; or, given one, just before that row. */
  createRow(source: GraphDataSource, parentId: string | null, afterRowId: string | null, isCurrent: () => boolean, seedValues?: Readonly<Record<string, unknown>>, beforeRowId?: string,
    /** The sub-item relations a row made under another names it in, which the page reads before the server says. */
    subitemPair?: { parentPropertyId: string; childrenPropertyId: string },
    /** Its title is typed where it shows, and no page of it opens next. */
    atOnce?: boolean,
    /** Its page opens as it is made, which makes it no step of the page's ⌘Z. */
    opensPage?: boolean,
    /** What the press changes with the row, run as it shows so both draw together (TableView, the card views). */
    alongside?: (row: GraphRow) => void): Promise<GraphRow | null>;
  publishCreatedRow(row: GraphRow, isCurrent: () => boolean): GraphRow | null;
  /** `typed`: a value typed into its cell, saved once the typing ends (lib/typedWrites.ts). */
  commitRows(rows: readonly GraphRow[], isCurrent: () => boolean, typed?: boolean): Promise<void>;
  /**
   * Saves a row's title as commitRows does, shown at once and a step of ⌘Z,
   * through the row's page's route over the title it was typed on (`read`),
   * merged there with another saved since, which it then shows (rowValuesKept.ts).
   */
  commitRowTitleAsRead?(row: GraphRow, propertyId: string, read: TitleValue | null, isCurrent: () => boolean): Promise<void>;
  /** The row's own page body was edited here: it shows itself last edited now, by this person (rowBodyEdits.ts). */
  rowEditedHere?(rowId: string): void;
  /** Puts back what a change of values replaced (or, made again, what it gave), over the cells still as it left them. */
  restoreValues(changes: readonly ValueChange[], forward: boolean): Promise<boolean>;
  /**
   * Rows moved in a view, a step of the page's ⌘Z (nativeRowMoveSteps.ts):
   * their new places, or the view with `rowIds` in its own order, and by row
   * the values the move sets with them (another group's), in the same step;
   * `withDates`, the rows' dates written at once by another write, whose step joins it.
   */
  moveRows?(rows: readonly GraphRow[], view: GraphView | undefined, rowIds: readonly string[], isCurrent: () => boolean,
    values?: Readonly<Record<string, GraphRow["values"]>>, withDates?: boolean): Promise<void>;
  /** Takes back (or makes again) a move. */
  stepMove?(step: RowMoveStep, forward: boolean): Promise<boolean>;
  /**
   * A row a row's "+" made beside it, placed so in `view` (the view as shown,
   * its own order holding the row there): a sorted view asks whether its
   * sort goes, true when it asks (useDatabasePersonalQueries, sortedViewDrop.ts).
   */
  rowMadeBeside?(view: GraphView): boolean;
  /** Saves properties and, when given, the view that shows them, in one step for the table. */
  /** `from` names the view a new property is made from: only it shows the property, of the database's tables. */
  commitProperties(properties: readonly GraphProperty[], isCurrent: () => boolean, view?: GraphView, from?: string): Promise<void>;
  deleteProperties(propertyIds: readonly string[], isCurrent: () => boolean): Promise<void>;
  deleteRows(rowIds: readonly string[], isCurrent: () => boolean): Promise<void>;
  /** Takes away a row just made and left untitled with Escape, a step of the page's ⌘Z. */
  discardRow?(rowId: string): Promise<void>;
  /**
   * Rows moved to the trash at the press, as drawn before (with their sub-items), a step of the page's ⌘Z
   * from then; `letGo` stops keeping them hidden for the server's answer. What it gives takes the step out again.
   */
  rowsTrashed?(rowIds: readonly string[], drawn?: readonly GraphRow[], letGo?: () => void): (() => void) | void;
  /** Rows their own pages changed (their menu's icon, copy, move): a view that reads its rows itself ("내 작업") reads them again. */
  rowsChanged?(): void;
  /** Takes back (or makes again) a step of rows: a row made, rows to the trash. */
  stepRows?(step: RowStep, forward: boolean): Promise<boolean>;
  /**
   * A paste's rows: those it fills (`changed`) take their values and its lines past them are rows made after
   * `afterRowId` (`made`: the values each new row starts with, `seeds` theirs all), one step of ⌘Z; gives the rows made.
   */
  pasteRows?(source: GraphDataSource, changed: readonly GraphRow[], made: { parentId: string | null; afterRowId: string | null;
    seeds: Readonly<Record<string, unknown>>; rows: readonly GraphRow["values"][] }, isCurrent: () => boolean,
    subitemPair?: { parentPropertyId: string; childrenPropertyId: string }): Promise<readonly string[]>;
  /** Puts properties back or forward as a step of the page's ⌘Z history. */
  stepProperties?(properties: readonly GraphProperty[]): Promise<boolean>;
  /** Duplicates the rows, in the order given, and says which rows are their copies: drawn at once, the server making them behind (rowCopiesHere.ts). */
  /** Given the view they're copied in, a view that keeps its own order keeps the copies where `place` says (viewRowOrder.ts), else after the last row. */
  duplicateRows(rowIds: readonly string[], isCurrent: () => boolean, inView?: GraphView, place?: CopiesPlace): Promise<string[]>;
  /** A person's change to the view's settings (`step`) is a step of the page's ⌘Z; one a program makes is not. */
  updateView(view: GraphView, isCurrent: () => boolean, step?: boolean): Promise<void>;
  /** Says the view was shown, once this page's writes are saved; the page is read again when the server wrote its fallback (useShownViewFallback). */
  viewShown?(view: GraphView): Promise<void>;
  /** Runs a row's database button on the server; once it ran, the page is read again. */
  runButton?(
    rowId: string,
    propertyId: string,
    input: InteractiveAutomationExecuteInput,
    signal?: AbortSignal,
  ): Promise<InteractiveAutomationExecuteResponse>;
  /** The organization whose people a person cell picks from. */
  organizationId?: string;
  searchPeople?(
    input: SearchOrganizationPeopleInput,
    options: { signal?: AbortSignal },
  ): Promise<SearchOrganizationPeopleResult>;
};

type AdapterOptions = {
  graph: ContentGraph;
  pageId: string | null;
  persistGraphOperations(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  /** Saves a value typed into a cell once the typing ends, with the rest collected (the coordinator's collect). */
  collectGraphOperations?(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  /** Saves a person's change to views as one step of the page's ⌘Z (useContentGraphWrites commitViewStep). */
  persistViewStep?(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  rereadGraph?(pageId: string): Promise<boolean>;
  /** Reads from the server once every write made here to the page is saved. */
  readAfterWrites?<Value>(pageId: string, read: () => Promise<Value | null>): Promise<Value | null>;
  session?: Session | null;
  organizationId?: string;
  /** The workspace whose people a person cell picks from, among its organization's. */
  workspaceId?: string | null;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
  /** The open page's graph store, where a ⌘Z's rows show ahead of the server (nativeTableRowSteps.ts). */
  graphStore?: PageGraphStore | null;
  /** Records a step of values as it shows; what it gives takes the step back if the server refuses the save. */
  recordValues?(changes: readonly ValueChange[]): (() => void) | void;
  /** A row made or rows trashed here, which join it too; what it gives takes the step out again. */
  recordRows?: RecordRows;
  /** A property's type changed with nothing to convert, which joins it too. */
  recordProperties?(step: { key: string; before: readonly GraphProperty[]; after: readonly GraphProperty[] }): void;
  /** Rows moved in a view, which join it too. */
  /** `withDates`: the moved rows' dates were written at once by another write, whose date command joins the step. */
  recordMove?(step: RowMoveStep, withDates?: boolean): void;
};

type CellValue = GraphRow["values"][string] | undefined;

/** One cell's change: what it held before and what it was given. */
export type ValueChange = { rowId: string; propertyId: string; type: string; before: CellValue; after: CellValue };

/** Cells a step of ⌘Z or ⌘⇧Z put back (`forward` false) or again, said as they show, as a timeline's new bar opens its title again. */
export const ROW_VALUES_STEPPED_EVENT = "leafen:row-values-stepped";

/**
 * The cells a save changed, with what each held before. A new row's aren't a
 * step, nor are dates: the server answers a date's change with a command of
 * its own that undoes the dates it moved with it (useDatabaseDateCommandHistory).
 */
export function valueChangesOf(graph: ContentGraph, changes: readonly GraphRow[]): ValueChange[] {
  return changes.flatMap((change) => {
    const shown = graph.rows.find(({ id }) => id === change.id);
    if (!shown) return [];
    return Object.entries(change.values).map(([propertyId, after]) => ({
      rowId: change.id, propertyId, type: after?.type ?? shown.values[propertyId]?.type ?? "rich_text", before: shown.values[propertyId], after,
    })).filter((value) => value.type !== "date");
  });
}

/**
 * A row as the values it changes from the row shown, which the server merges
 * into the row's others: a value edited in another cell a moment before, still
 * on its way, is not written back over with what this edit's row held.
 */
function rowChange(shown: readonly GraphRow[], row: GraphRow): GraphRow {
  const before = shown.find(({ id }) => id === row.id);
  if (!before) return row;
  return { ...row, values: Object.fromEntries(Object.entries(row.values).filter(([id, value]) => !sameValue(value, before.values[id]))) };
}

export function createNativeTableProductAdapter({
  graph,
  pageId,
  persistGraphOperations,
  collectGraphOperations,
  persistViewStep,
  rereadGraph,
  readAfterWrites,
  session,
  organizationId,
  workspaceId,
  setGraph,
  setMessage,
  recordValues,
  recordRows,
  recordProperties,
  recordMove,
  graphStore,
}: AdapterOptions): NativeTableProductAdapter {
  async function persist(operations: GraphOperation[], typed = false) {
    return persistGraphOperationRuns(graph, pageId, operations, typed && collectGraphOperations ? collectGraphOperations : persistGraphOperations);
  }
  // Rows' values, People's About going to its person's profile (peopleSourceCells.ts); one refused there reads the graph again.
  async function persistRows(operations: GraphOperation[], typed: boolean) {
    const { rest, abouts } = peopleAboutsOf(graph, operations);
    const [saved, aboutsSaved] = await Promise.all([rest.length ? persist(rest, typed) : true, savePeopleAbouts(session, workspaceId, abouts, typed)]);
    if (!aboutsSaved) {
      setMessage(i18next.t("databaseView:toast.couldntSaveChanges"), "error");
      if (pageId) void rereadGraph?.(pageId);
    }
    return saved && aboutsSaved;
  }
  const moveWrites = { graph, pageId, persist, setGraph, editedBy: session?.user.id };
  const rowSteps = createTableRowSteps({ graph, pageId, persist, rereadGraph, session, setGraph, setMessage, recordRows, store: graphStore, readAfterWrites });

  // Each save of values is a step of the page's ⌘Z, as Notion's table (seen
  // 2026-10-21); taking one back or making it again saves without a step of
  // its own. The step is there as the edit shows, so a ⌘Z right after typing
  // takes it back (2026-11-09: it came only with the server's answer, and the
  // first ⌘Z found nothing); a save the server refuses takes its step back.
  // A value typed into its cell goes through the row's page, kept on the device at each key, in its database's turn (rowValuesTyped.ts);
  // a refused one reads the graph again.
  async function saveRows(rows: readonly GraphRow[], isCurrent: () => boolean, record: boolean, typed = false) {
    const changes = peopleRowsWritten(graph, rows.map((row) => rowChange(graph.rows, row)));
    if (!changes.length) return false;
    let written = false;
    const steps = record ? valueChangesOf(graph, changes) : [];
    const withdraw = steps.length ? recordValues?.(steps) : undefined;
    // People's rows keep to the way its own values go (peopleSourceCells.ts).
    const cell = typed && session && changes.length === 1 && graph.dataSources.find(({ id }) => id === changes[0].dataSourceId)?.peopleDatabase !== true
      ? typedRowValueIn(changes[0]) : null;
    const lane = cell && graphDataSourceAuthorityPageId(graph, pageId, changes[0].dataSourceId);
    const persisted = cell && session && lane ? async () => (written = await typedRowValueSent(session, cell, lane,
      graph.rows.find(({ id }) => id === cell.rowId)?.values[cell.propertyId], (value) => setGraph((current) => ({ ...current, rows: current.rows.map((row) =>
        row.id === cell.rowId ? { ...row, values: { ...row.values, [cell.propertyId]: value as GraphRow["values"][string] } } : row) })))
      .then((saved) => { if (!saved) { setMessage(i18next.t("databaseView:toast.couldntSaveChanges"), "error"); if (pageId) void rereadGraph?.(pageId); } return saved; }))
      : async (operations: GraphOperation[]) => (written = await persistRows(operations, typed));
    const shown = await saveRowsShownAtOnce(graph, pageId, changes, persisted, isCurrent, setGraph, session?.user.id);
    if (!written) withdraw?.();
    if (shown) setMessage(i18next.t("databaseView:status.saved"));
    return written;
  }

  async function commitRows(rows: readonly GraphRow[], isCurrent: () => boolean, typed = false) {
    await saveRows(rows, isCurrent, true, typed);
  }

  // Written through the row's page over the title it was typed on, kept in the outbox behind the page's other changes (rowValuesKept.ts);
  // its step of ⌘Z is the typing's, taken as the typing paused or ended, whatever the server makes of it.
  async function commitRowTitleAsRead(row: GraphRow, propertyId: string, read: TitleValue | null, isCurrent: () => boolean) {
    const value = row.values[propertyId], reader = session;
    if (!value || !reader || peopleOwnValue(graph, row, propertyId)) return;
    const changes = [rowChange(graph.rows, row)];
    const steps = valueChangesOf(graph, changes);
    const withdraw = steps.length ? recordValues?.(steps) : undefined;
    try {
      let answer: RowValuesAnswer | null = null;
      await saveRowsShownAtOnce(graph, pageId, changes, async () => {
        answer = await sendRowValues(reader, row.id, { values: { [propertyId]: value }, ...(read ? { expected: { [propertyId]: {
          type: value.type, value: read.title, ...(read.spans?.length ? { spans: read.spans.map((span) => ({ ...span })) } : {}) } } } : {}) },
        graphDataSourceAuthorityPageId(graph, pageId, row.dataSourceId));
        return true;
      }, isCurrent, setGraph, reader.user.id);
      // Merged with a title saved elsewhere meanwhile, the row shows the title written.
      const merged = (answer as RowValuesAnswer | null)?.merged?.[propertyId];
      if (merged && isCurrent()) setGraph((current) => ({ ...current, rows: current.rows.map((shown) => shown.id === row.id
        ? { ...shown, values: { ...shown.values, [propertyId]: merged as GraphRow["values"][string] } } : shown) }));
    } catch (error) {
      withdraw?.();
      if (!isRowValueChanged(error)) {
        setMessage(i18next.t("databaseView:toast.couldntSaveChanges"), "error");
        if (pageId) void rereadGraph?.(pageId);
      }
      throw error;
    }
  }

  // A step is taken back only over the cells as it left them: one changed
  // since, here or by someone else, isn't written over and the table says so,
  // as a date's command restores only the dates still as it left them.
  async function restoreValues(changes: readonly ValueChange[], forward: boolean) {
    const rows = new Map<string, GraphRow>();
    let left = false;
    for (const change of changes) {
      const row = rows.get(change.rowId) ?? graph.rows.find(({ id }) => id === change.rowId);
      if (!row || !sameCell(row.values[change.propertyId], forward ? change.before : change.after)) {
        left = true;
        continue;
      }
      const value = (forward ? change.after : change.before) ?? { type: change.type as GraphValueType, value: null };
      rows.set(change.rowId, { ...row, values: { ...row.values, [change.propertyId]: value } });
    }
    if (left) setMessage(i18next.t("databaseView:status.undoChanged"), "default");
    if (rows.size === 0) return true;
    // Shown at once (saveRowsShownAtOnce), they are said in the same frame.
    const saved = saveRows([...rows.values()], () => true, false);
    window.dispatchEvent(new CustomEvent(ROW_VALUES_STEPPED_EVENT, { detail: { changes: changes.filter(({ rowId }) => rows.has(rowId)), forward } }));
    return await saved;
  }
  // A view that lives with its source is saved with the properties; a linked
  // database's view lives on the page showing it and is saved after them.
  // Both show at once, so a new column appears where the view puts it and a
  // setting changed right after builds on this one.
  // A property made from a view stays out of the database's other tables whose
  // shown properties were set, as Notion's (seen 2026-11-04: "오른쪽에 삽입" in
  // one table, not in the next; a table never touched shows it).
  function otherTablesWithout(properties: readonly GraphProperty[], view: GraphView | undefined, from: string | undefined) {
    if (!from) return [];
    const made = properties.filter((property) => !graph.properties.some((known) => known.id === property.id));
    return made.length ? graph.views
      .filter((other) => other.type === "table" && other.id !== from && other.id !== view?.id && other.dataSourceId === made[0].dataSourceId
        && tableKeepsItsShownProperties(other.config))
      .map((other) => ({ ...other, config: {
        ...other.config,
        hiddenProperties: [...new Set([...((other.config.hiddenProperties as string[] | undefined) ?? []), ...made.map(({ id }) => id)])],
      } })) : [];
  }

  // A linked table on another page than its source, a new property made from
  // it, names the property even where it would show it anyway: the server
  // hides it in the source's tables that set what they show but for those
  // that name it (database_new_property_hiding.go).
  function linkedTableNaming(properties: readonly GraphProperty[], from: string | undefined): GraphView | undefined {
    const table = graph.views.find((candidate) => candidate.id === from && candidate.type === "table");
    const made = properties.filter((property) => property.dataSourceId === table?.dataSourceId
      && !graph.properties.some((known) => known.id === property.id));
    if (!table || !made.length || !tableKeepsItsShownProperties(table.config)
      || graphDataSourceAuthorityPageId(graph, pageId, table.dataSourceId) === pageId) return undefined;
    const config = made.reduce<Record<string, unknown>>((next, property) => tableConfigAfterAdd(next, [
      ...graph.properties.filter((known) => known.dataSourceId === table.dataSourceId), ...made,
    ], property.id), table.config);
    return { ...table, config };
  }

  async function commitProperties(changed: readonly GraphProperty[], isCurrent: () => boolean, view?: GraphView, from?: string) {
    const properties = distinctGraphPropertyNames(graph.properties, changed);
    if (!properties.length) return;
    // A change between types that store their cells alike converts nothing;
    // ⌘Z takes it back all the same, as Notion's (seen 2026-11-04).
    const retyped = properties.filter((next) => {
      const was = graph.properties.find((property) => property.id === next.id);
      return was && was.type !== next.type && typesKeepTheirCells(was.type, next.type);
    });
    const before = retyped.flatMap((next) => graph.properties.filter((property) => property.id === next.id));
    // A name or setting changed on a property ⌘Z takes back too (Notion's, seen 2026-11-06).
    const edited = properties.filter((next) => {
      const was = graph.properties.find((property) => property.id === next.id);
      return was && was.type === next.type && JSON.stringify(was) !== JSON.stringify(next);
    });
    const editedBefore = edited.flatMap((next) => graph.properties.filter((property) => property.id === next.id));
    const saved: GraphOperation[] = properties.map((property) => ({ kind: "upsertProperty", property }));
    const others = otherTablesWithout(properties, view, from);
    const shownIn = view ?? linkedTableNaming(properties, from);
    const shown: GraphOperation[] = [...(shownIn ? [shownIn] : []), ...others].map((next) => ({ kind: "upsertView", view: next }));
    const together = shown.length > 0 && graphOperationsAuthorityPageId(graph, pageId, [...saved, ...shown]) !== null;
    // It joins the history once saved, whether or not the menu that asked is still open.
    const persistStep = async (operations: GraphOperation[]) => {
      const done = await persist(operations);
      if (done && retyped.length && !stepping) recordProperties?.({ key: `property-type/${newId()}`, before, after: retyped });
      if (done && edited.length && !stepping) {
        recordProperties?.({ key: `property-edit/${edited.map(({ id }) => id).join(",")}/${newId()}`, before: editedBefore, after: edited });
      }
      return done;
    };
    // The views of a page other than the source's go first, naming the new
    // property before the source has it: the server leaves that page's tables
    // as they are and hides it in the source's (database_new_property_hiding.go).
    // The writes go out in the order made (nativeContentGraphWriteCoordinator).
    const viewsSaved = shown.length && !together ? persist(shown) : undefined;
    if (!await saveShownAtOnce(graph, pageId, together ? [...saved, ...shown] : saved, persistStep, isCurrent, () => setGraph((current) => ({
      ...current,
      rows: rowsWhileTypeChanges(current.rows, current.properties, properties),
      properties: graphPropertiesAfterCommit(current.properties, properties),
      views: current.views.map((candidate) => [...(shownIn ? [shownIn] : []), ...others].find((next) => next.id === candidate.id) ?? candidate),
    })))) return;
    if (viewsSaved && (!await viewsSaved || !isCurrent())) return;
    // A new Verification property brings its Owner, which the server adds.
    const verifying = properties.some((property) => property.type === "verification"
      && graph.properties.find((candidate) => candidate.id === property.id)?.type !== "verification");
    if (verifying && pageId && rereadGraph) await rereadGraph(pageId);
    setMessage(i18next.t("databaseView:status.saved"));
  }

  let stepping = false;
  async function stepProperties(properties: readonly GraphProperty[]) {
    stepping = true;
    try {
      await commitProperties(properties, () => true);
      return true;
    } finally {
      stepping = false;
    }
  }

  return {
    organizationId,
    rowEditedHere: (rowId) => {
      const now = new Date().toISOString();
      const by = session?.user.id;
      setGraph((current) => current.rows.some(({ id }) => id === rowId)
        ? { ...current, rows: current.rows.map((row) => row.id === rowId ? { ...row, updatedAt: now, ...(by ? { lastEditedBy: by } : {}) } : row) }
        : current);
    },
    searchPlaces: session && pageId ? (query,signal) => api.searchPlaces(session,pageId,query,signal) : undefined,
    // A person cell offers its workspace's people, as Notion's does, not its
    // organization's other workspaces' (organization_people_workspace.go),
    // and those it invited by an address without an account yet.
    searchPeople: session
      ? (input, options) => api.searchOrganizationPeople(session, { ...input, ...(workspaceId ? { workspaceId, invitees: true } : {}) }, options)
      : undefined,
    runButton: session ? async (rowId, propertyId, input, signal) => {
      const row = graph.rows.find(({ id }) => id === rowId);
      const authorityPageId = row ? graphDataSourceAuthorityPageId(graph, pageId, row.dataSourceId) : null;
      if (!row || !authorityPageId) throw new Error("A database button runs on its row's page.");
      const response = await api.executeDatabaseButton(session, {
        pageId: authorityPageId, dataSourceId: row.dataSourceId, rowId, propertyId,
      }, input, signal);
      if (pageId && rereadGraph) await rereadGraph(pageId);
      return response;
    } : undefined,
    // Rows made, trashed and brought back, taken away as made, and duplicated (nativeTableRowSteps.ts).
    ...rowSteps,
    // A paste's step takes its rows to the trash and puts back what the rows it filled held; made again, both come back.
    async stepRows(step, forward) {
      if (!step.values?.length) return rowSteps.stepRows!(step, forward);
      if (forward) return (!step.rowIds.length || await rowSteps.stepRows!(step, true)) && restoreValues(step.values, true);
      return await restoreValues(step.values, false) && (!step.rowIds.length || rowSteps.stepRows!(step, false));
    },
    // Taken as the press found the table: what it shows on the way (its rows, the values) is its own (tableCellPaste.ts).
    async pasteRows(source, changed, made, isCurrent, subitemPair) {
      if (!isCurrent()) return [];
      const rows = peopleRowsWritten(graph, changed.map((row) => rowChange(graph.rows, row)));
      const values = valueChangesOf(graph, rows).filter(({ before, after }) => !sameCell(before, after));
      const shown = rowSteps.makeRows(source, made.parentId, made.afterRowId, made.seeds, made.rows, subitemPair);
      const drop = shown.rows.length ? recordRows?.({ key: `rows/pasted/${newId()}`, kind: "made", rowIds: shown.rows.map(({ id }) => id),
        made: shown.rows[0].values, values }) : undefined;
      const written = rows.length ? await saveRows(rows, () => true, false) : true;
      if (!written || !await shown.saved) drop?.();
      return shown.rows.map(({ id }) => id);
    },
    commitRows,
    commitRowTitleAsRead,
    restoreValues,
    // Only their places go, with the values the move sets (`values`, another group's): the others stay as they are.
    async moveRows(rows, view, rowIds, isCurrent, values, withDates) {
      const moved = rows.map((row) => ({ ...row, values: values?.[row.id] ?? {} }));
      const step = rowMoveStep(graph, moved, view, rowIds);
      if (step.rows.length || step.view) recordMove?.(step, withDates);
      if (await saveRowMove(moveWrites, moved, view, isCurrent)) setMessage(i18next.t("databaseView:status.saved"));
    },
    stepMove: (step, forward) => stepRowMove(moveWrites, step, forward, setMessage),
    commitProperties,
    stepProperties,
    async deleteProperties(propertyIds, isCurrent) {
      const ids = [...new Set(propertyIds)].filter((id) => graph.properties.some((property) => property.id === id && property.type !== "title"));
      if (!ids.length || !await persist(ids.map((id) => ({ kind: "deleteProperty", propertyDeletion: propertyDeletion(id) })))) return;
      if (!isCurrent()) return;
      const deleted = new Set(ids);
      setGraph((current) => ({ ...current, properties: current.properties.filter((property) => !deleted.has(property.id)) }));
      setMessage(i18next.t("databaseView:status.deleted"));
    },
    async updateView(view, isCurrent, step) {
      if (refuseViewQueryGrowth(graph.views.find((candidate) => candidate.id === view.id)?.config, view.config, setMessage)) return;
      const save = step && persistViewStep ? (operations: GraphOperation[]) => persistGraphOperationRuns(graph, pageId, operations, persistViewStep) : persist;
      if (!await saveShownAtOnce(graph, pageId, [{ kind: "upsertView", view }], save, isCurrent, () => setGraph((current) => ({
        ...current, views: current.views.map((candidate) => candidate.id === view.id ? view : candidate),
      })))) return;
      setMessage(i18next.t("databaseView:status.saved"));
    },
    // After a deletion still being sent, so the server holds it.
    async viewShown(view) {
      const authorityPageId = graphOperationsAuthorityPageId(graph, pageId, [{ kind: "upsertView", view }]);
      if (!authorityPageId || !session || !readAfterWrites) return;
      const shown = await readAfterWrites(authorityPageId, () => markDatabaseViewShown(session, authorityPageId, view.id));
      if (shown?.changed && rereadGraph) await rereadGraph(authorityPageId);
    },
  };
}
