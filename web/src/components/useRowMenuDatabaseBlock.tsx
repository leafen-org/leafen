import { useState, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { useProductPagesNow, useProductPagesSelect } from "@/components/product/productPagesStore";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { showInlineDatabaseMovedTo } from "@/lib/inlineDatabaseMovedShown";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import { pushRoute } from "@/lib/router";
import { useOptionalProductToast, type useProductToast } from "@/lib/productToast";
import { BlockMoveToDialog } from "./editor/BlockMoveToDialog";
import { useInlineDatabaseAlone, type InlineDatabaseAlone } from "./editor/inlineDatabaseAlone";
import { type MovePopoverAnchor, movePopoverAnchorOf } from "./movePopoverPlacement";

type Notify = ReturnType<typeof useProductToast>["notify"];

/** Where its move window opens: under what opened the menu, 12px above where the menu was, as a page's "옮기기". */
function moveAnchorOf(menu: HTMLElement | null): MovePopoverAnchor | null {
  const anchor = movePopoverAnchorOf(menu);
  return anchor ? { ...anchor, top: anchor.top - 12, bottom: anchor.top - 12 } : null;
}

/** The move window of an inline database opened alone, from its page's menu or its keys. */
export function InlineDatabaseMoveDialog({ alone, anchor, onClose }: {
  alone: InlineDatabaseAlone; anchor: MovePopoverAnchor | null; onClose: () => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("moveToDialog");
  const toast = useOptionalProductToast();
  const store = useOpenPageGraphStore();
  const move = alone.move;
  if (!move) return null;
  const { blockId, pageId: from, title } = alone;
  return (
    <BlockMoveToDialog anchor={anchor} blockId={blockId} sourcePageId={from} workspaceId={alone.workspaceId}
      pages={move.pages} session={move.session} onPageChange={move.onPageChange} onStatus={move.onStatus} onClose={onClose}
      // It goes onto a page; a database is no choice, as Notion's (seen 2026-11-11).
      databaseMoving
      // Moved, it stays open alone where it went, a notice saying where with
      // "이동" and "실행 취소", as Notion's (seen 2026-11-11). The window
      // closes at the press and the database shows alone where it went,
      // and back where it was at the press of "실행 취소" (user decision
      // 2026-11-12: a press answers on the next frame); the server moves it
      // behind, a refusal saying so and showing it where it was.
      onChooseDestination={(_id, _signal, destination) => {
        const going = showInlineDatabaseMovedTo(store, blockId, destination.id, move.session.user.id);
        void move.to(destination, new AbortController().signal).then((moved) => {
          going.moved();
          toast?.notify(t("toast.movedTo", { title: title || pageNames.title({ title: "", kind: "database" }), place: pageNames.title(destination) }),
            "default", [
              { label: t("toast.go"), onClick: () => pushRoute(pageHref(destination.id)) },
              ...(moved.undo ? [{ label: t("toast.undo"), onClick: () => {
                const back = showInlineDatabaseMovedTo(store, blockId, from, move.session.user.id);
                void Promise.resolve(moved.undo?.(back.moved)).then((saved) => saved === false ? back.refused() : back.moved(), back.refused);
              } }] : []),
            ]);
        }, () => {
          going.refused();
          move.onStatus?.(t("blockMoveToDialog:toast.moveFailed"), "error");
        });
      }} />
  );
}

/**
 * The page "⋯" menu of an inline database opened alone works on the
 * database, as Notion's (seen 2026-11-11): "데이터베이스 잠금" locks it (a
 * view block's "보기 잠금" its views), and once locked the menu keeps no lock,
 * move, merge or history rows, its unlock being the lock mark's; "복제" copies
 * it beside itself and opens the copy; "옮기기" moves it; "휴지통으로 이동"
 * asks, takes it off its page and goes back to the page; "CSV와 병합" merges
 * into its source; "보관" (Leafen's own) archives it. Its foot counts no words.
 * None for any other page.
 */
export function useRowMenuDatabaseBlock({ blockId, canEdit, menuRef, onClose, notify }: {
  blockId?: string;
  canEdit: boolean;
  menuRef: RefObject<HTMLElement | null>;
  onClose: () => void;
  notify: Notify;
}) {
  const { t } = useTranslation(["rowMenu", "pageArchive", "blockItem"]);
  const alone = useInlineDatabaseAlone(blockId);
  const imports = useProductPagesSelect((pages) => Boolean(pages?.openImport)), pagesNow = useProductPagesNow();
  const [moveAnchor, setMoveAnchor] = useState<MovePopoverAnchor | null | undefined>(undefined);
  if (!alone || !canEdit) return null;
  const duplicate = (withoutContent: boolean) => {
    onClose();
    void alone.duplicate(withoutContent).then((copy) => {
      if (copy) pushRoute(inlineDatabaseFullPageHref(alone.pageId, copy));
      else notify(t("blockItem:database.duplicateFailed"), "error");
    }, () => notify(t("blockItem:database.duplicateFailed"), "error"));
  };
  const trash = () => {
    onClose();
    void alone.trash().then((gone) => { if (gone) pushRoute(pageHref(alone.pageId)); },
      () => notify(t("rowMenu:toast.couldntMoveToTrash"), "error"));
  };
  // Archived, a notice offers it back, as its block menu's (seen 2026-10-11).
  const archive = () => {
    onClose();
    alone.setArchived(!alone.archived);
    if (!alone.archived) {
      notify(t("pageArchive:archivedPages"), "default", { label: t("pageArchive:unarchive"), onClick: () => alone.setArchived(false) });
    }
  };
  const onMove = alone.move ? () => setMoveAnchor(moveAnchorOf(menuRef.current)) : undefined;
  const lockLabel = t(alone.views ? "rowMenu:actions.viewLock" : "rowMenu:actions.databaseLock");
  const onLock = () => alone.setLocked(!alone.locked);
  const onMergeCSV = imports && !alone.views ? () => {
    onClose();
    pagesNow()?.openImport?.({ pageId: alone.pageId, title: alone.title, format: "csv", mergeIntoPage: true, mergeDataSourceId: alone.dataSourceId });
  } : undefined;
  return {
    lockedForMe: alone.locked,
    moving: moveAnchor !== undefined,
    /** What the menu's rows do to the database in place of its page. */
    groups: {
      lockKind: alone.views ? "views" as const : "database" as const, locked: alone.locked, onLock,
      onDuplicate: () => duplicate(false), onDuplicateWithoutContent: alone.views ? undefined : () => duplicate(true),
      onDuplicateToPrivate: undefined, onMove: onMove ?? (() => {}), onTrash: trash, onMergeCSV,
      archiveState: alone.archived ? "archived" as const : "active" as const, onArchive: alone.views ? undefined : archive,
    },
    /** The same on a phone's sheet. */
    phone: {
      onDuplicate: () => duplicate(false), onDuplicateWithoutContent: alone.views ? undefined : () => duplicate(true), onDuplicateToPrivate: undefined,
      move: onMove ? { onPress: onMove } : undefined, onTrash: trash,
      lock: { on: alone.locked, label: lockLabel, onToggle: onLock },
      archive: alone.views || alone.locked ? undefined : { label: t(alone.archived ? "rowMenu:actions.unarchive" : "rowMenu:actions.archive"), onToggle: archive },
    },
    /** Its own version history, which "버전 기록" opens in place of its page's. */
    history: { pageId: alone.pageId, workspaceId: alone.workspaceId, blockId: alone.blockId, sourceId: alone.dataSourceId, title: alone.title },
    dialog: moveAnchor !== undefined ? <InlineDatabaseMoveDialog alone={alone} anchor={moveAnchor} onClose={() => { setMoveAnchor(undefined); onClose(); }} /> : null,
  };
}
