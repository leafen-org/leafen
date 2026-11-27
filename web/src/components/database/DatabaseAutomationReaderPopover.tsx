import { useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LightningIcon, X } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import { toolbarMenuColumn } from "./databaseToolbarModel";
import { toolbarButtonsEnd } from "./useDatabaseToolbarController";
import styles from "./DatabaseAutomationReaderPopover.module.css";

// The toolbar's "⚡" it opened from: the one whose menu is open.
function openedFrom() {
  return document.querySelector<HTMLElement>('[data-database-toolbar-menu="automations"][aria-expanded="true"]');
}

/**
 * What a desktop reader's "⚡" opens on a database with no automations: a
 * 290×352 window 3px under the button, lined up across as the view settings
 * are (6px past the toolbar's end, no nearer than 12px to the window's
 * edge), holding a "×" and, in its middle, "⚡", "자동화" and why they can't
 * make one — not the list's panel with its head and search, as Notion's
 * read-only guest (seen 2026-11-15: 1138.5–1428.5 under a toolbar ending at
 * 1187 in a 1440 window, and from 1058.5 under one ending at 1052.5 with the
 * sidebar closed). With automations, a reader reads them in the list
 * (DatabaseAutomationPanel).
 */
export function DatabaseAutomationReaderPopover({ onClose }: { onClose(): void }) {
  const { t } = useTranslation("databaseView");
  const panel = useRef<HTMLDivElement>(null);
  const placement = usePopoverPlacement(panel, (size, viewport) => {
    const button = openedFrom();
    const rect = popoverRectOf(button) ?? { left: 0, right: 0, top: 0, bottom: 0 };
    const column = toolbarMenuColumn(rect, "view", viewport.width, button ? toolbarButtonsEnd(button) : undefined, undefined,
      button ? readsRightToLeft(button) : false);
    return placeAround({ ...rect, left: column.left, right: column.left }, size, viewport, { gap: column.gap, margin: column.margin });
  }, [], undefined, { kind: "picker", restoreFocus: false, onEscape: onClose, onOutsidePress: onClose, inside: () => [openedFrom()] });
  return createPortal(
    <div ref={panel} className={styles.popover} role="dialog" aria-label={t("automationReaderTitle")} data-automation-reader-popover
      style={popoverPlacementStyle(placement)}>
      <button type="button" className={styles.close} aria-label={t("closeAutomations")} onClick={onClose}>
        <X size="tiny" aria-hidden="true" />
      </button>
      <div className={styles.note}>
        <LightningIcon size="large" aria-hidden="true" />
        <strong>{t("automationReaderTitle")}</strong>
        <p>{t("automationReaderDetail")}</p>
      </div>
    </div>,
    document.body,
  );
}
