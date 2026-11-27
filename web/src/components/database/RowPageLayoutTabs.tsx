import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import type { DatabasePageLayout, PageLayoutTabView as TabView } from "@/lib/databasePageLayout";
import { showDatabasePageTab, useShownDatabasePageTab } from "@/lib/databasePageTab";
import type { Page } from "@/lib/types";
import { useSidebarLayout } from "../shell/AppShell";
import { PhoneSheet, PhoneSheetGroup } from "../shell/PhoneSheet";
import { databaseViewLabels } from "./databaseViewLabels";
import sheetStyles from "./DatabaseViewPhoneSheets.module.css";
import { ContentTabIcon } from "./PageLayoutContentTabFields";
import { ViewTypeIcon } from "./databaseViewShared";
import { LayoutTabStrip, type LayoutStripTab } from "./LayoutTabStrip";
import { CONTENT_TAB_KEY, layoutTabKeys } from "./pageLayoutTabOrder";
import { PageLayoutTabView } from "./PageLayoutTabView";
import type { PropertyValueReadContext } from "./PropValue";
import { RowPageDataTab } from "./RowPageDataTab";
import styles from "./RowPageLayout.module.css";
import "./rowPageTabDatabaseBox.css";

/**
 * A tabbed layout's tabs under the title, as Notion's (seen 2026-10-12):
 * "콘텐츠" holds the page's modules, each other tab a data source's pages in
 * its view, whose bar sits beside the tabs. A simple layout, or one without
 * tabs beside the content, has none (seen again 2026-11-01). On a phone the
 * tabs are one pill, the open tab's icon and name with "⌄", that opens "N개
 * 보기" with the open one ticked, for a member and a published page's
 * visitor alike (Notion's, seen 2026-11-12 at 402pt). The tab shown is the
 * one its database's pages last showed (lib/databasePageTab).
 */
export function RowPageLayoutTabs({ layout, row, context, onSaveTabView, underDetails = false, memory, children }: {
  layout: DatabasePageLayout;
  row: Page | undefined;
  context: PropertyValueReadContext;
  /** The data source whose pages share the tab shown; none, a tab of this page's alone. */
  memory?: string;
  /** Right under "세부 정보 보기", which the tabs or pill stand under as under the title (RowPageLayout.module.css). */
  underDetails?: boolean;
  /** Keeps a tab's view for every page; none where the reader may not change the database. */
  onSaveTabView?: (tabId: string, view: TabView) => Promise<void>;
  /** The page's modules, the content tab's. */
  children: ReactNode;
}) {
  const { t } = useTranslation("rowProperties");
  const [own, setOwn] = useState<string | null>(null);
  const remembered = useShownDatabasePageTab(memory);
  const chosen = memory ? remembered : own;
  const setChosen = (id: string | null) => { if (memory) showDatabasePageTab(memory, id); else setOwn(id); };
  const { phone } = useSidebarLayout(true);
  const [picking, setPicking] = useState(false);
  if (layout.structure !== "tabbed" || layout.tabs.length === 0) return <>{children}</>;
  const open = layout.tabs.find((tab) => tab.id === chosen);
  // The content tab under the name and icon the layout gives it, its description in its tip.
  const content = layout.contentTab?.name || t("layoutContentTab");
  const icon = (item: (typeof layout.tabs)[number] | undefined) => item
    ? <ViewTypeIcon type={item.view?.type ?? "table"} size="regular" /> : <ContentTabIcon tab={layout.contentTab} size="regular" />;
  const name = (item: (typeof layout.tabs)[number] | undefined) => item ? item.name || t("untitledPage") : content;
  // Another tab open, the content's modules stay, unseen: its details panel, a column of the page's frame,
  // stays open beside the tab, as Notion's (seen 2026-11-12).
  const kept = <div hidden>{children}</div>;
  // The tabs in the order they stand, the content tab among them (pageLayoutTabOrder), undefined for it.
  const ordered = layoutTabKeys(layout).map((key) => layout.tabs.find(({ id }) => id === key));
  if (phone) {
    const labels = databaseViewLabels();
    const sheetRow = (item: (typeof layout.tabs)[number] | undefined) => (
      <div key={item?.id ?? "content"} className={sheetStyles.viewRow} data-row-layout-tab-row={item?.id ?? "content"}>
        <button type="button" className={sheetStyles.viewPick} aria-current={open?.id === item?.id ? "true" : undefined}
          onClick={() => { setChosen(item?.id ?? null); setPicking(false); }}>
          <span className={sheetStyles.viewIcon} aria-hidden="true">{icon(item)}</span>
          <span className={sheetStyles.viewName}>{name(item)}</span>
        </button>
        {open?.id === item?.id ? <CheckIcon size="page" aria-hidden="true" className={sheetStyles.viewTick} data-alone="true" /> : null}
      </div>
    );
    const pill = (
      <>
        <button type="button" className={styles.phoneTabPill} aria-haspopup="dialog" aria-label={t("layoutTabs")}
          onClick={() => setPicking(true)} data-row-layout-tab-pill data-under-details={underDetails || undefined}>
          {icon(open)}<span>{name(open)}</span><ChevronDown pixels={18} aria-hidden="true" />
        </button>
        {picking ? (
          <PhoneSheet title={labels.viewsCount(layout.tabs.length + 1)} onClose={() => setPicking(false)}
            action={{ label: labels.sheetDone, onPress: () => setPicking(false) }} data={{ "data-row-layout-tabs-sheet": "" }}>
            <PhoneSheetGroup>{ordered.map((item) => sheetRow(item))}</PhoneSheetGroup>
          </PhoneSheet>
        ) : null}
      </>
    );
    if (!open) return <>{pill}<div role="tabpanel">{children}</div></>;
    return (
      <div role="tabpanel" className={styles.tabPanel} data-row-layout-tab-panel={open.id} data-under-details={underDetails || undefined}>
        <PageLayoutTabView key={open.id} tab={open} rowId={row?.id} title={pill} mode="page"
          onSaveView={onSaveTabView ? (view) => onSaveTabView(open.id, view) : undefined}
          fallback={<RowPageDataTab tab={open} row={row} context={context} />} />
        {kept}
      </div>
    );
  }
  // One line, those that don't fit beside the tools under "N개 더 보기" (LayoutTabStrip).
  const stripTabs: LayoutStripTab[] = ordered.map((item) => ({ id: item?.id ?? CONTENT_TAB_KEY, name: name(item), icon: icon(item) }));
  const choose = (id: string) => setChosen(id === CONTENT_TAB_KEY ? null : id);
  const tabs = (
    <LayoutTabStrip className={styles.tabs} tabs={stripTabs} activeId={open?.id ?? CONTENT_TAB_KEY} maxTab={220} onPick={choose}
      attributes={{ role: "tablist", "aria-label": t("layoutTabs"), "data-under-details": underDetails || undefined }}
      renderTab={(item) => (
        <button key={item.id} type="button" role="tab" className={styles.tab} aria-selected={(open?.id ?? CONTENT_TAB_KEY) === item.id}
          onClick={() => choose(item.id)} data-row-layout-tab={item.id === CONTENT_TAB_KEY ? "content" : item.id}
          title={item.id === CONTENT_TAB_KEY && layout.contentTab?.description ? `${content}\n${layout.contentTab.description}` : item.name}>
          {item.icon}<span>{item.name}</span>
        </button>
      )} />
  );
  if (!open) return <>{tabs}<div role="tabpanel">{children}</div></>;
  return (
    <div role="tabpanel" className={styles.tabPanel} data-row-layout-tab-panel={open.id} data-under-details={underDetails || undefined}>
      <PageLayoutTabView key={open.id} tab={open} rowId={row?.id} title={tabs} mode="page"
        onSaveView={onSaveTabView ? (view) => onSaveTabView(open.id, view) : undefined}
        fallback={<RowPageDataTab tab={open} row={row} context={context} />} />
      {kept}
    </div>
  );
}
