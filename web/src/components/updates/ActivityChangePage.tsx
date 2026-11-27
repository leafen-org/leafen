import type { ReactNode } from "react";
import { FileText } from "@/icons/leafen";
import type { Page } from "@/types";
import { PageIconGlyph } from "../page/PageIcon";
import styles from "./ActivityChangePage.module.css";

/**
 * A sub-page among an update's changed lines, added or deleted, as Notion's
 * (seen 2026-11-16 at 1440 dark): its own icon — an emoji at 16.2px, else a
 * 20px mark, a page with none its page glyph — 6px before its name, the name
 * cut at the line's end. A page the reader's list no longer holds is its
 * page glyph. An inline database made in the edit is the page glyph too,
 * 4px before its name (Notion's, seen 2026-11-16, three edits: name 24px in).
 */
export function ActivityChangePage({ page, database, children }: { page?: Page; database?: boolean; children: ReactNode }) {
  return <span className={styles.page}>
    <span className={styles.icon} aria-hidden="true" data-database={database || undefined}>
      {page ? <PageIconGlyph page={page} size={20} box={20}/> : <FileText pixels={20} aria-hidden="true"/>}
    </span>
    <span className={styles.name}>{children}</span>
  </span>;
}
