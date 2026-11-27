import type { HTMLAttributes } from "react";
import { useTranslation } from "react-i18next";
import { ArchivedMarkIcon } from "@/icons/leafen";
import styles from "./PickerArchivedPage.module.css";

/**
 * How a page picker shows an archived page — archived itself or with a page
 * above it (pageArchiveRoot) — which it keeps finding, as Notion's pickers
 * do (seen 2026-11-19, twice in Chrome, a page archived from the sidebar and
 * its sub-page): the "@", "[[" and "+" menus put the filled archive box at
 * the row's end, the "/페이지 링크" picker greys the title and puts the box
 * 4px after it, the selection's link field greys the title alone, and "옮기기"
 * lists it as any other page. The box is 16px in the disabled grey, centred
 * on the title's line, with no tip.
 */
export function PickerArchivedMark({ place }: { place: "end" | "after" }) {
  const { t } = useTranslation("pageArchive");
  return (
    <span className={`${styles.mark} ${place === "end" ? styles.end : styles.after}`} role="img"
      aria-label={t("archivedPage")} data-picker-archived>
      <ArchivedMarkIcon size="small" aria-hidden="true" />
    </span>
  );
}

/**
 * A page's title in the "/페이지 링크" picker: as it is, or greyed with the
 * archive box 4px after it while the page is archived.
 */
export function PickerPageTitle({ title, archived, labelProps }: {
  title: string; archived: boolean; labelProps: HTMLAttributes<HTMLElement>;
}) {
  if (!archived) return <span {...labelProps}>{title}</span>;
  return (
    <span className={styles.line}>
      <span {...labelProps} className={styles.title}>{title}</span>
      <PickerArchivedMark place="after" />
    </span>
  );
}
