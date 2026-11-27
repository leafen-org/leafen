import { useTranslation } from "react-i18next";
import { HoverTip } from "@/components/IconTipButton";
import { teamspaceSecurity, type DialogTeamspace } from "./teamspaceDialogModel";
import { TeamspaceKindIcon } from "./TeamspaceSecurityIcon";
import styles from "./TeamspaceSettings.module.css";

/**
 * The settings window's "N 멤버" under a teamspace's name and, for one that
 * is asked to join ("참가 제한"), its kind's 16px mark 8 after it in the same
 * grey, telling under it that joining asks first — as Notion's (seen
 * 2026-11-16 in Chrome, 1440 dark; an open one shows the count alone).
 */
export function TeamspaceHeadCount({ teamspace, count }: { teamspace: DialogTeamspace; count: number }) {
  const { t } = useTranslation("teamspaceDialog");
  const closed = teamspaceSecurity(teamspace) === "closed";
  return (
    <span className={styles.headCount}>
      <span>{t("head.memberCount", { count })}</span>
      {closed ? (
        <HoverTip label={t("head.closedTip")} width={220} className={styles.headKind}>
          <span data-teamspace-head-kind="closed"><TeamspaceKindIcon security="closed" size="small" /></span>
        </HoverTip>
      ) : null}
    </span>
  );
}
