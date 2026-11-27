import { pagePermanentDeleteErrorCodes } from "@/pagePermanentDeleteWire.generated";
import { api } from "@/api";
import { newId } from "@/lib/ids";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import { drainPagePermanentDelete, isLegalHoldPermanentDeleteError } from "@/lib/nativeTrashLifecycle";
import { useProductToast } from "@/lib/productToast";
import { useTrashLegalHolds } from "@/lib/trashLegalHolds";
import type { Page, Session } from "@/types";
import { useTranslation } from "react-i18next";
import { DatabaseRowTrashBanner } from "../database/DatabaseRowTrashBanner";
import { confirmAction } from "../ConfirmActionDialog";

/**
 * The banner on a page in the trash, as on a trashed Notion page: who moved
 * it there and when, a restore, and a permanent delete that asks once more.
 * A sub-page shows the trashed page it went with, and is in the trash on its
 * own as Notion's trash lists it (seen 2026-11-12, "P sub" under "W674 P
 * page"): its restore asks, then takes it alone to the top of the reader's
 * private pages, and it can be deleted forever alone.
 */
export function PageTrashBanner({
  page,
  trashRoot,
  session,
  onPageMetadataChange,
  onPageDeleted,
  canDeleteForever,
}: {
  page: Page;
  trashRoot: Page;
  session: Session;
  onPageMetadataChange?: (
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
  onPageDeleted?: () => void;
  /** Whether the reader may delete pages forever: the workspace's owners and admins. */
  canDeleteForever: boolean;
}) {
  const { t } = useTranslation(["trashView", "syncStatusBadge", "moveToDialog"]);
  const { dismissMessage, notify } = useProductToast();

  const nested = trashRoot.id !== page.id;
  // One a legal hold keeps says so before the press (deleting it is refused).
  const held = useTrashLegalHolds(session, page.workspaceId, canDeleteForever, trashRoot.trashedAt);

  async function restore() {
    if (!onPageMetadataChange) return false;
    if (nested && !(await confirmAction({
      message: t("confirm.restoreToPrivate"), detail: t("confirm.restoreToPrivateDetail"),
      confirmLabel: t("actions.restore"), accent: true,
    }))) return false;
    const result = await onPageMetadataChange(nested ? page : trashRoot,
      nested ? { parentId: "", teamspaceId: "" } : { inTrash: false }, new AbortController().signal)
      .catch(() => false as const);
    if (result === false) notify(t("toast.couldntRestore"), "error");
    return result !== false;
  }

  async function deleteForever() {
    // A big page takes minutes: past the moment one expects, it says so until it ends.
    const progress = t("syncStatusBadge:pageBusy.deleting");
    const slow = window.setTimeout(() => notify(progress, "default",
      { label: t("moveToDialog:copy.dismiss"), onClick: () => {}, autoDismissMs: null, dismissible: false }, "progress"), 2_000);
    try {
      await drainPagePermanentDelete({
        pageId: page.id,
        requestId: newId(),
        request: (input, signal) => api.deletePagePermanently(session, page.id, input, signal),
      });
    } catch (error) {
      notify(t(isLegalHoldPermanentDeleteError(error) ? "confirm.legalHoldError"
        : (error as { code?: unknown } | null)?.code === pagePermanentDeleteErrorCodes.conflict ? "toast.deletingAlready" : "toast.couldntDelete"), "error");
      return false;
    } finally {
      window.clearTimeout(slow);
      dismissMessage(progress);
    }
    notify(t("toast.deletedForever"), "success");
    onPageDeleted?.();
    return true;
  }

  return (
    <DatabaseRowTrashBanner
      trashedAt={trashRoot.trashedAt ?? ""}
      trashedBy={trashRoot.lastEditedBy ?? ""}
      currentUserId={session.user.id}
      currentUserName={session.user.name}
      onRestore={onPageMetadataChange ? restore : undefined}
      onDeleteForever={deleteForever}
      canDeleteForever={canDeleteForever}
      keptByHold={held.has(page.id)}
    />
  );
}
