import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { serverUrl } from "@/lib/serverOrigin";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import { createPageAnalyticsPanelService } from "@/lib/pageAnalyticsPanelService";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { newId } from "@/lib/ids";
import { restoreDatabaseRowPage } from "@/databaseRowPageApi";
import { confirmAction } from "../ConfirmActionDialog";
import { copyText } from "@/lib/copyText";
import { isLegalHoldPermanentDeleteError } from "@/lib/nativeTrashLifecycle";
import { useProductToast } from "@/lib/productToast";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsGone, holdRowsShown, rowsDrawn, rowsHolderPage, withRowsArchived, withRowsAsDrawn, writeRowPage } from "@/lib/rowsShownAtPress";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import type { Session } from "@/types";
import type { DatabaseRowMoveTarget, DatabaseRowNotifications } from "./DatabaseRowMenu";
import { collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { readUntilServerAnswers } from "@/lib/serverReach";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";
import type { RowPathDatabase } from "./RowPagePath";
import type { TextSpan } from "@/lib/types";
import { useProductPages } from "@/components/product/productPagesContext";
import { useRowContextMenu } from "./useRowContextMenu";

/**
 * The inline database a page opened at its own address is in, as a piece of
 * its path, as Notion's (seen 2026-11-05). A full-page database is its page's
 * own, and its page is that piece: its rows' path names it once, as Notion's
 * (2026-11-05: "T / T / 행").
 */
export function useRowPathDatabase(session: Session, pageId: string, dataSourceId: string, fallbackTitle: string) {
  const [database, setDatabase] = useState<RowPathDatabase>();
  const databasePage = useProductPages()?.pages.find(({ id }) => id === pageId)?.kind === "database";
  useEffect(() => {
    setDatabase(undefined);
    if (!pageId || !dataSourceId || databasePage) return;
    let live = true;
    // Its block and source, without the database's rows; read while the server was out of reach, read again once it may answer.
    const stop = readUntilServerAnswers(() => collectContentGraphStructure(session, pageId, () => live), (read) => {
      const block = read?.graph.blocks.find(({ type, content }) => type === "inline_database" && content.dataSourceId === dataSourceId);
      const source = read?.graph.dataSources.find(({ id }) => id === dataSourceId);
      if (!live || !block || !source) return;
      const viewId = typeof block.content.viewId === "string" ? block.content.viewId : undefined;
      setDatabase({ blockId: block.id, title: source.title?.trim() || fallbackTitle, titleSpans: source.titleSpans as TextSpan[] | undefined,
        icon: { icon: source.icon ?? "", iconType: source.iconType ?? "none" }, href: inlineDatabaseFullPageHref(pageId, block.id, viewId) });
    });
    return () => { live = false; stop(); };
  }, [dataSourceId, databasePage, fallbackTitle, pageId, session]);
  return database;
}

/**
 * The name of a full-page database's source while the database holds
 * several: Notion's layout builder is named by the source the page is in, as
 * its path names it under the database (seen 2026-11-07: "첫 소스"). One
 * source shares its database's name, so nothing then.
 */
export async function severalSourcesRowSourceTitle(session: Session, pageId: string, dataSourceId: string) {
  const read = await collectContentGraphStructure(session, pageId);
  const own = read?.graph.dataSources.filter((source) => (source.pageId ?? pageId) === pageId) ?? [];
  const source = own.find(({ id }) => id === dataSourceId);
  return own.length > 1 && source ? dataSourceDisplayTitle(source.title, own.length) : undefined;
}

/**
 * The "⋯" menu of a database's page opened at its own address: the same as
 * when it opens over its database, as Notion's (seen 2026-11-05) — it copies,
 * moves, archives and goes to trash from here too.
 */
export function useStandaloneRowMenu({ session, rowId, snapshot, rowTitle, readOnly, notifications, publish, onFavorite, onLeave, docked = false }: {
  session: Session;
  rowId: string;
  snapshot: DatabaseRowPageSnapshot | null;
  rowTitle: string;
  readOnly: boolean;
  notifications: DatabaseRowNotifications;
  publish: (snapshot: DatabaseRowPageSnapshot) => void;
  onFavorite: (favorite: boolean) => Promise<void>;
  /** Goes where a page that left its address goes. */
  onLeave: () => void;
  /** The row is open as a page: its updates and analytics dock beside it. */
  docked?: boolean;
}) {
  const { t } = useTranslation(["rowMenu", "pageTreeItem", "pageArchive", "trashView"]);
  const product = useProductPages();
  const { notify, dismissMessage } = useProductToast();
  const [busy, setBusy] = useState(false);
  const service = useMemo(() => createDatabaseRowPageService(session, publish), [publish, session]);
  useEffect(() => () => service.dispose(), [service]);
  // Opened beside a page that draws its database (a peek), the row leaves or
  // is archived there at the press too (lib/pageGraphStore.ts).
  const store = useOpenPageGraphStore();
  const analyticsService = useMemo(() => createPageAnalyticsPanelService(session), [session]);
  const workspaceId = snapshot?.workspaceId ?? "";
  const dataSourceId = snapshot?.row.dataSourceId ?? "";
  const moveTargets = useMemo<DatabaseRowMoveTarget[]>(() => [{ id: "", title: t("rowMenu:moveTopLevel"), kind: "page" }], [t]);
  const searchMoveTargets = useMemo(() => {
    if (readOnly || !workspaceId) return undefined;
    const search = createRelationDatabaseSearch(session, workspaceId, { addsRows: true });
    return async (query: string, signal: AbortSignal) => {
      const [databases, pages] = await Promise.all([
        search(query, signal),
        query ? searchWorkspacePages(session, query, 20, workspaceId, { signal, places: true }) : Promise.resolve({ pages: [] }),
      ]);
      return [
        ...databases.filter(({ id }) => id !== dataSourceId).map(({ id, title }) => ({ id, title, kind: "database" as const })),
        ...pages.pages.map(({ id, title }) => ({ id, title, kind: "page" as const })),
      ];
    };
  }, [dataSourceId, readOnly, session, workspaceId]);
  async function run(action: () => Promise<unknown>, done: string | null, failed: string, undo?: { label: string; onClick: () => void }) {
    setBusy(true);
    try {
      await action();
      if (done) notify(done, "success", undo);
      return true;
    } catch {
      notify(failed, "error");
      return false;
    } finally {
      setBusy(false);
    }
  }
  // Archiving shows on the page at once — its banner comes or goes at the press,
  // and its notice — and the server is written after; one it refuses puts the
  // page back as it was and takes the notice away, the toast saying why (user
  // decision 2026-11-12). "보관 취소" waits for the write, whose hold would
  // archive it again meanwhile.
  async function setArchived(archived: boolean) {
    const before = snapshot;
    if (before?.row.id === rowId) {
      publish({ ...before, row: { ...before.row, archivedAt: archived ? new Date().toISOString() : "", archivedBy: archived ? session.user.id : "" } });
    }
    const release = holdRowsShown(store, withRowsArchived(new Set([rowId]), archived, session.user.id), withRowsAsDrawn(rowsDrawn(store, [rowId])));
    const writing = writeRowPage(session, rowId, archived ? "archive" : "unarchive");
    const said = t("rowMenu:toast.archivedPage");
    if (archived) notify(said, "success", { label: t("pageArchive:unarchive"), onClick: () => { void writing.then(() => setArchived(false), () => undefined); } });
    try {
      await writing;
      release();
    } catch {
      release(true);
      if (before?.row.id === rowId) publish(before);
      if (archived) dismissMessage(said);
      notify(t(archived ? "rowMenu:toast.couldntArchivePage" : "rowMenu:toast.couldntUnarchivePage"), "error");
      return false;
    }
    void service.refresh(rowId).catch(() => {});
    return true;
  }
  const menu = useRowContextMenu({
    analytics: snapshot ? { workspaceId, pageId: snapshot.row.containerPageId, service: analyticsService, session, docked } : undefined,
    busy,
    moveTargets,
    searchMoveTargets,
    readOnly,
    rowId,
    rowTitle,
    snapshot,
    notifications: () => notifications,
    onArchive: readOnly ? undefined : async (_, archived) => { await setArchived(archived); },
    onOpenMode: () => undefined,
    onCopyLink: async () => {
      if (await copyText(serverUrl(pageHref(rowId)))) notify(t("rowMenu:toast.copiedLink"), "success");
      else notify(t("rowMenu:toast.couldntCopyLink"), "error");
    },
    onFavorite: (_, favorite) => onFavorite(favorite),
    onDuplicate: async () => { await run(() => service.duplicate(rowId), null, t("rowMenu:toast.couldntDuplicatePage")); },
    onMove: async (_, target, accepted) => {
      await run(() => service.move(rowId, { kind: target.kind ?? "database", id: target.id }, accepted),
        t("pageTreeItem:movedPage"), t("pageTreeItem:couldntMovePage"));
    },
    onPreviewMove: (_, target) => service.previewMove(rowId, { kind: target.kind ?? "database", id: target.id }),
    // The page leaves at the press, as a page sent to the trash does (user
    // decision 2026-11-12), said so at once as Notion's ("휴지통으로 이동됨 ·
    // 복원하기", seen 2026-11-13), its write going on past it; one the server
    // refuses opens again, the notice gone and the toast saying why. Kept for
    // a server out of reach (rowTrashKept.ts), it stays gone.
    onTrash: async () => {
      // Gone from the page beside it in the drawing that closes it (holdRowsShown).
      const drawn = rowsDrawn(store, [rowId]), release = holdRowsGone(store, drawn, false, true);
      const place = { pageId: snapshot?.row.containerPageId || rowsHolderPage(store, drawn), drawn };
      onLeave();
      const trashing = writeRowPage(session, rowId, "trash", place);
      const said = t("rowMenu:toast.movedToTrash");
      // "복원하기" puts it back where it was drawn, at the press — once the
      // trash is written, whose hold would take it out again meanwhile.
      notify(said, "success", { label: t("rowMenu:trashDatabase.restore"), onClick: () => {
        void trashing.then(() => {
          const back = holdRowsGone(store, drawn, true);
          return writeRowPage(session, rowId, "restore", place).then((done) => back(done === "kept" ? "kept" : false), () => {
            back(true);
            notify(t("rowMenu:toast.couldntRestorePage"), "error");
          });
        }, () => undefined);
      } });
      try {
        release(await trashing === "kept" ? "kept" : false);
      } catch {
        release(true);
        dismissMessage(said);
        notify(t("rowMenu:toast.couldntMoveToTrash"), "error");
        pushRoute(pageHref(rowId));
      }
    },
  });
  return {
    ...menu,
    /** A page in trash comes back, or goes for good, from its banner. */
    restore: async () => {
      if (!snapshot?.row.sourceInTrash) return run(() => service.restore(rowId), null, t("rowMenu:toast.couldntRestorePage"));
      // A page of a data source in the trash comes back to the reader's
      // private pages, which Notion asks first (seen 2026-11-12), and opens
      // there as a page.
      if (!(await confirmAction({
        message: t("trashView:confirm.restoreToPrivate"), detail: t("trashView:confirm.restoreToPrivateDetail"),
        confirmLabel: t("trashView:actions.restore"), accent: true,
      }))) return false;
      const restored = await run(() => restoreDatabaseRowPage(session, rowId, {
        requestId: newId(), expectedAuthorityRevision: snapshot.row.authorityRevision,
      }), null, t("rowMenu:toast.couldntRestorePage"));
      if (restored) {
        await product?.refreshPages?.();
        pushRoute(pageHref(rowId));
      }
      return restored;
    },
    deleteForever: async () => {
      setBusy(true);
      try {
        await service.deletePermanently(rowId);
        notify(t("rowMenu:toast.deletedPageForever"), "success");
        onLeave();
        return true;
      } catch (error) {
        notify(t(isLegalHoldPermanentDeleteError(error) ? "rowMenu:toast.legalHoldKeepsPage" : "rowMenu:toast.couldntDeletePageForever"), "error");
        return false;
      } finally {
        setBusy(false);
      }
    },
  };
}
