import { useTranslation } from "react-i18next";

import { useOfflinePill, useOfflineMarkHost } from "@/lib/offlinePill";

import styles from "./TopBarChrome.module.css";

/**
 * "오프라인" at the head of the bar's right side while the connection is lost,
 * as Notion's (seen 2026-11-14, 1440 dark): 14px words in the secondary
 * color in a 24px box with a faint 1px line, round 4, 6px in, the next of
 * the bar 28px after it; its tip says what waits to be sent. Notion's does
 * nothing when pressed; Leafen's tries to connect again at once, which the
 * notice it replaced offered as "다시 시도" (decision 2026-10-01), saying
 * "확인하는 중…" in its place meanwhile, as that button did. A phone's bar
 * has none, as Notion's (seen 2026-11-14 in iPhone Safari 402): its drawer's
 * struck-out cloud says it (SidebarOfflineMark), and the notice at the foot.
 */
export function TopBarOffline({ active }: { active: boolean }) {
  const { t } = useTranslation("syncStatusBadge");
  useOfflineMarkHost(active);
  const pill = useOfflinePill();
  if (!active || !pill) return null;
  const tip = pill.readOnly ? t("offlinePill.tipReadOnly")
    : pill.pending > 0 ? t("offlinePill.tipPending", { count: pill.pending })
    : t("offlinePill.tip");
  return (
    <button type="button" className={styles.offlinePill} data-topbar-offline data-icon-tip={tip}
      data-tip-wrap data-tip-keys={t("offlinePill.retryHint")} aria-busy={pill.retrying || undefined}
      disabled={pill.retrying} onClick={pill.retry}>
      {t(pill.retrying ? "retrying" : "offlinePill.label")}
    </button>
  );
}
