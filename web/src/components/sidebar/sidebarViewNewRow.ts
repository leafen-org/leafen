import { startTransition, useEffect, useMemo, useState } from "react";
import { compareGraphOrder, type ContentGraph, type GraphOperation, type GraphRow, type GraphView } from "@/contentGraph";
import type { DatabaseRowPagePresentationInput } from "@/databaseRowPageWire.generated";
import { createDatabaseToolbarTemplateRow, readDatabaseToolbarConfig } from "@/databaseToolbarConfigApi";
import { placeCaret } from "@/components/editor/focus";
import { rowOrderWithNew, rowsInViewOrder } from "@/components/database/viewRowOrder";
import { collectAuthenticatedContentGraph } from "@/lib/authenticatedContentGraph";
import type { PagePresentationChange } from "@/components/page/pagePresentation";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { newId } from "@/lib/ids";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import { newRowStart } from "@/lib/newRowStart";
import { makeRowHere, rowPageMadeHere, tellRowMadeHere } from "@/lib/rowMadeHere";
import { sendRowPresentation } from "@/lib/rowValuesKept";
import {
  fillRowFromTemplateAhead, heldRowTemplate, madeFromTemplate, readRowTemplateAhead, rowTemplateToMakeFrom, rowTemplateValues, rowWithTemplateIcon,
} from "@/lib/rowTemplateHere";
import { isNewSprintName } from "@/lib/sprintRows";
import type { TextSpan } from "@/lib/types";
import { sameTitle } from "@/lib/titleMerge";
import type { Session } from "@/types";
import type { SidebarLayoutShortcut, SidebarLayoutView } from "@/workspaceMembershipWire.generated";

// The pages holding the databases the sidebar's shortcuts add to, as last
// read here, so a shortcut's "새 페이지" makes its row on this device at the
// press (useSidebarShortcutAhead); and the first place each database's new
// rows took here, so one made before the page is read again still goes
// first, not level with the last one.
const heldGraphs = new Map<string, { graph: ContentGraph; canEdit: boolean }>();
const madeFirst = new Map<string, string>();
const heldKey = (session: Pick<Session, "user">, id: string) => `${session.user.id}\u0000${id}`;

/** The view a section shows or a shortcut adds to: the one it names, else its database's first. */
export function sidebarTargetView(view: SidebarLayoutView, graph: ContentGraph): GraphView | undefined {
  const same = graph.views.filter((candidate) => candidate.dataSourceId === view.dataSourceId);
  return same.find((candidate) => candidate.id === view.viewId)
    ?? [...same].sort((left, right) => (left.position < right.position ? -1 : left.position > right.position ? 1 : 0))[0];
}

/**
 * The view with rows made from the sidebar placed in its own order where its
 * New places a row (viewRowOrder.ts): a table's, list's or gallery's first, a
 * board's last; null for a view that keeps the database's one order or is
 * sorted. Left out of its order, a row a section's "+" made showed after the
 * rows the view had placed — second, behind one made in its table (2026-11-14).
 */
export function viewWithRowsPlaced(target: GraphView, rows: readonly GraphRow[], madeIds: readonly string[]): GraphView | null {
  const sorted = Array.isArray(target.config.sorts) && target.config.sorts.length > 0;
  const place = target.type === "board" ? "last" : target.type === "table" || target.type === "list" || target.type === "gallery" ? "first" : null;
  if (!place || sorted || !Array.isArray(target.config.rowOrder)) return null;
  const own = rows.filter((row) => row.dataSourceId === target.dataSourceId && !row.subitemParentId);
  let config: Record<string, unknown> = target.config;
  for (const rowId of madeIds) {
    const rowOrder = rowOrderWithNew(config, rowId, place, rowsInViewOrder(own, config).map(({ id }) => id));
    if (!rowOrder) return null;
    config = { ...config, rowOrder };
  }
  return { ...target, config };
}

/** The page holding a shortcut's database as last read here, or none. */
export function heldSidebarGraph(session: Session, pageId: string) {
  return heldGraphs.get(heldKey(session, pageId));
}

/**
 * The row a sidebar view section's "새 페이지" makes, first among its
 * database's rows, as Notion's shows it at the top of the section (seen
 * 2026-10-02), from the section's template when it names one still there.
 * Returns its id, which is its page's, the values it was made with, and
 * whether it waits here for a server out of reach (kept), to be sent once
 * the server can be reached. Notion's ⌘Z doesn't take it back (seen
 * 2026-11-09), nor does Leafen's.
 *
 * Given where it is shown (`here`), it is made on this device first, as a
 * database's own new rows are (rowMadeHere.ts): `shown` hears it at once,
 * its page opens from copies made here, and it goes to the server behind,
 * kept in the outbox while the server is out of reach; `saved` says when the
 * server has it (user decision 2026-11-12: a press answers on the next
 * frame). One from a template comes with the template's title, values, icon
 * and body as read ahead here (useSidebarViewTemplateAhead,
 * rowTemplateHere.ts), and the server fills what it leaves empty as it makes
 * the row — the body too when it wasn't copied here. Without `here` a
 * template's row is made by the server first.
 */
export async function createSidebarViewRow(
  session: Session, view: SidebarLayoutView, graph: ContentGraph,
  here?: { workspaceId: string; shown: (row: GraphRow, saved: Promise<boolean>) => void },
): Promise<{ rowId: string; values: GraphRow["values"]; kept: boolean }> {
  const first = graph.rows
    .filter((row) => row.dataSourceId === view.dataSourceId && !row.subitemParentId)
    .sort(compareGraphOrder)[0]?.position;
  const madeHere = madeFirst.get(heldKey(session, view.dataSourceId));
  const position = graphPositionBetween(undefined, madeHere && (!first || madeHere < first) ? madeHere : first);
  madeFirst.set(heldKey(session, view.dataSourceId), position);
  if (view.templateId && !here) {
    const config = await readDatabaseToolbarConfig(session, view.pageId, view.dataSourceId).catch(() => null);
    const template = config?.templates.find((candidate) => candidate.id === view.templateId && !candidate.deleted);
    if (template) {
      const created = await createDatabaseToolbarTemplateRow(session, view.pageId, view.dataSourceId, {
        requestId: newId(),
        templateId: template.id,
        expectedTemplateRevision: template.templateAuthorityRevision,
        position,
        timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      });
      return { rowId: created.rowId, values: {}, kept: false };
    }
  }
  const title = graph.properties.find((property) => property.dataSourceId === view.dataSourceId && property.type === "title");
  // The view's template as held here; one deleted since gives nothing.
  const held = view.templateId ? heldRowTemplate(session, view.templateId) : undefined;
  const template = held && !held.deleted ? held : undefined;
  const templateValues = Object.entries(template ? rowTemplateValues(session, template, graph.properties, view.dataSourceId) : {})
    .flatMap(([id, value]) => {
      const property = graph.properties.find((candidate) => candidate.id === id);
      return property ? [[id, { type: property.type, value }] as const] : [];
    });
  const target = sidebarTargetView(view, graph);
  const row: GraphRow = {
    id: newId(),
    dataSourceId: view.dataSourceId,
    subitemParentId: null,
    position,
    values: {
      ...title ? newRowStart(graph.dataSources.find(({ id }) => id === view.dataSourceId), title.id, graph.properties)?.values
        ?? { [title.id]: { type: "title" as const, value: "" } } : {},
      ...Object.fromEntries(templateValues),
    },
  };
  // The view it is added to places it as its New would, once the server has the row.
  const placed = target ? viewWithRowsPlaced(target, graph.rows, [row.id]) : null;
  // A shortcut's database as read ahead holds it too, so its next press places the next row before it.
  const ahead = heldGraphs.get(heldKey(session, view.pageId));
  if (ahead?.graph === graph) {
    heldGraphs.set(heldKey(session, view.pageId), { ...ahead, graph: { ...graph, rows: [...graph.rows, row],
      views: placed ? graph.views.map((candidate) => candidate.id === placed.id ? placed : candidate) : graph.views } });
  }
  const place = () => placed ? sendContentGraphChange(session, view.pageId, [{ kind: "upsertView", view: placed }]).catch(() => undefined) : undefined;
  if (here) {
    // Its making names the view's template, which the server fills it from as
    // it makes it: its icon, the values it couldn't give here, its body when
    // not copied here (rowTemplateToMakeFrom).
    const templateId = held?.deleted ? undefined : view.templateId;
    const making = templateId ? rowTemplateToMakeFrom(session, templateId) : null;
    const { saved, pageReady } = makeRowHere(row, session.user.id,
      () => sendContentGraphChange(session, view.pageId, [{ kind: "upsertRow", row }]).then(() => true), () => {}, making?.template);
    void saved.then((made) => { if (made) void place(); });
    const made = making ? madeFromTemplate(session, row.id, making) : null;
    const shown = rowWithTemplateIcon(row, template);
    const sourcePageId = graph.dataSources.find(({ id }) => id === view.dataSourceId)?.pageId ?? view.pageId;
    rowPageMadeHere(session, shown, graph.properties, sourcePageId, here.workspaceId);
    // The page holding the database, open, shows it too, drawn behind the
    // press's frame (startTransition): drawn in it, its table held the press
    // 190 ms (2026-11-13, NAS condition).
    startTransition(() => tellRowMadeHere(view.pageId, shown));
    // Shown made now by its maker, so a view ordered newest first places it first, as the server will.
    here.shown({ ...shown, createdAt: new Date().toISOString(), createdBy: session.user.id }, saved);
    // Its page offers no templates while the server copies the template's body.
    if (templateId && made && !made.copied) fillRowFromTemplateAhead(session, row.id, templateId, made.filled.then(() => undefined));
    await Promise.all([pageReady, made?.shown]);
    return { rowId: row.id, values: row.values, kept: false };
  }
  const sent = await sendContentGraphChange(session, view.pageId, [{ kind: "upsertRow", row }]);
  void place();
  return { rowId: row.id, values: row.values, kept: sent === "kept" };
}

/**
 * Reads a view section's template ahead while the section shows to one who
 * may add to it, and again as the tab comes back, so its "새 페이지" makes
 * the row with the template's title, values, icon and body at the press
 * (createSidebarViewRow).
 */
export function useSidebarViewTemplateAhead(session: Session, view: SidebarLayoutView, editable: boolean) {
  const { pageId, dataSourceId, templateId } = view;
  useEffect(() => {
    if (!editable || !templateId) return;
    let live = true;
    const read = () => void readDatabaseToolbarConfig(session, pageId, dataSourceId).then((config) => {
      const template = config.templates.find(({ id }) => id === templateId);
      if (live && template) readRowTemplateAhead(session, template.id, template);
    }, () => undefined);
    read();
    const again = () => { if (document.visibilityState === "visible") read(); };
    document.addEventListener("visibilitychange", again);
    return () => {
      live = false;
      document.removeEventListener("visibilitychange", again);
    };
  }, [session, pageId, dataSourceId, templateId, editable]);
}

/**
 * Reads ahead the page holding a shortcut's database while the shortcut
 * shows, and again as the tab comes back, with its template, so its "새
 * 페이지" makes the row here at the press, as a view section's does
 * (openSidebarShortcut).
 */
export function useSidebarShortcutAhead(session: Session, shortcut: SidebarLayoutShortcut) {
  const view = useMemo(() => sidebarShortcutView(shortcut), [shortcut]);
  const [editable, setEditable] = useState(false);
  const { pageId } = shortcut;
  useEffect(() => {
    let live = true;
    const read = () => void collectAuthenticatedContentGraph(session, pageId, null, () => live).then((found) => {
      if (!live || !found) return;
      heldGraphs.set(heldKey(session, pageId), { graph: found.graph, canEdit: found.canEdit === true });
      setEditable(found.canEdit === true);
    }, () => undefined);
    read();
    const again = () => { if (document.visibilityState === "visible") read(); };
    document.addEventListener("visibilitychange", again);
    return () => {
      live = false;
      document.removeEventListener("visibilitychange", again);
    };
  }, [session, pageId]);
  useSidebarViewTemplateAhead(session, view, editable);
}

/** A shortcut's read ahead, drawing nothing. */
export function SidebarShortcutAhead({ session, shortcut }: { session: Session; shortcut: SidebarLayoutShortcut }) {
  useSidebarShortcutAhead(session, shortcut);
  return null;
}

/**
 * Puts the cursor in a new row's title once its page opens, as Notion's "새
 * 페이지" does (seen 2026-10-02), after the page takes the keys itself.
 */
export function focusNewRowTitle(rowId: string, tries = 40) {
  const title = document.querySelector<HTMLElement>(
    `[data-standalone-row="${CSS.escape(rowId)}"] [data-row-page-title], [data-row-peek-panel] [data-row-page-title]`,
  );
  if (title) {
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
      if (!isNewSprintName(title.textContent) || !placeCaret(title, "select")) title.focus();
    }));
    return;
  }
  if (tries > 0) window.setTimeout(() => focusNewRowTitle(rowId, tries - 1), 50);
}

/** A database's new-page shortcut read as the view its page is added to. */
export function sidebarShortcutView(shortcut: SidebarLayoutShortcut): SidebarLayoutView {
  return {
    key: shortcut.key, pageId: shortcut.pageId, dataSourceId: shortcut.dataSourceId ?? "", viewId: shortcut.viewId ?? "",
    name: shortcut.name, showIcons: false, templateId: shortcut.templateId,
  };
}

/** A view section's row's name, as its title holds it. */
export function sidebarViewRowName(view: SidebarLayoutView, graph: ContentGraph, rowId: string): SidebarViewRowName | null {
  const row = graph.rows.find((candidate) => candidate.id === rowId);
  const property = graph.properties.find((candidate) => candidate.dataSourceId === view.dataSourceId && candidate.type === "title");
  if (!row || !property) return null;
  const value = row.values[property.id];
  const title = String(value?.value ?? "");
  const spans = value?.spans as TextSpan[] | undefined;
  return spans?.length && spans.map((span) => span.text).join("") === title ? { title, spans } : { title };
}

/** A row's name, with its spans while it has any. */
export type SidebarViewRowName = { title: string; spans?: TextSpan[] };

/** Whether two names are one, however their spans are shaped (sameTitle). */
export function sameSidebarViewRowName(a: SidebarViewRowName, b: SidebarViewRowName) {
  return sameTitle(a, b);
}

/**
 * Renames a view section's row, as a sidebar page's "이름 바꾸기" does: the
 * row's title alone is written through the page holding its database, with
 * the spans its rename box kept — its mentions — or as plain words when the
 * box opened with none; a name it already has only as a step of ⌘Z
 * (`again`). Answers the name written, or null when nothing was.
 */
export async function renameSidebarViewRow(
  session: Session, view: SidebarLayoutView, graph: ContentGraph, rowId: string, name: SidebarViewRowName, again = false,
): Promise<SidebarViewRowName | null> {
  const change = sidebarViewRowNameChange(view, graph, rowId, name);
  if (!change) return null;
  const now = sidebarViewRowName(view, graph, rowId);
  if (!again && now && (sameSidebarViewRowName(change.written, now) || !name.spans && change.written.title === now.title)) return null;
  await sendContentGraphChange(session, view.pageId, [change.operation]);
  return change.written;
}

/**
 * The change renaming a view section's row writes — its title alone, on
 * one line — and the name it gives, which its rename shows at once
 * (lib/renameShownAtPress.ts); none for a row or title the graph lacks.
 */
export function sidebarViewRowNameChange(view: SidebarLayoutView, graph: ContentGraph, rowId: string, name: SidebarViewRowName) {
  const row = graph.rows.find((candidate) => candidate.id === rowId);
  const property = graph.properties.find((candidate) => candidate.dataSourceId === view.dataSourceId && candidate.type === "title");
  if (!row || !property) return null;
  const value = name.title.replace(/\s*\n+\s*/gu, " ");
  const spans = name.spans && name.spans.map((span) => span.text).join("") === value ? name.spans : undefined;
  const written: SidebarViewRowName = spans ? { title: value, spans } : { title: value };
  const operation: GraphOperation = { kind: "upsertRow", row: { ...row, values: { [property.id]: { type: "title", value, ...(name.spans ? { spans: spans ?? [] } : {}) } } } };
  return { operation, written };
}

/**
 * Gives a sidebar view row the icon its rename popover picked, as its page's
 * header does: any page icon, kept in the outbox behind the row's other
 * changes on `pageId`, the page holding its database (rowValuesKept.ts).
 */
export async function changeSidebarViewRowIcon(session: Session, rowId: string, change: PagePresentationChange, pageId?: string | null): Promise<boolean> {
  if (change.iconType === undefined) return false;
  await sendRowPresentation(session, rowId, { icon: change.icon ?? "", iconType: change.iconType as DatabaseRowPagePresentationInput["iconType"] }, pageId);
  return true;
}
