import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { announceAppsChanged } from "@/lib/appsChangedEvents";
import type { Session } from "@/types";
import { AppPlaceFrame } from "./AppPlaceFrame";
import { appPlaceName, AppUnavailableNotice, useAppAvailability } from "./AppUnavailable";
import styles from "./AppSidePanel.module.css";
import { usableAppFor, useUsableAppList } from "./useUsableApps";

// An app beside a page (docs/app-platform.md 2), docked where the
// page's discussions dock and closed by the bar's "»" the same way: the app
// knows the page it is beside and reads it with what the app may read. Where
// it can't dock (a phone, pageSidePanelDocks) it is a sheet over the page,
// closed by its own "완료".

export function AppSidePanel({ session, appId, pageId, organizationId, docked = true, onClose }: {
  session: Session;
  appId: string;
  pageId: string;
  organizationId?: string;
  docked?: boolean;
  onClose(): void;
}) {
  const { t, i18n } = useTranslation("common");
  const { apps, loaded } = useUsableAppList(session);
  const usable = usableAppFor(apps.filter((app) => app.sidePanel), appId, organizationId);
  const port = useRef<MessagePort | null>(null);
  const availability = useAppAvailability(session, { app: appId }, organizationId ? `org:${organizationId}` : "", loaded && !usable);
  const name = appPlaceName(usable, usable ? null : availability, i18n.language);

  const body = usable ? (
    <AppPlaceFrame session={session} usable={usable} place="sidePanel" opens={{ surface: "sidePanel" }} title={name} style={{ flex: "1 1 auto", minHeight: 0 }}
      context={() => ({ pageId })} port={port} />
  ) : (
    <div className={styles.state} role="status">{loaded ? <AppUnavailableNotice availability={availability} session={session} onRetry={announceAppsChanged} /> : t("appPlace.loading")}</div>
  );
  if (!docked) {
    return (
      <PhoneSheet title={name || t("appPlace.mark")} action={{ label: t("actions.done"), onPress: onClose }} onClose={onClose}
        bodyClassName={styles.sheetBody} data={{ "data-app-side-panel": appId }}>{body}</PhoneSheet>
    );
  }
  return (
    <aside className={styles.panel} role="complementary" aria-label={name || t("appPlace.mark")} data-app-side-panel={appId}
      data-page-side-panel="docked">
      <div className={styles.header}>
        <span className={styles.title}>{name}</span>
        <span className={styles.mark}>{t("appPlace.mark")}</span>
      </div>
      {body}
    </aside>
  );
}
