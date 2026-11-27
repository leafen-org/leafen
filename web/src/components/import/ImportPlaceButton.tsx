import type { ButtonHTMLAttributes, Ref } from "react";
import { ChevronDown, FileText, LockIcon, TableIcon } from "@/icons/leafen";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import styles from "./ImportPlaceButton.module.css";

/**
 * The field that opens where an import goes, as Notion's import windows'
 * "위치 가져오기" (measured 2026-11-07): the page's mark small and gray, its
 * name, and a small chevron at the end. Every import window that picks a
 * place shows this one field; a database chosen as the place shows the
 * table mark its list row has, a teamspace its sidebar mark (its icon or its
 * name's first letter, as Notion's after "+ 새 팀스페이스", seen 2026-11-19).
 */
export function ImportPlaceButton({ label, emoji, isPrivate, isDatabase, teamspace, ref, className, ...rest }: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children" | "type"> & {
  label: string;
  /** The page's own emoji, shown in place of the page mark. */
  emoji?: string | null;
  /** The reader's private pages: a lock in place of the page mark. */
  isPrivate?: boolean;
  /** A database whose rows the files become. */
  isDatabase?: boolean;
  /** A teamspace, at whose top the import goes. */
  teamspace?: { label: string; icon?: string };
  ref?: Ref<HTMLButtonElement>;
}) {
  return (
    <button ref={ref} type="button" aria-haspopup="dialog" {...rest} className={className ? `${styles.place} ${className}` : styles.place}>
      {isPrivate ? <LockIcon size="small" aria-hidden="true" className={styles.mark} />
        : emoji ? <i aria-hidden="true" className={styles.mark}>{emoji}</i>
        : isDatabase ? <TableIcon size="small" aria-hidden="true" className={`${styles.mark} ${styles.database}`} />
        : teamspace ? <i aria-hidden="true" className={`${styles.mark} ${styles.teamspace}`} data-letter={teamspace.icon?.trim() ? undefined : "true"}>
          {teamspaceGlyph({ icon: teamspace.icon ?? "", name: teamspace.label })}</i>
        : <FileText size="small" aria-hidden="true" className={styles.mark} />}
      <span>{label}</span>
      <ChevronDown size="micro" aria-hidden="true" className={styles.chevron} />
    </button>
  );
}
