import type { NativePageMetadataChange, PageMetadataBaseline } from "./nativePageMetadata";
import type { NativePageMutationBounds } from "./nativePageMutationOutbox";
import type { GraphOperation, GraphRow, GraphRowTemplateFill } from "@/contentGraphWire.generated";
import type { DatabaseRowPagePresentationInput, DatabaseRowPageValuesInput } from "@/databaseRowPageWire.generated";
import type { Page, PageCreateInput } from "@/workspaceMembershipWire.generated";

// The outbox's ordered lane of a page's changes besides its plain text
// (nativePageMutationOutbox.ts): structure changes and settings changes,
// their shapes as kept, and how a kept entry reads back.

/**
 * A page's structure changes (blocks, databases) made here: kept in the
 * outbox before they go out, so changes the server can't take yet wait for
 * it in the order made, across a reload, instead of vanishing.
 */
export interface NativeGraphMutation {
  mutationId: string;
  operations: unknown[];
  pageId: string;
  /**
   * The page's text written with a change of its blocks, one Yjs update in
   * base64, which goes with it whichever tab sends it
   * (lib/textBehindBlockChanges.ts).
   */
  words?: string;
  /** The page's text written before the change that goes with it, as `words`. */
  typing?: string;
  /**
   * The page becomes a full-page database with the change that makes its
   * database, in the same write, kept with it: a second write after it,
   * which only the window held, was lost to a reload or a closed tab in
   * between, the page left a page holding its database (2026-11-18).
   */
  pageKind?: "database";
  /** With `pageKind`, started on a page of the person's: the page as it was goes to the trash (database_start_page_trash.go). */
  replacesPage?: true;
  /**
   * The database the page starts as that only the server makes, sent by its
   * own route instead of `operations` (databaseStartKept.ts), which then hold
   * just the page's empty lines it takes away, shown gone meanwhile.
   */
  start?: NativeDatabaseStart;
  /**
   * The templates the rows the change makes start from, kept with the
   * change: the server fills each row from its template as it makes it
   * (content_graph_row_templates.go). Filled from the page that made the row
   * once it was saved, a row sent after a reload or a closed tab was left
   * with an empty title and no icon or body (2026-11-25).
   */
  rowTemplates?: GraphRowTemplateFill[];
  /**
   * Rows of the page's databases going to the trash, or out of it, with no
   * `operations`: sent by the rows' own route (rowTrashKept.ts), shown so
   * meanwhile where the page's kept changes are (rowTrashShown). Sent
   * straight to the server, a row sent to the trash while the server was out
   * of reach came back 7 ms after the press (2026-11-25).
   */
  rowTrash?: NativeRowTrash;
  /**
   * A row's values written through its own page's route, or a task's status
   * through 내 작업's, with no `operations` (rowValuesKept.ts): a row page
   * opened alone, a task's values in 내 작업, a sidebar row's name. Sent
   * straight to the server, a title typed in a new task's side peek while its
   * making waited for the server went with the tab at a reload (2026-11-25).
   */
  rowValues?: NativeRowValues;
}

/**
 * A row's values as its row page's route takes them (`input`,
 * DatabaseRowPageValuesInput), a task's status as 내 작업 sets it
 * (`taskStatus`, markMyTaskDone — by group or name, for the server to find
 * in the task's database), or its page's icon, cover or style
 * (`presentation`, written over the row as the server holds it when it
 * goes); the kept change's ID is its write's. One of the three.
 */
export interface NativeRowValues {
  rowId: string;
  input?: Omit<DatabaseRowPageValuesInput, "mutationId">;
  taskStatus?: { workspaceId: string; done: boolean; group?: string; statusName?: string };
  presentation?: Omit<DatabaseRowPagePresentationInput, "expectedAuthorityRevision">;
}

function rowValuesOf(value: unknown, bounds: NativePageMutationBounds): NativeRowValues {
  const kept = recordOf(value), input = recordOf(kept?.input), task = recordOf(kept?.taskStatus), look = recordOf(kept?.presentation);
  if (!kept || [input, task, look].filter(Boolean).length !== 1 || (input && !recordOf(input.values))
    || (task && (typeof task.workspaceId !== "string" || typeof task.done !== "boolean"))) {
    throw new RangeError("Row values are not one row's.");
  }
  const rowId = boundedIdentity(kept.rowId, bounds.maxPageIdCharacters, "rowId");
  return input ? { rowId, input: input as NativeRowValues["input"] } : task ? { rowId, taskStatus: task as NativeRowValues["taskStatus"] }
    : { rowId, presentation: look as NativeRowValues["presentation"] };
}

/** Rows going to the trash, or out of it (`trash` false), by their own route (rowTrashKept.ts). */
export interface NativeRowTrash {
  rowIds: string[];
  trash: boolean;
  /** The rows as drawn at the press, their sub-items with them: shown gone meanwhile, or back as drawn. */
  drawn?: GraphRow[];
}

function rowTrashOf(value: unknown, bounds: NativePageMutationBounds, maximum: number): NativeRowTrash {
  const kept = recordOf(value);
  const drawn = kept?.drawn === undefined ? [] : kept.drawn;
  if (!kept || !Array.isArray(kept.rowIds) || kept.rowIds.length === 0 || kept.rowIds.length > maximum || typeof kept.trash !== "boolean"
    || !Array.isArray(drawn) || drawn.length > maximum || drawn.some((row) => typeof recordOf(row)?.id !== "string")) {
    throw new RangeError("Row trash is not one change's rows.");
  }
  return { rowIds: kept.rowIds.map((id) => boundedIdentity(id, bounds.maxPageIdCharacters, "rowId")), trash: kept.trash,
    ...(drawn.length ? { drawn: drawn as GraphRow[] } : {}) };
}

/**
 * What a kept change of rows going to the trash, or out of it, shows of
 * them on the page's copies until it goes, as its page's other kept changes
 * show theirs (pendingGraphOverlay.ts): the rows and those drawn with them
 * taken away, one made by a change kept before it staying gone; or the rows
 * back as drawn. Never sent: a row's deletion is for good.
 */
export function rowTrashShown({ rowIds, trash, drawn = [] }: NativeRowTrash): GraphOperation[] {
  if (!trash) return drawn.map((row) => ({ kind: "upsertRow", row }));
  return [...new Set([...rowIds, ...drawn.map(({ id }) => id)])].map((id) => ({ kind: "deleteRow", rowDeletion: { id } }));
}

/**
 * A database a page starts as that only the server makes — a template's
 * content, a linked view of an existing source, a form — kept in the lane as
 * the page's other structure changes are, so a reload or a closed tab right
 * after the press doesn't lose it (2026-11-18: sent from the window alone,
 * after two round trips, the page left empty with its "시작하기" again).
 */
export interface NativeDatabaseStart {
  /** A template's selection, a source, or a view (a form): PageDatabaseChoice. */
  choice: { kind: "template" | "source" | "view" } & Record<string, unknown>;
  /** The page's empty lines it takes the place of. */
  lineIds: string[];
  /** The new, still empty database a template takes the place of instead (the start panel's suggestion). */
  replace?: { blockId: string; dataSourceId?: string };
  /** Started on a page of the person's, which goes to the trash as it was. */
  replacesPage?: true;
}

function rowTemplatesOf(value: unknown, bounds: NativePageMutationBounds): GraphRowTemplateFill[] {
  if (!Array.isArray(value)) throw new RangeError("Row templates are not a list.");
  return value.map((fill) => {
    const kept = recordOf(fill);
    if (!kept) throw new RangeError("A row template is not one row's.");
    return { ...kept, rowId: boundedIdentity(kept.rowId, bounds.maxPageIdCharacters, "rowId") } as GraphRowTemplateFill;
  });
}

function databaseStartOf(value: unknown, bounds: NativePageMutationBounds): NativeDatabaseStart {
  const start = recordOf(value), choice = recordOf(start?.choice), replace = start?.replace === undefined ? undefined : recordOf(start.replace);
  const kind = choice?.kind;
  if (!start || !choice || (kind !== "template" && kind !== "source" && kind !== "view") || !Array.isArray(start.lineIds)
    || replace === null || (replace && (replace.dataSourceId !== undefined && typeof replace.dataSourceId !== "string"))) {
    throw new RangeError("Database start is not one page's.");
  }
  return {
    choice: { ...choice, kind },
    lineIds: start.lineIds.map((id) => boundedIdentity(id, bounds.maxPageIdCharacters, "lineId")),
    ...(replace ? { replace: { blockId: boundedIdentity(replace.blockId, bounds.maxPageIdCharacters, "blockId"),
      ...(replace.dataSourceId ? { dataSourceId: replace.dataSourceId as string } : {}) } } : {}),
    ...(start.replacesPage === true ? { replacesPage: true as const } : {}),
  };
}

/**
 * A page's settings change (its title, icon, cover, place, trash, …) made
 * here: kept in the same lane as its structure changes, in the order made,
 * so one the server can't take yet waits for it across a reload (2026-10-26:
 * a title typed while the server was out of reach was gone after a reload).
 * The page as it was when made goes along, for the server's cursor.
 */
export interface NativeMetadataMutation {
  baseline: PageMetadataBaseline;
  change: NativePageMetadataChange;
  mutationId: string;
  pageId: string;
  /**
   * The page's changes made here before it and not yet answered: the server
   * takes it past its base cursor when the page's last change is one of them
   * (page_metadata_after_own.go). One that carries them (`carriesAfter`) —
   * sent for them as the page goes (nativeLeavingTogether.ts) — takes them
   * with it.
   */
  after?: string[];
  carriesAfter?: true;
}

/**
 * A page made on this device first: it shows and opens at once, its changes
 * kept behind it in the lane, and the server makes it under the same ID when
 * it can be reached, the request ID telling a try sent again from a new one
 * (2026-11-07: on a stalled network the sidebar's new page waited for the
 * server, gave up, and what was typed for it went).
 */
export interface NativePageCreation {
  input: PageCreateInput & { pageId: string };
  mutationId: string;
  /** The page as shown here until the server has it. */
  page: Page;
  pageId: string;
}

/** What became of a graph mutation handed to the outbox. */
export type NativeGraphMutationDelivery =
  | { result: unknown; state: "committed" }
  // Kept for the server, which couldn't be reached; it goes out later.
  | { state: "kept" }
  // Another tab sent it first.
  | { state: "published" };

/** What the outbox's lane of graph mutations did, for those waiting on it. */
export type NativeGraphLaneEvent =
  | { kind: "drained" }
  | { kind: "kept"; pageId: string }
  // A page made here that the server now has.
  | { kind: "made"; pageId: string }
  | { kind: "merged"; pageId: string }
  // A graph mutation the server answered, taking or refusing it (`refused`).
  | { kind: "answered"; mutationId: string; pageId: string; refused?: true }
  // A kept change the server refused for good, dropped from the device;
  // `settings` when it was the page's title, icon or other setting, and
  // `written` the entry as it was kept (for what it wrote: refusedEditsNotice.ts).
  | { error: unknown; kind: "rejected"; pageId: string; settings?: true; written?: unknown }
  // A change neither the store nor the server could take, with no room to
  // write it down past the store: on screen, not saved yet, sent again
  // until one takes it (nativePageMutationPastStore.ts).
  | { kind: "unsaved"; pageId: string };

export interface NativeGraphMutationOperation {
  causalityKeys: string[];
  kind: "content_graph_mutation";
  mutationId: string;
  operations: unknown[];
  pageId: string;
}

export interface NativeMetadataMutationOperation extends NativeMetadataMutation {
  causalityKeys: string[];
  kind: "page_metadata";
}

export interface NativePageCreationOperation extends NativePageCreation {
  causalityKeys: string[];
  kind: "page_create";
}

export function boundedIdentity(value: unknown, maximum: number, label: string): string {
  if (typeof value !== "string" || value.length === 0 || value.length > maximum) {
    throw new RangeError(`${label} exceeds its configured character bound.`);
  }
  return value;
}

function recordOf(stored: unknown): Record<string, unknown> | null {
  return stored && typeof stored === "object" && !Array.isArray(stored) ? stored as Record<string, unknown> : null;
}

const DEFAULT_MAX_GRAPH_OPERATIONS = 1000;

export function validateGraphMutation(
  input: NativeGraphMutation,
  bounds: NativePageMutationBounds,
): NativeGraphMutation {
  const maximum = bounds.maxGraphOperations ?? DEFAULT_MAX_GRAPH_OPERATIONS;
  // A database start on a page with no line takes none away.
  const start = input?.start === undefined ? undefined : databaseStartOf(input.start, bounds);
  const rowTemplates = input?.rowTemplates === undefined ? [] : rowTemplatesOf(input.rowTemplates, bounds);
  // Rows going to or out of the trash, and a row's values, go by their own routes, with no operations.
  const rowTrash = input?.rowTrash === undefined ? undefined : rowTrashOf(input.rowTrash, bounds, maximum);
  const rowValues = input?.rowValues === undefined ? undefined : rowValuesOf(input.rowValues, bounds);
  if (!input || !Array.isArray(input.operations) || (input.operations.length === 0 && !start && !rowTrash && !rowValues)
    || input.operations.length > maximum || ((rowTrash || rowValues) && input.operations.length > 0) || (rowTrash && rowValues)) {
    throw new RangeError("Graph mutation exceeds its configured operation bound.");
  }
  return {
    mutationId: boundedIdentity(input.mutationId, bounds.maxMutationIdCharacters, "mutationId"),
    operations: input.operations,
    pageId: boundedIdentity(input.pageId, bounds.maxPageIdCharacters, "pageId"),
    ...(typeof input.words === "string" && input.words ? { words: input.words } : {}),
    ...(typeof input.typing === "string" && input.typing ? { typing: input.typing } : {}),
    ...(input.pageKind === "database" ? { pageKind: input.pageKind, ...(input.replacesPage === true ? { replacesPage: true as const } : {}) } : {}),
    ...(start ? { start } : {}),
    ...(rowTemplates.length ? { rowTemplates } : {}),
    ...(rowTrash ? { rowTrash } : {}),
    ...(rowValues ? { rowValues } : {}),
  };
}

/**
 * The graph mutation an outbox entry keeps, null for a page's text, or
 * "unreadable" for one whose shape no longer reads, which stays kept.
 */
export function graphMutationOf(
  stored: unknown,
  bounds: NativePageMutationBounds,
): NativeGraphMutation | "unreadable" | null {
  const value = recordOf(stored);
  if (value?.kind !== "content_graph_mutation") return null;
  try {
    return validateGraphMutation({
      mutationId: String(value.mutationId ?? ""),
      operations: value.operations as unknown[],
      pageId: String(value.pageId ?? ""),
      ...(typeof value.words === "string" ? { words: value.words } : {}),
      ...(typeof value.typing === "string" ? { typing: value.typing } : {}),
      ...(value.pageKind === "database" ? { pageKind: value.pageKind, ...(value.replacesPage === true ? { replacesPage: true as const } : {}) } : {}),
      ...(value.start !== undefined ? { start: value.start as NativeDatabaseStart } : {}),
      ...(value.rowTemplates !== undefined ? { rowTemplates: value.rowTemplates as GraphRowTemplateFill[] } : {}),
      ...(value.rowTrash !== undefined ? { rowTrash: value.rowTrash as NativeRowTrash } : {}),
      ...(value.rowValues !== undefined ? { rowValues: value.rowValues as NativeRowValues } : {}),
    }, bounds);
  } catch {
    return "unreadable";
  }
}

export function validateMetadataMutation(
  input: NativeMetadataMutation,
  bounds: NativePageMutationBounds,
): NativeMetadataMutation {
  const record = (value: unknown) => Boolean(value) && typeof value === "object" && !Array.isArray(value);
  const baseline = input?.baseline;
  if (!record(input?.change) || !record(baseline) || !Number.isSafeInteger(baseline.cursor) || baseline.cursor < 0
    || baseline.cursor > bounds.maxBaseCursor || typeof baseline.title !== "string" || typeof baseline.inTrash !== "boolean") {
    throw new RangeError("Page settings change exceeds its configured bounds.");
  }
  // The title's marks it was typed on, and a wiki's page's wiki, go with it
  // as made: sent again after a reload, it is sent as it was the first time.
  return {
    baseline: { cursor: baseline.cursor, inTrash: baseline.inTrash, parentId: baseline.parentId, title: baseline.title,
      ...(Array.isArray(baseline.titleSpans) && baseline.titleSpans.length ? { titleSpans: baseline.titleSpans } : {}),
      ...(typeof baseline.wikiDataSourceId === "string" && baseline.wikiDataSourceId
        ? { wikiDataSourceId: boundedIdentity(baseline.wikiDataSourceId, bounds.maxPageIdCharacters, "wikiDataSourceId") } : {}) },
    change: input.change,
    mutationId: boundedIdentity(input.mutationId, bounds.maxMutationIdCharacters, "mutationId"),
    pageId: boundedIdentity(input.pageId, bounds.maxPageIdCharacters, "pageId"),
    ...(Array.isArray(input.after) && input.after.length
      ? { after: input.after.slice(0, 64).map((id) => boundedIdentity(id, bounds.maxMutationIdCharacters, "after")) } : {}),
    ...(input.carriesAfter === true ? { carriesAfter: true as const } : {}),
  };
}

/** The settings change an outbox entry keeps, null for another, or "unreadable". */
export function metadataMutationOf(
  stored: unknown,
  bounds: NativePageMutationBounds,
): NativeMetadataMutation | "unreadable" | null {
  const value = recordOf(stored);
  if (value?.kind !== "page_metadata") return null;
  try {
    return validateMetadataMutation({
      baseline: value.baseline as PageMetadataBaseline,
      change: value.change as NativePageMetadataChange,
      mutationId: String(value.mutationId ?? ""),
      pageId: String(value.pageId ?? ""),
    }, bounds);
  } catch {
    return "unreadable";
  }
}

export function validatePageCreation(
  input: NativePageCreation,
  bounds: NativePageMutationBounds,
): NativePageCreation {
  const pageId = boundedIdentity(input?.pageId, bounds.maxPageIdCharacters, "pageId");
  const mutationId = boundedIdentity(input?.mutationId, bounds.maxMutationIdCharacters, "mutationId");
  const page = recordOf(input?.page) as Page | null;
  const request = recordOf(input?.input) as NativePageCreation["input"] | null;
  if (!page || page.id !== pageId || typeof page.workspaceId !== "string" || !request
    || request.pageId !== pageId || request.requestId !== mutationId || request.workspaceId !== page.workspaceId) {
    throw new RangeError("Page creation is not one page's.");
  }
  return { input: request, mutationId, page, pageId };
}

/** The page creation an outbox entry keeps, null for another, or "unreadable". */
export function pageCreationOf(
  stored: unknown,
  bounds: NativePageMutationBounds,
): NativePageCreation | "unreadable" | null {
  const value = recordOf(stored);
  if (value?.kind !== "page_create") return null;
  try {
    return validatePageCreation(value as unknown as NativePageCreation, bounds);
  } catch {
    return "unreadable";
  }
}
