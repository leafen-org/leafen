import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ArchivedMarkIcon, ArchiveIcon } from "@/icons/leafen";
import type { DatabaseArchivedRowsSurface } from "./databaseArchivedRowsContext";
import styles from "./DatabaseArchivedRows.module.css";

/** Which of a database's views show their archived rows, for as long as the database is shown. */
export function useArchivedRowsMode(viewId: string): DatabaseArchivedRowsSurface {
  const [viewIds, setViewIds] = useState<ReadonlySet<string>>(() => new Set());
  const set = useCallback((on: boolean) => setViewIds((current) => {
    if (current.has(viewId) === on) return current;
    const next = new Set(current);
    if (on) next.add(viewId); else next.delete(viewId);
    return next;
  }), [viewId]);
  const viewing = viewIds.has(viewId);
  return useMemo(() => ({ viewing, enter: () => set(true), exit: () => set(false) }), [set, viewing]);
}

/**
 * Over a view showing its archived rows, as Notion's (seen 2026-10-12): a
 * rounded 40px bar across the view, "🗄 보관된 페이지 보는 중" and a blue
 * "나가기" back to the others.
 */
export function ArchivedRowsBar({ onExit }: { onExit(): void }) {
  const { t } = useTranslation("pageArchive");
  return (
    <div className={styles.bar} role="status" data-archived-rows-bar>
      <ArchiveIcon size="tiny" aria-hidden="true" />
      <span>{t("viewingArchivedPages")}</span>
      <button type="button" className={styles.exit} data-archived-rows-exit onClick={onExit}>{t("exitArchivedPages")}</button>
    </div>
  );
}

/** An archived row's title: faint, and the archive mark after it. */
export function ArchivedRowMark() {
  const { t } = useTranslation("pageArchive");
  return <span className={styles.mark} title={t("archivedPage")} data-archived-row-mark>
    <ArchivedMarkIcon size="small" aria-hidden="true" />
  </span>;
}

/** A row's title text: as it is, or faint with the mark after it when the row is archived. */
export function ArchivedRowTitle({ archived, children }: { archived: boolean; children: ReactNode }) {
  return archived
    ? <><span className={styles.title} data-archived-row-title>{children}</span><ArchivedRowMark /></>
    : <>{children}</>;
}
