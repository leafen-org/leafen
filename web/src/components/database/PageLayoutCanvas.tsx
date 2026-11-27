import { Fragment, Suspense, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import {
  ArrowDownLeftIcon, ArrowUp, ArrowUpRightIcon, AtIcon, ChevronDown, DotsHorizontal, DoubleChevronLeft, DoubleChevronRight, DragHandleIcon,
  PaperclipIcon, Plus,
} from "@/icons/leafen";
import type { DatabasePageLayout, PageLayoutModule, PageLayoutTabView as TabView } from "@/lib/databasePageLayout";
import type { Page } from "@/lib/types";
import type { Session } from "@/types";
import { lazyScreen } from "@/lib/lazyScreen";
import { galleryPresentationProperty } from "./galleryViewModel";
import type { PropertyValueReadContext } from "./PropValue";
import { PropertyIcon } from "./PropertyIcon";
import { RowPagePropertyValue } from "./RowPagePropertyList";
import { PinnedPropertyLine } from "./RowPagePinnedProperties";
import { layoutHasDetailsPanel, RelationLineContext } from "./RowPageLayoutBody";
import { isHiddenRowProperty, rowPagePropertyOrder } from "./rowPropertiesModel";
import sheet from "./databaseSheet";
import { propertyGroupItems, relationShownAs, type PageLayoutPlace } from "./pageLayoutModel";
import { usePageLayoutDrag } from "./usePageLayoutDrag";
import { PageLayoutTabView } from "./PageLayoutTabView";
import { PageLayoutTabPillContext } from "./pageLayoutTabPill";
import { ViewTypeIcon } from "./databaseViewShared";
import { ContentTabIcon } from "./PageLayoutContentTabFields";
import { LayoutTabStrip } from "./LayoutTabStrip";
import { useLayoutTabListActions } from "./layoutTabListActions";
import { CONTENT_TAB_KEY, layoutTabKeys } from "./pageLayoutTabOrder";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { commentFieldToolsShown, PREVIEW_COMMENT_FIELD } from "@/components/comments/commentFieldTools";
import { RowPageDataTab } from "./RowPageDataTab";
import { RowPageDiscussionsPreview } from "./RowPageComments";
import { ROW_DISCUSSIONS_HEAD, ROW_DISCUSSIONS_HINT } from "./rowDiscussionWords";
import styles from "./DatabasePageLayoutBuilder.module.css";
import moduleStyles from "./DatabasePageLayoutModules.module.css";
import canvasStyles from "./PageLayoutCanvas.module.css";
import phoneStyles from "./PhonePageLayoutBuilder.module.css";
import rowPageStyles from "./DatabaseRowPage.module.css";
import rowLayoutStyles from "./RowPageLayout.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { hasExplicitPageIcon, isPagePictureIcon, PageIconGlyph, type PageIconSource } from "../page/PageIcon";
import { rowPageIconSize } from "./RowPageHeader";
import { plainTitleOf } from "@/lib/closedMentions";

// The body's editor stays out of the code until a builder shows a body.
const PageLayoutContentPreview = lazyScreen(() => import("./PageLayoutContentPreview"));

/** "heading", a module's ID, a tab's as "tab:〈id〉", or nothing (the page's settings). */
export type PageLayoutSelection = "heading" | string | null;

export const TAB_SELECTION = "tab:";
/** The content tab chosen, its settings open under it. */
export const CONTENT_TAB_SELECTION = "content-tab";

// How long a chosen tab's view settings wait for its view to be drawn.
const TAB_SETTINGS_WAIT_MS = 5_000;

/**
 * The builder's previewed page, as Notion's (seen 2026-10-12): framed, its
 * modules each under a light blue head with its name and "⋯", a "+" between
 * the page's modules, and the details panel opened by "«" with "패널에 추가".
 */
export function PageLayoutCanvas({ layout, properties, row, propertyValueContext, selected, onSelect, onModuleMenu, onAdd, adding = null, tabSettings = null, onTabSettings, panelShown = true, onPanel, onDropModule, onAddTab, onTabView, onTabActions, onMoveTab, previewPages, phoneArea, phoneTabs, touch = false, viewer, session, databasePageId }: {
  layout: DatabasePageLayout;
  /** Reads the previewed page's body for the "콘텐츠" module, and its discussions for "페이지". */
  session?: Session;
  /** The database's page, which holds its pages' discussions. */
  databasePageId?: string;
  /** Who is looking: the comment line's picture is theirs, as Notion's (seen 2026-10-25). */
  viewer?: { id: string; name: string };
  properties: readonly GraphProperty[];
  row: Page | undefined;
  propertyValueContext: PropertyValueReadContext;
  selected: PageLayoutSelection;
  onSelect(selection: PageLayoutSelection): void;
  onModuleMenu(module: PageLayoutModule, button: HTMLButtonElement): void;
  onAdd(place: PageLayoutPlace, index: number, button: HTMLButtonElement): void;
  /**
   * Where an add menu is open: its "+" there is the blue slot the card will
   * take while it is, as Notion's (seen 2026-11-12), which the menu opens beside.
   */
  adding?: PageLayoutPlace | null;
  /**
   * A data tab's view settings asked for: its pill pressed again, or the tab
   * just added; each request opens them once (tabs are otherwise only chosen).
   */
  tabSettings?: { tabId: string; request: number } | null;
  /** The shown data tab's pill pressed again. */
  onTabSettings?(tabId: string): void;
  /** Whether the panel's column is unfolded: the builder's own, not the layout's. */
  panelShown?: boolean;
  onPanel(open: boolean): void;
  /** A module dragged by its "⠿" lands before or after another. */
  onDropModule(moduleId: string, targetId: string, after: boolean): void;
  /** The tabbed structure's "+" beside its tabs; a phone's "새 보기" has no button to stand by. */
  onAddTab(button?: HTMLButtonElement): void;
  /** A tab's view changed in its settings. */
  onTabView(tabId: string, view: TabView, name: string): void;
  /** The tab's view settings' "보기 복제 · 보기 삭제". */
  onTabActions(tabId: string): { duplicate(): string; remove(): void };
  /** A tab, the content tab too, dropped before or after another in "N개 더 보기". */
  onMoveTab?(source: string, target: string, side: "before" | "after"): void;
  /** The page each tab just connected lives on, read for its preview. */
  previewPages: Readonly<Record<string, string>>;
  /**
   * On a phone, one area's modules alone, as Notion's (seen 2026-10-24 at
   * 375px): no frame, handles or "⋯" — a module pressed is selected, and the
   * builder opens its sheet.
   */
  phoneArea?: PageLayoutPlace;
  /**
   * A phone's tabs, as Notion's (seen 2026-10-25 at 375px): one grey pill of
   * the tab shown, which opens the sheet of them all.
   */
  phoneTabs?: { openTabId: string | null; onOpen(): void };
  /**
   * A wide touch screen's: both areas side by side, as Notion's iPad (seen
   * 2026-11-14 at 834 and 1210 wide), but with nothing that waits for a
   * hover — no handles or "⋯" on the heads, no comment box tools and no "»"
   * folding the panel; a card pressed is selected and the builder opens its sheet.
   */
  touch?: boolean;
}) {
  const { t } = useTranslation(["pageLayout", "commentsPanel"]);
  const drag = usePageLayoutDrag([...layout.page, ...layout.panel].map((module) => module.id), onDropModule);
  const property = (id: string) => properties.find((candidate) => candidate.id === id);
  // With no page to preview (a grouped view's, an empty database's), the page "새 페이지" would make.
  const viewerId = viewer?.id;
  const valueRow = useMemo(() => row ?? newPagePreview(viewerId), [row, viewerId]);
  const value = (id: string) => {
    const index = properties.findIndex((candidate) => candidate.id === id);
    if (index < 0) return null;
    const found = properties[index];
    return (
      <RowPagePropertyValue property={found} presentationProperty={galleryPresentationProperty(found, index)}
        value={{ value: valueRow.properties?.[found.id] }} valueRow={valueRow} propertyValueContext={propertyValueContext} />
    );
  };
  const propertyLine = (id: string) => {
    const found = property(id);
    if (!found) return null;
    return (
      // The database page's own property line, so the preview is the page.
      <div key={id} className={rowPageStyles.rowProperty} data-layout-preview-property={id}>
        <span className={rowPageStyles.rowPropertyName}>
          {layout.options.propertyIcons ? (
            <span className={rowPageStyles.rowPropertyGlyph} aria-hidden="true"><PropertyIcon property={found} size="regular" /></span>
          ) : null}
          <span>{found.name}</span>
        </span>
        <span className={`${rowPageStyles.rowPropertyValue} ${rowPageStyles.rowPropertyValueInset}`}>{value(id)}</span>
      </div>
    );
  };
  // A phone's area, or a wide touch screen's columns: the heads' names alone.
  const bare = Boolean(phoneArea) || touch;
  const head = (name: string, menu: ReactNode, moduleId?: string) => bare ? (
    <div className={moduleStyles.moduleHead}><span>{name}</span></div>
  ) : (
    <div className={moduleStyles.moduleHead}>
      {moduleId ? (
        <span role="button" tabIndex={0} className={moduleStyles.dragHandle} aria-label={t("dragModule", { name })}
          onClick={(event) => event.stopPropagation()} {...drag.handle(moduleId)} data-layout-drag={moduleId}>
          <DragHandleIcon size="tiny" aria-hidden="true" />
        </span>
      ) : <DragHandleIcon size="tiny" aria-hidden="true" />}
      <span>{name}</span>
      {menu}
    </div>
  );
  const menuButton = (module: PageLayoutModule | null) => (
    <button type="button" aria-label={t("moduleMenu")} onClick={(event) => {
      event.stopPropagation();
      if (module) onModuleMenu(module, event.currentTarget);
      else onSelect("heading");
    }}>
      <DotsHorizontal size="small" aria-hidden="true" />
    </button>
  );
  const moduleName = (module: PageLayoutModule) => module.kind === "properties" ? t("modules.properties")
    : module.kind === "comments" ? t("modules.comments") : module.kind === "content" ? t("modules.content")
      : module.kind === "relations" ? t("modules.relations") : property(module.propertyId)?.name ?? "";
  // A page section's "↗ 기존 항목 연결 · + 새 페이지 추가하기", drawn only.
  const sectionFoot = (
    <span data-relation-section-foot aria-hidden="true">
      <span data-relation-section-action><ArrowUpRightIcon size="small" />{t("propertyCell:linkExisting")}</span>
      <span data-relation-section-action><Plus size="small" />{t("propertyCell:addNewPage")}</span>
    </span>
  );
  const moduleBody = (module: PageLayoutModule): ReactNode => {
    switch (module.kind) {
      case "properties": {
        // Those set to hide wait behind "속성 N개 더 보기", in the page's own order, as on the page.
        const hides = (id: string) => {
          const index = properties.findIndex((candidate) => candidate.id === id);
          return row ? isHiddenRowProperty(row, galleryPresentationProperty(properties[index], index)) : Boolean(properties[index].config?.hideInPagePanel);
        };
        let more = 0;
        const ordered = rowPagePropertyOrder(properties.filter((item) => item.type !== "title").map((property) => ({ property })));
        const lines = propertyGroupItems(layout, ordered.map(({ property }) => property.id)).map((item) => {
          if (item.kind === "section") return <div key={item.id} className={moduleStyles.section}><span>{item.name || t("newSection")}</span><ChevronDown size="tiny" aria-hidden="true" /></div>;
          if (!property(item.propertyId)) return null;
          if (hides(item.propertyId)) { more += 1; return null; }
          return propertyLine(item.propertyId);
        });
        return <>{lines}{more > 0 ? (
          <div className={rowPageStyles.rowProperty} data-layout-preview-more>
            <span className={sheet.rowHiddenProperties}>
              <ChevronDown className={sheet.rowHiddenPropertiesIcon} size="tiny" aria-hidden="true" />{t("rowProperties:moreProperties", { n: more })}
            </span>
          </div>
        ) : null}</>;
      }
      case "property":
        // A relation of its own is a page section as on the page, empty too (Notion's builder, seen 2026-11-13).
        if (property(module.propertyId)?.type === "relation") {
          return <div data-relation-line="section">{propertyLine(module.propertyId)}{sectionFoot}</div>;
        }
        // Its value alone under the module's name, as Notion's builder shows it (seen 2026-11-12).
        return property(module.propertyId) ? (
          <span className={`${rowPageStyles.rowPropertyValue} ${rowPageStyles.rowPropertyValueInset}`}
            data-layout-preview-property={module.propertyId} data-layout-preview-value>{value(module.propertyId)}</span>
        ) : null;
      case "relations": {
        // As the page draws them: "관계형" over the minimized ones' lines, a section under its own name (Notion's, seen 2026-11-06).
        const shown = module.relations.map((relation) => ({ ...relation, display: relationShownAs(relation.display, row?.properties?.[relation.propertyId]) }));
        return <div className={moduleStyles.comments} data-layout-relations>
          {shown.some(({ display }) => display === "minimized") ? <div data-relations-heading>{t("relationsHeading")}</div> : null}
          {shown.map((relation) => <RelationLineContext.Provider key={relation.propertyId} value={relation.display === "minimized"}>
            <div data-relation-line={relation.display}>
              {propertyLine(relation.propertyId)}
              {relation.display === "section" ? sectionFoot : null}
            </div>
          </RelationLineContext.Provider>)}
        </div>;
      }
      case "comments":
        return <div className={moduleStyles.comments}><div>{t(ROW_DISCUSSIONS_HEAD)}</div>
          {/* The previewed page's open discussions over its field, read and drawn as the page shows them (Notion's builder, seen 2026-11-25). */}
          {session && row && databasePageId ? <RowPageDiscussionsPreview key={row.id} rowId={row.id} containerPageId={databasePageId}
            session={session} workspaceId={row.workspaceId} className={moduleStyles.discussions} /> : null}
          <div className={moduleStyles.commentLine}>
          <PersonAvatar userId={viewer?.id} name={viewer?.name} size={24} />
          <span className={moduleStyles.placeholder}>{t(ROW_DISCUSSIONS_HINT)}</span>
          {/* A field never focused nor typed in: its tools follow the page's one rule (none, as Notion's builder). */}
          {commentFieldToolsShown(PREVIEW_COMMENT_FIELD) && <span className={moduleStyles.commentTools} aria-hidden="true">
            <span><PaperclipIcon size="regular" /></span><span><AtIcon size="regular" /></span>
            <span><span className={moduleStyles.commentSend}><ArrowUp size="tiny" /></span></span>
          </span>}
        </div></div>;
      case "content": {
        const empty = <div className={`${moduleStyles.placeholder} ${moduleStyles.emptyContent}`}>{t("empty")}</div>;
        return session && row ? (
          <Suspense fallback={empty}>
            <PageLayoutContentPreview key={row.id} session={session} pageId={row.id} workspaceId={row.workspaceId} empty={empty} />
          </Suspense>
        ) : empty;
      }
    }
  };
  // The page's "+" sits after its property group and the relations groups right after it, as Notion's
  // (seen 2026-11-07: after "관계형 그룹"), or first where the group is in the panel.
  let addAt = layout.page.findIndex((module) => module.kind === "properties") + 1;
  while (addAt > 0 && layout.page[addAt]?.kind === "relations") addAt += 1;
  const slot = (place: PageLayoutPlace, alone = false) => (
    <div key="adder" className={canvasStyles.addSlot} data-alone={alone || undefined} aria-hidden="true" data-layout-add-slot={place} />
  );
  // A desktop's "+" is Notion's 32px blue circle round a 20px plus (seen 2026-11-12), the panel's as the page's.
  const roundAdd = (place: PageLayoutPlace, index: number, data: Record<string, string>) => (
    <div className={moduleStyles.adder} key="adder">
      <button type="button" aria-label={t("addPageModule")} onClick={(event) => onAdd(place, index, event.currentTarget)} {...data}>
        <Plus size={phoneArea ? "small" : "regular"} aria-hidden="true" />
      </button>
    </div>
  );
  const adder = adding === "page" ? slot("page") : roundAdd("page", addAt, { "data-layout-add-module": "" });
  // The page's discussion shows while "페이지" is on, as the page itself does;
  // Notion's card goes with it (seen 2026-10-24).
  const modules = (list: readonly PageLayoutModule[], place: PageLayoutPlace) => list.map((module, index) => module.kind === "comments" && !layout.options.pageComments ? (
    place === "page" && index === addAt && addAt === 0 ? <Fragment key={module.id}>{adder}</Fragment> : null
  ) : (
    <Fragment key={module.id}>
      {place === "page" && index === addAt && addAt === 0 ? adder : null}
      <section className={moduleStyles.module} data-selected={selected === module.id ? "true" : "false"} data-layout-module={module.kind}
        data-layout-module-id={module.id} onClick={() => onSelect(module.id)} {...drag.target(module.id)}>
        {head(moduleName(module), menuButton(module), module.id)}
        <div className={moduleStyles.moduleBody}>{moduleBody(module)}</div>
      </section>
      {place === "page" && index + 1 === addAt ? adder : null}
    </Fragment>
  ));
  const pinned = layout.heading.pinned.filter((id) => property(id));
  // The previewed page's icon over its backlinks, as its page heads with it (Notion's builder, seen 2026-11-14).
  const rowIcon: PageIconSource | null = row ? { icon: row.icon ?? "", iconType: row.iconType as PageIconSource["iconType"] } : null;
  const pageIcon = rowIcon && hasExplicitPageIcon(rowIcon) ? rowIcon : null;
  // "세부 정보 보기" under the title while the panel holds something, as the page shows it and Notion's builder (seen 2026-11-14).
  const details = layoutHasDetailsPanel(layout, properties.filter((item) => item.type !== "title").map((item) => item.id));
  const openTab = phoneTabs ? layout.tabs.find((tab) => tab.id === phoneTabs.openTabId)
    : typeof selected === "string" && selected.startsWith(TAB_SELECTION) ? layout.tabs.find((tab) => `${TAB_SELECTION}${tab.id}` === selected) : undefined;
  // A tab is chosen with its settings closed; they open as it is added or its
  // pill is pressed again, as Notion's (seen 2026-11-12, three tabs, each way
  // three times); a phone's open from its sheet's "⋯". Its view is drawn once
  // its source is read: the settings open as its "설정" comes, within a bound.
  const frame = useRef<HTMLDivElement>(null);
  const phoneTabbed = Boolean(phoneTabs);
  const shownTab = useRef(openTab?.id);
  shownTab.current = openTab?.id;
  useEffect(() => {
    if (!tabSettings || phoneTabbed || tabSettings.tabId !== shownTab.current) return undefined;
    const root = frame.current;
    if (!root) return undefined;
    const open = () => {
      const trigger = root.querySelector<HTMLElement>("[data-layout-module=tab] [data-database-settings-trigger]");
      if (!trigger) return false;
      stop();
      trigger.click();
      return true;
    };
    const watch = new MutationObserver(() => { open(); });
    const timer = window.setTimeout(() => stop(), TAB_SETTINGS_WAIT_MS);
    const first = window.setTimeout(() => { if (!open()) watch.observe(root, { childList: true, subtree: true }); }, 0);
    function stop() {
      watch.disconnect();
      window.clearTimeout(timer);
      window.clearTimeout(first);
    }
    return stop;
  }, [tabSettings, phoneTabbed]);
  // Its settings stand under its pill (pageLayoutTabPill); a phone's are a sheet.
  const openTabId = openTab?.id;
  const openPill = useCallback(() => openTabId ? frame.current?.querySelector<HTMLElement>(`[data-layout-tab="${CSS.escape(openTabId)}"]`) ?? null : null,
    [openTabId]);
  // The tabs' row, on one line with those that don't fit under "N개 더 보기" (LayoutTabStrip); a data tab's
  // view takes it as its title, as the page itself does (RowPageLayoutTabs).
  const shownStripTab = openTab?.id ?? CONTENT_TAB_KEY;
  const press = (id: string) => id === CONTENT_TAB_KEY ? onSelect(!openTab && selected !== CONTENT_TAB_SELECTION ? CONTENT_TAB_SELECTION : null)
    : openTab?.id === id ? onTabSettings?.(id) : onSelect(`${TAB_SELECTION}${id}`);
  // In the order they stand, the content tab among them (pageLayoutTabOrder).
  const stripTabs = layoutTabKeys(layout).flatMap((key) => {
    const tab = layout.tabs.find(({ id }) => id === key);
    return key === CONTENT_TAB_KEY ? [{ id: key, name: layout.contentTab?.name || t("contentTab"), icon: <ContentTabIcon tab={layout.contentTab} size="regular" /> }]
      : tab ? [{ id: tab.id, name: tab.name || t("untitled"), icon: <ViewTypeIcon type={tab.view?.type ?? "table"} size="regular" /> }] : [];
  });
  // Its "N개 더 보기" stays open as the row is drawn anew under another tab.
  const [tabList, setTabList] = useState(false);
  const rowActions = useLayoutTabListActions({ nameOf: (id) => stripTabs.find((tab) => tab.id === id)?.name ?? "",
    onChooseContent: () => onSelect(CONTENT_TAB_SELECTION), onChoose: (id) => onSelect(`${TAB_SELECTION}${id}`),
    onSettings: (id) => onTabSettings?.(id), onTabActions });
  const tabRow = layout.structure === "tabbed" ? (
    <LayoutTabStrip className={canvasStyles.tabs} gap={5} attributes={{ "data-layout-tabs": "" }} activeId={shownStripTab} tabs={stripTabs}
      onPick={(id) => { if (id !== shownStripTab) press(id); }} onAdd={onAddTab} onMove={onMoveTab} rowActions={rowActions}
      listOpen={tabList} onListOpen={setTabList}
      // The shown tab's pill pressed again opens its settings, and once more closes them.
      renderTab={(tab) => (
        <button key={tab.id} type="button" className={canvasStyles.tab} aria-pressed={tab.id === shownStripTab} onClick={() => press(tab.id)}
          data-layout-tab={tab.id === CONTENT_TAB_KEY ? "content" : tab.id}>{tab.icon}{tab.name}</button>
      )}
      after={<button type="button" className={canvasStyles.tabAdd} aria-label={t("addTab")} onClick={(event) => onAddTab(event.currentTarget)} data-layout-add-tab>
        <Plus size="small" aria-hidden="true" />
      </button>} />
  ) : null;
  const tabView = (title: ReactNode) => openTab ? (
    <PageLayoutTabView key={openTab.id} tab={openTab} rowId={row?.id} title={title} mode="builder"
      onSaveView={(view, name) => onTabView(openTab.id, view, name)} actions={onTabActions(openTab.id)}
      previewPageId={previewPages[openTab.id]}
      fallback={<RowPageDataTab tab={openTab} row={row} context={propertyValueContext} unloaded={t("tabAfterApply")} />} />
  ) : null;
  const pageColumn = (
    <>
      <section className={moduleStyles.module} data-selected={selected === "heading" ? "true" : "false"} data-layout-module="heading"
        data-layout-module-id="heading" onClick={() => onSelect("heading")}>
        <div className={moduleStyles.moduleHead}><span>{t("modules.heading")}</span>{bare ? null : menuButton(null)}</div>
        <div className={moduleStyles.moduleBody}>
          {pageIcon ? (
            <span className={canvasStyles.pageIcon} data-icon-picture={isPagePictureIcon(pageIcon) || undefined} aria-hidden="true" data-layout-page-icon>
              <PageIconGlyph page={pageIcon} size={rowPageIconSize(pageIcon)} box={rowPageIconSize(pageIcon)} fallback="none" />
            </span>
          ) : null}
          <div className={moduleStyles.backlinks}><ArrowDownLeftIcon pixels={phoneArea ? 18 : 16} aria-hidden="true" />{t("noBacklinks")}</div>
          {/* An untitled page's name in the title's placeholder color, as its page and Notion's builder show it (seen 2026-11-14). */}
          <h1 className={`${moduleStyles.title} ${canvasStyles.title}`} data-empty={plainTitleOf(row) ? undefined : "true"}>{plainTitleOf(row) || untitledPageDisplayTitle(row)}</h1>
          {details ? <span className={`${rowLayoutStyles.detailsToggle} ${canvasStyles.detailsToggle}`} data-layout-details-toggle>
            {t("rowProperties:layoutDetailsShow")}</span> : null}
          {pinned.length > 0 ? (
            <PinnedPropertyLine shown={pinned.flatMap((id) => property(id) ?? [])} labels={layout.heading.labels}
              icons={layout.options.propertyIcons} value={(shown) => value(shown.id)} preview />
          ) : null}
        </div>
      </section>
      {layout.structure === "tabbed" && phoneTabs ? (
        <button type="button" className={phoneStyles.tabPill} aria-haspopup="dialog" onClick={phoneTabs.onOpen} data-layout-tab-pill>
          {openTab ? <ViewTypeIcon type={openTab.view?.type ?? "table"} size="regular" /> : <ContentTabIcon tab={layout.contentTab} size="regular" />}
          <span>{openTab ? openTab.name || t("untitled") : layout.contentTab?.name || t("contentTab")}</span>
          <ChevronDown pixels={18} aria-hidden="true" />
        </button>
      ) : openTab ? null : tabRow}
      {openTab ? (
        <PageLayoutTabPillContext.Provider value={phoneTabbed ? null : openPill}>
        {phoneTabbed ? (
          <section className={moduleStyles.module} data-layout-module="tab">
            <div className={moduleStyles.moduleBody}>{tabView(<span />)}</div>
          </section>
        ) : (
          // Unframed under the page's cards, the tabs its title row and its tools at their end (DatabaseToolbar).
          <section className={canvasStyles.tabView} data-layout-module="tab">{tabView(tabRow)}</section>
        )}
        </PageLayoutTabPillContext.Provider>
      ) : modules(layout.page, "page")}
    </>
  );
  const panelCards = layout.panel.filter((module) => module.kind !== "comments" || layout.options.pageComments).length;
  // The panel's "패널에 추가" pill while it has no card, then its round "+" under its cards, as Notion's (seen 2026-11-12).
  const addToPanel = adding === "panel" && !phoneArea ? slot("panel", panelCards === 0)
    : !phoneArea && panelCards > 0 ? roundAdd("panel", layout.panel.length, { "data-layout-add-to-panel": "" }) : (
    <button type="button" className={styles.addToPanel} onClick={(event) => onAdd("panel", layout.panel.length, event.currentTarget)}
      data-layout-add-to-panel>
      <Plus size="small" aria-hidden="true" />{t("addToPanel")}
    </button>
  );
  if (phoneArea) {
    return (
      <div ref={frame} className={phoneStyles.area} data-layout-phone-area={phoneArea}>
        {phoneArea === "page" ? pageColumn : <>{modules(layout.panel, "panel")}{addToPanel}</>}
      </div>
    );
  }
  return (
    <div ref={frame} className={`${styles.frame} ${moduleStyles.frame}`} data-panel={panelShown || touch ? "true" : "false"} data-touch={touch || undefined}>
      {!panelShown && !touch ? (
        <button type="button" className={styles.panelToggle} aria-label={t("showPanel")} onClick={() => onPanel(true)} data-layout-panel-toggle>
          <DoubleChevronLeft size="small" aria-hidden="true" />
        </button>
      ) : null}
      <div className={styles.pageColumn} data-layout-page-column><div className={`${styles.modules} ${moduleStyles.modules}`}>{pageColumn}</div></div>
      {panelShown || touch ? (
        <aside className={styles.panel} aria-label={t("panel")} data-layout-panel>
          {touch ? null : (
            <button type="button" className={styles.panelToggle} aria-label={t("hidePanel")} onClick={() => onPanel(false)} data-layout-panel-toggle>
              <DoubleChevronRight size="small" aria-hidden="true" />
            </button>
          )}
          {modules(layout.panel, "panel")}
          {addToPanel}
        </aside>
      ) : null}
    </div>
  );
}

/**
 * The page a database's "새 페이지" would make, previewed where there is none,
 * as Notion's builder shows it (seen 2026-11-14 at 1440, an empty database):
 * made now by the viewer, its ID still to come ("...", uniqueIdText), its
 * status at its default and its formulas worked over nothing; the rest reads
 * "비어 있음".
 */
function newPagePreview(viewerId: string | undefined): Page {
  const now = new Date().toISOString();
  return {
    id: "", workspaceId: "", parentType: "database", kind: "page", title: "", iconType: "none", position: 0, properties: {},
    createdAt: now, updatedAt: now, createdBy: viewerId, lastEditedBy: viewerId,
  };
}
