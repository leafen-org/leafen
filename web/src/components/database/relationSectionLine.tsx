import type { MutableRefObject } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "../icons";
import styles from "./relationSectionLine.module.css";

/** A page section's line: the box its "+" and ⠿ stand out from at its start (relationSectionLine.module.css). */
export const relationSectionLineClass = styles.line;

/**
 * The "+" a page section's line brings out at its start under the pointer,
 * as Notion's (seen 2026-11-15, twice, pressed on the first and on a middle
 * line): it does what the section's "새 페이지 추가하기" does — a "제목 입력…"
 * line after the section's last page, its typing going there, Enter making
 * the page named and related, Escape on it empty making none.
 */
export function RelationSectionLineAdd({ startFrom }: { startFrom: MutableRefObject<(() => void) | null> }) {
  const { t } = useTranslation("propertyCell");
  return <button type="button" className={styles.add} data-relation-section-line-add aria-label={t("newRelatedPage")} title={t("newRelatedPage")}
    onPointerDown={(event) => event.stopPropagation()}
    onClick={(event) => { event.preventDefault(); event.stopPropagation(); startFrom.current?.(); }}>
    <Plus size="small" aria-hidden="true" />
  </button>;
}
