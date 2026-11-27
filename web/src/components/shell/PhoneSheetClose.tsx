import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import styles from "./PhoneSheetClose.module.css";

/** A head with the round ✕, and its title, take this layout too (PhoneSheetClose.module.css). */
export const phoneSheetCloseStyles = styles;

/**
 * The round ✕ at a sheet head's right, as Notion's phone and iPad sheets draw
 * it (PhoneSheetClose.module.css): the share sheets' 32px one, or with
 * `small` the discussions' 24px one ("모든 토론", "필터" and a block's
 * discussion, seen 2026-11-16 at 402).
 */
export function PhoneSheetCloseButton({ onClose, small = false }: { onClose(): void; small?: boolean }) {
  const { t } = useTranslation("common");
  return <button type="button" className={small ? `${styles.close} ${styles.small}` : styles.close} aria-label={t("actions.close")}
    data-phone-sheet-close onClick={onClose}>
    {small ? <X size="regular" aria-hidden="true" /> : (
      <svg width="11" height="11" viewBox="0 0 11 11" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
        <path d="M1 1 10 10M10 1 1 10" />
      </svg>
    )}
  </button>;
}
