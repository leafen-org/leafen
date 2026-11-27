import type { ProductStatusSink } from "@/lib/productStatus";
import type { Dispatch, SetStateAction } from "react";
import { i18next } from "@/i18n";
import type { ContentGraph, GraphDataSource, GraphOperation, GraphRow } from "@/contentGraph";
import { duplicateDatabaseRowPage, readDatabaseRowPage } from "@/databaseRowPageApi";
import { newId } from "@/lib/ids";
import { LocalizedError } from "@/lib/settingsErrors";
import { ROWS_COPIED_EVENT, copyPositions, rowOrderWithCopies, viewKeepsOwnRowOrder, viewOrderIds } from "@/components/database/viewRowOrder";
import { graphPositionBetween } from "@/lib/graphPosition";
import { graphOperationsAuthorityPageId } from "@/lib/nativeGraphAuthority";
import type { NativeTableProductAdapter, ValueChange } from "@/lib/nativeTableProductAdapter";
import type { Session } from "@/types";
import { newRowStart } from "@/lib/newRowStart";
import { holdRowsGone, rowsDrawn, rowsHolderPage, withoutRows, withRowsAsDrawn } from "@/lib/rowsShownAtPress";
import { sendRowTrash } from "@/lib/rowTrashKept";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { rowCopiesHere, rowCopyIds } from "@/lib/rowCopiesHere";
import { copiesOnTheirWay, withCopiesOnTheirWay } from "@/lib/copiesOnTheirWay";
import { makeRowHere } from "@/lib/rowMadeHere";
import { sameCell } from "@/lib/valueSteps";
import { storedTimeZone, writtenDateRange } from "@/components/database/dateUtils";
import { spliceItems } from "@/lib/rowsReplaced";
import { keptViewRows, saveViewShown, viewOrderStepWrite, viewShown, type ViewOrderStep } from "@/lib/nativeRowMoveSteps";
import { chooseAsAtUndo, chooseStepRows, keepChoiceForRedo } from "@/lib/stepRowChoice";

/**
 * A step of rows: one made, which ⌘Z sends to the trash when it still holds
 * the values it was made with, or rows trashed, which it brings back. A title
 * typed into a new row is a step of its own, taken back first (Notion's,
 * seen 2026-10-21).
 */
/**
 * Rows a step made, trashed, copied or took away as made (`drawn`); a copy's
 * undo chooses the rows it came from again, and puts back the order of the
 * view keeping its own that the copies were placed in (`view`). Rows a paste
 * made carry the values it gave the rows it filled (`values`), taken back and
 * given again with them (nativeTableProductAdapter pasteRows).
 */
export type RowStep = { key: string; kind: "made" | "trashed" | "copied" | "discarded"; rowIds: readonly string[]; made?: GraphRow["values"];
  from?: readonly string[]; drawn?: readonly GraphRow[]; view?: ViewOrderStep; values?: readonly ValueChange[];
  /** Rows a press keeps hidden until the server has them in the trash (useDatabaseRowActions): a step of them taken meanwhile lets them go first. */
  letGo?: () => void };

/** A table's row steps: rows made, trashed and brought back, taken away as made, and duplicated. */
export type TableRowSteps = Pick<NativeTableProductAdapter, "createRow" | "publishCreatedRow" | "deleteRows" | "discardRow" | "rowsTrashed"
  | "stepRows" | "duplicateRows"> & { makeRows: MakeRows };

/**
 * Rows made after a row among its parent's, in order — each with the values
 * `seeds` give (written as a new row's are) and its own — shown together, no
 * step of their own; `saved` says whether the server took them all.
 */
type MakeRows = (source: GraphDataSource, parentId: string | null, afterRowId: string | null, seeds: Readonly<Record<string, unknown>>,
  rows: readonly GraphRow["values"][], subitemPair?: { parentPropertyId: string; childrenPropertyId: string }) => { rows: GraphRow[]; saved: Promise<boolean> };

type RowStepOptions = {
  graph: ContentGraph;
  pageId: string | null;
  /** Saves the table's writes on the pages that hold them (nativeTableProductAdapter persist). */
  persist(operations: GraphOperation[]): Promise<boolean>;
  rereadGraph?(pageId: string): Promise<boolean>;
  session?: Session | null;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
  /** A row made or rows trashed here, which join the page's ⌘Z; what it gives takes the step out again. */
  recordRows?: RecordRows;
  /** The open page's graph store, where a step's rows show ahead of the server (holdRowsGone). */
  store?: PageGraphStore | null;
  /** Reads a page from the server once every write made here to it is saved (nativeTableProductAdapter). */
  readAfterWrites?<Value>(pageId: string, read: () => Promise<Value | null>): Promise<Value | null>;
};

/** Records a step of rows as it shows; what it gives takes the step out again (the save refused). */
export type RecordRows = (step: RowStep) => (() => void) | void;

/**
 * A row made where it shows is a step of the page's ⌘Z from the press that
 * made it, before what is typed into it next — a title typed at once is
 * taken back first, then the row, as Notion's table, board, list and
 * gallery (seen 2026-11-14); recorded at the server's answer, it came after
 * the title's step, so the first ⌘Z found the row changed and did nothing
 * and the row was never taken away. The server refusing the row takes the
 * step out again.
 */
export function recordRowMade(recordRows: RecordRows | undefined, row: GraphRow, saved: Promise<boolean>) {
  const drop = recordRows?.({ key: `rows/made/${row.id}`, kind: "made", rowIds: [row.id], made: row.values });
  if (drop) void saved.then((ok) => { if (!ok) drop(); });
}

// The values people set, not those Leafen works out (a formula, a rollup,
// when and by whom a row was made or changed).
const VALUES_SET_BY_PEOPLE = new Set(["title", "rich_text", "number", "url", "email", "phone", "select", "multi_select", "status", "person",
  "relation", "date", "files", "checkbox"]);
// A value as a row was made with it, however it is written: a date range
// made here as "start/end" comes back from the server as its start and end
// (2026-11-14: a timeline bar brought back with ⌘⇧Z read as changed, and the
// next ⌘Z kept it).
function sameMade(shown: GraphRow["values"][string] | undefined, made: GraphRow["values"][string] | undefined) {
  if (sameCell(shown, made) || shown?.type !== "date" || made?.type !== "date") return sameCell(shown, made);
  const left = writtenDateRange(shown.value), right = writtenDateRange(made.value);
  return left.start === right.start && left.end === right.end && storedTimeZone(shown.value) === storedTimeZone(made.value);
}

// The values Leafen works out and writes itself, which a row's write never
// carries: the server refuses a row sent with them. An ID's number goes with
// a row brought back as made, which keeps it (database_unique_id_allocation.go).
const VALUES_WORKED_OUT = new Set(["created_time", "last_edited_time", "created_by", "last_edited_by", "rollup", "formula",
  "button", "location", "last_visited_time"]);

/** A row's values as a write bringing it back carries them: those Leafen works out left for the server, its ID's number kept. */
function valuesWritten(values: GraphRow["values"]): GraphRow["values"] {
  return Object.fromEntries(Object.entries(values).filter(([, value]) => !VALUES_WORKED_OUT.has(value?.type ?? "")));
}

function descendantIds(rows: readonly GraphRow[], rootIds: readonly string[]) {
  const admitted = new Set(rootIds);
  let changed = true;
  while (changed) {
    changed = false;
    for (const row of rows) {
      if (row.subitemParentId && admitted.has(row.subitemParentId) && !admitted.has(row.id)) {
        admitted.add(row.id);
        changed = true;
      }
    }
  }
  return admitted;
}

// A row put in its place among its siblings, as the page shows it read again: a row made before or after another
// isn't left at the end until then. A splice of the rows shown (lib/rowsReplaced.ts), so what the views work out of
// their rows — their pages, their order — is worked out for it alone: made anew, every row's page was made again and
// a list drew all its rows again for the one made (2026-11-14).
function withRowPlaced(shown: readonly GraphRow[], row: GraphRow) {
  const was = shown.findIndex(({ id }) => id === row.id);
  const rows = was < 0 ? shown : shown.filter(({ id }) => id !== row.id);
  const at = rows.findIndex((candidate) => candidate.dataSourceId === row.dataSourceId
    && (candidate.subitemParentId ?? null) === (row.subitemParentId ?? null) && candidate.position > row.position);
  return spliceItems(shown, was < 0 ? [] : [was], [[at < 0 ? rows.length : at, row]]);
}

// The rows a step last took to the trash with their sub-items, as drawn
// then: the next step back brings them back so, at the press.
const drawnWhenTrashed = new WeakMap<RowStep, readonly GraphRow[]>();
// The hold a step's rows are shown by while its server half is on its way: the step taken again meanwhile (⌘Z
// then ⌘⇧Z at once, the step landing as it is taken) lets it go, the rows shown one way at a time.
const stepHolds = new WeakMap<RowStep, () => void>();

export function createTableRowSteps({ graph, pageId, persist, rereadGraph, session, setGraph, setMessage, recordRows, store, readAfterWrites }: RowStepOptions): TableRowSteps {
  // A step's rows leave for the trash, or come back as drawn, at the press,
  // shown so over every read of the page meanwhile (holdRowsGone); their
  // row pages are written behind, each read first — one already where the
  // step takes it is left — and the page read again. They go as the
  // trash's one way (rowTrashKept.ts): kept for a server out of reach, they
  // stay as shown and go once it can take them, the page read then. One the
  // server refuses, changed since, puts the rows back as they were, the table
  // saying so (user decision 2026-11-12: a press answers on the next frame —
  // ⌘Z of a copy waited for the server, 925 ms on the NAS, 2026-11-14;
  // 2026-11-25: a server out of reach was said to be a row changed elsewhere,
  // and the row came back 25 ms after ⌘Z).
  function showMoved(step: RowStep, rowIds: readonly string[], trash: boolean) {
    const drawn = trash ? (store ? rowsDrawn(store, rowIds) : graph.rows.filter(({ id }) => descendantIds(graph.rows, rowIds).has(id)))
      : drawnWhenTrashed.get(step) ?? step.drawn ?? [];
    if (trash && drawn.length) drawnWhenTrashed.set(step, drawn);
    if (!drawn.length) return { drawn, shown: false, release: () => {} };
    if (store) {
      stepHolds.get(step)?.();
      const release = holdRowsGone(store, drawn, !trash, true);
      stepHolds.set(step, () => release());
      return { drawn, shown: true, release };
    }
    setGraph(trash ? withoutRows(new Set(drawn.map(({ id }) => id))) : withRowsAsDrawn(drawn));
    return { drawn, shown: true, release: (refused: boolean | "kept" = false) => {
      if (refused === true) setGraph(trash ? withRowsAsDrawn(drawn) : withoutRows(new Set(drawn.map(({ id }) => id))));
    } };
  }
  // `choose`: what is chosen as rows brought back show (stepRowChoice.ts), at once where they come back drawn, else once the server has them.
  async function moveRows(step: RowStep, rowIds: readonly string[], trash: boolean, choose?: () => void) {
    if (!session) return false;
    const { drawn, shown, release } = showMoved(step, rowIds, trash);
    const chooseBack = () => choose?.();
    if (shown && !trash) chooseBack();
    const sent = await sendRowTrash(session, rowIds, trash, { pageId: rowsHolderPage(graph, drawn, pageId), drawn }).catch(() => null);
    const reading = pageId && rereadGraph ? rereadGraph(pageId).catch(() => false) : null;
    if (sent === "kept") {
      release("kept");
      return true;
    }
    if (sent === null) release(true);
    await reading;
    release();
    if (sent === null) setMessage(i18next.t("databaseView:status.undoChanged"), "default");
    else if (!trash && !shown) chooseBack();
    return true;
  }

  // The title cell of a row drawn again, chosen once the table shows it: a
  // table's chosen cell is the one holding the focus.
  function chooseTitleOnceDrawn(rowId: string) {
    if (typeof document === "undefined") return;
    requestAnimationFrame(() => document.querySelector<HTMLElement>(
      `[data-table-row-id="${CSS.escape(rowId)}"] > [data-table-cell][data-title="true"]`)?.focus({ preventScroll: true }));
  }

  // ⌘Z takes a row made here to the trash only as it was left: one changed
  // since by someone else stays, the table saying so.
  async function stepRows(step: RowStep, forward: boolean) {
    // ⌘Z brings trashed rows back chosen; ⌘⇧Z gives back what was chosen at that ⌘Z (stepRowChoice.ts).
    if (step.kind === "trashed") {
      if (forward) chooseAsAtUndo(step);
      else keepChoiceForRedo(step);
      return moveRows(step, step.rowIds, forward, () => chooseStepRows(step.rowIds));
    }
    // ⌘Z brings back a row taken away as made, as it was drawn, its title cell
    // chosen; ⌘⇧Z takes it away again, the choice left on its line
    // (useChosenCellOutlivesRow) — Notion's (seen 2026-11-14). It goes with
    // the values people set and the ID number it was given, which it keeps
    // as Notion's (3 however often it comes back, seen 2026-11-14), the
    // server working out its others.
    if (step.kind === "discarded") {
      const drawn = step.drawn ?? [];
      setGraph(forward ? withoutRows(new Set(step.rowIds)) : withRowsAsDrawn(drawn));
      if (!forward && step.rowIds[0]) chooseTitleOnceDrawn(step.rowIds[0]);
      return persist(forward ? step.rowIds.map((id) => ({ kind: "deleteRow" as const, rowDeletion: { id } }))
        : drawn.map(({ id, dataSourceId, subitemParentId, position, values }) => ({ kind: "upsertRow" as const,
          row: { id, dataSourceId, subitemParentId, position, values: valuesWritten(values) } })));
    }
    // ⌘Z takes copies away and chooses their rows again; ⌘⇧Z brings them back (Notion's, seen 2026-11-06),
    // chosen as the views were at that ⌘Z — the copies, unless a press let them go since (stepRowChoice.ts).
    // The view's order the copies were written into goes back and comes again
    // with them, shown at the press: in a grouped view no row of which was
    // placed, ⌘D wrote the group's rows down first, and Notion's ⌘Z took that
    // back with the copy (seen 2026-11-24 in two tabs).
    if (step.kind === "copied") {
      const view = step.view && viewOrderStepWrite(graph, step.view, forward);
      if (view) void saveViewShown({ graph, pageId, persist, setGraph }, view).catch(() => false);
      if (!forward) keepChoiceForRedo(step);
      else if (chooseAsAtUndo(step)) return moveRows(step, step.rowIds, false);
      const moved = moveRows(step, step.rowIds, !forward, () => chooseStepRows(step.rowIds));
      if (!forward && step.from?.length) chooseStepRows(step.from);
      return moved;
    }
    // A row made again with ⌘⇧Z is chosen as it shows (Notion's timeline, seen 2026-11-15).
    if (forward) return moveRows(step, step.rowIds, false, () => chooseStepRows(step.rowIds));
    const shown = graph.rows.find(({ id }) => id === step.rowIds[0]);
    const made = step.made ?? {};
    const setByPeople = (id: string) => VALUES_SET_BY_PEOPLE.has(graph.properties.find((property) => property.id === id)?.type ?? "");
    if (!shown || Object.keys({ ...made, ...shown.values }).filter(setByPeople).some((id) => !sameMade(shown.values[id], made[id]))) {
      setMessage(i18next.t("databaseView:status.undoChanged"), "default");
      return true;
    }
    return moveRows(step, step.rowIds, true);
  }

  function publishCreatedRow(row: GraphRow, isCurrent: () => boolean) {
    if (!isCurrent()) return null;
    // In its place among its siblings (withRowPlaced).
    setGraph((current) => ({ ...current, rows: withRowPlaced(current.rows, row) }));
    setMessage(i18next.t("databaseView:status.saved"));
    return row;
  }

  // Each made at once as createRow makes one (makeRowHere), shown in one drawing.
  const makeRows: MakeRows = (source, parentId, afterRowId, seeds, rowsValues, subitemPair) => {
    const title = graph.properties.find((property) => property.dataSourceId === source.id && property.type === "title");
    if (!title || !rowsValues.length) return { rows: [], saved: Promise.resolve(true) };
    const siblings = graph.rows.filter((row) => row.dataSourceId === source.id && row.subitemParentId === parentId);
    const at = afterRowId ? siblings.findIndex((row) => row.id === afterRowId) : -1;
    const next = at < 0 ? siblings.length : at + 1, after = siblings[next]?.position;
    let before = siblings[next - 1]?.position;
    const seeded = Object.fromEntries(Object.entries(seeds).flatMap(([propertyId, value]) => {
      const property = graph.properties.find((candidate) => candidate.dataSourceId === source.id && candidate.id === propertyId);
      return property ? [[property.id, { type: property.type, value }]] : [];
    }));
    const made = rowsValues.map((values) => {
      const start = newRowStart(source, title.id, graph.properties);
      before = graphPositionBetween(before, after);
      const row: GraphRow = { id: newId(), dataSourceId: source.id, subitemParentId: parentId, position: before,
        values: { [title.id]: { type: "title", value: "" }, ...start?.values, ...seeded, ...values } as GraphRow["values"] };
      const { saved } = makeRowHere(row, session?.user.id, () => persist([{ kind: "upsertRow", row }]),
        () => setGraph((current) => ({ ...current, rows: current.rows.filter((candidate) => candidate.id !== row.id) })));
      const parent = parentId && subitemPair ? { [subitemPair.parentPropertyId]: { type: "relation" as const, value: [parentId] } } : {};
      return { saved, shown: { ...row, values: { ...row.values, ...parent }, hasContent: false, ...(start ? { icon: start.icon, iconType: start.iconType } : {}) } as GraphRow };
    });
    const ids = made.map(({ shown }) => shown.id);
    setGraph((current) => ({ ...current, rows: made.reduce((rows, { shown }) => withRowPlaced(rows, shown), current.rows).map((candidate) => {
      if (!parentId || !subitemPair || candidate.id !== parentId) return candidate;
      const held = candidate.values[subitemPair.childrenPropertyId]?.value;
      const children = Array.isArray(held) ? held.filter((id) => !ids.includes(id)) : [];
      return { ...candidate, values: { ...candidate.values, [subitemPair.childrenPropertyId]: { type: "relation", value: [...children, ...ids] } } };
    }) }));
    setMessage(i18next.t("databaseView:status.saved"));
    return { rows: made.map(({ shown }) => shown), saved: Promise.all(made.map(({ saved }) => saved)).then((oks) => oks.every(Boolean)) };
  };

  return {
    publishCreatedRow,
    makeRows,
    async createRow(source, parentId, afterRowId, isCurrent, seedValues = {}, beforeRowId, subitemPair, _atOnce = false, opensPage = false, alongside) {
      const title = graph.properties.find((property) => property.dataSourceId === source.id && property.type === "title");
      if (!title) return null;
      const siblings = graph.rows.filter((row) => row.dataSourceId === source.id && row.subitemParentId === parentId);
      const beforeIndex = beforeRowId ? siblings.findIndex((row) => row.id === beforeRowId) : -1;
      const afterIndex = beforeIndex >= 0 ? beforeIndex - 1 : afterRowId ? siblings.findIndex((row) => row.id === afterRowId) : -1;
      const beforePosition = afterIndex >= 0 ? siblings[afterIndex]?.position : beforeIndex >= 0 ? undefined : siblings.at(-1)?.position;
      const afterPosition = afterIndex >= 0 || beforeIndex >= 0 ? siblings[afterIndex + 1]?.position : undefined;
      const values = Object.fromEntries(Object.entries(seedValues).flatMap(([propertyId, value]) => {
        const property = graph.properties.find((candidate) => candidate.dataSourceId === source.id && candidate.id === propertyId);
        return property ? [[property.id, { type: property.type, value }]] : [];
      }));
      const start = newRowStart(source, title.id, graph.properties);
      const given = { [title.id]: { type: "title", value: "" }, ...start?.values, ...values } as GraphRow["values"];
      const row: GraphRow = {
        id: newId(),
        dataSourceId: source.id,
        subitemParentId: parentId,
        position: graphPositionBetween(beforePosition, afterPosition),
        values: given,
      };
      // A row comes at once, as Notion's, its title typed where it shows or
      // its page open: it is made on this device first and goes out behind
      // (rowMadeHere.ts), so what is typed next goes in however long the
      // server takes (2026-11-05: on a network that stopped a row waited six
      // seconds, and a title typed meanwhile went when the person pressed
      // elsewhere; 2026-11-12: "새로 만들기"'s page never opened, and the
      // body typed in it was lost). One the server refuses goes again, and
      // the page says so and is read again.
      const { saved, pageReady } = makeRowHere(row, session?.user.id, () => persist([{ kind: "upsertRow", row }]),
        () => setGraph((current) => ({ ...current, rows: current.rows.filter((candidate) => candidate.id !== row.id) })));
      // A row made where it shows is a step of the page's ⌘Z (recordRowMade); one whose page
      // opens as it is made ("새로 만들기") is not, as Notion's (seen
      // 2026-11-07: ⌘Z after closing that page kept the row, and took a row
      // typed in under the table away).
      if (!opensPage) recordRowMade(recordRows, row, saved);
      // A page opening next opens from what is kept here.
      if (opensPage) await pageReady;
      if (!isCurrent()) return null;
      // An empty page, under its parent in the sub-item relations, which name it in theirs too.
      const shown: GraphRow = { ...row, hasContent: false, ...(start ? { icon: start.icon, iconType: start.iconType } : {}) };
      if (parentId && subitemPair) {
        shown.values = { ...row.values, [subitemPair.parentPropertyId]: { type: "relation", value: [parentId] } };
        setGraph((current) => ({ ...current, rows: current.rows.map((candidate) => {
          if (candidate.id !== parentId) return candidate;
          const held = candidate.values[subitemPair.childrenPropertyId]?.value;
          const children = Array.isArray(held) ? held.filter((id) => id !== row.id) : [];
          return { ...candidate, values: { ...candidate.values, [subitemPair.childrenPropertyId]: { type: "relation", value: [...children, row.id] } } };
        }) }));
      }
      const published = publishCreatedRow(shown, isCurrent);
      // What else the press changes with the row — its place in a view keeping
      // its own order, the rows a view shows, its title's focus — is drawn
      // with it: set later, it drew the page's database a second time inside
      // the press (2026-11-14: +8 ms a list, +14 ms a table, CPU ×1).
      if (published) alongside?.(published);
      return published;
    },
    rowsTrashed: recordRows ? (rowIds, drawn, letGo) => recordRows({ key: `rows/trashed/${newId()}`, kind: "trashed", rowIds: [...rowIds],
      ...(drawn?.length ? { drawn } : {}), ...(letGo ? { letGo } : {}) }) : undefined,
    stepRows,
    // The rows leave the table at the press (user decision 2026-11-12: a
    // press answers on the next frame — they stayed until the server had
    // deleted them); refused, they are back where they were drawn.
    async deleteRows(rowIds, isCurrent) {
      const ids = descendantIds(graph.rows, rowIds);
      if (!ids.size) return;
      const drawn = graph.rows.filter(({ id }) => ids.has(id));
      setGraph(withoutRows(ids));
      let deleted = false;
      try {
        deleted = await persist([...ids].map((id) => ({ kind: "deleteRow", rowDeletion: { id } })));
      } finally {
        if (!deleted && isCurrent()) setGraph(withRowsAsDrawn(drawn));
      }
      if (deleted && isCurrent()) setMessage(i18next.t("databaseView:status.deleted"));
    },
    // A row "+ 새 페이지" made, left with Escape before anything was written in its title, goes at
    // once and leaves no page (freshRows.ts), as Notion's (seen 2026-11-14); refused, it is back.
    // Its step is the page's from the press, so a ⌘Z pressed before the server has taken it away finds it, its
    // writes going after the removal's; refused, the step goes.
    async discardRow(rowId) {
      const drawn = graph.rows.filter(({ id }) => id === rowId);
      if (!drawn.length) return;
      setGraph(withoutRows(new Set([rowId])));
      const drop = recordRows?.({ key: `rows/discarded/${rowId}`, kind: "discarded", rowIds: [rowId], drawn });
      if (!await persist([{ kind: "deleteRow", rowDeletion: { id: rowId } }])) { setGraph(withRowsAsDrawn(drawn)); drop?.(); }
    },
    async duplicateRows(rowIds, isCurrent, inView, place) {
      // Each selected item is duplicated by the server with its sub-items
      // and page content, in the order given, once for each time it is
      // given (a row chosen on two lines of a grouped view, copiesPlace); an
      // item under another selected item comes along with it.
      const selected = new Set(rowIds);
      const byId = new Map(graph.rows.map((row) => [row.id, row]));
      const underSelected = (row: GraphRow) => {
        for (let parent = row.subitemParentId; parent; parent = byId.get(parent)?.subitemParentId ?? null) {
          if (selected.has(parent)) return true;
        }
        return false;
      };
      const roots = rowIds.flatMap((id) => byId.get(id) ?? []).filter((row) => !underSelected(row));
      if (!roots.length || !session || !pageId || !rereadGraph) return [];
      // The copies go together after the last row, or where `place` puts
      // them in the order shown, as Notion's (seen 2026-11-08): in the view's
      // own order as saved (not a person's own sort or search over it), or,
      // in a view showing the database's one order, by their positions there.
      const saved = inView && (viewShown(graph, inView.id) ?? inView);
      const keepsOrder = Boolean(saved && viewKeepsOwnRowOrder(saved.config));
      const positions = saved && !keepsOrder && roots.length > 1
        ? copyPositions(graph.rows.filter(({ dataSourceId }) => dataSourceId === roots[0]!.dataSourceId), roots.length, roots.at(-1)!.id, place) : null;
      const requests = roots.map(() => newId());
      const ordered = (copyIds: readonly string[]) => {
        // The view's own database's rows: a page holding more than one keeps the others' out of its order.
        const kept = viewOrderIds(keptViewRows(saved!.id) ?? graph.rows.filter(({ dataSourceId }) => dataSourceId === saved!.dataSourceId), saved!.config);
        const rowOrder = rowOrderWithCopies(kept, copyIds, place, roots.at(-1)!.id,
          Array.isArray(saved!.config.rowOrder) && saved!.config.rowOrder.length === 0);
        return { ...saved!, config: { ...saved!.config, rowOrder } };
      };
      // The copies show at the press, in their places and chosen, under the
      // IDs the server gives them (rowCopiesHere.ts), on their way until the
      // server has made them behind (copiesOnTheirWay.ts); a view keeping its
      // own order places them as they show (user decision 2026-11-12: a press
      // answers on the next frame — they came 645–737 ms after it, 2026-11-14).
      let targets: ReadonlyMap<string, string>[];
      try {
        targets = await Promise.all(roots.map((row, index) => rowCopyIds(session.user.id, graph.rows, row.id, requests[index]!)));
      } catch (error) {
        throw new LocalizedError(i18next.t("rowMenu:toast.couldntDuplicatePage"), { cause: error });
      }
      const copies = roots.map((row, index) => targets[index]!.get(row.id)!);
      const now = new Date().toISOString();
      const drawn = roots.flatMap((row, index) => rowCopiesHere(graph.rows, targets[index]!, row.id, positions?.[index] || undefined, session.user.id, now));
      const answered = copiesOnTheirWay(drawn);
      setGraph(withCopiesOnTheirWay);
      const placedView = keepsOrder ? ordered(copies) : null;
      const viewWrites = { graph, pageId, persist, setGraph };
      const placed = placedView ? saveViewShown(viewWrites, placedView).catch(() => false) : Promise.resolve(false);
      const from = roots.map(({ id }) => id);
      // The order written with the copies is of their step (stepRows).
      const orderStep = placedView ? { id: placedView.id, rowIds: copies, before: (saved!.config.rowOrder as unknown[]).map(String),
        after: (placedView.config.rowOrder as unknown[]).map(String) } : undefined;
      const drop = recordRows?.({ key: `rows/copied/${newId()}`, kind: "copied", rowIds: copies, from, ...(orderStep ? { view: orderStep } : {}) });
      window.dispatchEvent(new CustomEvent(ROWS_COPIED_EVENT, { detail: { from, copies } }));
      void (async () => {
        const made: string[] = [];
        try {
          // The view's order is written before any copy is asked for.
          const placedFirst = await placed;
          for (const [index, row] of roots.entries()) {
            // The server copies the row as it holds it: what this device has
            // still to send of it (a name typed just before, a row made just
            // now) is sent first, whichever way the copy was asked for — ⌘D,
            // the selection bar, a row's or card's menu (2026-11-19: at a 5 s
            // round trip a name typed right before ⌘D was not in the copy).
            const read = () => readDatabaseRowPage(session, row.id, { limit: 1 });
            const holder = graphOperationsAuthorityPageId(graph, pageId, [{ kind: "upsertRow", row }]);
            const snapshot = readAfterWrites && holder ? await readAfterWrites(holder, read) : await read();
            if (!snapshot) throw new Error("The row to copy wasn't read.");
            made.push((await duplicateDatabaseRowPage(session, row.id, {
              requestId: requests[index]!, expectedAuthorityRevision: snapshot.row.authorityRevision,
              ...(positions?.[index] ? { position: positions[index] } : {}),
            })).rowId);
            answered(targets[index]!.values());
          }
          // Placed after they came where placing them ahead didn't take.
          if (keepsOrder && (!placedFirst || made.some((copy, index) => copy !== copies[index]))) {
            const view = ordered(made);
            if (orderStep) Object.assign(orderStep, { rowIds: made, after: (view.config.rowOrder as unknown[]).map(String) });
            await saveViewShown(viewWrites, view);
          }
          if (isCurrent()) setMessage(i18next.t("databaseView:status.duplicated"));
        } catch {
          // The rows are copied all or none: refused, the copies leave at
          // once, their rows chosen again in their place, and their step
          // leaves ⌘Z; those made go to the trash and the view's order lets
          // them go; the failure reads as one row's does.
          window.dispatchEvent(new CustomEvent(ROWS_COPIED_EVENT, { detail: { from: copies, copies: from } }));
          answered();
          setGraph(withoutRows(new Set(drawn.map(({ id }) => id))));
          drop?.();
          setMessage(i18next.t("rowMenu:toast.couldntDuplicatePage"), "error");
          if (made.length) await sendRowTrash(session, made, true, { pageId: rowsHolderPage(graph, roots, pageId) }).catch(() => undefined);
          if (await placed) await saveViewShown(viewWrites, saved!).catch(() => false);
        } finally {
          await rereadGraph(pageId).catch(() => false);
        }
      })();
      return copies;
    },
  };
}
