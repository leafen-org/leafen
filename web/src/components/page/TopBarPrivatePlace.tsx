import { useTranslation } from "react-i18next";
import { ChevronDown, LockIcon } from "@/icons/leafen";
import { styles } from "./topBarStyles";

/**
 * A private page's place after its path, as Notion's (seen 2026-10-28): a
 * top page only its reader can open reads "🔒 개인 페이지 ⌄", is tipped
 * "본인만 액세스 가능 / 클릭하여 옮기기" beside it, and opens the move
 * popover under it. A page under another, or one others share, has none.
 */
export function TopBarPrivatePlace({ onMove }: { onMove: (from: HTMLElement) => void }) {
  const { t } = useTranslation(["topBar", "sidebar"]);
  return (
    <button type="button" className={styles.privatePlace} data-topbar-private-place aria-haspopup="dialog"
      data-icon-tip={t("topBar:privatePlaceTip")} data-tip-keys={t("topBar:clickToMove")} data-tip-side="end"
      onClick={(event) => onMove(event.currentTarget)}>
      <LockIcon size="small" weight="fill" aria-hidden="true" />
      <span>{t("sidebar:privatePages")}</span>
      <ChevronDown size="micro" aria-hidden="true" />
    </button>
  );
}
