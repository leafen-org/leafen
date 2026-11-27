import { ArchivedMarkIcon } from "@/icons/leafen";
import styles from "./SearchArchivedMark.module.css";

/**
 * The mark after a result's title for a page in an archived teamspace the
 * reader still opens, as Notion's: an archive box with no words or tip
 * (seen 2026-11-14: its badge "Archived" is read by no one). The results
 * keep their order; an archived teamspace's pages are not sent down.
 */
export function SearchArchivedMark() {
  return <ArchivedMarkIcon size="small" className={styles.mark} aria-hidden="true" data-search-archived-teamspace />;
}
