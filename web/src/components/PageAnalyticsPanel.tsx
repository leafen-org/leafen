import { X } from "@/icons/leafen";
"use client";

import { useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";

import { useOverlayLayer } from "@/lib/overlayLayer";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import type { PageAnalyticsPanelService } from "@/lib/pageAnalyticsView";

import type { Session } from "@/types";

import { PageAnalyticsTab } from "./PageAnalyticsTab";
import { TouchPageUpdatesSheet } from "./TouchPageUpdatesSheet";
import { PageUpdatesList, type PageUpdatesLockTarget, type PageUpdatesSource } from "./PageUpdatesList";
import type { PageUpdateRecordProperty } from "@/lib/pageUpdateRecord";
import styles from "./PageAnalyticsPanel.module.css";
import headStyles from "./PageUpdatesHead.module.css";

type AnalyticsTab = "updates" | "analytics";

export function PageAnalyticsPanel({
  initialTab = "updates",
  docked = false,
  pageId,
  rowId,
  containerId,
  workspaceId,
  onClose,
  onOpenPageHistory,
  service,
  updates,
}: {
  initialTab?: AnalyticsTab;
  /**
   * Beside the open page, as Notion's (seen 2026-11-01): it takes the page's
   * right side under its bar without covering or dimming it, begins with its
   * tabs, stays open through Escape and clicks in the page, and the bar's
   * "패널 닫기" closes it. Elsewhere it is a window over the page. A touch
   * screen's, docked or not, is Notion's "페이지 업데이트" sheet (TouchPageUpdatesSheet).
   */
  docked?: boolean;
  pageId: string;
  /** A database row page, shown through the page that holds it. */
  rowId?: string;
  /** An inline database opened alone, whose own analytics the tab shows. */
  containerId?: string;
  workspaceId: string;
  onClose(): void;
  /** Absent where the page has no history of its own to open. */
  /** Opens the page's history, at an update's version when one is named. */
  onOpenPageHistory?(versionId?: string): void;
  service: PageAnalyticsPanelService;
  /** The page's updates, read from its history: its title, where it is, and who reads them. */
  updates?: { pageTitle: string; path: string; pathLinks?: ReadonlyArray<{ id: string; title: string; href?: string }>; session: Session;
    /** A database row's title property's name, its title change's label (Notion's, seen 2026-10-27). */
    titleLabel?: string;
    /** A database row's properties, whose values its updates show. */
    properties?: readonly PageUpdateRecordProperty[];
    /** Whether the reader edits a row's page, which lets them resolve its discussions others began. */
    canEdit?: boolean;
    /** An inline database opened alone, whose own locks are listed. */
    lockTarget?: PageUpdatesLockTarget;
    /** The database the page is — a wiki's page — whose own updates are listed with the page's. */
    source?: PageUpdatesSource };
}) {
  const { t } = useTranslation("pageAnalytics");
  const titleId = useId();
  // A guest of the page's workspace reads its updates alone: Notion draws a
  // guest's panel — one who edits the page or has full access alike — with
  // only its "업데이트" head, no analytics tab (seen 2026-11-28 in Chrome at
  // 1440, two tabs at different times), and the server keeps the analytics
  // from them (page_analytics_readers.go).
  const guest = useProductPagesSelect((pages) => pages?.workspaces?.find(({ id }) => id === workspaceId)?.guest === true);
  const shownTabs: readonly AnalyticsTab[] = guest ? ["updates"] : ["updates", "analytics"];
  const [chosen, setTab] = useState<AnalyticsTab>(initialTab);
  const tab = guest ? "updates" : chosen;
  const tabListRef = useRef<HTMLDivElement>(null);
  const tabList = useNativeTabs<AnalyticsTab>({
    tabs: shownTabs.map((value) => ({ key: value, text: t(`tabs.${value}`) })),
    selected: tab, onSelect: setTab, label: t("title"), listRef: tabListRef,
  });
  // A modal layer: Escape closes it once nothing opened over it takes the
  // key, and the focus goes back to what opened it.
  const panelRef = useRef<HTMLElement>(null);
  const { touch } = useSidebarLayout(true);
  const { zIndex } = useOverlayLayer(!docked && !touch, { kind: "dialog", ref: panelRef, modalOwned: () => [panelRef.current?.previousElementSibling], onEscape: onClose });
  const updatesList = updates ? (
    <PageUpdatesList pageId={rowId ?? pageId} row={Boolean(rowId)} pageTitle={updates.pageTitle} path={updates.path}
      rowOf={rowId ? { containerPageId: pageId, workspaceId, canEditPage: updates.canEdit === true } : undefined}
      pathLinks={updates.pathLinks} titleLabel={updates.titleLabel} properties={updates.properties} lockTarget={updates.lockTarget} source={updates.source} session={updates.session}
      // A touch screen's updates have no version button, as Notion's phone and iPad (TouchPageUpdatesSheet).
      onOpenVersion={touch ? undefined : onOpenPageHistory} onNavigate={onClose} />
  ) : <p>{t("updates.description")}</p>;
  if (touch) return <TouchPageUpdatesSheet pageId={rowId ?? pageId} session={updates?.session} listClassName={styles.updates} onClose={onClose}>{updatesList}</TouchPageUpdatesSheet>;

  const panel = (
    <>
      {docked ? null : (
        <button
          type="button"
          className={styles.backdrop}
          style={zIndex === undefined ? undefined : { zIndex: zIndex - 1 }}
          aria-hidden="true"
          tabIndex={-1}
          onClick={onClose}
        />
      )}
      <aside
        ref={panelRef}
        style={zIndex === undefined ? undefined : { zIndex }}
        className={`${styles.panel} ${docked ? styles.docked : ""}`}
        role={docked ? "complementary" : "dialog"}
        aria-modal={docked ? undefined : "true"}
        aria-labelledby={titleId}
        data-docked={docked ? "true" : undefined}
        data-page-side-panel={docked ? "docked" : undefined}
      >
        {docked && guest ? <h2 id={titleId} className={headStyles.head} data-page-updates-head="">{t("tabs.updates")}</h2>
        : docked ? <h2 id={titleId} className={styles.dockedTitle}>{t("title")}</h2> : (
          <header className={styles.header}>
            <h2 id={titleId}>{t(guest ? "tabs.updates" : "title")}</h2>
            <button type="button" className={styles.close} aria-label={t("close")} onClick={onClose}>
              <X size="small" aria-hidden="true" />
            </button>
          </header>
        )}
        {guest ? null : (
          <div {...tabList.tabListProps} ref={tabListRef} className={styles.tabs}>
            {shownTabs.map((value) => (
              <NativeTab key={value} state={tabList.state} tabKey={value} className={styles.tab}
                data-active={tab === value ? "true" : undefined}>
                {t(`tabs.${value}`)}
              </NativeTab>
            ))}
          </div>
        )}

        {tab === "updates" ? (
          <div className={styles.updates} role={guest ? undefined : "tabpanel"}>
            {updatesList}
          </div>
        ) : <PageAnalyticsTab pageId={pageId} rowId={rowId} containerId={containerId} workspaceId={workspaceId} service={service} />}
      </aside>
    </>
  );

  return typeof document === "undefined" ? panel : createPortal(panel, document.body);
}
