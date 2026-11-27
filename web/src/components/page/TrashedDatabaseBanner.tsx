import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import type { ContentGraph } from "@/contentGraph";
import { deleteDeletedDatabase, restoreDeletedDatabase } from "@/lib/deletedDatabases";
import { inlineDatabaseOpenedAlone, type InlineDatabasePage } from "@/lib/inlineDatabaseFullPage";
import { pageHref } from "@/lib/navigation";
import { useProductToast } from "@/lib/productToast";
import { pushRoute } from "@/lib/router";
import { heldDatabaseKey, useTrashLegalHolds } from "@/lib/trashLegalHolds";
import { useTrashedDatabaseAlone, type TrashedDatabaseAlone } from "@/lib/trashedDatabaseAlone";
import type { Page, Session } from "@/types";
import { DatabaseRowTrashBanner } from "../database/DatabaseRowTrashBanner";

/**
 * The banner over an inline database opened alone whose block alone is in
 * the trash (lib/trashedDatabaseAlone.ts), as Notion's (seen 2026-11-24 in
 * Chrome at 1440, two tabs): a page's in the trash — who sent it there and
 * when, "페이지 복원", which puts it back on its page and leaves it open, out
 * of the trash, and for whoever may, "영구 삭제하기", asking once more as a
 * page's does, which deletes it as the trash's list does and goes to its page.
 */
export function TrashedDatabaseBanner({ trashed, session, workspaceId, canDeleteForever, onGone }: {
  trashed: TrashedDatabaseAlone;
  session: Session;
  workspaceId: string;
  /** Whether the reader may delete it forever: the workspace's owners and admins. */
  canDeleteForever: boolean;
  /** Goes to its page once it is deleted forever. */
  onGone: () => void;
}) {
  const { t } = useTranslation("trashView");
  const { notify } = useProductToast();
  const held = useTrashLegalHolds(session, workspaceId, canDeleteForever, trashed.trashedAt);
  const restore = () => restoreDeletedDatabase(session, workspaceId, trashed.blockId).then(() => true, () => {
    notify(t("toast.couldntRestore"), "error");
    return false;
  });
  const deleteForever = () => deleteDeletedDatabase(session, workspaceId, trashed.blockId).then(() => {
    notify(t("toast.deletedForever"), "success");
    onGone();
    return true;
  }, () => {
    notify(t("toast.couldntDelete"), "error");
    return false;
  });
  return (
    <DatabaseRowTrashBanner trashedAt={trashed.trashedAt} trashedBy={trashed.trashedBy} currentUserId={session.user.id}
      currentUserName={session.user.name} onRestore={restore} onDeleteForever={deleteForever} canDeleteForever={canDeleteForever}
      keptByHold={held.has(heldDatabaseKey(trashed.blockId))} />
  );
}

/**
 * The page's inline database its address opens alone where its block alone
 * is in the trash (useTrashedDatabaseAlone), and the database's page view
 * with its banner over it (`withBanner`); a page's graph without it, the
 * page's other databases, as it was.
 */
export function useTrashedDatabasePage(session: Session, page: Page | null | undefined, graph: ContentGraph, canDeleteForever: boolean) {
  const aloneId = page && page.kind !== "database" ? inlineDatabaseOpenedAlone() : null;
  const shown = Boolean(aloneId && graph.blocks.some(({ id }) => id === aloneId));
  const trashed = useTrashedDatabaseAlone(session, page?.id, aloneId, shown);
  const pageId = page?.id ?? "", workspaceId = page?.workspaceId ?? "";
  const withBanner = useCallback((view: InlineDatabasePage | undefined) => view && trashed && view.blockId === trashed.blockId ? {
    ...view, trashBanner: <TrashedDatabaseBanner trashed={trashed} session={session} workspaceId={workspaceId}
      canDeleteForever={canDeleteForever} onGone={() => pushRoute(pageHref(pageId))} />,
  } : view, [canDeleteForever, pageId, session, trashed, workspaceId]);
  return { trashed, withBanner };
}
