import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { ArrowLeft, ArrowRight } from "@/icons/leafen";
import { useHistoryReach } from "@/lib/historyReach";
import { styles } from "./topBarStyles";

/**
 * ← and → after the sidebar's mark on a touch screen wider than a phone's,
 * once the tab has moved within the app, as Notion's iPad (seen 2026-11-16 in
 * iPad Safari 834: two 35×44 buttons 14 after the sidebar's mark, their 24px
 * arrows, the path 2 after them; one that leads nowhere is dim and does
 * nothing). A phone's bar and a pointer's have none; Leafen's Mac app has
 * them in its tab strip.
 */
export function TopBarHistoryArrows() {
  const { t } = useTranslation("topBar");
  const { frame, phone, touch } = useSidebarLayout(true);
  const reach = useHistoryReach();
  if (!frame || !touch || phone || !reach.moved) return null;
  return (
    <div className={styles.historyArrows} data-topbar-history>
      <button type="button" className={styles.historyArrow} aria-label={t("goBack")} aria-disabled={reach.back ? undefined : true}
        data-topbar-history-back onClick={() => { if (reach.back) window.history.back(); }}>
        <ArrowLeft size="regular" aria-hidden="true" />
      </button>
      <button type="button" className={styles.historyArrow} aria-label={t("goForward")} aria-disabled={reach.forward ? undefined : true}
        data-topbar-history-forward onClick={() => { if (reach.forward) window.history.forward(); }}>
        <ArrowRight size="regular" aria-hidden="true" />
      </button>
    </div>
  );
}
