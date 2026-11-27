import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RestoreIcon, Trash } from "@/icons/leafen";
import { confirmAction } from "../ConfirmActionDialog";
import { HoverTip } from "../IconTipButton";
import { actorLabel, personName } from "@/lib/peopleDirectory";
import { canManageNativeWorkspace } from "@/lib/nativeWorkspaceRolePolicy";
import { useTrashLegalHolds } from "@/lib/trashLegalHolds";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import type { Session } from "@/types";
import styles from "../page/PageArchiveBanner.module.css";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * How long ago the page went to the trash: "지금에", "5분 전에", "3시간 전에",
 * "2일 전에" — Notion's words, its "지금" for the first minute as everywhere
 * else it says how long ago (seen 2026-11-08 to 11-15). Its "30일일" after
 * them is a slip this banner does not copy ("30일 후", 2026-09-30).
 */
function movedWhen(trashedAt: string): { key: string; count?: number } {
  const elapsed = Date.now() - new Date(trashedAt).getTime();
  if (!Number.isFinite(elapsed) || elapsed < MINUTE) return { key: "rowBanner.when.justNow" };
  if (elapsed < HOUR) return { key: "rowBanner.when.minutes", count: Math.floor(elapsed / MINUTE) };
  if (elapsed < DAY) return { key: "rowBanner.when.hours", count: Math.floor(elapsed / HOUR) };
  return { key: "rowBanner.when.days", count: Math.floor(elapsed / DAY) };
}

/**
 * Who sent something to the trash, by name, as its banner and its mention's
 * tip name them (TrashedMentionTip.tsx): the people directory's name, the
 * reader's own while it is them, else the directory's word for them.
 */
export function trashMoverName(trashedBy: string, currentUserId?: string, currentUserName?: string) {
  const mine = !trashedBy || trashedBy === currentUserId;
  return personName(trashedBy) || (mine ? currentUserName?.trim() : "") || actorLabel(trashedBy, currentUserId);
}

// A page stays in the trash 30 days (the workspace's fixed keeping); the
// banner says how many are left, as Notion's (its language file, 2026-11-08).
const TRASH_KEPT_DAYS = 30;
function daysLeft(trashedAt: string) {
  const left = new Date(trashedAt).getTime() + TRASH_KEPT_DAYS * DAY - Date.now();
  return Number.isFinite(left) ? Math.max(1, Math.ceil(left / DAY)) : TRASH_KEPT_DAYS;
}

/**
 * Whether the reader may delete a database page in the trash forever (the
 * workspace's owners and admins, as a page's band reads it) and whether an
 * active legal hold keeps it (lib/trashLegalHolds.ts), for its band to say
 * both before the press (2026-11-15: a row page's band still said "30일 후"
 * with a live "영구 삭제하기"). Unknown workspaces keep the button.
 */
export function useRowTrashDeletion(session: Session | undefined, workspaceId: string, rowId: string, trashedAt: string) {
  const workspace = useProductPagesSelect((pages) => pages?.workspaces?.find((candidate) => candidate.id === workspaceId));
  const canDeleteForever = workspace ? canManageNativeWorkspace(workspace) : true;
  const held = useTrashLegalHolds(session, workspaceId, Boolean(trashedAt) && canDeleteForever, trashedAt);
  return { canDeleteForever, keptByHold: held.has(rowId) };
}

/**
 * The banner on a page in the trash, as on a trashed Notion page (seen
 * 2026-09-30): white on red, who moved the page to the trash and how long
 * ago, "페이지 복원", and for whoever may, "영구 삭제하기" that asks
 * once more in Notion's confirmation card, as its trash list does (seen
 * 2026-10-29). The mover is named even when it is the reader, as in Notion.
 */
export function DatabaseRowTrashBanner({
  trashedAt,
  trashedBy,
  currentUserId,
  currentUserName,
  onRestore,
  onDeleteForever,
  canDeleteForever = true,
  keptByHold = false,
}: {
  trashedAt: string;
  trashedBy: string;
  currentUserId?: string;
  currentUserName?: string;
  onRestore?: () => Promise<boolean>;
  onDeleteForever?: () => Promise<boolean>;
  /**
   * Whether the reader may delete it forever (the server keeps that to the
   * workspace's owners): someone else sees the button dim and, on
   * it, why, as Notion tells a reader who may not (seen 2026-11-13 as an edit
   * guest: in the dark tip under it, that they lack the right to delete the
   * page forever); the trash window's lines say the same.
   */
  canDeleteForever?: boolean;
  /**
   * An active legal hold keeps it from deletion for good: in place of the 30
   * days the banner says so, and its "영구 삭제하기" is dim with why on it,
   * as before the press rather than the refusal after it.
   */
  keptByHold?: boolean;
}) {
  const { t } = useTranslation("trashView");
  const [busy, setBusy] = useState(false);
  // "페이지 복원" takes the banner away at the press; one the server refuses
  // brings it back (user decision 2026-11-12: a press answers on the next
  // frame). Deleting for good is the server's to decide: the banner stays,
  // its buttons pressed, until it answers.
  const [leftAt, setLeftAt] = useState("");
  if (!trashedAt || leftAt === trashedAt) return null;
  const restore = (action: () => Promise<boolean>) => {
    setLeftAt(trashedAt);
    void action().then((done) => { if (!done) setLeftAt(""); }, () => setLeftAt(""));
  };
  const deleteForever = async (action: () => Promise<boolean>) => {
    if (!await confirmAction({ message: t("confirm.deletePage"), confirmLabel: t("rowBanner.deleteForever"), danger: true })) return;
    setBusy(true);
    void action().finally(() => setBusy(false));
  };
  const actor = trashMoverName(trashedBy, currentUserId, currentUserName);
  const moved = movedWhen(trashedAt);
  const refused = canDeleteForever ? t("confirm.legalHoldError") : t("deleteForeverNotAllowed");
  const when = moved.count === undefined ? t(moved.key) : t(moved.key, { count: moved.count });
  return (
    <div className={`${styles.banner} ${styles.trash}`} role="status" data-row-trash-banner>
      <span className={styles.message}>
        {`${t("rowBanner.movedToTrash", { actor, when })} ${keptByHold ? t("rowBanner.keptByHold") : t("rowBanner.deletedIn", { count: daysLeft(trashedAt) })}`}
      </span>
      {/* Its buttons go as one group: a wider touch screen wraps them under the words together. */}
      <span className={styles.actions}>
        {onRestore ? (
          <button type="button" className={styles.action} data-row-restore disabled={busy}
            aria-busy={busy || undefined} onClick={() => restore(onRestore)}>
            <RestoreIcon size="tiny" aria-hidden="true" />
            {t("rowBanner.restore")}
          </button>
        ) : null}
        {onDeleteForever && canDeleteForever && !keptByHold ? (
          <button type="button" className={styles.action} data-row-delete-forever disabled={busy}
            aria-busy={busy || undefined} onClick={() => void deleteForever(onDeleteForever)}>
            <Trash size="tiny" aria-hidden="true" />
            {t("rowBanner.deleteForever")}
          </button>
        ) : onDeleteForever ? (
          <HoverTip label={refused} wide>
            <button type="button" className={styles.action} data-row-delete-forever aria-disabled="true"
              aria-description={refused} data-kept-by-hold={keptByHold && canDeleteForever ? "" : undefined}>
              <Trash size="tiny" aria-hidden="true" />
              {t("rowBanner.deleteForever")}
            </button>
          </HoverTip>
        ) : null}
      </span>
    </div>
  );
}
