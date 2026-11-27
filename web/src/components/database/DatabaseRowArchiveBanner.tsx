import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArchiveIcon } from "@/icons/leafen";
import { archiveBandText } from "@/lib/pageArchive";
import styles from "../page/PageArchiveBanner.module.css";

/**
 * The yellow banner on an archived database row page, as on an archived
 * page: who archived it and when, and the way back.
 */
export function DatabaseRowArchiveBanner({
  archivedAt,
  archivedBy,
  currentUserId,
  onUnarchive,
}: {
  archivedAt: string;
  archivedBy: string;
  currentUserId?: string;
  onUnarchive?: () => Promise<boolean>;
}) {
  const { t } = useTranslation("pageArchive");
  // "보관 취소" takes the banner away at the press; one the server refuses
  // brings it back (user decision 2026-11-12: a press answers on the next frame).
  const [leftAt, setLeftAt] = useState("");
  if (!archivedAt || leftAt === archivedAt) return null;
  return (
    <div className={styles.banner} role="status" data-row-archive-banner>
      <ArchiveIcon size="regular" aria-hidden="true" />
      <span className={styles.message}>
        {archiveBandText(archivedBy, archivedAt, currentUserId)}
      </span>
      {onUnarchive ? (
        <button
          type="button"
          className={styles.action}
          data-row-unarchive
          onClick={() => {
            setLeftAt(archivedAt);
            void onUnarchive().then((done) => { if (!done) setLeftAt(""); }, () => setLeftAt(""));
          }}
        >
          {t("unarchive")}
        </button>
      ) : null}
    </div>
  );
}
