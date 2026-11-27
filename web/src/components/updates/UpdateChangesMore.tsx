import { useTranslation } from "react-i18next";
import { workspaceActivityBounds } from "@/workspaceActivityWire.generated";
import styles from "./UpdateChangesMore.module.css";

/**
 * How many of an update's changes show before the rest fold — the page's
 * updates tab and the workspace's updates alike, a title one of them, as
 * Notion's (seen 2026-11-13: the title, four lines, "6개 항목 더 보기").
 * The server reads the workspace's to the same bound.
 */
export const UPDATE_CHANGES_SHOWN = workspaceActivityBounds.changesShown;

/**
 * "N개 항목 더 보기" under an update's first changes, which shows the rest in
 * its place and is gone, with no folding back, as Notion's (seen 2026-11-13,
 * page and workspace updates alike): 12px/18px secondary words 4px in, 10px
 * under the last change, a faint ground under the pointer. The list around it
 * sets how far it sits before the changes' start (--update-more-shift).
 */
export function UpdateChangesMore({ count, onOpen }: { count: number; onOpen(): void }) {
  const { t } = useTranslation("updatesPanel");
  return (
    <button type="button" className={styles.more} data-update-changes-more onClick={onOpen}>
      {t("inbox.activity.moreChanges", { count })}
    </button>
  );
}
