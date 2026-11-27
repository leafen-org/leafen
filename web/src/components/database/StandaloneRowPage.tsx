import { setDocumentChrome } from "@/lib/documentChrome";
import { usePageFaviconHref } from "@/components/page/pageFavicon";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { usePageVisitRecorder } from "@/components/product/usePageVisitRecorder";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { GraphOperation, GraphProperty, GraphRow } from "@/contentGraph";
import {
  readDatabaseRowPage, setDatabaseRowPageFavorite, setDatabaseRowPageLock, setDatabaseRowPageLockForMe, writeDatabaseRowPageValues,
} from "@/databaseRowPageApi";
import type { DatabaseRowPageNewOption, DatabaseRowPageSnapshot, DatabaseRowPageValue } from "@/databaseRowPageWire.generated";
import { databaseRowCommentServiceFor } from "@/lib/commentService";
import { copyText } from "@/lib/copyText";
import { newId } from "@/lib/ids";
import { pageHref } from "@/lib/navigation";
import { pushRoute, replaceRoute } from "@/lib/router";
import { TEMPLATE_PAGE_PARAM } from "./useTemplateAddress";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import { RELATION_CREATION_BOUNDS } from "@/lib/relationPropertyCreationModel";
import { useProductToast } from "@/lib/productToast";
import { productLockRefusalReason } from "@/lib/productErrorPresentation";
import type { Page, PropertyType, TextSpan } from "@/lib/types";
import type { TitleValue } from "@/lib/titleMerge";
import { isRowValueChanged } from "@/lib/rowTitleSave";
import { lockForMeShownFirstIn } from "@/lib/lockForMeShownFirst";
import type { Session } from "@/types";
import { useHeldSignIn } from "@/lib/heldSignIn";
import { useDatabasePageLayoutBuilder } from "./useDatabasePageLayoutBuilder";
import { CommentsPanel } from "../CommentsPanel";
import type { PageSuggestingControl } from "../RowMenu";
import { useCommentLinkOpen } from "../comments/commentLinks";
import type { OpenCommentsDetail } from "../comments/commentPanelEvents";
import { nativeBoardQueryBounds } from "./boardViewModel";
import {
  rowPageActionsAnchor, type DatabaseRowNotifications, type DatabaseRowPageLockControl,
  type RowPageMenuExtras,
} from "./DatabaseRowMenu";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import type { DatabaseViewOpenMode } from "./DatabaseView";
import { DatabaseRowPage } from "./DatabaseRowPage";
import { DatabaseTaskRowsContext, rowPageUntitledName } from "./untitledRowName"; import { DatabaseStructureLockContext } from "./databaseStructureLock";
import { CellCommentsProvider } from "./CellComments";
import { rowPagePresentationSaver, rowPageStyle } from "./rowPageStyleSave";
import { newRowPageProperty } from "./rowPagePropertyAdd";
import { galleryPresentationPage, galleryPresentationProperty } from "./galleryViewModel";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import type { PropertyValueReadContext } from "./PropValue";
import { serverUrl } from "@/lib/serverOrigin";
import { uploadWorkspaceFile } from "@/lib/storage";
import { FILE_DROP_BOUNDS } from "../editor/fileDrop";
import { graphFilePropertyValue, normalizeFileAttachments } from "./files";
import { buttonExecutionCanRestart, databaseButtonResultFromWire } from "@/lib/buttonExecution";
import { useProductPages } from "@/components/product/productPagesContext";
import { recordRowDateCommand } from "@/lib/databaseDateCommandHistory";
import { dependencyShiftNoticesLeave, sayDependencyShift } from "@/lib/dependencyShiftNotice";
import { requestDatabaseTemplate } from "@/lib/databaseTemplateRequests";
import { StandaloneRowTemplates, templateBandDatabase } from "./StandaloneRowTemplates";
import { severalSourcesRowSourceTitle, useRowPathDatabase, useStandaloneRowMenu } from "./useStandaloneRowMenu";
import { useRowPageDockedPanel } from "./useRowPageDockedPanel";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { RowPageDatabaseAlone, rowPageDatabaseOpenedAlone } from "./RowPageDatabaseAlone";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { notePropertySteps, withPropertyChanges } from "./standaloneRowPropertySteps";
import { useRowPageValueSteps } from "./standaloneRowValueSteps";
import { useStandaloneRelationRows } from "./useStandaloneRelationRows";
import { addRelationTargetIds } from "./databaseFilterSnapshot";
import { useRowBodyEdits } from "@/lib/rowBodyEdits";
import { useRowPageReadAgain } from "./useRowPageReadAgain";
import { propertyDeletion } from "@/lib/relatedPropertyDeletion";
import { answeredUnlessAway, isServerUnreachable } from "@/lib/serverReach";
import { readGraphAhead } from "@/lib/graphReadAhead";
import { keepRowPage, rowPageInPlaceOf } from "@/lib/keptRowPage";
import { readOverMadeHere, rowPageWhileNotYetMade } from "@/lib/rowMadeHere";
import { sendRowValues } from "@/lib/rowValuesKept";
import { HANDED_PAGE_READ_AFTER_MS, rowPageHandedOver } from "@/lib/rowPageHandoff";
import { useReadAgainWhenAnswered } from "@/lib/unansweredRead";
import { holdTypedRowValue } from "@/lib/rowValuesTyped";
import { RowPageUnavailable } from "./RowPageUnavailable"; import { RowPageReading } from "./RowPageReading";
import { PageMoveKey, RowPeekAddTo } from "./RowPeekAddTo";
import type { MovePopoverAnchor } from "../movePopoverPlacement";
import { PageLayoutTabHolderContext } from "./pageLayoutTabEnvironment";

const standaloneSchemaReadOwner = Object.freeze({ request: () => false, retire: () => {} });

/** The properties a row page read carries, as the row page draws them. */
export function standaloneRowProperties(snapshot: DatabaseRowPageSnapshot): GraphProperty[] {
  return snapshot.properties.map((property, index) => ({
    id: property.id,
    dataSourceId: snapshot.row.dataSourceId,
    type: property.type as GraphProperty["type"],
    name: property.name,
    position: property.position || `p${String(index).padStart(4, "0")}`,
    config: property.config ?? {},
  }));
}

/** The row a row page read carries, as a database row. */
export function standaloneRow(snapshot: DatabaseRowPageSnapshot): GraphRow {
  return {
    id: snapshot.row.id,
    dataSourceId: snapshot.row.dataSourceId,
    subitemParentId: snapshot.row.subitemParentId,
    position: snapshot.row.position,
    icon: snapshot.row.icon,
    iconType: snapshot.row.iconType,
    archivedAt: snapshot.row.archivedAt || undefined,
    archivedBy: snapshot.row.archivedBy || undefined,
    createdAt: snapshot.row.createdAt || undefined,
    createdBy: snapshot.row.createdBy || undefined,
    updatedAt: snapshot.row.updatedAt || undefined,
    lastEditedBy: snapshot.row.lastEditedBy || undefined,
    values: snapshot.row.values as GraphRow["values"],
  };
}

/**
 * A database page opened on its own, for someone its database's page does
 * not open to: the row page's properties and body from the row page read,
 * with values and body editable when its share allows, its discussion, and
 * the actions that need no database. Without the row page, the fallback
 * shows. It may open in a peek over the page that is open, beside it or
 * over its center, as a sidebar database view opens its rows (Notion, seen
 * 2026-10-02); the open page's own database peeks its own rows.
 */
export function StandaloneRowPage({
  session,
  rowId,
  organizationId,
  fallback,
  onClose,
  onShown,
  peek,
  pageMenu,
  pageMove,
}: {
  session: Session;
  rowId: string;
  /** The organization whose people a person property picks from. */
  organizationId?: string | null;
  fallback: ReactNode;
  onClose: () => void;
  /** At its own address: the workspace it is in, its name and the page holding its database, which the page around it follows. */
  onShown?(shown: { workspaceId: string; title: string; inTrash: boolean; parentId: string }): void;
  peek?: {
    mode: "side" | "center";
    onOpenMode: (mode: DatabaseViewOpenMode) => void;
    /** The open page, whose database peeks the rows it holds. */
    openPageId?: string;
  };
  /**
   * The page's own "⋯" menu, for a page of the workspace that a database
   * holds — a wiki's page — which Notion gives the page menu (duplicate,
   * move, archive, trash, updates and analytics), not a row's; its updates are the bar's edit time's (`openUpdates`).
   */
  pageMenu?: (anchor: { x: number; y: number }, close: () => void,
    context: { canEdit: boolean; suggesting?: PageSuggestingControl; style?: RowPageMenuExtras["style"]; openUpdates?: () => void }) => ReactNode;
  /** With `pageMenu`, that menu's "옮기기", which Cmd/Ctrl+Shift+P opens under the "⋯" (PageMoveKey). */
  pageMove?: (anchor: MovePopoverAnchor | null, close: () => void) => ReactNode;
}) {
  const { t } = useTranslation(["rowMenu", "databaseView"]);
  const { notify } = useProductToast();
  const product = useProductPages();
  // Its reads last as long as the person and the row: their sign-in handed over or renewed reads on with the new one.
  const signIn = useHeldSignIn(session), readScope = useRef(`${session.user.id}/${rowId}`), mounted = useRef(true);
  readScope.current = `${session.user.id}/${rowId}`;
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const isCurrent = useCallback(() => mounted.current && readScope.current === `${session.user.id}/${rowId}`, [rowId, session.user.id]);
  // Its whole page left for another takes the notices of dates moved on it with it; its window closing leaves them their time.
  useEffect(() => () => dependencyShiftNoticesLeave(rowId, !!peek), [peek, rowId]);
  // A row opened whole from its peek draws at once from it (rowPageHandoff.ts).
  const [snapshot, setSnapshot] = useState<DatabaseRowPageSnapshot | null>(() => rowPageHandedOver(rowId));
  useRowBodyEdits((rowId) => setSnapshot((current) => current?.row.id === rowId
    ? { ...current, row: { ...current.row, updatedAt: new Date().toISOString(), lastEditedBy: session.user.id } } : current));
  const [missing, setMissing] = useState(false);
  // The server not there to read it, or known lost while its read stalls (answeredUnlessAway), is no missing
  // page: it says so, as a page does, and is read again once the server may answer (unansweredRead.ts).
  const [unanswered, setUnanswered] = useState(false);
  const [comments, setComments] = useState<{ detail?: OpenCommentsDetail } | null>(null);
  const [actions, setActions] = useState<{
    anchor: { x: number; y: number }; extras: RowPageMenuExtras; lock?: DatabaseRowPageLockControl;
  } | null>(null);
  // The row page read is a visit, as a page's is.
  usePageVisitRecorder(session, snapshot?.row.id === rowId ? rowId : null);
  const load = useCallback((signal?: AbortSignal) => answeredUnlessAway(readDatabaseRowPage(signIn(), rowId, { signal }))
    .then((next) => {
      if (signal?.aborted || !isCurrent()) return false;
      setSnapshot((current) => readOverMadeHere(current, next));
      setUnanswered(false);
      keepRowPage(signIn(), next);
      return true;
    })
    .catch(async (error: unknown) => {
      if (signal?.aborted || !isCurrent()) return false;
      // Without the server it shows as kept here, if it was (keptRowPage.ts).
      const kept = await rowPageInPlaceOf(signIn(), rowId, error);
      if (signal?.aborted || !isCurrent()) return false;
      if (isServerUnreachable(error)) {
        setUnanswered(true);
        if (kept) setSnapshot((current) => current ?? kept);
      } else setMissing(true);
      return false;
    }), [isCurrent, rowId, signIn]);
  useReadAgainWhenAnswered(unanswered, () => void load());
  useEffect(() => {
    const controller = new AbortController();
    setMissing(false);
    setUnanswered(false);
    // A row made here the server doesn't have yet opens as made here at once;
    // its read goes once the server has it (rowMadeHere.ts). One its peek
    // handed over is read a moment later, once what the peek wrote as it
    // closed — a title typed — has gone: read at once, the server's title
    // from before showed for a moment (rowPageHandoff.ts).
    const handed = rowPageHandedOver(rowId, true);
    const madeHere = rowPageWhileNotYetMade(rowId);
    setSnapshot(madeHere ?? handed);
    if (!handed) {
      // Its body is read beside it, not once it came (graphReadAhead.ts).
      if (!madeHere) readGraphAhead(signIn(), rowId);
      void load(controller.signal);
      return () => controller.abort();
    }
    const wait = window.setTimeout(() => void load(controller.signal), HANDED_PAGE_READ_AFTER_MS);
    return () => { window.clearTimeout(wait); controller.abort(); };
  }, [load, rowId, signIn]);
  // A template's page in a peek opens over its database, as its window does (useTemplateAddress); at its own address, whole (below).
  useEffect(() => {
    if (snapshot?.template && peek) replaceRoute(pageHref(snapshot.row.containerPageId, new URLSearchParams({ [TEMPLATE_PAGE_PARAM]: snapshot.row.id })));
  }, [peek, snapshot]);
  // A value shows as soon as it is set and its write follows the ones before it in the outbox's turn;
  // the row is read again once none is on its way, so a read between two writes doesn't put an older
  // value back under the next edit.
  const writes = useRef({ pending: 0 });
  const latestSnapshot = useRef(snapshot);
  latestSnapshot.current = snapshot;
  // Each value saved is a step of the page's ⌘Z, as from its database's table.
  const recordValueStep = useRowPageValueSteps({
    session, rowId, workspaceId: snapshot?.workspaceId, shown: () => isCurrent() ? latestSnapshot.current : null,
    changedSince: () => notify(t("databaseView:status.undoChanged")),
  });
  const writeRowTitleAsRead = useRef<((property: { id: string; type: string }, value: string, spans: TextSpan[] | undefined, read: TitleValue | null) => Promise<unknown>) | null>(null);
  // Its values or what the person may do there changed elsewhere: the row is read again (useRowPageReadAgain).
  useRowPageReadAgain(rowId, () => void load(), () => writes.current.pending > 0);
  const buttonRuns = useRef(new Map<string, string>());
  // A comment's address opens its discussion once the row's page is there.
  useCommentLinkOpen(snapshot ? databaseRowCommentServiceFor(session, api) : null, rowId);

  const properties = useMemo(() => snapshot ? standaloneRowProperties(snapshot) : [], [snapshot]);
  const valueRow = useMemo(() => {
    if (!snapshot) return null;
    const titleProperty = properties.find((property) => property.type === "title");
    return galleryPresentationPage({ row: standaloneRow(snapshot), titleProperty, workspaceId: snapshot.workspaceId, position: 0 });
  }, [properties, snapshot]);
  const shownTo = useRef(onShown);
  shownTo.current = onShown;
  // At its own address the row names the window, as a page does.
  const rowFavicon = usePageFaviconHref(valueRow ? { icon: valueRow.icon ?? "", iconType: valueRow.iconType, workspaceId: snapshot?.workspaceId } : null);
  useEffect(() => {
    if (peek || !snapshot || snapshot.template || !valueRow) return;
    // Its mentions as a plain title says them, in the window's title and the screen's name (closedMentions.ts).
    const title = titleWithClosedMentions(valueRow.title, valueRow.titleSpans) || rowPageUntitledName(snapshot);
    setDocumentChrome({ title, iconHref: rowFavicon, pageId: rowId });
    shownTo.current?.({ workspaceId: snapshot.workspaceId, title, inTrash: Boolean(snapshot.row.trashedAt), parentId: snapshot.row.containerPageId });
  }, [peek, rowFavicon, rowId, snapshot, valueRow]);
  const relationRows = useStandaloneRelationRows(session, snapshot?.row.containerPageId, snapshot?.workspaceId);
  // One read of the row's values for its page and for what shows it elsewhere
  // (the page layout builder's preview): its related databases' pages and
  // properties with it, so a rollup reads the same in both.
  const propertyValueContext = useMemo<PropertyValueReadContext | null>(() => snapshot ? {
    bounds: nativeBoardQueryBounds.formula,
    contextIdentity: `standalone-row:${snapshot.row.id}`,
    // Away from its database a row's relations are named by its own read.
    hydratedRelationTargetIds: addRelationTargetIds(snapshot.row.values,
      new Set(properties.filter(({ type }) => type === "relation").map(({ id }) => id)), new Set(relationRows.hydratedIds)),
    pagesById: {
      ...Object.fromEntries((snapshot.relatedPages ?? []).map((page) => [page.id, {
        ...page, inTrash: false, iconType: page.iconType || undefined,
      } as unknown as Page])),
      ...relationRows.pagesById,
    },
    propsByDatabaseId: { ...relationRows.propertiesByDataSourceId, [snapshot.row.dataSourceId]: properties.map(galleryPresentationProperty) },
    schemaReadOwner: standaloneSchemaReadOwner,
    snapshotIdentity: snapshot.snapshotIdentity,
    userId: session.user.id,
  } : null, [properties, relationRows.hydratedIds, relationRows.pagesById, relationRows.propertiesByDataSourceId, session.user.id, snapshot]);

  // The page's other properties save as their cells do, through the row
  // page's values; a person picks from the organization's people. An option
  // made in a select cell goes with the value that picks it.
  const propertyCellEnvironment = useMemo<PropertyCellEnvironment | undefined>(() => {
    if (!snapshot || !propertyValueContext) return undefined;
    const newOptions = new Map<string, DatabaseRowPageNewOption[]>();
    // A value is written as set, or as a step of ⌘Z takes it back or makes it again; given the value it was typed on, over that,
    // merged with another saved since; it waits in the outbox behind the row's making and the page's other changes, read again once
    // it went (rowValuesKept.ts). A refused write takes its step out, a lock's said as such; one typed goes as rowValuesTyped.ts says.
    const send = (propertyId: string, written: DatabaseRowPageValue, options?: DatabaseRowPageNewOption[], read?: DatabaseRowPageValue, withdraw?: () => void, typed = false): Promise<boolean> => {
      setSnapshot((current) => current?.row.id === snapshot.row.id ? { ...current, row: { ...current.row, values: { ...current.row.values, [propertyId]: written } } } : current);
      if (typed) return holdTypedRowValue({ rowId: snapshot.row.id, propertyId, value: written, hostPageId: snapshot.row.containerPageId, before: latestSnapshot.current?.row.values[propertyId] },
        (base) => send(propertyId, written, options, read ?? base ?? undefined)).then((saved) => { if (!saved) withdraw?.(); return saved; });
      const queue = writes.current;
      queue.pending += 1;
      const sent = sendRowValues(session, snapshot.row.id, {
        values: { [propertyId]: written },
        ...(options ? { options: { [propertyId]: options } } : {}),
        ...(read ? { expected: { [propertyId]: read } } : {}),
      }, snapshot.row.containerPageId).then((response) => {
        if (!response) return true;
        recordRowDateCommand({ session, snapshot, response, product, isCurrent, reload: load,
          failed: () => notify(t("databaseView:toast.couldntSaveChanges"), "error") });
        // The server moved dependent tasks' dates with this one, or with a
        // link it drew, said so with a step back as a table's cell does — this page's, while it is shown
        // (sayDependencyShift) — and left those this person may not change, which the notice adds.
        const command = response.dependencyShifts ? response.dateCommand : undefined;
        if (response.dependencyShifts || response.dependencyShiftsDenied) sayDependencyShift({ notify, pageId: snapshot.row.id, count: response.dependencyShifts ?? 0, tasks: response.dependencyShiftTasks, denied: response.dependencyShiftsDenied,
          step: command ? { key: `${response.containerPageId}/${command}`, shown: isCurrent, back: () => writeDatabaseRowPageValues(session, snapshot.row.id, { mutationId: newId(), values: {}, restoreDateCommand: command })
            .then(() => undefined, () => notify(t("databaseView:toast.couldntSaveChanges"), "error")).finally(() => load()) } : undefined });
        return true;
      }).catch((error) => {
        withdraw?.();
        if (read && isRowValueChanged(error)) throw error;
        notify(productLockRefusalReason(error) || t("databaseView:toast.couldntSaveChanges"), "error");
        return false;
      });
      return sent.finally(() => {
        queue.pending -= 1;
        if (queue.pending === 0) void load();
      });
    };
    // A value set is a step of the page's ⌘Z as it shows (standaloneRowValueSteps.ts).
    const save = (propertyId: string, type: string, value: unknown, spans?: unknown[], read?: DatabaseRowPageValue, typed = false) => {
      const options = newOptions.get(propertyId);
      newOptions.delete(propertyId);
      const written: DatabaseRowPageValue = {
        type, value: type === "files" ? graphFilePropertyValue(value) : value,
        ...(spans?.length ? { spans: spans as Array<{ text: string } & Record<string, unknown>> } : {}),
      };
      const shown = latestSnapshot.current?.row.id === snapshot.row.id ? latestSnapshot.current : snapshot;
      const withdraw = recordValueStep({
        propertyId, type, before: shown.row.values[propertyId], after: written,
        write: (step) => send(propertyId, step).then(Boolean),
      });
      return send(propertyId, written, options, read, withdraw, typed).then(() => undefined);
    };
    writeRowTitleAsRead.current = (property, value, spans, read) => save(property.id, property.type, value, spans, read ? { type: property.type, value: read.title, ...(read.spans?.length ? { spans: read.spans.map((span) => ({ ...span })) } : {}) } : undefined);
    return {
      snapshot: {
        fileDropBounds: FILE_DROP_BOUNDS,
        formulaBounds: nativeBoardQueryBounds.formula,
        userId: session.user.id,
        organization: organizationId ? { id: organizationId, name: "" } : null,
        organizationProfiles: [],
        pagesById: propertyValueContext.pagesById,
        propertiesByDataSourceId: { ...relationRows.propertiesByDataSourceId, [snapshot.row.dataSourceId]: properties.map(galleryPresentationProperty) },
        hydratedRelationTargetIds: propertyValueContext.hydratedRelationTargetIds,
        loadedDataSourceIds: new Set([snapshot.row.dataSourceId, ...relationRows.dataSourceIds]),
        relationPagesByDataSourceId: {},
      },
      context: {
        workspaceId: snapshot.workspaceId,
        pageId: snapshot.row.id,
        dataSourceId: snapshot.row.dataSourceId,
        sessionIdentity: `standalone-row:${snapshot.row.id}`,
        generation: 0,
        currentGeneration: () => 0,
      },
      operations: {
        setValue: (_rowId, property, value) => save(property.id, property.type, value),
        setTextValue: (_rowId, property, value, _context, spans) => save(property.id, property.type, value, spans, undefined, true),
        // Whoever may change its database renames, recolors, removes and orders a select's options here as from its view, as
        // Notion's row page does (seen 2026-11-12); someone else only makes new ones, and no one while its database's own lock
        // holds it, for whoever reads it (databaseStructureLocked, which the cells read below as inside its view; 423 then).
        addOptionsOnly: !snapshot.databaseWritable,
        relationShownFixed: !snapshot.databaseWritable,
        updateProperty: (propertyId, patch) => {
          // What a relation's picker shows is kept with the relation, as from its view.
          const shown = (patch.config as { relationShownProperties?: string[] } | undefined)?.relationShownProperties;
          const relation = properties.find((property) => property.id === propertyId);
          if (shown && relation?.type === "relation" && snapshot.databaseWritable) {
            void sendContentGraphChange(session, snapshot.row.containerPageId, [{
              kind: "upsertProperty", property: { ...relation, config: { ...relation.config, relationShownProperties: shown } },
            }]).then((sent) => { if (sent !== "kept") void load(); }, () => {
              notify(t("databaseView:toast.couldntSaveChanges"), "error");
              void load();
            });
            return;
          }
          const listed = (config: unknown) => {
            const options = config && typeof config === "object" ? (config as { options?: unknown }).options : undefined;
            return Array.isArray(options) ? options as Array<{ id: string; name: string; color?: string; group?: string }> : [];
          };
          const current = properties.find((property) => property.id === propertyId);
          const known = new Set(listed(current?.config).map((option) => option.id));
          const added = listed(patch.config).filter((option) => !known.has(option.id));
          if (added.length > 0) {
            newOptions.set(propertyId, added.map(({ id, name, color, group }) => ({
              id, name, color: color ?? "default", ...(group ? { group } : {}),
            })));
            return;
          }
          // Its options changed otherwise change the property, shown at once.
          if (!current || !patch.config || !snapshot.databaseWritable) return;
          const changes: GraphOperation[] = [{ kind: "upsertProperty", property: { ...current, config: patch.config as Record<string, unknown> } }];
          setSnapshot((shown) => shown && withPropertyChanges(shown, changes));
          void sendContentGraphChange(session, snapshot.row.containerPageId, changes).then((sent) => { if (sent !== "kept") void load(); }, () => {
            notify(t("databaseView:toast.couldntSaveChanges"), "error");
            void load();
          });
        },
        // Its workspace's people, as from its database's cells.
        searchPeople: (input, options) => api.searchOrganizationPeople(session, { ...input, workspaceId: snapshot.workspaceId, invitees: true }, options),
        // A place is found by its name through the provider the server's administrator set, as from its database's cells.
        searchPlaces: (query, signal) => api.searchPlaces(session, snapshot.row.containerPageId, query, signal),
        // A file goes up to the row's workspace, as from its database's cells.
        uploadFile: async (file, _row, property, onProgress, options) => {
          const uploaded = await uploadWorkspaceFile(file, "database/files", {
            workspaceId: snapshot.workspaceId, pageId: snapshot.row.containerPageId,
            databaseId: snapshot.row.dataSourceId, propertyId: property.id,
          }, { onProgress, signal: options.signal });
          return {
            id: uploaded.fileId, fileId: uploaded.fileId, name: uploaded.name, url: uploaded.url,
            type: uploaded.type, contentType: uploaded.type, size: uploaded.size, sha256: uploaded.sha256,
          };
        },
        // A button's run keeps its id while its question waits; done, the row is read again.
        runButton: async (_rowId, propertyId, confirmationToken, options) => {
          const executionId = buttonRuns.current.get(propertyId) ?? newId();
          buttonRuns.current.set(propertyId, executionId);
          try {
          const response = await api.executeDatabaseButton(session, {
            pageId: snapshot.row.containerPageId, dataSourceId: snapshot.row.dataSourceId, rowId: snapshot.row.id, propertyId,
          }, { executionId, ...(confirmationToken ? { confirmationToken } : {}), ...(options.cancel ? { cancel: true } : {}) }, options.signal);
          if (!response.confirmation) buttonRuns.current.delete(propertyId);
          void load();
          return databaseButtonResultFromWire(response, executionId);
          } catch (error) {
            if (buttonExecutionCanRestart(error)) buttonRuns.current.delete(propertyId);
            throw error;
          }
        },
        undoButton: async (_rowId, propertyId, executionId) => {
          await api.executeDatabaseButton(session, {
            pageId: snapshot.row.containerPageId, dataSourceId: snapshot.row.dataSourceId, rowId: snapshot.row.id, propertyId,
          }, { executionId, undo: true });
          void load();
        },
        discardButton: (_rowId, propertyId) => { buttonRuns.current.delete(propertyId); },
        // Away from its database a rollup has only this read to go by: without it it stays "…". A
        // relation shown asks for its database alone, which a page section's "새 페이지 추가하기" makes into.
        loadRelation: (_dataSourceId, options) => { if (options.rows || options.values) relationRows.load(); else relationRows.loadSources(); },
        setRelation: (_rowId, property, relatedRowIds) => save(property.id, property.type, relatedRowIds),
        createRelatedRow: (dataSourceId) => relationRows.createRow(dataSourceId).catch(() => {
          notify(t("databaseView:toast.couldntSaveChanges"), "error");
          return null;
        }),
        renameRelatedRow: (rowId, title) => relationRows.renameRow(rowId, title).catch(() => notify(t("databaseView:toast.couldntSaveChanges"), "error")),
        discardRelatedRow: (rowId) => relationRows.discardRow(rowId).catch(() => notify(t("databaseView:toast.couldntSaveChanges"), "error")),
        notify: (message, kind) => notify(message, kind === "info" ? "default" : kind),
        getCurrentFiles: (_rowId, propertyId) => normalizeFileAttachments(snapshot.row.values[propertyId]?.value),
      },
    };
  }, [isCurrent, load, notify, organizationId, product, properties, propertyValueContext, recordValueStep, relationRows, session, snapshot, t]);

  // Who may change the database's properties does so here as from its view,
  // through the page that holds the database (Notion's list is the same
  // however the page opens, seen 2026-10-01); the page reads them again. A
  // property made or deleted is a step of the row page's ⌘Z, as Notion's.
  const pathDatabase = useRowPathDatabase(session, snapshot?.row.containerPageId ?? "", snapshot?.row.dataSourceId ?? "", inlineDatabasePlaceholderTitle());
  const database = snapshot ? product?.pages.find((page) => page.id === snapshot.row.containerPageId) : undefined;
  // Its database is named as its path names it, or as its read names the
  // source once read, as a view's row page names it (2026-11-11: the question
  // to delete a property names the database).
  const relatedPages: Record<string, Page | undefined> = relationRows.pagesById;
  const sourceName = (snapshot && relatedPages[snapshot.row.dataSourceId]?.title) || pathDatabase?.title || database?.title || "";
  const propertyEditing = useMemo<DatabasePropertyEditing | undefined>(() => {
    if (!snapshot?.databaseWritable || snapshot.row.trashedAt) return undefined;
    const failed = () => notify(t("databaseView:toast.couldntSaveChanges"), "error");
    const holderPageId = snapshot.row.containerPageId;
    // A change shows at once, as from its view, so the next one made builds
    // on it, and its write follows the ones before it; the row is read again
    // once none is on its way (a read between two writes would put an older
    // setting back under the next), or at once on a refusal. Kept for a
    // server out of reach, it shows as made, which can't be read again meanwhile.
    const write = (operations: GraphOperation[]) => {
      setSnapshot((current) => current && withPropertyChanges(current, operations));
      const queue = writes.current;
      queue.pending += 1;
      let reread = true;
      return sendContentGraphChange(session, holderPageId, operations).then((sent) => { reread = sent !== "kept"; }).finally(() => {
        queue.pending -= 1;
        if (reread && queue.pending === 0) void load();
      });
    };
    const taken = (operations: GraphOperation[]) => notePropertySteps({ session, rowId, holderPageId, operations, known: properties, write, reload: load });
    const persist = (operations: GraphOperation[]) => write(operations).then(() => taken(operations), failed);
    // Who reads its properties is changed here as from its database, by whom may (propertyAccessManager).
    const source = { id: snapshot.row.dataSourceId, title: sourceName, pageId: snapshot.row.containerPageId, manageAccess: Boolean(snapshot.manageAccess) };
    const contextIdentity = `standalone-row:${snapshot.row.id}`;
    return {
      contextIdentity, dataSource: source, dataSources: [source],
      properties, snapshotIdentity: snapshot.snapshotIdentity, writable: true,
      // The databases its relations reach, read as a property's menu opens, name them there.
      pagesById: relationRows.pagesById as Record<string, Page>, readRelated: relationRows.load,
      propertiesByDataSourceId: relationRows.propertiesByDataSourceId,
      onCommitProperties: (next) => persist(next.map((property) => ({ kind: "upsertProperty", property }))),
      onDeleteProperty: (propertyId) => persist([{ kind: "deleteProperty", propertyDeletion: propertyDeletion(propertyId) }]),
      // A relation's database is picked from those found by name.
      relationCreation: {
        snapshot: { databases: [], snapshotIdentity: snapshot.snapshotIdentity },
        context: {
          bounds: RELATION_CREATION_BOUNDS, identity: contextIdentity, notifyFailure: failed,
          searchDatabases: createRelationDatabaseSearch(session, snapshot.workspaceId),
          commit: async (command) => {
            const property = {
              ...newRowPageProperty("relation", snapshot.row.dataSourceId, properties),
              name: command.name, config: command.propertyConfig,
            };
            const operations: GraphOperation[] = [{ kind: "upsertProperty", property }];
            await write(operations);
            taken(operations);
            return property;
          },
        },
      },
    };
  }, [load, notify, properties, relationRows.load, relationRows.pagesById, relationRows.propertiesByDataSourceId, rowId, session, snapshot, sourceName, t]);

  const notifications = useMemo<DatabaseRowNotifications>(() => ({
    read: async (signal) => (await api.readDatabaseRowNotificationPreference(session, rowId, signal)).level,
    save: async (level) => (await api.saveDatabaseRowNotificationPreference(session, rowId, level)).level,
  }), [rowId, session]);

  // Its icon, cover and style, the style shown at the press; its own head, menu and a wiki's page menu save here (rowPageStyleSave.ts).
  const changePresentation = rowPagePresentationSaver({ session, rowId, shown: () => latestSnapshot.current, show: setSnapshot, load: () => load() });
  const rowMenu = useStandaloneRowMenu({
    session, rowId, snapshot, rowTitle: valueRow?.title ?? "", readOnly: !(snapshot?.accessRole === "edit" || snapshot?.accessRole === "full_access") || Boolean(snapshot?.row.trashedAt),
    notifications, publish: setSnapshot, onFavorite: changeFavorite,
    onLeave: () => { if (snapshot) pushRoute(pageHref(snapshot.row.containerPageId)); else onClose(); }, docked: !peek,
  });
  const sidePanel = useRowPageDockedPanel({ rowId, whole: !peek && !snapshot?.template, read: Boolean(snapshot), keep: true, updates: { open: rowMenu.analyticsShown, show: () => rowMenu.openRowUpdates?.(rowId), close: rowMenu.closeAnalytics },
    all: { open: Boolean(comments?.detail?.all), show: () => setComments({ detail: { pageId: rowId, all: true } }), close: () => setComments((open) => open?.detail?.all ? null : open) } }), analyticsDocked = sidePanel.docked;

  async function copyLink() {
    setActions(null);
    if (await copyText(serverUrl(pageHref(rowId)))) notify(t("toast.copiedLink"), "success");
    else notify(t("toast.couldntCopyLink"), "error");
  }

  // The lock's switch shows it, unlocking for oneself at the press (lockForMeShownFirst.ts); only a failure says so, as Notion's (seen 2026-10-31).
  async function changeLock(change: () => Promise<unknown>, failed: string) {
    try {
      await change();
    } catch {
      notify(failed, "error");
    }
    await load();
  }

  async function changeFavorite(favorite: boolean) {
    if (!snapshot) return;
    try {
      await setDatabaseRowPageFavorite(session, rowId, { favorite, expectedAuthorityRevision: snapshot.row.authorityRevision });
    } catch {
      notify(t("toast.couldntUpdateFavorites"), "error");
    }
    await load();
  }

  // Opened on its own, the page lays out its database's pages itself; under
  // its database's view, the view does.
  const ownLayout = !missing && !(peek?.openPageId && snapshot?.row.containerPageId === peek.openPageId);
  // The builder is named as the page's path names its database (Notion's, seen 2026-11-06: "스프린트").
  const layoutBuilder = useDatabasePageLayoutBuilder(ownLayout && snapshot && valueRow && propertyValueContext && propertyEditing ? {
    session, pageId: snapshot.row.containerPageId, dataSourceId: snapshot.row.dataSourceId, title: pathDatabase?.title ?? database?.title ?? "",
    resolveTitle: !pathDatabase && database?.kind === "database"
      ? () => severalSourcesRowSourceTitle(session, snapshot.row.containerPageId, snapshot.row.dataSourceId) : undefined,
    properties, propertyValueContext, editing: propertyEditing,
    // Its database's pages, read as the builder opens; this page alone until then.
    previewRows: () => [valueRow, ...(Object.values(relationRows.pagesById) as Page[]).filter((page) => page.kind === "page"
      && page.parentId === snapshot.row.dataSourceId && page.id !== valueRow.id && !page.archivedAt)],
    onOpen: relationRows.load,
  } : null);
  // Its layout's tabs read their sources from its database's page, as a view of it there shows them.
  const containerPageId = snapshot?.row.containerPageId, snapshotWorkspaceId = snapshot?.workspaceId;
  const tabHolder = useMemo(() => containerPageId && snapshotWorkspaceId ? { session, pageId: containerPageId, workspaceId: snapshotWorkspaceId } : null,
    [containerPageId, session, snapshotWorkspaceId]);
  if (missing || (peek?.openPageId && snapshot?.row.containerPageId === peek.openPageId)) return <>{fallback}</>;
  if (unanswered && !snapshot) return <RowPageUnavailable mode={peek?.mode ?? "full"} onClose={onClose} title={product?.pages.find((page) => page.id === rowId)?.title} />;
  // One of its body's databases opened alone shows alone (RowPageDatabaseAlone); a template's page in a peek is on its way to its database's page.
  const aloneBlockId = peek ? null : rowPageDatabaseOpenedAlone(rowId);
  if (!snapshot || (snapshot.template && peek) || !valueRow || !propertyValueContext) return !snapshot && !peek ? <RowPageReading rowId={rowId} /> : <div aria-busy="true" data-standalone-row-loading />;
  const canWrite = snapshot.accessRole === "edit" || snapshot.accessRole === "full_access", editable = canWrite && !snapshot.row.trashedAt;
  // Its database's templates, as the database's toolbar keeps them, where no view of it shows; a locked page holding it keeps them.
  const templatesWritable = editable && snapshot.databaseWritable !== false && !snapshot.holderLocked;
  // The page's lock, as its … menu (open across presses, so at the page's latest revision) and its bar's "Locked" change it.
  const lock = editable ? {
    locked: snapshot.row.locked, unlockedForMe: snapshot.row.unlockedForMe,
    onLock: (locked: boolean) => changeLock(() => setDatabaseRowPageLock(session, rowId, {
      locked, expectedAuthorityRevision: (latestSnapshot.current ?? snapshot).row.authorityRevision,
    }), t("toast.couldntChangePageLock")),
    onLockForMe: (unlocked: boolean) => void changeLock(() => lockForMeShownFirstIn(setSnapshot, rowId, unlocked, () => setDatabaseRowPageLockForMe(session, rowId, { unlocked })), t("toast.couldntChangeLockForMe")),
  } : undefined;
  // The database its template band names, not the page holding an inline one.
  const band = templateBandDatabase(database, pathDatabase ?? undefined, snapshot.workspaceId), templateProps = { session, pageId: snapshot.row.containerPageId, dataSourceId: snapshot.row.dataSourceId,
    ...band, properties, propertyCells: { environment: propertyCellEnvironment, valueContext: propertyValueContext }, workspaceId: snapshot.workspaceId, onCommitProperties: propertyEditing?.onCommitProperties };
  // A template at its own address opens whole as its database's templates open it (StandaloneRowTemplates), its layout builder over it, leaving for its database once gone.
  if (snapshot.template && !aloneBlockId) return <><StandaloneRowTemplates key={rowId} {...templateProps} whole={rowId} writable={canWrite && snapshot.databaseWritable !== false && !snapshot.holderLocked}
    pathDatabase={pathDatabase ?? undefined} onLeave={() => pushRoute(pathDatabase?.href ?? pageHref(snapshot.row.containerPageId))} />{layoutBuilder}</>;
  const templates = templatesWritable ? <StandaloneRowTemplates {...templateProps} /> : null;
  if (aloneBlockId) {
    return <RowPageDatabaseAlone key={aloneBlockId} blockId={aloneBlockId} session={session} snapshot={snapshot} readOnly={!editable}
      rowTitle={titleWithClosedMentions(valueRow.title, valueRow.titleSpans) || rowPageUntitledName(snapshot)}
      pathDatabase={pathDatabase ?? undefined} onReturn={onClose}
      template={snapshot.template ? { ...band, viewing: !canWrite } : undefined}>
      {templates}
    </RowPageDatabaseAlone>;
  }
  // Its values' words take discussions as the table's and a peek's do (Notion's, seen 2026-11-12).
  return (
    <CellCommentsProvider session={session} containerPageId={snapshot.row.containerPageId} workspaceId={snapshot.workspaceId}
      canEditPage={canWrite} canComment={snapshot.accessRole !== "view" && !snapshot.row.trashedAt} rowId={snapshot.row.id}>
    <DatabaseTaskRowsContext.Provider value={snapshot.taskDatabase === true}>
    <PageLayoutTabHolderContext.Provider value={tabHolder}><DatabaseStructureLockContext.Provider value={snapshot.databaseStructureLocked === true}>
    <div data-standalone-row={snapshot.row.id}>
      <DatabaseRowPage mode={peek?.mode ?? "full"} properties={properties} snapshot={snapshot} pathDatabase={pathDatabase}
        sidePanelShown={analyticsDocked}
        propertyValueContext={propertyValueContext}
        valueRow={valueRow} editableRow={editable ? valueRow : undefined}
        // A text value typed in saves as the page's other values do, one write
        // after another and read again once they are all through: written each
        // at once and read back after each, a write landing late put back
        // what was typed before it (2026-11-07: "12" typed back to "12xyz").
        onRichTextChange={editable && propertyCellEnvironment ? (property, value, spans, read) => {
          const cellProperty = propertyCellEnvironment.snapshot.propertiesByDataSourceId[snapshot.row.dataSourceId]
            ?.find((candidate) => candidate.id === property.id);
          // A title goes over the one it was typed on, as it is typed (rowValuesKept.ts).
          if (cellProperty && (read || cellProperty.type === "title")) return writeRowTitleAsRead.current?.(cellProperty, value, spans, read ?? null);
          if (cellProperty) void propertyCellEnvironment.operations.setTextValue?.(snapshot.row.id, cellProperty, value, propertyCellEnvironment.context, spans);
        } : undefined}
        propertyCellEnvironment={editable ? propertyCellEnvironment : undefined}
        propertyEditing={propertyEditing}
        onAddProperty={propertyEditing ? (type: PropertyType, name?: string, icon?: string) => {
          const property = newRowPageProperty(type, snapshot.row.dataSourceId, properties, name, icon);
          void propertyEditing.onCommitProperties([property]);
          return property.id;
        } : undefined}
        onPresentationChange={editable ? changePresentation : undefined}
        onClose={onClose} onOpenMode={(mode) => peek?.onOpenMode(mode)}
        onOpenComments={(detail) => setComments({ detail })}
        onHistoryRestored={() => void load()}
        onRestore={snapshot.row.trashedAt && canWrite ? rowMenu.restore : undefined}
        onDeleteForever={snapshot.row.trashedAt && canWrite ? rowMenu.deleteForever : undefined}
        onOpenActions={(trigger, extras) => {
          if (pageMenu) setActions({ anchor: rowPageActionsAnchor(trigger), extras, lock });
          else rowMenu.openRowPageActions(snapshot.row.id, trigger, extras, lock);
        }}
        currentUserId={session.user.id} session={session} workspaceId={snapshot.workspaceId}
        bodyReadOnly={!editable}
        onCreateTemplate={templatesWritable ? () => void requestDatabaseTemplate(snapshot.row.dataSourceId) : undefined}
        topBar={{
          onCopyLink: () => void copyLink(), onFavorite: (favorite) => void changeFavorite(favorite), lock, ...rowMenu.topBarUpdates(snapshot.row.id, analyticsDocked), onClosePanel: sidePanel.onClosePanel, discussionsDocked: sidePanel.discussionsDocked,
          ...(peek ? { onChangeMode: peek.onOpenMode } : {}),
        }} />
      {comments ? (
        <CommentsPanel key={`${comments.detail?.all ? "all" : comments.detail?.blockId ?? ""}:${comments.detail?.activeCommentId ?? ""}:${comments.detail?.compose ? "new" : ""}`}
          pageId={snapshot.row.containerPageId} rowId={snapshot.row.id} blockId={comments.detail?.blockId} compose={comments.detail?.compose}
          all={comments.detail?.all} docked={sidePanel.discussionsDocked} workspaceId={snapshot.workspaceId}
          activeCommentId={comments.detail?.activeCommentId} initialQuote={comments.detail?.initialQuote}
          threadIds={comments.detail?.threadIds} initialQuoteStart={comments.detail?.initialQuoteStart} initialQuoteEnd={comments.detail?.initialQuoteEnd}
          initialQuotePlaces={comments.detail?.initialQuotePlaces}
          organizationId={organizationId ?? undefined} session={session}
          canEditPage={snapshot.accessRole === "edit" || snapshot.accessRole === "full_access"}
          canComment={snapshot.accessRole !== "view" && !snapshot.row.trashedAt} onClose={() => setComments(null)}
          onStatus={(message) => notify(message, "error")} />
      ) : null}
      {actions && pageMenu ? pageMenu(actions.anchor, () => setActions(null), { canEdit: editable, suggesting: actions.extras.suggesting,
        openUpdates: rowMenu.topBarUpdates(snapshot.row.id, analyticsDocked).onOpenUpdates, style: editable ? { value: rowPageStyle(snapshot.row), onChange: (change) => void changePresentation(change) } : undefined }) : null}
      {rowMenu.rowPageActionsMenu}
      {rowMenu.rowContextMenu}
      {/* On its own page ⌘⇧P moves it as its "⋯ › 옮기기" does; a peek's bar holds its own (DatabaseRowPage). */}
      {!peek && editable && !pageMenu ? <RowPeekAddTo snapshot={snapshot} session={session} addTo={false} onPage
        title={titleWithClosedMentions(valueRow.title, valueRow.titleSpans) || rowPageUntitledName(snapshot)} /> : null}
      {!peek && editable && pageMenu && pageMove ? <PageMoveKey move={pageMove} locked={snapshot.row.locked && !snapshot.row.unlockedForMe} /> : null}
      {layoutBuilder}
      {templates}
    </div>
    </DatabaseStructureLockContext.Provider></PageLayoutTabHolderContext.Provider>
    </DatabaseTaskRowsContext.Provider>
    </CellCommentsProvider>
  );
}
