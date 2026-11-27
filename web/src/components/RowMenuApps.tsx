import { NativeActionMenuItem, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { useTranslation } from "react-i18next";
import { appText } from "@/components/admin/administratorAppsApi";
import { AppMenuLines, appLinesFound } from "@/components/apps/AppMenuLines";
import { useUsableAppList } from "@/components/apps/useUsableApps";
import { useProductPages } from "@/components/product/productPagesContext";
import { ServerAppsIcon } from "@/icons/leafen";
import type { Session } from "@/types";
import styles from "./RowMenu.module.css";

/**
 * The page menu's apps (docs/app-platform.md 2·10-1): the lines
 * apps add for pages, and, in the open page's own menu, each app with a
 * panel that opens beside the page, the line naming it as an app with the
 * app's name. The menu's search finds them by their words (appLinesFound).
 */
export function RowMenuApps({ session, pageId, query, onOpenPanel, onClose }: {
  session: Session;
  pageId: string;
  /** What the menu's search holds. */
  query: string;
  onOpenPanel?: (appId: string) => void;
  onClose(): void;
}) {
  const { t, i18n } = useTranslation("rowMenu");
  const product = useProductPages();
  const organizationId = product?.organizationId;
  const { apps } = useUsableAppList(session, Boolean(organizationId));
  const panels = appLinesFound(query, onOpenPanel ? apps.filter((app) => app.sidePanel && app.tenancyKey === `org:${organizationId}`)
    .map((app) => ({ app, label: t("apps.openBeside", { app: appText(app.name, i18n.language) }) })) : [], (panel) => panel.label);
  return (
    <>
      <AppMenuLines menu="page" targetId={pageId} query={query} className={styles.item} dividerClassName={styles.divider}
        hintClassName={styles.itemHint} onDone={onClose} />
      {panels.length && !query.trim() ? <NativeMenuSeparator className={styles.divider} /> : null}
      {panels.map(({ app, label }) => (
        <NativeActionMenuItem key={app.installId} type="button" className={styles.item} data-menu-item role="menuitem"
          data-page-app-panel={app.appId} onClick={() => { onOpenPanel?.(app.appId); onClose(); }}>
          <ServerAppsIcon size="regular" aria-hidden="true" />
          <span>{label}</span>
        </NativeActionMenuItem>
      ))}
    </>
  );
}
