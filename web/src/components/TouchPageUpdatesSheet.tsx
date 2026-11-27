import { useId, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { TouchSheetLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { PageNotifyBell } from "@/components/comments/PageNotifyBell";
import type { Session } from "@/types";
import styles from "./TouchPageUpdatesSheet.module.css";

/**
 * A touch screen's page updates, as Notion's phone and iPad (seen 2026-11-13
 * in iPhone Safari at 402 and iPad Safari at 834 and 1210, measured again
 * 2026-11-15 at 402): the "페이지 업데이트" sheet with "완료" — a phone's
 * whole, a tablet's in the middle (TouchSheetLayout) — over the page rather
 * than beside it, its "페이지 알림 설정" first and the updates under it,
 * with no analytics, and no clock at an update's end to open its version
 * (seen 2026-11-15 at 402 and 834; the computer's panel keeps it).
 */
export function TouchPageUpdatesSheet({ pageId, session, listClassName, onClose, children }: {
  /** The page, or a database row's page, whose notifications are set. */
  pageId: string;
  session?: Session;
  /** The panel's own list class, which lays its updates out (PageAnalyticsPanel.module.css). */
  listClassName: string;
  onClose(): void;
  /** The updates' list. */
  children: ReactNode;
}) {
  const { t } = useTranslation(["rowMenu", "common", "commentsPanel"]);
  const labelId = useId();
  return (
    <TouchSheetLayout>
      <PhoneSheet title={t("rowMenu:phone.updates")} action={{ label: t("common:actions.done"), onPress: onClose }} onClose={onClose}
        bodyClassName={styles.body} data={{ "data-page-updates-sheet": "" }}>
        {session ? (
          <div className={styles.notify}>
            <span className={styles.notifyLabel} id={labelId}>{t("commentsPanel:discussions.notifications")}</span>
            <PageNotifyBell pageId={pageId} session={session}
              field={{ labelledBy: labelId, className: styles.notifyField, menuClassName: styles.notifyMenu }} />
          </div>
        ) : null}
        <div className={`${listClassName} ${styles.updates}`}>{children}</div>
      </PhoneSheet>
    </TouchSheetLayout>
  );
}
