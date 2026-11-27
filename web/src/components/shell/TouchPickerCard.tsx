import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "./AppShell";
import sheet from "./PhoneSheet.module.css";
import styles from "./TouchPickerCard.module.css";

/**
 * Whether a page's cover or icon picker opens as Notion's touch card: on any
 * touch screen, a card in the middle of a tablet's and the whole of a
 * phone's (TouchPickerCard.module.css).
 */
export function useTouchPickerCard() {
  return useSidebarLayout(true).touch;
}

/**
 * Whether that card is a phone's, the whole screen: there a page icon's
 * emoji are a phone's reaction sheet's list (PhoneEmojiSheet), the
 * workspace's own offered with no adding one, as Notion's (seen 2026-11-17
 * in Android Chrome at 411: its bar ends with "사용자 지정", no "추가").
 */
export function usePhonePickerCard() {
  const layout = useSidebarLayout(true);
  return layout.touch && layout.phone;
}

/** The card's class, its shade's and its tab row's, for the picker drawing them. */
export const touchPickerCard = { card: styles.card, shade: styles.shade, tabRow: styles.tabRow };

/**
 * The card's head, as Notion's iPad and iPhone (seen 2026-11-13 in Safari 834
 * and 402 wide: "제거 · 페이지 커버 · 닫기" and "제거 · 페이지 아이콘 · 닫기"): a blue
 * "제거" at its left where there is something to remove, the title in the
 * middle on the head's top line and a blue "닫기" at its right, the phone
 * sheets' head (PhoneSheet.module.css) across the card.
 */
export function TouchPickerHead({ title, removeLabel, onRemove, removeDisabled = false, onClose }: {
  title: ReactNode;
  removeLabel?: string;
  onRemove?: () => void;
  removeDisabled?: boolean;
  onClose(): void;
}) {
  const { t } = useTranslation("common");
  return (
    <div className={`${sheet.head} ${styles.head}`} data-touch-picker-head>
      <span className={sheet.headSide}>
        {onRemove ? (
          <button type="button" className={sheet.headButton} disabled={removeDisabled} onClick={onRemove} data-touch-picker-remove>
            {removeLabel}
          </button>
        ) : null}
      </span>
      <strong className={sheet.title}>{title}</strong>
      <span className={`${sheet.headSide} ${sheet.headEnd}`}>
        <button type="button" className={sheet.headButton} onClick={onClose} data-touch-picker-close>{t("actions.close")}</button>
      </span>
    </div>
  );
}
