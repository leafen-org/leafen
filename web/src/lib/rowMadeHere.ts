import {
  emptyContentGraph, type GraphBlock, type GraphOperation, type GraphProperty, type GraphRow, type GraphRowTemplateFill,
} from "@/contentGraph";
import { databaseRowPageBounds, type DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { keepRowPageReads } from "@/lib/keptRowPage";
import { localFirstProductCache } from "@/lib/localFirstProductCache";
import { appendYgoRecoveryUpdate } from "@/offline";
import type { NativeGraphMutationOutbox } from "@/lib/nativePageMutationBrowser";
import { tellPagesNotYetMade } from "@/lib/pagesNotYetMade";
import { serverIdentity } from "@/lib/serverOrigin";
import { forgetShownGraph, holdShownGraph } from "@/lib/shownGraphs";
import { notePageMadeHereReach } from "@/lib/usePageOnlyReader";
import { holdPageTextAhead } from "@/lib/pageTextAhead";
import type { Session } from "@/types";

// A database row is made on this device first wherever it is made — a view's
// "새로 만들기", a board's, list's, gallery's or calendar's "+", a sub-item, a
// row typed in place — as Notion's opens a new row's page at once, however
// the network is (2026-11-12: on a stalled network "새로 만들기" waited for
// the server, its page never opened, and what was typed after the title's
// Enter was lost). It shows at once, its page opens from copies made here,
// and it goes to the server behind, kept in the outbox. Until the server has
// it, what asks the server about it waits (pagesNotYetMade): its page's read,
// its body's, its room, which were refused before.

// Rows made here the server doesn't have yet: the change that makes each, once
// it is sent, and the page that change goes to.
const making = new Map<string, { mutationId: string; pageId: string } | null>();
const listeners = new Set<() => void>();
const announce = () => { for (const listener of [...listeners]) listener(); };
tellPagesNotYetMade({
  has: (rowId) => making.has(rowId),
  ready: () => Promise.resolve(),
  subscribe(listener) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  },
});

// How long a new row's body from a template waits for its copy here before it shows.
const PAGE_COPY_WAIT_MS = 300;

// Whether each row made here reached the server or was kept to go behind.
const saves = new Map<string, Promise<boolean>>();
// What settles once the server made each row made here (true), or refused it.
const arrivals = new Map<string, (made: boolean) => void>();
// The template each row made here starts from, sent with the change that
// makes it (rowsMadeBy), which the server fills it from as it makes it
// (content_graph_row_templates.go): kept with the change in the outbox, the
// fill goes whenever the row does, after a reload or a closed tab too.
const startsFrom = new Map<string, GraphRowTemplateFill>();
// What each row made here from a template was made naming, kept past its
// making (rowMadeNamingTemplate): the template, or the database whose
// default the server names as it makes the row.
const namedFrom = new Map<string, RowNamingTemplate>();

/** A template a row's making names: one by id, or its database's default as the server names it. */
export type RowNamingTemplate = { templateId: string } | { dataSourceId: string };

/**
 * The template a row made here starts from (rowTemplateHere.ts
 * rowTemplateToMakeFrom): one the toolbar names, or none for its
 * database's default as the server names it then; with the body copied
 * here, its blocks and words.
 */
export type RowMadeFromTemplate = Omit<GraphRowTemplateFill, "rowId">;

// The template of the row the next makeRowHere makes, while a view's
// adapter makes it in makeRowsFromTemplate.
let nextRowFrom: RowMadeFromTemplate | undefined;

/**
 * Makes the row `make` makes here — a view's adapter making it at once, in
 * its call (nativeTableRowSteps.ts, nativeBoardProductAdapter.ts,
 * nativeTimelineProductAdapter.ts) — start from `template`, sent with its
 * making as makeRowHere's own `template` is.
 */
export function makeRowsFromTemplate<T>(template: RowMadeFromTemplate | undefined, make: () => T): T {
  const before = nextRowFrom;
  nextRowFrom = template;
  try {
    return make();
  } finally {
    nextRowFrom = before;
  }
}
// Each row's page copy as made here, written; and the body it starts with,
// a template's copied here (rowTemplateHere.ts), by row.
const copiesKept = new Map<string, Promise<void>>();
const bodiesMadeHere = new Map<string, GraphBlock[]>();

function pageCopy(blocks: GraphBlock[]) {
  return { bootstrapRevision: 0, graphRevision: 0, graph: { ...emptyContentGraph, blocks }, canEdit: true, canSuggest: false };
}

function waitAtMostAMoment(kept: Promise<void>) {
  return Promise.race([kept, new Promise<void>((done) => { globalThis.setTimeout(done, PAGE_COPY_WAIT_MS); })]);
}

/** Whether a row just made here is on the server or kept to go there: at once for any other. */
export function whenRowSaved(rowId: string): Promise<boolean> {
  return saves.get(rowId) ?? Promise.resolve(true);
}

/**
 * Settles once the server made a row made here (true) — filled from its
 * template too, in the same request — or refused it (false); at once for
 * any other row.
 */
export function whenRowMadeThere(rowId: string): Promise<boolean> {
  if (!arrivals.has(rowId)) return Promise.resolve(true);
  return new Promise((settle) => {
    const before = arrivals.get(rowId)!;
    arrivals.set(rowId, (made) => { before(made); settle(made); });
  });
}

/** Whether the row was made here and the server hasn't made it yet. */
export function rowNotYetMade(rowId: string) {
  return making.has(rowId);
}

/**
 * Has a row made here whose making hasn't gone to the outbox yet start from
 * `template` instead (one chosen on its page, rowTemplateChosenHere.ts), sent
 * with its making; false once its making went, or for any other row.
 */
export function startRowFrom(rowId: string, template: RowMadeFromTemplate) {
  if (making.get(rowId) !== null) return false;
  startsFrom.set(rowId, { ...template, rowId });
  if (template.templateId) namedFrom.set(rowId, { templateId: template.templateId });
  return true;
}

/**
 * The template a row made here was made naming (makeRowHere, startRowFrom),
 * after its making went too; none for a row made as the empty page, one the
 * server refused, or one not made here.
 */
export function rowMadeNamingTemplate(rowId: string): RowNamingTemplate | undefined {
  return namedFrom.get(rowId);
}

/** The page the change making a row made here went to, once it went to the outbox. */
export function pageRowMadeOn(rowId: string) {
  return making.get(rowId)?.pageId;
}

function answered(rowIds: readonly string[], made = true) {
  let changed = false;
  for (const rowId of rowIds) {
    changed = making.delete(rowId) || changed;
    copiesKept.delete(rowId);
    bodiesMadeHere.delete(rowId);
    startsFrom.delete(rowId);
    if (!made) namedFrom.delete(rowId);
    const arrived = arrivals.get(rowId);
    arrivals.delete(rowId);
    arrived?.(made);
  }
  if (changed) announce();
}

/**
 * Makes a row on this device first: it is a row made here from now (what asks
 * the server about it waits), sent with persist, and when refused it goes
 * again (refused). Gives whether the server took it or keeps it to go behind
 * (saved), and when its page can open from here (pageReady): in the task
 * after the press, its body drawn empty from what this tab holds
 * (shownGraphs.ts), as a page made here is, so the body takes typing at once;
 * its copy is kept behind. Waiting for the copy, a gallery's "+ 새 페이지"
 * opened its page 50–100 ms later (2026-11-15, NAS + CPU ×2). It still waits
 * for a task of its own, as it did for the copy: what opens the page once the
 * making settled finds the view as the press left it, the row not yet drawn,
 * and the row and its page are drawn together. Made from a template
 * (`template`, or makeRowsFromTemplate's), the change that makes it says
 * so, and the server fills it as it makes it.
 */
export function makeRowHere(row: GraphRow, principalId: string | undefined, persist: () => Promise<boolean>, refused: () => void,
  template = nextRowFrom) {
  nextRowFrom = undefined;
  making.set(row.id, null);
  arrivals.set(row.id, () => {});
  if (template) {
    startsFrom.set(row.id, { ...template, rowId: row.id });
    namedFrom.set(row.id, template.templateId ? { templateId: template.templateId } : { dataSourceId: row.dataSourceId });
  }
  announce();
  if (principalId) holdShownGraph(principalId, row.id, pageCopy([]));
  const saved = persist().catch(() => false).then((ok) => {
    saves.delete(row.id);
    if (!ok) {
      answered([row.id], false);
      if (principalId) forgetShownGraph(principalId, row.id);
      refused();
    }
    return ok;
  });
  saves.set(row.id, saved);
  const kept = principalId ? localFirstProductCache.graph.writeCache({
    complete: true, key: row.id, principalId, serverKey: serverIdentity(), version: 1, value: pageCopy([]),
  }).then(() => undefined, () => undefined) : Promise.resolve();
  copiesKept.set(row.id, kept);
  const pageReady = new Promise<void>((done) => { globalThis.setTimeout(done, 0); });
  return { saved, pageReady };
}

/**
 * Gives a row just made here the body it starts with, a template's copied
 * here, its words as one update: its page's copy here holds them, as the
 * page's own read shows them, so its page opens with them. Settles once
 * kept, or after a moment at most.
 */
export function keepRowBodyMadeHere(session: Session, rowId: string, blocks: GraphBlock[], words: Uint8Array) {
  if (making.has(rowId)) bodiesMadeHere.set(rowId, blocks);
  const shown = pagesMadeHere.get(rowId);
  if (shown) {
    const page = { ...shown, body: rowPageBody(blocks) };
    pagesMadeHere.set(rowId, page);
    keepRowPageReads(session, rowId, { page });
  }
  const principalId = session.user.id;
  // Its page opening next draws its body from what this tab holds (makeRowHere), its words too (pageTextAhead.ts).
  if (making.has(rowId)) {
    holdShownGraph(principalId, rowId, pageCopy(blocks));
    holdPageTextAhead(principalId, rowId, words);
  }
  // After the empty copy makeRowHere keeps, which it takes the place of.
  const kept = (copiesKept.get(rowId) ?? Promise.resolve()).then(() => Promise.all([
    localFirstProductCache.graph.writeCache({
      complete: true, key: rowId, principalId, serverKey: serverIdentity(), version: 1, value: pageCopy(blocks),
    }),
    // As this tab's own writing of the page, which its room takes from the copy too.
    appendYgoRecoveryUpdate(principalId, rowId, words, true),
  ])).then(() => undefined, () => undefined);
  copiesKept.set(rowId, kept);
  return waitAtMostAMoment(kept);
}

function rowPageBody(blocks: readonly GraphBlock[]): DatabaseRowPageSnapshot["body"] {
  return blocks.slice(0, databaseRowPageBounds.bodyPageSize)
    .map(({ id, parentId, position, type, content }) => ({ id, parentId, position, type, content }));
}

// Who shows a page's rows, told of a row made for it elsewhere (a sidebar "+").
const madeFor = new Map<string, Set<(row: GraphRow) => void>>();

/** Hears of rows made here for a page's databases elsewhere in the app; the returned function stops it. */
export function onRowMadeHere(pageId: string, listener: (row: GraphRow) => void) {
  const listeners = madeFor.get(pageId) ?? new Set();
  listeners.add(listener);
  madeFor.set(pageId, listeners);
  return () => { listeners.delete(listener); };
}

/** Tells the page shown, if it is the row's, of a row made here for it elsewhere: it shows the row at once. */
export function tellRowMadeHere(pageId: string, row: GraphRow) {
  for (const listener of [...madeFor.get(pageId) ?? []]) listener(row);
}

/**
 * A change sent to the server that makes rows made here: they are made once
 * it reaches the server (reached), or, kept in the outbox, once the outbox
 * says it went (sentFrom); refused, they are no longer waited for either.
 * The templates they start from go with it (`templates`, makeRowHere).
 */
export function rowsMadeBy(pageId: string, mutationId: string, operations: readonly GraphOperation[]) {
  const rowIds = operations.flatMap((operation) => operation.kind === "upsertRow"
    && making.has(operation.row.id) && making.get(operation.row.id) === null ? [operation.row.id] : []);
  for (const rowId of rowIds) making.set(rowId, { mutationId, pageId });
  const templates = rowIds.flatMap((rowId) => startsFrom.get(rowId) ?? []);
  const reached = (made = true) => answered(rowIds, made);
  return {
    templates,
    reached,
    sentFrom(outbox: Pick<NativeGraphMutationOutbox, "graphPagesWaiting" | "subscribeGraphLane">) {
      if (!rowIds.length) return;
      // The outbox says when the change went or was refused; another tab
      // sending it shows as the page no longer waiting.
      let finished = false, stopLane = () => {}, stopWaiting = () => {};
      const done = (made = true) => {
        if (finished) return;
        finished = true;
        stopLane();
        stopWaiting();
        reached(made);
      };
      stopLane = outbox.subscribeGraphLane((event) => {
        if (event.kind === "answered" && event.mutationId === mutationId) done(event.refused !== true);
        else if (event.kind === "drained") done();
      });
      stopWaiting = outbox.graphPagesWaiting.subscribe(() => { if (!outbox.graphPagesWaiting.has(pageId)) done(); });
      if (finished) { stopLane(); stopWaiting(); }
    },
  };
}

// The pages of rows made here as they open before the server has them, by
// row, and what each was made from.
const pagesMadeHere = new Map<string, DatabaseRowPageSnapshot>();
const madeFrom = new WeakMap<DatabaseRowPageSnapshot, { values: unknown; icon: unknown; properties: readonly GraphProperty[] }>();

/** Whether a row page is one made here as it opens, its body empty: one with nothing to read yet. */
export function rowPageMadeEmpty(snapshot: DatabaseRowPageSnapshot) {
  return snapshot.snapshotIdentity === `made-here:${snapshot.row.id}` && snapshot.body.length === 0;
}

/**
 * A row page read in place of its page as made here: the icon its template
 * gave it here stays while the read has none — the server makes the row,
 * then fills it from its template — till its page is read again once filled
 * (applyDatabaseRowTemplate; 2026-11-25: a new task's side peek lost its
 * template's icon at its first read).
 */
export function readOverMadeHere(current: DatabaseRowPageSnapshot | null, next: DatabaseRowPageSnapshot): DatabaseRowPageSnapshot {
  return current?.snapshotIdentity === `made-here:${next.row.id}` && current.row.icon && !next.row.icon
    ? { ...next, row: { ...next.row, icon: current.row.icon, iconType: current.row.iconType } } : next;
}

/** A row made here the server doesn't have yet, as its page opens meanwhile; none for any other. */
export function rowPageWhileNotYetMade(rowId: string) {
  return making.has(rowId) ? pagesMadeHere.get(rowId) ?? null : null;
}

/**
 * The page of a row made here, as it opens before the server has it: the
 * row with its database's properties, writable by its maker, its body empty
 * or the template's copied here (keepRowBodyMadeHere); kept as the page's
 * copy here, as a page read is (keptRowPage.ts).
 */
export function rowPageMadeHere(session: Session, row: GraphRow, properties: readonly GraphProperty[], containerPageId: string,
  workspaceId: string): DatabaseRowPageSnapshot {
  // Asked again for the row as it was — its page opened as New made it, then
  // its peek's read asking — it is the same page, so nothing draws it again.
  const kept = pagesMadeHere.get(row.id), from = kept && madeFrom.get(kept);
  if (kept && from && from.values === row.values && from.icon === row.icon && from.properties === properties
    && kept.row.containerPageId === containerPageId && kept.workspaceId === workspaceId) return kept;
  const now = new Date().toISOString();
  const snapshot: DatabaseRowPageSnapshot = {
    snapshotIdentity: `made-here:${row.id}`,
    containerSnapshotRevision: 0,
    bodySnapshotRevision: 0,
    row: {
      id: row.id, containerPageId, dataSourceId: row.dataSourceId, subitemParentId: row.subitemParentId,
      position: row.position, values: row.values as DatabaseRowPageSnapshot["row"]["values"],
      icon: row.icon ?? "", iconType: (row.iconType ?? "none") as DatabaseRowPageSnapshot["row"]["iconType"],
      cover: "", coverPosition: 50, font: "default", smallText: false, fullWidth: false, tableOfContentsDisplay: "default",
      favorite: false, locked: false, unlockedForMe: false, archivedAt: "", archivedBy: "", trashedAt: "", trashedBy: "",
      createdAt: row.createdAt ?? now, createdBy: row.createdBy ?? session.user.id,
      updatedAt: row.updatedAt ?? now, lastEditedBy: row.lastEditedBy ?? session.user.id, authorityRevision: 0,
    },
    body: rowPageBody(bodiesMadeHere.get(row.id) ?? []),
    nextCursor: null,
    hasMore: false,
    properties: properties.filter(({ dataSourceId }) => dataSourceId === row.dataSourceId)
      .map(({ id, name, type, config, position, access }) => ({ id, name, type, config: config ?? {}, position, ...(access ? { access } : {}) })),
    accessRole: "edit",
    workspaceId,
    databaseWritable: true,
  };
  keepRowPageReads(session, row.id, { page: snapshot });
  pagesMadeHere.set(row.id, snapshot);
  madeFrom.set(snapshot, { values: row.values, icon: row.icon, properties });
  // Its bar shows how far it reaches before the server can say: as its database's page.
  notePageMadeHereReach(session, { id: row.id, parentId: containerPageId });
  return snapshot;
}
