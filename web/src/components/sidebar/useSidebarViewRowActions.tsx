import { useMemo, useState } from "react";
import type { GraphRow } from "@/contentGraph";
import { useTranslation } from "react-i18next";
import { useRowPageReads } from "@/components/database/useRowPageReads";
import { copyText } from "@/lib/copyText";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import { pageHref } from "@/lib/navigation";
import { openPageInNewTab } from "@/lib/databasePageLink";
import { holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { openPagePeek } from "@/lib/pagePeek";
import { editorName } from "@/lib/peopleDirectory";
import { pageEditedActivity, usePageLastEditVia } from "@/lib/pageEditedActivity";
import { useProductToast } from "@/lib/productToast";
import { createRelationDatabaseSearch } from "@/lib/relationDatabaseSearch";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsGone, rowsDrawn, rowsHolderPage, writeRowPage } from "@/lib/rowsShownAtPress";
import { serverUrl } from "@/lib/serverOrigin";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import type { ToastAction } from "@/lib/toastStackView";
import type { Session } from "@/types";
import { usePageTreeContext } from "./pageTreeContext";
import { SidebarViewRowMenu } from "./SidebarViewRowMenu";
import { databaseRowMoveSubject } from "@/components/database/DatabaseRowMovePopover";
import { useAccessHold, usePageActionAccess } from "@/lib/pageActionAccessContext";
import { knownPathAboveId, rememberDrawnRowPiece, rowPiecePage } from "@/lib/pagePathPieces";
import { readPagePathAhead } from "@/lib/usePagePathsAbove";
import { focusOpenerAfterPaint } from "@/lib/overlayLayer";
import { addFailedNotice } from "./addFailedNotice";

/**
 * A view section's row's "하위 페이지 추가"(+) and "삭제, 복제 등…"(…), as on
 * Notion's page rows (seen 2026-10-02 and 2026-10-03): "+" makes a page
 * inside the row's page and opens it, "…" opens a sidebar page's menu at
 * the button — favorite, link, duplicate, rename, move, trash, a new tab and
 * the side peek — made on the row's page.
 */
export function useSidebarViewRowActions({ session, workspaceId, rowTitle, rowMark, onChanged, onRename }: {
  session: Session;
  workspaceId: string;
  rowTitle: (rowId: string) => string;
  /** The row's icon, as the section draws it. */
  rowMark: (rowId: string) => { icon?: string | null; iconType?: string | null };
  /** The section reads its rows again after a change. */
  onChanged: () => void;
  /** Starts renaming the row where the section shows it. */
  onRename: (rowId: string) => void;
}) {
  const { t } = useTranslation(["rowMenu", "pageTreeItem"]);
  const { notify, dismissMessage } = useProductToast();
  const { onCreateSubpage, onOpenPage, onPageChange, pageAccess } = usePageTreeContext();
  const { set: setSnapshot, of: rowPageOf } = useRowPageReads();
  const [menu, setMenu] = useState<{ rowId: string; anchor: { x: number; y: number }; trigger: HTMLElement } | null>(null);
  const service = useMemo(() => createDatabaseRowPageService(session, setSnapshot), [session]);
  // The open page's graph, whose databases — and the sidebar's sections
  // beside it, this one among them — a row leaves at the press.
  const store = useOpenPageGraphStore();
  // Each choice writes through a service of its own, beside the menu's reads,
  // which keep each row's page apart (useRowPageReads).
  const writer = () => createDatabaseRowPageService(session, () => {});
  const searchMoveTargets = useMemo(() => {
    const search = createRelationDatabaseSearch(session, workspaceId, { addsRows: true });
    return async (query: string, signal: AbortSignal) => {
      const [databases, pages] = await Promise.all([
        search(query, signal),
        query ? searchWorkspacePages(session, query, 20, workspaceId, { signal, places: true }) : Promise.resolve({ pages: [] }),
      ]);
      return [
        ...databases.map(({ id, title }) => ({ id, title, kind: "database" as const })),
        ...pages.pages.map(({ id, title }) => ({ id, title, kind: "page" as const })),
      ];
    };
  }, [session, workspaceId]);

  // Focus goes back to the "…" unless what was chosen takes it elsewhere.
  function close(returnFocus = true) {
    const trigger = menu?.trigger;
    setMenu(null);
    if (returnFocus) focusOpenerAfterPaint(trigger);
  }

  // The menu closes at the press and what was chosen goes on behind it; a
  // row it takes out of the section (`leaves`) goes at once, from the open
  // page's databases and the sidebar's sections as they drew it — or,
  // `back`, comes back to them — and is as it was when the server refuses
  // (user decision 2026-11-12: a press answers on the next frame; the open
  // page's table showed it once the server had moved it, 988 ms on a NAS).
  // What it says it says at the press, as Notion's ("휴지통으로 이동됨 ·
  // 복원하기", seen 2026-11-13: it came after the server, 1.7–3.0 s on a
  // NAS), and takes back when the server refuses. A result the sidebar shows
  // says nothing more, as Notion's (seen 2026-10-31). One kept for a server
  // out of reach (the trash, rowTrashKept.ts) stays as shown.
  function run(action: () => Promise<unknown>, success: string | null, failure: string, undo?: ToastAction,
    leaves?: { drawn: readonly GraphRow[]; back?: boolean }) {
    close();
    const release = leaves ? holdRowsGone(store, leaves.drawn, leaves.back) : () => undefined;
    if (success) notify(success, "success", undo);
    return action().then((done) => {
      release(done === "kept" ? "kept" : false);
      onChanged();
    }, () => {
      release(true);
      if (success) dismissMessage(success);
      notify(failure, "error");
    });
  }

  // A page inside the row's page opens at its title, taking what is typed
  // meanwhile, its path whole from its first frame: the row names itself as
  // the piece above it, under the piece the server named above the row —
  // read as the pointer came to it (approachRow), as its access is. A path
  // not read by then is drawn once the server reads the new page's.
  async function addSubpage(target: string) {
    const above = knownPathAboveId(target);
    if (above) rememberDrawnRowPiece(rowPiecePage({ id: target, title: rowTitle(target), ...rowMark(target) }, above, workspaceId));
    await holdTypingWhile(async () => {
      try {
        if (!await pageAccess.read(target)) return;
        const page = await onCreateSubpage(target, workspaceId);
        if (!page) return;
        onPageChange(page);
        requestPageTitleFocus(page.id);
        onOpenPage(page, false);
      } catch (error) {
        notify(...addFailedNotice(error, t("pageTreeItem:pageAccessRequired")));
      }
    });
  }

  const rowId = menu?.rowId ?? "";
  const access = usePageActionAccess(rowId, Boolean(menu));
  const row = rowId ? rowPageOf(rowId) : null;
  // Its rows follow the row's access and its page's role, read as it opens: it shows once they are in (useAccessHold).
  const held = useAccessHold(menu ? rowId : null, !access.known || !row);
  const via = usePageLastEditVia(session, rowId, row ? row.row.lastEditedBy || row.row.createdBy || null : null, Boolean(menu && row));
  const rowMenu = menu ? (
    <SidebarViewRowMenu
      anchor={menu.anchor}
      held={held}
      busy={false}
      edited={row ? { name: editorName(row.row.lastEditedBy || row.row.createdBy, session.user.id), at: row.row.updatedAt,
        editorId: row.row.lastEditedBy || row.row.createdBy || null, via, activity: pageEditedActivity(row.row, session.user.id) } : null}
      favorite={row?.row.favorite ?? false}
      moveTargets={[{ id: "", title: t("rowMenu:moveTopLevel"), kind: "page" }]}
      readOnly={!access.canEdit || !row || row.accessRole === "view" || row.accessRole === "comment"}
      searchMoveTargets={searchMoveTargets}
      title={rowTitle(rowId)}
      onClose={() => close()}
      onCopyLink={() => {
        close();
        void copyText(serverUrl(pageHref(rowId))).then((copied) => notify(t(copied ? "rowMenu:toast.copiedLink" : "rowMenu:toast.couldntCopyLink"), copied ? "success" : "error"));
      }}
      onDuplicate={() => void run(
        () => writer().duplicate(rowId), null, t("rowMenu:toast.couldntDuplicatePage"),
      )}
      onFavorite={(favorite) => void run(
        () => writer().setFavorite(rowId, favorite), null, t("rowMenu:toast.couldntUpdateFavorites"),
      )}
      onMove={(destination, accepted) => run(
        () => writer().move(rowId, { kind: destination.kind ?? "database", id: destination.id }, accepted),
        t("pageTreeItem:movedPage"), t("pageTreeItem:couldntMovePage"), undefined, { drawn: rowsDrawn(store, [rowId]) },
      )}
      onOpenInNewTab={() => {
        close(false);
        openPageInNewTab({ id: rowId }, session.user.id);
      }}
      onOpenInSidePeek={() => {
        close(false);
        openPagePeek(rowId);
      }}
      onPreviewMove={(destination) => service.previewMove(rowId, { kind: destination.kind ?? "database", id: destination.id })}
      moveSubject={row ? databaseRowMoveSubject(row, rowTitle(rowId)) : undefined}
      onMoved={onChanged}
      onRename={() => {
        if (!access.canEdit || !row || row.accessRole === "view" || row.accessRole === "comment") return;
        close(false);
        onRename(rowId);
      }}
      onTrash={() => {
        // "복원하기" puts it back where it was drawn, at the press — once the
        // trash is written, whose hold would take it out again meanwhile.
        const drawn = rowsDrawn(store, [rowId]), place = { pageId: rowsHolderPage(store, drawn), drawn };
        const trashing: Promise<unknown> = run(() => writeRowPage(session, rowId, "trash", place), t("rowMenu:toast.movedToTrash"),
          t("rowMenu:toast.couldntMoveToTrash"), { label: t("rowMenu:trashDatabase.restore"), onClick: () => void trashing.then(() => run(
            () => writeRowPage(session, rowId, "restore", place), null, t("rowMenu:toast.couldntRestorePage"), undefined, { drawn, back: true },
          )) }, { drawn });
      }}
    />
  ) : null;

  return {
    addSubpage,
    approachRow: (target: string) => {
      void pageAccess.read(target).catch(() => undefined);
      readPagePathAhead(session, workspaceId, target);
    },
    // It opens where a sidebar page's does, its top left on the button, as
    // Notion's (seen 2026-10-03).
    openRowMenu: (target: string, trigger: HTMLElement, point: { x: number; y: number }) => {
      setMenu({ rowId: target, anchor: point, trigger });
      void service.refresh(target).catch(() => {});
    },
    rowMenu,
  };
}
