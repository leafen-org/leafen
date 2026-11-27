import { createContext, useContext } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight, Trash } from "@/icons/leafen";
import { pushRoute } from "@/lib/router";
import styles from "./DatabaseArchive.module.css";

/**
 * Whether the database shown is archived, as Notion's inline database is
 * from its block menu's "보관" (seen 2026-10-11): it stays where it is, its
 * rows still open and new ones are still made, all of them dimmed with the
 * archive mark, under a note that it is archived.
 */
export const DatabaseArchivedContext = createContext(false);

export function useDatabaseArchived() {
  return useContext(DatabaseArchivedContext);
}

/** "이 데이터베이스가 보관되었습니다.", between the database's tools and its rows. */
export function DatabaseArchivedBanner() {
  const { t } = useTranslation("databaseView");
  return (
    <div className={styles.banner} role="note" data-database-archived-banner>
      <Trash size="regular" aria-hidden="true" />
      <span>{t("archivedDatabase")}</span>
    </div>
  );
}

/**
 * A note that the source database is in the trash and can't be edited, and
 * "데이터베이스 보기 →", over an inline database whose source is in the trash where it
 * stands, or with its page — "데이터베이스 보기" then opening that page in the
 * trash — as Notion's (seen 2026-11-11 and 2026-11-14): its rows stay, to
 * read only. The database's own page shows no note.
 */
export function DatabaseSourceTrashedBanner({ href }: { href?: string | null }) {
  const { t } = useTranslation("databaseView");
  return (
    <div className={styles.sourceTrashed} role="note" data-database-source-trashed-banner>
      <span className={styles.sourceTrashedNote}>
        <Trash size="large" aria-hidden="true" />
        <span>{t("sourceInTrash")}</span>
      </span>
      {href ? (
        <button type="button" className={styles.sourceTrashedOpen} onClick={() => pushRoute(href)}>
          {t("viewDatabase")}<ArrowRight size="small" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}
