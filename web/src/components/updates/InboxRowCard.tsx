import { useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArchiveIcon, MarkReadIcon, MarkUnreadIcon, UnarchiveIcon } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import styles from "./InboxRowTouch.module.css";

/**
 * A notification's actions on a long press, as Notion's Android app shows
 * them (seen 2026-11-21 on a Galaxy A90, twice): a small card under the row,
 * 20 in from the screen's start, as wide as its words — "읽음으로 표시" or
 * "읽지 않음으로 표시" and "보관" (an archived one's "보관 취소", the app's
 * not seen) — each a 42 high row of a 20px mark and 16px words, on 4px
 * corners with the menus' shadow. A row swiped open stays open under it.
 */
export function InboxRowCard({ row, label, unread, archived, onToggleRead, onArchive, onUnarchive, onClose }: {
  row: HTMLElement;
  label: string;
  unread: boolean;
  archived: boolean;
  onToggleRead(): void;
  onArchive(): void;
  onUnarchive(): void;
  onClose(): void;
}) {
  const { t } = useTranslation("updatesPanel");
  const ref = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => {
    const at = popoverRectOf(row)!;
    return placeAround({ ...at, left: at.left + 20 }, size, viewport, { align: "start", gap: 0 });
  }, [row], undefined, { kind: "picker", restoreFocus: false, onEscape: onClose, onOutsidePress: onClose });
  const act = (action: () => void) => () => { onClose(); action(); };
  return createPortal(
    <div ref={ref} className={styles.card} role="menu" aria-label={label} data-inbox-row-card
      style={{ position: "fixed", ...popoverPlacementStyle(place) }}>
      {archived ? null : (
        <button type="button" role="menuitem" className={styles.cardItem} onClick={act(onToggleRead)}>
          {unread ? <MarkReadIcon size="regular" aria-hidden="true" /> : <MarkUnreadIcon size="regular" aria-hidden="true" />}
          {t(unread ? "inbox.touchMarkRead" : "inbox.touchMarkUnread")}
        </button>
      )}
      <button type="button" role="menuitem" className={styles.cardItem} onClick={act(archived ? onUnarchive : onArchive)}>
        {archived ? <UnarchiveIcon size="regular" aria-hidden="true" /> : <ArchiveIcon size="regular" aria-hidden="true" />}
        {t(archived ? "inbox.unarchive" : "inbox.touchArchive")}
      </button>
    </div>,
    document.body,
  );
}
