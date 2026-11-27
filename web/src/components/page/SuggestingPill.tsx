import { SuggestEditIcon, X } from "@/icons/leafen";
import { useTranslation } from "react-i18next";
import styles from "./SuggestingPill.module.css";

/**
 * Suggesting mode's indicator in a page's top bar, which turns it off, as
 * Notion's (seen 2026-11-13 at 1440): a blue pill of the suggesting mark,
 * "제안 모드 사용 중" and ✕ on a page's own bar; a peek's — a page's or a
 * database page's, at the side or the center — keeps the mark and ✕ alone.
 * It names what a press does and shows no tip, as Notion's. While it shows,
 * the bar leaves out when the page was last edited, as Notion's.
 */
export function SuggestingPill({ onStop, compact = false }: { onStop: () => void; compact?: boolean }) {
  const { t } = useTranslation("topBar");
  return (
    <button type="button" className={styles.suggestingPill} data-topbar-suggesting
      data-compact={compact ? "true" : undefined} aria-label={t("stopSuggesting")} onClick={onStop}>
      <SuggestEditIcon size="regular" aria-hidden="true" />
      {compact ? null : <span>{t("suggesting")}</span>}
      <X size="small" className={styles.stop} aria-hidden="true" />
    </button>
  );
}
