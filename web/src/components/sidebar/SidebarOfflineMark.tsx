import { useTranslation } from "react-i18next";

import { CloudOffIcon } from "@/icons/leafen";
import { useOfflineMarkHost, useOfflinePill } from "@/lib/offlinePill";
import { useSidebarLayout } from "@/components/shell/AppShell";

import { styles } from "./sidebarStyles";

/**
 * Notion's struck-out cloud after the workspace's name while the connection
 * is lost, at the sidebar's foot and in a phone's drawer alike (seen
 * 2026-11-14), beside the top bar's "오프라인" (offlinePill.ts). Notion's
 * opens a small card saying so when pressed; here, inside the switcher's
 * button, the same words are its tip. While a phone's drawer is open it is
 * what says so: the notice at the window's foot steps aside, as it covered
 * the drawer's foot.
 */
export function SidebarOfflineMark() {
  const { t } = useTranslation("syncStatusBadge");
  const { phone, sidebarOpen } = useSidebarLayout(true);
  useOfflineMarkHost(phone && sidebarOpen);
  const pill = useOfflinePill();
  if (!pill) return null;
  return (
    <span className={styles.wsOffline} role="img" aria-label={t("offlineMark.title")} data-sidebar-offline
      data-icon-tip={`**${t("offlineMark.title")}**\n${t("offlineMark.body")}`} data-tip-wrap>
      <CloudOffIcon size="regular" aria-hidden="true" />
    </span>
  );
}
