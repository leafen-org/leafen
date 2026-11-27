import { useDeferredValue, useEffect, useMemo, useRef, type ComponentProps, type ReactNode } from "react";
import { type CaretLanding, caretOnceAnswered, getEditable, placeCaret } from "@/components/editor/focus";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { holdingTyping } from "@/lib/pageTitleFocus";
import { ROW_PAGE_READING } from "@/lib/rowPageHandoff";
import { DatabaseRowPage } from "./DatabaseRowPage";
import { openingTakesTitleCaret } from "./peekFocusReturn";
import type { RowPageTopBarActions } from "./RowPageTopBarItems";

/**
 * A row's peek opens on its press, as Notion's (seen 2026-11-12: its frame
 * 73–105ms after the press, its content at 118–170ms): its frame and head —
 * title and properties — drawn from the row as its view holds it, its body
 * when the row page's read arrives. Waiting for the read drew nothing for
 * 270–330ms (a 37-row database of relations and rollups, seen 2026-11-12).
 *
 * Until the read arrives what writes by the read's revisions and roles waits:
 * its title, body, lock, favorite, comments and actions. Its properties stand
 * as the read draws them, the same parts, which the read then only fills: they
 * write through the view, as its cells do (drawn read-only first, the read made
 * a 40-property page's 760 parts anew, 54–65ms, seen 2026-11-13). A row its
 * view holds locked keeps them as read. A peek the reader hasn't moved into
 * since takes its title's caret once the read makes it editable, as a peek
 * opened on a read does at once.
 */
const READING = ROW_PAGE_READING;

/**
 * The row page shown for the opened row: the read once it is the row's, else,
 * in a peek, the row as its view holds it — unless the server wasn't there to
 * read it (`unanswered`), which its view says as such (RowPageUnavailable).
 */
export function useRowPageWhileReading(
  read: DatabaseRowPageSnapshot | null,
  selection: { rowId: string; mode: string } | null | undefined,
  row: GraphRow | undefined,
  properties: readonly GraphProperty[],
  containerPageId: string,
  workspaceId: string,
  unanswered: string | null,
): DatabaseRowPageSnapshot | null {
  const opened = selection && selection.mode !== "full" ? selection.rowId : undefined;
  const rowId = opened !== unanswered ? opened : undefined;
  // The read, once it comes, is drawn behind any press (useDeferredValue: in
  // pieces): its editable page is a new drawing of the whole page, 760 parts
  // for a row of 40 properties, which held one frame 64–68 ms (2026-11-13).
  // A page made here as it opens (rowPageMadeHere) is drawn as it comes: it
  // comes with the press that opens it, so what it replaces is not drawn first.
  // Drawn once for the row opened, a page stays drawn while the server doesn't
  // answer (`unanswered`), so the read that comes after takes its place at
  // once: drawn behind the row as its view holds it, the page made here, its
  // body being typed in, went for a frame and its caret with it, and what was
  // typed after went nowhere (2026-11-18, 2 s round trip: the read refused as
  // the connection seemed lost, read again 5–12 s on).
  const readHere = read?.row.id === opened;
  const readShown = useDeferredValue(readHere);
  const madeHere = readHere && read?.snapshotIdentity === `made-here:${opened}`;
  const waiting = Boolean(rowId && row?.id === rowId && !(readHere && (readShown || madeHere)));
  const shown = useMemo(
    () => (waiting && row ? rowPageAsViewHolds(row, properties, containerPageId, workspaceId) : null),
    [waiting, row, properties, containerPageId, workspaceId],
  );
  return shown ?? read;
}

function rowPageAsViewHolds(row: GraphRow, properties: readonly GraphProperty[], containerPageId: string,
  workspaceId: string): DatabaseRowPageSnapshot {
  const now = new Date().toISOString();
  return {
    snapshotIdentity: `${READING}${row.id}`,
    containerSnapshotRevision: 0,
    bodySnapshotRevision: 0,
    row: {
      id: row.id, containerPageId, dataSourceId: row.dataSourceId, subitemParentId: row.subitemParentId,
      position: row.position, values: row.values as DatabaseRowPageSnapshot["row"]["values"],
      // What the reader may not see or change, as the view holds it, holds here too.
      ...(row.hiddenValues ? { hiddenValues: row.hiddenValues } : {}), ...(row.readOnlyValues ? { readOnlyValues: row.readOnlyValues } : {}),
      icon: row.icon ?? "", iconType: (row.iconType ?? "none") as DatabaseRowPageSnapshot["row"]["iconType"],
      cover: row.cover ?? "", coverPosition: row.coverPosition ?? 50, font: "default", smallText: row.smallText ?? false, fullWidth: row.fullWidth ?? false, tableOfContentsDisplay: "default",
      favorite: false, locked: false, unlockedForMe: false, archivedAt: row.archivedAt ?? "", archivedBy: row.archivedBy ?? "", trashedAt: "", trashedBy: "",
      createdAt: row.createdAt ?? now, createdBy: row.createdBy ?? "",
      updatedAt: row.updatedAt ?? now, lastEditedBy: row.lastEditedBy ?? "", authorityRevision: 0,
    },
    body: [],
    nextCursor: null,
    hasMore: false,
    properties: properties.filter(({ dataSourceId }) => dataSourceId === row.dataSourceId)
      .map(({ id, name, type, config, position, access }) => ({ id, name, type, config: config ?? {}, position, ...(access ? { access } : {}) })),
    accessRole: "view",
    workspaceId,
  };
}

type HeldWhileReading = {
  snapshot: DatabaseRowPageSnapshot;
  editableRow?: { isLocked?: boolean };
  body?: ReactNode;
  bodyReadOnly?: boolean;
  topBar?: RowPageTopBarActions;
};

/** A row page with what writes held while its read is on its way. */
export function RowPageWhileReading(props: ComponentProps<typeof DatabaseRowPage>) {
  const reading = props.snapshot.snapshotIdentity.startsWith(READING);
  const wasReading = useRef(false);
  const rowId = props.snapshot.row.id;
  // The title's caret its read brings, asked for as the read begins: what is
  // typed meanwhile waits for it and is typed there (caretOnceAnswered) —
  // 2026-11-21: a letter typed right after a row's "열기" went nowhere, the
  // caret coming to the title once the row was read.
  const caret = useRef<{ land: CaretLanding; from: Element | null } | null>(null);
  // The row whose opening has had its read: a read again takes no caret — 2026-11-22: another row's right-click
  // menu read that row into the view's one row page, and this peek, read again, asked anew for its title's caret,
  // taking the menu's focus.
  const openedFor = useRef<string | null>(null);
  useEffect(() => () => { caret.current?.land(null); caret.current = null; }, []);
  useEffect(() => {
    const opening = openedFor.current !== rowId;
    if (reading) {
      wasReading.current = true;
      if (opening && !caret.current && openingTakesTitleCaret() && !holdingTyping()) {
        caret.current = { from: document.activeElement, land: caretOnceAnswered(() => rowPagePlace(rowId)) };
      }
      return;
    }
    if (opening) openedFor.current = rowId;
    if (!wasReading.current) return;
    wasReading.current = false;
    if (!opening) return;
    // The read made its title editable: the caret goes to its end if the peek still holds focus itself — what
    // is typed still held for it, or the focus where it was as the read began (not on a touch screen,
    // openingTakesTitleCaret); what took the focus meanwhile keeps it.
    const asked = caret.current, active = document.activeElement;
    const peek = active instanceof HTMLElement && active.matches("[data-row-peek-panel]");
    caret.current = null;
    if (asked) {
      const title = getEditable(`title:${rowId}`);
      asked.land(!title ? null : holdingTyping() || peek || active === asked.from ? { field: title, position: "end" } : "kept");
      return;
    }
    const panel = openingTakesTitleCaret() && peek ? active : null;
    const title = panel?.querySelector<HTMLElement>("[data-row-page-title]");
    if (title) placeCaret(title, "end");
  }, [reading, rowId]);
  return <DatabaseRowPage {...(reading ? rowPageWhileReading(props) : props)} />;
}

// Where a row page's title caret goes: the peek it is drawn in (DatabaseRowPage).
function rowPagePlace(rowId: string) {
  const page = document.querySelector(`[data-row-page-row="${CSS.escape(rowId)}"]`);
  return page?.closest("[data-row-peek-panel]") ?? page;
}

function rowPageWhileReading<P extends HeldWhileReading>(props: P): P {
  return {
    ...props,
    body: props.body ?? <div data-row-page-body-reading="" aria-busy="true" />,
    bodyReadOnly: true,
    ...(props.editableRow?.isLocked ? { editableRow: undefined, onRichTextChange: undefined, propertyCellEnvironment: undefined,
      propertyEditing: undefined, onAddProperty: undefined } : {}),
    onOpenComments: undefined, onOpenActions: undefined, onPresentationChange: undefined,
    onCreateTemplate: undefined, onHistoryRestored: undefined, onBodyContentChange: undefined, onUnarchive: undefined,
    onRestore: undefined, onDeleteForever: undefined, onCustomizeLayout: undefined,
    topBar: props.topBar && { ...props.topBar, onFavorite: undefined, lock: undefined, onOpenUpdates: undefined, onChangeDefaultMode: undefined },
  };
}
