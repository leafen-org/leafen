import { i18next } from "@/i18n";
import { confirmAction } from "./ConfirmActionDialog";

/**
 * Notion's question before a page or a database's row leaves a teamspace for
 * the top of private pages, from "옮기기" or a drop in the sidebar (seen
 * 2026-11-13 for a teamspace page and row through "옮기기"): only its mover
 * reaches it after, its action white on red. Moving to another teamspace asks nothing.
 */
export function confirmMovePrivate(title: string) {
  return confirmAction({
    message: i18next.t("moveToDialog:privateConfirm", { title }),
    detail: i18next.t("moveToDialog:privateConfirmDetail"),
    confirmLabel: i18next.t("moveToDialog:privateConfirmAction"),
    danger: "filled",
  });
}
