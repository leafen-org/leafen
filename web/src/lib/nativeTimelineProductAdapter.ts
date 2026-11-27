import type { ProductStatusSink } from "@/lib/productStatus";
import type { Dispatch, SetStateAction } from "react";
import { i18next } from "@/i18n";
import { refuseViewQueryGrowth } from "@/lib/viewQueryBound";
import { api } from "@/api";
import {
  createGraphProperty,
  type ContentGraph,
  type GraphDataSource,
  type GraphOperation,
  type GraphProperty,
  type GraphRow,
  type GraphRowValue,
  type GraphView,
} from "@/contentGraph";
import type { DatabaseDependencyGraphResponse } from "@/databaseQueryWire.generated";
import type { ViewFilterSeedValues } from "@/components/database/query";
import { writtenDateRange } from "@/components/database/dateUtils";
import { firstViewDateSource } from "@/components/database/viewDateSource";
import { newId } from "@/lib/ids";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import {
  graphDataSourceAuthorityPageId,
  persistGraphOperationRuns,
  saveRowsShownAtOnce,
  saveShownAtOnce,
} from "@/lib/nativeGraphAuthority";
import type { Session } from "@/types";
import { newRowStart } from "@/lib/newRowStart";
import { makeRowHere } from "@/lib/rowMadeHere";
import { spliceItems } from "@/lib/rowsReplaced";
import { recordRowMade, type RecordRows } from "@/lib/nativeTableRowSteps";
import { valueChangesOf, type ValueChange } from "@/lib/nativeTableProductAdapter";
import { sameCell } from "@/lib/valueSteps";

export type NativeTimelineDependencyRead = {
  cursor: string | null;
  dataSourceId: string;
  expectedRevision: number;
  signal: AbortSignal;
  visibleRowIds: readonly string[];
};

export type NativeTimelineProductAdapter = {
  /** alongside: what the press changes with the row (its title's editor), run as it shows so both draw together. */
  createDatedRow(source: GraphDataSource, view: GraphView, dayKey: string, seeds: ViewFilterSeedValues, isCurrent: () => boolean,
    alongside?: (row: GraphRow) => void): Promise<GraphRow | null>;
  createDatePropertyAndSelect(source: GraphDataSource, view: GraphView, name: string, isCurrent: () => boolean): Promise<void>;
  /** id: the link's, when the caller shows it before the server has it (TimelineView). */
  createDependency(source: GraphDataSource, successorRowId: string, predecessorRowId: string, revision: number, isCurrent: () => boolean, id?: string): Promise<void>;
  /** Takes the links away by their IDs, saying whether the server took it. */
  deleteDependency(source: GraphDataSource, edgeIds: readonly string[], revision: number, isCurrent: () => boolean): Promise<boolean>;
  loadDependencies(input: NativeTimelineDependencyRead): Promise<DatabaseDependencyGraphResponse>;
  /** Writes a row's values, and those of the rows that move with it (`alongside`, by row: chosen bars carried along the days), in one write. */
  moveOrResizeRow(rowId: string, values: Readonly<Record<string, unknown>>, isCurrent: () => boolean, alongside?: Readonly<Record<string, Readonly<Record<string, unknown>>>>): Promise<void>;
  /** A person's change to the view's settings (`step`) is a step of the page's ⌘Z; one a program makes is not. */
  updateView(view: GraphView, isCurrent: () => boolean, step?: boolean): Promise<void>;
};

type AdapterOptions = {
  graph: ContentGraph;
  pageId: string | null;
  persistGraphOperations(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  /** Saves a person's change to views as one step of the page's ⌘Z (useContentGraphWrites commitViewStep). */
  persistViewStep?(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  session: Session;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
  /** A bar made here joins the page's ⌘Z, as a table's row (nativeTableRowSteps.ts). */
  recordRows?: RecordRows;
  /** A value written here other than a date joins it too, as a table's (nativeTableProductAdapter.ts). */
  recordValues?(changes: readonly ValueChange[]): (() => void) | void;
};

function rowValue(property: GraphProperty, value: unknown): GraphRowValue {
  return { type: property.type, value: value ?? null };
}

export function createNativeTimelineProductAdapter({
  graph,
  pageId,
  persistGraphOperations,
  persistViewStep,
  session,
  setGraph,
  setMessage,
  recordRows,
  recordValues,
}: AdapterOptions): NativeTimelineProductAdapter {
  async function persist(operations: GraphOperation[]) {
    return persistGraphOperationRuns(graph, pageId, operations, persistGraphOperations);
  }

  async function updateView(view: GraphView, isCurrent: () => boolean, step?: boolean) {
    if (refuseViewQueryGrowth(graph.views.find((candidate) => candidate.id === view.id)?.config, view.config, setMessage)) return;
    const save = step && persistViewStep ? (operations: GraphOperation[]) => persistGraphOperationRuns(graph, pageId, operations, persistViewStep) : persist;
    if (!await saveShownAtOnce(graph, pageId, [{ kind: "upsertView", view }], save, isCurrent, () => setGraph((current) => ({
      ...current, views: current.views.map((candidate) => candidate.id === view.id ? view : candidate),
    })))) return;
    setMessage(i18next.t("databaseView:status.saved"));
  }

  return {
    async createDatedRow(source, view, dayKey, seeds, isCurrent, alongside) {
      const title = graph.properties.find((property) => property.dataSourceId === source.id && property.type === "title");
      const datePropertyId = typeof view.config.timelineBy === "string"
        ? view.config.timelineBy
        : typeof view.config.calendarBy === "string"
          ? view.config.calendarBy
          // The date the timeline shows by when it names none (TimelineView).
          : firstViewDateSource(graph.properties.filter((property) => property.dataSourceId === source.id))?.id;
      const dateProperty = graph.properties.find((property) => property.id === datePropertyId && property.dataSourceId === source.id);
      if (!title || !dateProperty) return null;
      const endProperty = graph.properties.find(property => property.id === view.config.timelineEndBy
        && property.dataSourceId === source.id && property.type === "date" && property.id !== dateProperty.id);
      const range = writtenDateRange(dayKey);
      // A rollup's dates are its related pages': a page made by one has none of its own; one made with no day
      // (the table's "+ 새로 만들기") has none either.
      const dates = dateProperty.type !== "date" || !dayKey ? {} : endProperty && range.end
        ? { [dateProperty.id]: rowValue(dateProperty, range.start), [endProperty.id]: rowValue(endProperty, range.end) }
        : { [dateProperty.id]: rowValue(dateProperty, dayKey) };
      const sourceProperties = new Map(graph.properties.filter((property) => property.dataSourceId === source.id).map((property) => [property.id, property]));
      const seeded = Object.fromEntries(Object.entries(seeds.properties).flatMap(([propertyId, value]) => {
        const property = sourceProperties.get(propertyId);
        return property ? [[propertyId, rowValue(property, value)]] : [];
      }));
      const siblings = graph.rows.filter((row) => row.dataSourceId === source.id);
      const start = newRowStart(source, title.id, graph.properties);
      const row: GraphRow = {
        id: newId(),
        dataSourceId: source.id,
        subitemParentId: null,
        position: graphPositionBetween(siblings.at(-1)?.position, undefined),
        values: { ...start?.values, ...seeded, ...(seeds.title || !start ? { [title.id]: rowValue(title, seeds.title ?? "") } : {}), ...dates },
        // A page made here has nothing in it yet.
        hasContent: false,
        ...(start ? { icon: start.icon, iconType: start.iconType } : {}),
      };
      // Made on this device first and sent behind (rowMadeHere.ts), its bar
      // drawn in the press with its title's editor (alongside), as a table's
      // row typed in place: its page opens later from what is kept here, as
      // that row's does, not waited for (it held the bar up to 300 ms).
      const { saved } = makeRowHere(row, session.user.id, () => persist([{ kind: "upsertRow", row }]),
        () => setGraph((current) => ({ ...current, rows: current.rows.filter((candidate) => candidate.id !== row.id) })));
      // A bar made here is a step of the page's ⌘Z from its press, left
      // empty with Escape or not: a title typed at once is taken back first,
      // then the bar, and ⌘⇧Z brings it back, as Notion's timeline (seen
      // 2026-11-14) and a table's row (recordRowMade).
      recordRowMade(recordRows, row, saved);
      if (!isCurrent()) return null;
      // Put in as a splice of the rows shown (lib/rowsReplaced.ts): what the views work out of their rows is worked out for it alone.
      setGraph((current) => ({ ...current, rows: spliceItems(current.rows, [], [[current.rows.length, row]]) }));
      setMessage(i18next.t("databaseView:status.saved"));
      alongside?.(row);
      return row;
    },
    async createDatePropertyAndSelect(source, view, name, isCurrent) {
      const created = createGraphProperty(
        graph.properties.filter((property) => property.dataSourceId === source.id),
        source.id,
        "date",
        name,
        {},
      );
      const nextView: GraphView = { ...view, config: { ...view.config, timelineBy: created.property.id } };
      // Shown in the press and saved behind it, as a setting is (user decision
      // 2026-11-12: a press answers on the next frame); one the server refuses
      // goes as the page is read again. Shown once saved, the timeline stayed
      // on "날짜 속성 없음" for the server's round trip.
      if (!await saveShownAtOnce(graph, pageId, [...created.operations, { kind: "upsertView", view: nextView }], persist, isCurrent, () => setGraph((current) => ({
        ...current,
        properties: [...current.properties, created.property],
        views: current.views.map((candidate) => candidate.id === view.id ? nextView : candidate),
      })))) return;
      setMessage(i18next.t("databaseView:status.saved"));
    },
    async createDependency(source, successorRowId, predecessorRowId, revision, isCurrent, id = newId()) {
      const dependencyEdge = { id, dataSourceId: source.id, predecessorRowId, successorRowId, expectedRevision: revision };
      if (!await persist([{ kind: "upsertDependencyEdge", dependencyEdge }]) || !isCurrent()) return;
      setMessage(i18next.t("databaseView:status.saved"));
    },
    // The server writes the rows' Blocked by along (database_dependency_relations.go).
    async deleteDependency(source, edgeIds, revision, isCurrent) {
      if (!await persist(edgeIds.map((id) => ({ kind: "deleteDependencyEdge", dependencyEdgeDeletion: { id, dataSourceId: source.id, expectedRevision: revision } })))) return false;
      if (isCurrent()) setMessage(i18next.t("databaseView:status.saved"));
      return true;
    },
    loadDependencies({ cursor, dataSourceId, expectedRevision, signal, visibleRowIds }) {
		const authorityPageId = graphDataSourceAuthorityPageId(graph, pageId, dataSourceId);
		if (!authorityPageId) throw new Error("Timeline dependency read requires a page.");
		return api.databaseDependencyGraph(session, authorityPageId, {
        cursor,
        dataSourceId,
        expectedRevision,
        limit: 100,
        visibleRowIds: [...visibleRowIds],
      }, signal);
    },
    async moveOrResizeRow(rowId, values, isCurrent, alongside = {}) {
      // Only the values changed go, which the server merges into the row's others; the rows moved
      // with it go in the same write, one date command and one step of ⌘Z (database_date_commands.go).
      const nexts = Object.entries({ ...alongside, [rowId]: values }).flatMap(([id, written]) => {
        const row = graph.rows.find((candidate) => candidate.id === id);
        if (!row) return [];
        const properties = new Map(graph.properties.filter((property) => property.dataSourceId === row.dataSourceId).map((property) => [property.id, property]));
        const nextValues: typeof row.values = {};
        for (const [propertyId, value] of Object.entries(written)) {
          const property = properties.get(propertyId);
          if (property) nextValues[propertyId] = rowValue(property, value);
        }
        return [{ ...row, values: nextValues }];
      });
      if (!nexts.some((next) => next.id === rowId)) return;
      // A value other than a date — a new bar's title — that changes is a
      // step of the page's ⌘Z as it shows, as a table's: a title typed into
      // a bar just made is taken back first, then the bar, as Notion's (seen
      // 2026-11-14); one left as it was (Escape out of an empty title) is
      // none. Its dates are the server's date commands' to undo.
      const steps = valueChangesOf(graph, nexts).filter(({ before, after }) => !sameCell(before, after));
      const withdraw = steps.length ? recordValues?.(steps) : undefined;
      let written = false;
      const shown = await saveRowsShownAtOnce(graph, pageId, nexts, async (operations) => (written = await persist(operations)),
        isCurrent, setGraph, session.user.id);
      if (!written) withdraw?.();
      if (!shown) return;
      setMessage(i18next.t("databaseView:status.saved"));
    },
    updateView,
  };
}
