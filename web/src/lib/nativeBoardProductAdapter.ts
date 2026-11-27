import type { ProductStatusSink } from "@/lib/productStatus";
import type { Dispatch, SetStateAction } from "react";
import { i18next } from "@/i18n";
import { refuseViewQueryGrowth } from "@/lib/viewQueryBound";
import { graphPositionBetween } from "@/lib/graphPosition";
import {
  createGraphProperty,
  type ContentGraph,
  type GraphDataSource,
  type GraphOperation,
  type GraphProperty,
  type GraphRow,
  type GraphRowValue,
  type GraphValueType,
  type GraphView,
} from "@/contentGraph";
import { newId } from "@/lib/ids";
import { activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import { createDefaultStatusConfig } from "@/lib/statusPropertyDefaults";
import type { BoardOption } from "@/components/database/boardViewModel";
import { boardGroupPropertyId, regroupedViewConfig } from "@/components/database/viewLayoutDefaults";
import { persistGraphOperationRuns, saveShownAtOnce } from "@/lib/nativeGraphAuthority";
import { rowMoveStep, saveRowMove, stepRowMove, type RowMoveStep } from "@/lib/nativeRowMoveSteps";
import { newRowStart } from "@/lib/newRowStart";
import { makeRowHere } from "@/lib/rowMadeHere";
import { recordRowMade, type RecordRows } from "@/lib/nativeTableRowSteps";
import { peopleOwnValue } from "@/components/database/peopleSourceCells";

export type NativeBoardMove = {
  propertyId: string;
  value: unknown;
  beforeId: string | null;
  afterId: string | null;
  subgroupPropertyId?: string;
  subgroupValue?: unknown;
  /** A view keeping its own order places the card itself; the row keeps the database's place. */
  keepPosition?: boolean;
  /** That view with the card in its place in it, saved with the move. */
  view?: GraphView;
};

export type NativeBoardProductAdapter = {
  addProperty(source: GraphDataSource, view: GraphView, type: "select" | "status", isCurrent: () => boolean): Promise<GraphProperty | null>;
  createRow(source: GraphDataSource, propertyId: string, value: unknown, seeds: NativeBoardRowSeeds, isCurrent: () => boolean): Promise<GraphRow | null>;
  moveRow(rowId: string, change: NativeBoardMove, isCurrent: () => boolean): Promise<void>;
  /** Several cards moved in one step of ⌘Z, a card whose `beforeId` is another of them placed after that one's new place. */
  moveCards?(moves: readonly { rowId: string; change: NativeBoardMove }[], isCurrent: () => boolean): Promise<void>;
  /** A recorded move taken back (forward false) or again. */
  stepMove(step: RowMoveStep, forward: boolean): Promise<boolean>;
  /** With a view, its settings are saved and shown with the options in one write. */
  updateOptions(propertyId: string, options: readonly BoardOption[], isCurrent: () => boolean, view?: GraphView): Promise<void>;
  /** A person's change to the view's settings (`step`) is a step of the page's ⌘Z; one a program makes is not. */
  updateView(view: GraphView, isCurrent: () => boolean, step?: boolean): Promise<void>;
};

/** A card made at a group's top goes before that group's first row (beforeRowId). */
export type NativeBoardRowSeeds = { title?: string; properties: Record<string, unknown>; beforeRowId?: string };

type AdapterOptions = {
  graph: ContentGraph;
  pageId: string | null;
  persistGraphOperations(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  /** Saves a person's change to views as one step of the page's ⌘Z (useContentGraphWrites commitViewStep). */
  persistViewStep?(pageId: string, operations: GraphOperation[]): Promise<boolean>;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
  /** The person moving cards, shown as a moved card's last editor. */
  editedBy?: string;
  /** A card moved, which joins the page's ⌘Z history (stepMove takes it back). */
  recordMove?(step: RowMoveStep): void;
  /** A card made, which joins the page's ⌘Z history as a table's row does (the table's stepRows takes it back). */
  recordRows?: RecordRows;
};

// An empty value is written as null, which clears it; left out, the server
// can't read it.
function rowValue(type: GraphValueType, value: unknown): GraphRowValue {
  return { type, value: value ?? null };
}

// Positions are made in one place for the content graph; callers here
// still find them under this module.
export { graphPositionBetween };

function nextRowPosition(rows: readonly GraphRow[], change: NativeBoardMove) {
  const before = change.beforeId ? rows.find((row) => row.id === change.beforeId)?.position : undefined;
  const after = change.afterId ? rows.find((row) => row.id === change.afterId)?.position : undefined;
  return graphPositionBetween(before, after);
}

export function createNativeBoardProductAdapter({
  graph,
  pageId,
  persistGraphOperations,
  persistViewStep,
  setGraph,
  setMessage,
  editedBy,
  recordMove,
  recordRows,
}: AdapterOptions): NativeBoardProductAdapter {
  async function persist(operations: GraphOperation[]) {
    return persistGraphOperationRuns(graph, pageId, operations, persistGraphOperations);
  }
  const writes = { graph, pageId, persist, setGraph, editedBy };

  async function addProperty(source: GraphDataSource, view: GraphView, type: "select" | "status", isCurrent: () => boolean) {
    const labels = activePersistentGeneratedLabels();
    const created = createGraphProperty(
      graph.properties.filter((property) => property.dataSourceId === source.id),
      source.id,
      type,
      type === "status" ? labels.propertyNames.status : labels.propertyNames.select,
      type === "status" ? createDefaultStatusConfig(newId, i18next.language) : { options: [] },
    );
    const properties = [...graph.properties.filter((property) => property.dataSourceId === source.id), created.property];
    const nextView = { ...view, config: regroupedViewConfig(view.config, boardGroupPropertyId(view.config, properties), created.property.id, properties, "board", true) };
    if (!await persist([...created.operations, { kind: "upsertView", view: nextView }])) return null;
    if (!isCurrent()) return null;
    setGraph((current) => ({
      ...current,
      properties: [...current.properties, created.property],
      views: current.views.map((candidate) => candidate.id === view.id ? nextView : candidate),
    }));
    setMessage(i18next.t("databaseView:status.saved"));
    return created.property;
  }

  async function createRow(source: GraphDataSource, propertyId: string, value: unknown, seeds: NativeBoardRowSeeds, isCurrent: () => boolean) {
    const id = newId();
    const property = graph.properties.find((candidate) => candidate.id === propertyId);
    const title = graph.properties.find((candidate) => candidate.dataSourceId === source.id && candidate.type === "title");
    if (!property || !title) return null;
    const siblings = graph.rows.filter((row) => row.dataSourceId === source.id);
    const ordered = seeds.beforeRowId ? [...siblings].sort((a, b) => (a.position < b.position ? -1 : a.position > b.position ? 1 : 0)) : siblings;
    const beforeIndex = seeds.beforeRowId ? ordered.findIndex((row) => row.id === seeds.beforeRowId) : -1;
    const seededValues = Object.fromEntries(Object.entries(seeds.properties).flatMap(([seedPropertyId, seedValue]) => {
      const seedProperty = graph.properties.find((candidate) => candidate.id === seedPropertyId && candidate.dataSourceId === source.id);
      return seedProperty ? [[seedProperty.id, rowValue(seedProperty.type, seedValue)]] : [];
    }));
    const row: GraphRow = {
      id,
      dataSourceId: source.id,
      subitemParentId: null,
      position: beforeIndex >= 0
        ? graphPositionBetween(ordered[beforeIndex - 1]?.position, ordered[beforeIndex].position)
        : graphPositionBetween(siblings.at(-1)?.position, undefined),
      values: {},
      // A page made here has nothing in it yet.
      hasContent: false,
    };
    // A group that stands for no one value gives the card none.
    const start = newRowStart(source, title.id, graph.properties);
    const given = { ...start?.values, ...seededValues, ...(seeds.title || !start ? { [title.id]: rowValue("title", seeds.title ?? "") } : {}),
      ...(value === undefined ? {} : { [property.id]: rowValue(property.type, value) }) };
    row.values = given;
    if (start) { row.icon = start.icon; row.iconType = start.iconType; }
    // Made on this device first, its page ready to open, and sent behind (rowMadeHere.ts).
    const { saved, pageReady } = makeRowHere(row, editedBy, () => persist([{ kind: "upsertRow", row }]),
      () => setGraph((current) => ({ ...current, rows: current.rows.filter((candidate) => candidate.id !== row.id) })));
    // A card made in its group is a step of the page's ⌘Z from its press,
    // left empty with Escape or not: ⌘Z takes it away, ⌘⇧Z brings it back
    // (Notion's board, list and gallery, seen 2026-11-14; recordRowMade).
    recordRowMade(recordRows, row, saved);
    await pageReady;
    if (!isCurrent()) return null;
    setGraph((current) => ({ ...current, rows: [...current.rows, row] }));
    setMessage(i18next.t("databaseView:status.saved"));
    return row;
  }

  async function updateOptions(propertyId: string, options: readonly BoardOption[], isCurrent: () => boolean, view?: GraphView) {
    const property = graph.properties.find((candidate) => candidate.id === propertyId);
    if (!property) return;
    const next = { ...property, config: { ...property.config, options: options.map((option) => ({ ...option })) } };
    // A view setting that goes with the options (where a new group lands) is
    // one write with them: saved one after the other, the view's change made
    // the board's request stale, so the options were saved but never shown
    // and the next group written from the shown list dropped them.
    const operations: GraphOperation[] = [{ kind: "upsertProperty", property: next }, ...(view ? [{ kind: "upsertView" as const, view }] : [])];
    if (!await saveShownAtOnce(graph, pageId, operations, persist, isCurrent, () => setGraph((current) => ({
      ...current,
      properties: current.properties.map((candidate) => candidate.id === propertyId ? next : candidate),
      views: view ? current.views.map((candidate) => candidate.id === view.id ? view : candidate) : current.views,
    })))) return;
    setMessage(i18next.t("databaseView:status.saved"));
  }

  async function updateView(view: GraphView, isCurrent: () => boolean, step?: boolean) {
    if (refuseViewQueryGrowth(graph.views.find((candidate) => candidate.id === view.id)?.config, view.config, setMessage)) return;
    const save = step && persistViewStep ? (operations: GraphOperation[]) => persistGraphOperationRuns(graph, pageId, operations, persistViewStep) : persist;
    if (!await saveShownAtOnce(graph, pageId, [{ kind: "upsertView", view }], save, isCurrent, () => setGraph((current) => ({
      ...current, views: current.views.map((candidate) => candidate.id === view.id ? view : candidate),
    })))) return;
    setMessage(i18next.t("databaseView:status.saved"));
  }

  // Only the values the move changes go, which the server merges into the
  // row's others, so an edit still on its way isn't written back over.
  // People's own values are its people's (peopleSourceCells.ts): a card grouped by one keeps its group, moving only its place.
  function movedCard(row: GraphRow, change: NativeBoardMove, placed: ReadonlyMap<string, string | undefined>) {
    const property = graph.properties.find((candidate) => candidate.id === change.propertyId);
    if (!property) return null;
    const siblings = graph.rows.filter((candidate) => candidate.dataSourceId === row.dataSourceId && candidate.id !== row.id)
      .map((candidate) => placed.has(candidate.id) ? { ...candidate, position: placed.get(candidate.id) ?? candidate.position } : candidate);
    const values: typeof row.values = peopleOwnValue(graph, row, property.id) ? {} : { [property.id]: rowValue(property.type, change.value) };
    if (change.subgroupPropertyId) {
      const subgroup = graph.properties.find((candidate) => candidate.id === change.subgroupPropertyId);
      if (subgroup && !peopleOwnValue(graph, row, subgroup.id)) values[subgroup.id] = rowValue(subgroup.type, change.subgroupValue);
    }
    return { ...row, position: change.keepPosition ? row.position : nextRowPosition(siblings, change), values };
  }

  const moveRow = (rowId: string, change: NativeBoardMove, isCurrent: () => boolean) => moveCards([{ rowId, change }], isCurrent);
  async function moveCards(moves: readonly { rowId: string; change: NativeBoardMove }[], isCurrent: () => boolean) {
    const placed = new Map<string, string | undefined>(), next: GraphRow[] = [];
    for (const { rowId, change } of moves) {
      const row = graph.rows.find((candidate) => candidate.id === rowId), moved = row && movedCard(row, change, placed);
      if (!moved) continue;
      placed.set(moved.id, moved.position);
      next.push(moved);
    }
    if (!next.length) return;
    const view = moves[0]!.change.view;
    // ⌘Z takes the cards back to their groups and their places there (nativeRowMoveSteps.ts).
    // It joins the history as it is shown — the board is drawn again before
    // the write answers — and a write refused leaves the step to say the card changed.
    const step = rowMoveStep(graph, next, view, next.map(({ id }) => id));
    if (step.rows.length || step.view) recordMove?.(step);
    if (!await saveRowMove(writes, next, view, isCurrent)) return;
    setMessage(i18next.t("databaseView:status.moved"));
  }

  const stepMove = (step: RowMoveStep, forward: boolean) => stepRowMove(writes, step, forward, setMessage);

  return { addProperty, createRow, moveCards, moveRow, stepMove, updateOptions, updateView };
}
