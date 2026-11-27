import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import type { DatabaseDeletedPropertiesSurface } from "@/lib/databaseDeletedProperties";
import { usePageLayoutTabMode } from "./pageLayoutTabEnvironment";
import { boardGroupingProperty, viewPlacingProperty } from "@/lib/nativeDatabaseViewProductAdapter";
import { createPortal } from "react-dom";
import { TouchSheetLayout } from "@/components/shell/AppShell";
import { DatabaseFullPageLinkContext, useDatabaseOwnPage } from "./databaseFullPageLink";
import { useRef, type CSSProperties, type ReactNode, type RefObject } from "react";
import { PageIconGlyph } from "@/components/page/PageIcon";
import type { OverlayLayerOptions } from "@/lib/overlayLayer";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import type { DatabaseToolbarTaskFeatureWire } from "@/databaseToolbarConfigWire.generated";
import type { DatabaseAutomationService } from "@/databaseAutomationApi";
import { isDashboardWidget } from "@/lib/dashboardLayout";
import type { DbProperty, DbView, FilterGroup, Page, ViewFilter } from "@/lib/types";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { CenterPeekIcon, CheckIcon, FullPagePeekIcon, SidePeekIcon, X } from "@/icons/leafen";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { DatabaseAutomationPanel } from "./DatabaseAutomationPanel";
import { useSettingsFirstAutomation } from "./settingsFirstAutomation";
import { advancedFilterParts } from "./DatabaseAdvancedFilter";
import { defaultOperator, defaultValue } from "./DatabaseFilterEditors";
import { withRollupFilterTargets } from "./databaseFilterPolicy";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { DatabaseGroupMenu } from "./DatabaseGroupMenu";
import { DatabaseSubGroupMenu } from "./DatabaseSubGroupMenu";
import { DatabaseFilterMenu, DatabaseFilterSettings } from "./DatabaseFilterSettings";
import { PhoneFilterList, phoneFilterCount, type PhoneOwnQuery } from "./DatabasePhoneFilterList";
import { DatabaseSortSettings, viewSorts } from "./DatabaseSortSettings";
import { DatabasePropertiesMenu } from "./DatabasePropertiesMenu";
import { regroupedViewConfig, switchedLayoutConfig, viewGroupsRows } from "./viewLayoutDefaults";
import { carryViewChoice } from "@/lib/selectionMemory";
import { DatabaseViewLayoutOptions } from "./DatabaseViewLayoutOptions";
import { ChartSettingsPanel } from "./ChartSettingsPanel";
import { ConditionalColorMenu } from "./ConditionalColorMenu";
import { DatabaseDataSourcesScreen, DatabaseSettingsMenu } from "./DatabaseDataSourcesPanel";
import type { DatabaseSettingsStart } from "./DatabaseMoreSettingsMenu";
import { DatabaseViewSettingsPanel, type DatabaseViewSettingsScreen } from "./DatabaseViewSettingsPanel";
import type { DatabaseTemplateMenuActions } from "./DatabaseTemplateMenu";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { dashboardFilterProperties, useDashboardFilterSources } from "./dashboardFilterSources";
import { databaseViewLabels } from "./databaseViewLabels";
import { defaultOpenPageIn, effectiveOpenPageIn, renamedView, viewOwnName, ViewTypeIcon } from "./databaseViewShared";
import type { GraphViewType } from "@/contentGraph";
import { NotionSelect } from "./NotionSelect";
import { offersInitialLoad, shownInitialLoadLimit, TABLE_INITIAL_LOAD_OPTIONS } from "./queryProjection";
import { LAYOUT_VIEW_TYPES, layoutViewTypes, propertyPickerOrder, type DatabaseToolbarMenu } from "./databaseToolbarModel";
import { DatabaseTouchFilterMenu } from "./DatabaseTouchFilterMenu";
import styles from "./databaseSheet";
import { PhoneSheet } from "../shell/PhoneSheet";
import { DatabaseTouchMenuWindow } from "./DatabaseTouchMenuWindow";
import { useTaskDatabaseConversion } from "./useTaskDatabaseConversion";
import { DatabaseCompactCardSettings } from "./DatabaseCompactCardSettings";

// A chart and a map show it too, though a chart counts every row whatever it says (Notion's, seen 2026-11-03).

type SharedToolbarDialogMenu = Exclude<DatabaseToolbarMenu, "automations" | "templates" | "view" | "newView">;

/** What the view settings panel shows beside its items, and does. */
type ViewSettingsSurface = {
  /** "추가 설정 › 페이지 레이아웃 사용자 지정": the pages' layout editor. */
  onEditLayout?(): void;
  /** The database is locked: its settings show, and change only what the lock allows. */
  locked?: boolean;
  /** Its source's properties and settings are locked too; unset, as `locked`. */
  sourceLocked?: boolean;
  /** A signed-in reader's: as locked, but its sub-items read, as Notion's (seen 2026-10-12). */
  reader?: boolean;
  shownCount: number;
  filterCount: number;
  /** Each filter that filters, by its property's name. */
  filterNames?: readonly (string | null)[];
  /** Each sort's property's name. */
  sortNames?: readonly string[];
  groupName?: string;
  subGroupName?: string;
  colorAvailable: boolean;
  onCopyLink?(): void;
  onAutomations?(): void;
  /**
   * Opens at this property's settings, as a cell's "속성 편집", or at the
   * layout or properties, as a card's menu; each request opens anew.
   */
  startProperty?: { id?: string; screen?: "layout" | "properties" | "sprints" | "subitems" | "group" | "subGroup" | "filter"; choosePagesIn?: boolean; request: number };
  /** The source a new view just turned to, whose views it may take after (kept across the panel's redraws). */
  sourceViews?: DatabaseContainerSource;
  onSourceViews?(candidate: DatabaseContainerSource | null): void;
};

type AutomationSurface = {
  /** A reader's: the automations listed and opened, nothing changed. */
  readOnly?: boolean;
  /** The database its automations belong to, whose properties and views they name. */
  databaseId: string;
  /** How many it has, as the page's read said: its list tells the toolbar what it holds after. */
  automationCount?: number;
  editorPages: Page[];
  properties: DbProperty[];
  /** The rows a relation's value may name, by the related database. */
  relationRows?: (dataSourceId: string) => Page[];
  service: DatabaseAutomationService;
  views: DbView[];
  /** The properties a condition may name, unset any it lists: People's Name, About, Membership Type and Person (Notion's, seen 2026-11-14). */
  triggerPropertyIds?: readonly string[];
  /** Whether a step may edit the page it ran for ("속성 편집…"): People's may not. */
  editsItsPage?: boolean;
  /** Its own icon where an automation applies, when the data source's isn't it: People's person in a circle. */
  databaseIcon?: ReactNode;
};

type MenusProps = {
  /** What the open menu belongs to: its panel starts over when this changes. */
  menuIdentity: string;
  activeView: GraphView;
  automation?: AutomationSurface | null;
  contextIdentity: string;
  dataSource: GraphDataSource;
  dataSources: readonly GraphDataSource[];
  filterSnapshot: DatabaseFilterSnapshot;
  inputModality: "pointer" | "keyboard";
  menuRef: RefObject<HTMLDivElement | null>;
  /** The toolbar, which a database's own page hangs its settings panel under. */
  toolbarRef?: RefObject<HTMLElement | null>;
  /**
   * A toolbar too narrow for "새로 만들기": its settings open under the
   * button, a database's own page's too, headed by "새 페이지" and "페이지
   * 템플릿 ›" (Notion's, seen 2026-11-02 at 768px).
   */
  narrowNew?: DatabaseTemplateMenuActions;
  menuStyle?: CSSProperties;
  /** Just under the menu, to catch presses beside it. */
  menuBackdropStyle?: CSSProperties;
  /** The layer of the view settings docked at the page's end, which the panel's own box takes. */
  dockedLayer?: Omit<OverlayLayerOptions, "ref"> | null;
  openMenu: DatabaseToolbarMenu | null;
  properties: readonly GraphProperty[];
  snapshotIdentity: string;
  taskConfiguration: DatabaseToolbarTaskFeatureWire;
  writable: boolean;
  /** The source's properties and settings may change, as under a linked database's "보기 잠금"; unset, as `writable`. */
  sourceWritable?: boolean;
  onClose(): void;
  /** The automations' "←" (a phone's "‹") back to the settings they opened from, as Notion's. */
  onBackToSettings?(): void;
  /** The view's settings, wherever the automations opened from. */
  onShowSettings?(): void;
  /** On a phone, the filter menu's "정렬" opens the sorts in its place. */
  onOpenSort?(): void;
  /**
   * On a phone the filter and sort menus and the settings are Notion's
   * bottom sheet (seen 2026-10-21), the sorts' "‹" going back to the filters.
   */
  phone?: boolean;
  /**
   * On a touch screen wider than a phone the view settings and a new view's
   * are the phone's sheet too, as Notion's iPad's "설정" and "새 보기" (seen
   * 2026-11-14 at 834); its filter and sort lists open in a window in the
   * screen's middle (DatabaseTouchMenuWindow).
   */
  wideSheets?: boolean;
  onOpenFilter?(): void;
  onCommitProperties(properties: readonly GraphProperty[]): void;
  onConfigureTaskFeatures(configuration: DatabaseToolbarTaskFeatureWire, madeProperties?: readonly GraphProperty[]): Promise<boolean>;
  onCreateProperty?(type: GraphProperty["type"], name: string, config?: Record<string, unknown>): Promise<GraphProperty | null>;
  onUpdateView(view: GraphView): void;
  /** Turns the view into a dashboard, the view as it was its first widget; the layout grid offers it then. */
  onTurnIntoDashboard?(): void;
  /** Turns a dashboard into another layout, its widgets gone. */
  onLeaveDashboard?(next: GraphView): void;
  searchRelationDatabases?: (query: string, signal: AbortSignal) => Promise<RelationDatabaseOption[]>;
  deletedProperties?: DatabaseDeletedPropertiesSurface;
  /** The database's row pages, for a formula's preview in the properties menu. */
  pagesById?: Record<string, Page>;
  /** The properties of the databases its relations reach, for a rollup's target in the properties menu. */
  propertiesByDataSourceId?: Readonly<Record<string, readonly DbProperty[]>>;
  viewSettings?: ViewSettingsSurface;
  /** Adds a property's pill to the filter bar and opens it. */
  onAddQuickFilter?(propertyId: string): void;
  /** "고급 필터 추가" made the advanced filter's pill: the list closes and the bar shows it. */
  onAdvancedAdded?(): void;
  /** A sort picked from the toolbar's list: the list closes and the sorts' pill opens its editor. */
  onSortPicked?(): void;
  /** A locked database's "필터 초기화", while the viewer's own filters and sorts apply. */
  onResetOwnQuery?(): void;
  /** The viewer's own filters and sorts while they apply, heading a phone's filter sheet. */
  ownQuery?: PhoneOwnQuery | null;
  /** On a phone, a property whose filter the sheet opens at once. */
  filterStart?: { propertyId: string; request: number } | null;
  /** Whether the viewer changes the view's filters, for everyone or for themselves alone. */
  queryEditable?: boolean;
  /** A dashboard widget's "필터": its own filters' list, as its settings' filter screen, once it has some under "필터 ✕". */
  widgetFilters?: boolean;
  /** The view's rows, whose values make its groups. */
  rows?: readonly GraphRow[];
  /** Moves a property to the deleted properties. */
  onDeleteProperty?(propertyId: string): void;
  onDoneNewView?(picked?: boolean): void;
};

function dbProperty(property: GraphProperty, index: number): DbProperty {
  return {
    id: property.id,
    databaseId: property.dataSourceId,
    name: property.name,
    position: index,
    type: property.type,
    config: property.config,
  } as DbProperty;
}

/**
 * Whether the view settings or a new view's open as a panel docked at a
 * database's own page's end (but a form's "폼 설정", which opens under its
 * button there too), not under their button; a phone's are sheets either
 * way. Its toolbar knows it as they open (useDatabaseToolbarController).
 */
export function settingsDocked(menu: string | null, { ownPage, narrowNew, viewType }: { ownPage: boolean; narrowNew: boolean; viewType: string }) {
  return (menu === "view" || menu === "newView") && ownPage && !narrowNew && !(menu === "view" && viewType === "form");
}

export function DatabaseToolbarMenus({
  activeView,
  menuIdentity,
  automation,
  contextIdentity,
  dataSource,
  dataSources,
  filterSnapshot,
  inputModality,
  menuRef,
  toolbarRef,
  narrowNew,
  menuStyle,
  menuBackdropStyle,
  dockedLayer,
  openMenu,
  properties,
  snapshotIdentity,
  taskConfiguration,
  writable,
  sourceWritable = writable,
  onClose,
  onBackToSettings,
  onShowSettings,
  onOpenSort,
  phone: phoneScreen,
  wideSheets = false,
  onOpenFilter,
  onCommitProperties,
  onConfigureTaskFeatures,
  onCreateProperty,
  onUpdateView,
  onTurnIntoDashboard,
  onLeaveDashboard,
  searchRelationDatabases,
  deletedProperties,
  pagesById,
  propertiesByDataSourceId,
  viewSettings,
  onAddQuickFilter,
  onAdvancedAdded,
  onSortPicked,
  onResetOwnQuery,
  ownQuery,
  filterStart,
  queryEditable = writable,
  widgetFilters = false,
  rows,
  onDeleteProperty,
  onDoneNewView,
}: MenusProps) {
  const layoutGridRef = useRef<HTMLDivElement>(null);
  const labels = databaseViewLabels();
  // The settings' screens are a phone's wherever the settings are a sheet, and so are the automations' (Notion's iPad sheets).
  const phone = phoneScreen || (wideSheets && (openMenu === "view" || openMenu === "newView" || openMenu === "automations"));
  const ownPage = useDatabaseOwnPage(), grouped = viewGroupsRows(activeView.config, properties, activeView.type);
  const { t } = useTranslation("propertyCell");
  const { t: chartT } = useTranslation("chartView");
  const layoutTab = usePageLayoutTabMode() !== null, dashboardSources = useDashboardFilterSources();
  const taskConversion = useTaskDatabaseConversion({ dataSourceId: dataSource.id, properties, taskConfiguration, writable: sourceWritable,
    onConfigure: onConfigureTaskFeatures });
  const firstAutomation = useSettingsFirstAutomation(openMenu === "view" && !phone, automation);
  if (!openMenu) return null;
  if (openMenu === "templates") return null;
  // The automations, or the first one's window over the view's settings (settingsFirstAutomation).
  const automationPanel = (exits: { onClose(): void; onBack?(): void; onSettings?(): void; wideSheet?: boolean }) => automation ? (
    <DatabaseAutomationPanel {...automation} phone={phone} {...exits} dockRef={toolbarRef} deletedProperties={deletedProperties}
      databaseIcon={automation.databaseIcon ?? (dataSource.icon && dataSource.iconType !== "none"
        ? <PageIconGlyph page={{ icon: dataSource.icon, iconType: dataSource.iconType ?? "none" }} size={16} box={18} fallback="database" /> : undefined)}
      databaseName={dataSource.title.trim() || inlineDatabasePlaceholderTitle()} />
  ) : null;
  if (openMenu === "automations") { const panel = automationPanel({ onClose, onBack: onBackToSettings, onSettings: onShowSettings, wideSheet: wideSheets }); return wideSheets ? <TouchSheetLayout>{panel}</TouchSheetLayout> : panel; }

  const dialogLabels: Record<SharedToolbarDialogMenu, string> = {
    layout: labels.layoutOptions,
    group: labels.groupBy,
    properties: labels.propertyVisibility,
    filter: labels.filters,
    sort: labels.sorts,
    color: labels.conditionalColor,
    settings: labels.databaseSettings,
    tasks: labels.databaseSettings,
  };
  // In the view settings panel a screen's heading names it, and it closes
  // with the panel.
  type Screen = SharedToolbarDialogMenu | DatabaseViewSettingsScreen;

  const dbProperties = propertyPickerOrder(properties.map(dbProperty));
  // Filters know what a rollup of the related values shows, to ask which of them (Notion's "최소 하나의 옵션 ⌄").
  // A dashboard's filters name its widgets' sources' properties too (dashboardFilterSources.ts).
  const filterProperties = withRollupFilterTargets(dashboardSources ? propertyPickerOrder(dashboardFilterProperties(dashboardSources).map(dbProperty)) : dbProperties, filterSnapshot.propertiesByDatabase);
  // A map, calendar or timeline without the property it places pages by makes one ("위치", "날짜"), as Notion's switch does,
  // and places by it. Each layout keeps its own cards and preview; the view keeps where its pages open (switchedLayoutConfig)
  // and what it has chosen (carryViewChoice), as Notion's. A view turns into a dashboard with itself as its first widget — a
  // new view only takes the layout — and a dashboard turned into another layout loses its widgets, as Notion's.
  async function switchLayout(type: GraphViewType) {
    if (type === "dashboard" && openMenu !== "newView" && onTurnIntoDashboard) { onTurnIntoDashboard(); return; }
    carryViewChoice(activeView.id);
    const switched = (next: GraphView) => activeView.type === "dashboard" && onLeaveDashboard ? onLeaveDashboard(next) : onUpdateView(next);
    const config = switchedLayoutConfig(activeView.config, activeView.type, type, properties);
    // A view still called its layout's name has none of its own: it takes the
    // new layout's, as Notion's unnamed view (seen 2026-11-06: "보드" became "리스트").
    const name = viewOwnName(activeView);
    const placing = viewPlacingProperty(type);
    if (placing && !properties.some((property) => property.type === placing.type) && onCreateProperty) {
      const made = await onCreateProperty(placing.type, placing.name());
      switched({ ...activeView, type, name, config: made ? { ...config, [placing.configKey]: made.id } : config });
      return;
    }
    // A board with nothing to group by makes its status and groups by it, as a new board does.
    const grouping = boardGroupingProperty(type, properties);
    if (grouping && onCreateProperty) {
      const made = await onCreateProperty(grouping.type, grouping.name, grouping.config);
      switched({ ...activeView, type, name, config: made ? regroupedViewConfig(config, undefined, made.id, [...properties, made], "board", true) : config });
      return;
    }
    switched({ ...activeView, type, name, config });
  }
  const quickFilters = Array.isArray(activeView.config.quickFilters) ? activeView.config.quickFilters as Array<ViewFilter | FilterGroup> : [];
  // The view's advanced filter: a group of rules and groups, applied with
  // its quick filters.
  const advanced = advancedFilterParts({ view: activeView, properties: filterProperties, snapshot: filterSnapshot, onUpdateView });
  // A dashboard's new widget offers the nine its "+" does (a dashboard holds no dashboard).
  const newViewLayoutTypes: GraphViewType[] = isDashboardWidget(activeView) ? [...LAYOUT_VIEW_TYPES]
    : [...LAYOUT_VIEW_TYPES, activeView.type === "form" ? "form" : "dashboard"];
  // A layout tab lists its pages, so it isn't turned into a dashboard, nor is a dashboard's widget. The tile follows
  // Notion's Business plan, Leafen's (user 2026-11-15): there an existing view's layout lists ten, the dashboard last;
  // Plus and free list nine, only "+" and "새 보기" offering a dashboard (seen 2026-11-05/06 free; 2026-11-18 Plus in
  // six databases, inline and full page, a chart too; Business a full page's table, board and chart).
  const layoutTypes = layoutViewTypes(activeView, Boolean(onTurnIntoDashboard) && activeView.type !== "form" && !layoutTab
    && !isDashboardWidget(activeView));
  const contentFor = (
    menu: Screen, inPanel = false, back: () => void = onClose, start?: DatabaseSettingsStart, propertyId?: string,
  ) => menu === "dataSources" ? (
    <DatabaseDataSourcesScreen onBack={back} onClose={onClose} />
  ) : menu === "compactCards" ? (
    <DatabaseCompactCardSettings view={activeView} properties={properties} disabled={!writable} onUpdateView={onUpdateView} />
  ) : menu === "editProperties" ? (
    <DatabasePropertiesMenu contextIdentity={contextIdentity} dataSource={dataSource}
      dataSources={dataSources} properties={properties} snapshotIdentity={snapshotIdentity}
      view={activeView} writable={sourceWritable} onCommitProperties={onCommitProperties}
      onCreateProperty={onCreateProperty} onUpdateView={onUpdateView}
      searchRelationDatabases={searchRelationDatabases} deletedProperties={deletedProperties} pagesById={pagesById}
      propertiesByDataSourceId={propertiesByDataSourceId}
      onDeleteProperty={onDeleteProperty} mode="edit" initialDetailId={propertyId}
      phoneBack={inPanel && phone ? back : undefined} />
  ) : menu === "layout" ? (
    // A locked database shows its layout without changing it, as Notion's (seen 2026-10-01).
    <fieldset className={styles.layoutFieldset} disabled={!writable} data-layout-locked={!writable ? "true" : undefined}>
      {!inPanel && <div className={styles.toolbarMenuLabel}>{labels.layout}</div>}
      <NativeChoiceGrid items={openMenu === "newView" ? newViewLayoutTypes : layoutTypes} keyOf={type => type} textOf={type => labels.viewTypes[type]}
        checkedKey={activeView.type} disabledKeys={writable ? [activeView.type] : layoutTypes}
        onAction={type => void switchLayout(type)} listRef={layoutGridRef} autoFocus={false}
        label={labels.databaseViewType} className={`${styles.layoutTypeGrid} ${openMenu === "newView" ? styles.layoutTypeGridNew : inPanel ? styles.layoutTypeGridScreen : ""}`} itemClassName={styles.layoutTypeButton}>
        {type => <><ViewTypeIcon type={type} size="regular" /><span>{labels.viewTypes[type]}</span>
          {activeView.type === type && <CheckIcon size="small" className={styles.check} aria-hidden="true" />}</>}
      </NativeChoiceGrid>
      <div className={styles.layoutSettings} data-layout-settings>
        <DatabaseViewLayoutOptions view={activeView} properties={properties} onUpdateView={onUpdateView} disabled={!writable}
          onCreateProperty={onCreateProperty} openPagesIn={activeView.type === "chart" ? null : (
            // "페이지 보기 선택" opens its choices with a line on each, as Notion's (seen 2026-09-30),
            // each by its icon, its "⌄" turned down only while open (seen 2026-11-06).
            <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
              <span>{labels.openPagesIn}</span>
              <NotionSelect
                ariaLabel={labels.openPagesIn}
                className={styles.layoutChoice}
                buttonClassName={styles.layoutChoiceButton} rowChoice
                value={effectiveOpenPageIn(activeView)}
                openAtFirst={viewSettings?.startProperty?.choosePagesIn}
                options={(["side", "center", "full"] as const).map((mode) => ({
                  value: mode, label: labels.peekMode(mode), description: labels.peekModeHint(mode),
                  icon: mode === "side" ? <SidePeekIcon size="regular" /> : mode === "center" ? <CenterPeekIcon size="regular" /> : <FullPagePeekIcon size="regular" />,
                  note: mode === defaultOpenPageIn(activeView.type) ? labels.defaultOpenPageFor(labels.viewTypes[activeView.type]) : undefined,
                }))}
                onChange={(mode) => onUpdateView({ ...activeView, config: { ...activeView.config, openPageIn: mode } })}
              />
            </label>
          )}
          initialLoad={offersInitialLoad(activeView, ownPage, grouped) ? (
            // "처음 불러오기": how many rows, cards or posts the view first shows (a board's in
            // each column, a grouped view's in each group), where its layout offers it
            // (offersInitialLoad), saying the one its layout and grouping take until one is
            // chosen (shownInitialLoadLimit), as Notion's.
            <label className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}>
              <span>{labels.initialLoad}</span>
              <NotionSelect
                ariaLabel={labels.initialLoad}
                className={styles.layoutChoice}
                buttonClassName={styles.layoutChoiceButton} rowChoice
                value={String(shownInitialLoadLimit(activeView, grouped))}
                options={TABLE_INITIAL_LOAD_OPTIONS.map((limit) => ({ value: String(limit), label: String(limit) }))}
                onChange={(limit) => onUpdateView({ ...activeView, config: { ...activeView.config, initialLoadLimit: Number(limit) } })}
              />
            </label>
          ) : null} />
      </div>
      {/* A chart's layout screen ends in "차트 편집", back to its settings, under its settings rather than among them (DatabaseLayoutEditChart.module.css). */}
      {activeView.type === "chart" && inPanel && openMenu !== "newView" ? (
        <button type="button" className={styles.layoutEditChart} data-layout-edit-chart onClick={back}>{chartT("editChart")}</button>
      ) : null}
    </fieldset>
  ) : menu === "group" ? (
    <DatabaseGroupMenu activeView={activeView} properties={properties} rows={rows} pagesById={pagesById}
      propsByDatabaseId={propertiesByDataSourceId} heading={!inPanel}
      sheet={inPanel && phone} onUpdateView={onUpdateView} />
  ) : menu === "subGroup" ? (
    <DatabaseSubGroupMenu activeView={activeView} properties={properties} rows={rows} pagesById={pagesById}
      propsByDatabaseId={propertiesByDataSourceId} onUpdateView={onUpdateView} />
  ) : menu === "properties" || menu === "timelineTable" ? (
    <DatabasePropertiesMenu contextIdentity={contextIdentity} dataSource={dataSource}
      dataSources={dataSources} properties={properties} snapshotIdentity={snapshotIdentity}
      view={activeView} writable={writable} onCommitProperties={onCommitProperties}
      onCreateProperty={onCreateProperty} onUpdateView={onUpdateView}
      searchRelationDatabases={searchRelationDatabases} deletedProperties={deletedProperties} pagesById={pagesById}
      propertiesByDataSourceId={propertiesByDataSourceId} mode={menu === "timelineTable" ? "timelineTable" : "visibility"}
      onDeleteProperty={menu === "timelineTable" ? onDeleteProperty : undefined}
      onClose={inPanel ? undefined : onClose} />
  ) : menu === "filter" ? (
    // A phone's filters are Notion's list (seen 2026-10-24), in the settings sheet without "정렬" (seen 2026-10-21).
    phone ? (
      <PhoneFilterList view={activeView} properties={filterProperties} snapshot={filterSnapshot} editable={queryEditable}
        advanced={advanced} ownQuery={ownQuery} onSort={inPanel ? undefined : onOpenSort} start={inPanel ? null : filterStart}
        dashboard={activeView.type === "dashboard"} onUpdateView={onUpdateView} />
    ) : inPanel || (widgetFilters && writable) ? (<>
      {/* A widget's, as Notion's (seen 2026-11-11): "필터 ✕" over its filters' list, its picker alone till it has one. */}
      {!inPanel && (quickFilters.length > 0 || advanced.count > 0) ? <div className={styles.widgetFiltersHead}>
        <strong>{labels.filters}</strong>
        <button type="button" className={styles.widgetFiltersClose} aria-label={t("closeMenu")} onClick={onClose}><X size="tiny" aria-hidden="true" /></button>
      </div> : null}
      <DatabaseFilterSettings view={activeView} properties={filterProperties} snapshot={filterSnapshot}
        writable={writable || Boolean(viewSettings?.locked)}
        advancedCount={advanced.count} advancedEditor={advanced.editor} onAddAdvanced={advanced.add}
        onUpdateView={onUpdateView} onReset={onResetOwnQuery} ownQuery={ownQuery} />
    </>) : !queryEditable ? (
      <>
        <div className={styles.toolbarMenuLabel}>{labels.filters}</div>
        {advanced.editor}
      </>
    ) : wideSheets && !inPanel ? (
      <DatabaseTouchFilterMenu view={activeView} properties={filterProperties} snapshot={filterSnapshot} writable={queryEditable}
        advanced={advanced} onAdvancedAdded={onAdvancedAdded} onUpdateView={onUpdateView} onClose={onClose} />
    ) : (
      <DatabaseFilterMenu properties={filterProperties} writable={queryEditable} advancedCount={advanced.count}
        advancedEditor={advanced.editor} onAddAdvanced={advanced.add} onAdvancedAdded={onAdvancedAdded}
        onPick={(propertyId) => {
          if (onAddQuickFilter) return onAddQuickFilter(propertyId);
          const property = filterProperties.find(({ id }) => id === propertyId);
          if (!property) return;
          onUpdateView({ ...activeView, config: { ...activeView.config, quickFilters: [...quickFilters, {
            propertyId, operator: defaultOperator(property, filterProperties), value: defaultValue(property, filterProperties),
          }] } });
          onClose();
        }} />
    )
  ) : menu === "color" ? (
    <ConditionalColorMenu view={activeView} properties={properties} dbProperties={filterProperties}
      filterSnapshot={filterSnapshot} heading={!inPanel} sheet={inPanel && phone} onUpdateView={onUpdateView} />
  ) : menu === "sort" ? (
    // A wider touch screen's sort window goes on to the sorts in its place, as Notion's iPad's.
    <DatabaseSortSettings view={activeView} properties={dbProperties} onUpdateView={onUpdateView} sheet={phone} touchWindow={wideSheets && !inPanel} ownQuery={ownQuery}
      onPicked={!inPanel && !phone && !wideSheets ? onSortPicked : undefined} />
  ) : (
    <DatabaseSettingsMenu properties={properties} taskConfiguration={taskConfiguration} view={activeView} onUpdateView={onUpdateView}
      writable={sourceWritable} onChange={onConfigureTaskFeatures} onCreateProperty={onCreateProperty}
      onBack={inPanel ? back : undefined} start={start} />
  );

  if (typeof document === "undefined") return null;
  if (openMenu === "view" || openMenu === "newView") {
    if (!viewSettings) return null;
    // An inline database's settings open under the button, as Notion's
    // (seen 2026-09-30), a new view's from the same place (seen 2026-10-30);
    // a database's own page keeps the panel at the window's right, but for a
    // form's "폼 설정", which opens under its button there too (seen
    // 2026-11-08); a phone's are a bottom sheet (seen 2026-10-21), a new
    // view's too ("새 보기", seen 2026-10-24).
    const settingsSheet = Boolean(phone) && (openMenu === "view" || openMenu === "newView");
    const docked = settingsDocked(openMenu, { ownPage, narrowNew: Boolean(narrowNew), viewType: activeView.type });
    const panel = (
      <DatabaseViewSettingsPanel key={`${menuIdentity}:${openMenu}:${viewSettings.startProperty?.request ?? 0}`} newView={openMenu === "newView"} onDoneNewView={onDoneNewView} panelRef={menuRef} view={activeView} dataSource={dataSource}
        {...viewSettings} onAutomations={firstAutomation.onAutomations(viewSettings.onAutomations)}
        popoverStyle={docked ? undefined : menuStyle}
        dockRef={docked ? toolbarRef : undefined}
        layerStyle={docked && openMenu === "newView" ? { zIndex: menuStyle?.zIndex } : undefined}
        layer={docked ? dockedLayer : null}
        newPage={openMenu === "view" ? narrowNew : undefined}
        onRename={(name) => onUpdateView(renamedView(activeView, name))}
        onUpdateConfig={writable ? (patch) => onUpdateView({ ...activeView, config: { ...activeView.config, ...patch } }) : undefined}
        taskDatabase={taskConfiguration.tasks?.enabled === true} subitems={taskConfiguration.subitems?.enabled === true}
        dependencies={taskConfiguration.dependencies?.enabled === true}
        startPropertyId={viewSettings.startProperty?.id} startScreen={viewSettings.startProperty?.screen}
        renderScreen={(screen, back, start, propertyId) => contentFor(screen, true, back, start, propertyId)}
        renderTaskConversion={taskConversion.renderTaskConversion} onRevertTaskDatabase={taskConversion.onRevertTaskDatabase}
        phone={settingsSheet}
        chartSettings={activeView.type === "chart" ? ({ subitems }) => (
          <ChartSettingsPanel view={activeView} properties={properties} disabled={!writable} subitemsRow={subitems} onUpdateView={onUpdateView} />
        ) : undefined}
        onClose={onClose} />
    );
    // An iPad's "설정" has no "전체 페이지로 열기": its toolbar keeps its "⤢" (Notion's, seen 2026-11-14).
    if (settingsSheet) return phoneScreen ? panel : (
      <TouchSheetLayout><DatabaseFullPageLinkContext.Provider value={null}>{panel}</DatabaseFullPageLinkContext.Provider></TouchSheetLayout>
    );
    return (<>{createPortal(docked ? panel : (<>
      <button type="button" className={styles.menuBackdrop} style={menuBackdropStyle} tabIndex={-1}
        aria-label={t("closeMenu")} onClick={onClose} data-toolbar-menu-backdrop />
      {panel}
    </>), document.body)}{firstAutomation.window(automationPanel, viewSettings.onAutomations)}</>);
  }
  const content = contentFor(openMenu);
  if (phone && (openMenu === "filter" || openMenu === "sort")) {
    const filters = phoneFilterCount(activeView, advanced.count);
    return (
      <PhoneSheet title={openMenu === "filter" ? labels.sheetFilters(filters) : viewSorts(activeView).length ? labels.sorts : labels.sheetNewSort}
        label={dialogLabels[openMenu]} sheetRef={menuRef} zIndex={menuStyle?.zIndex as number | undefined} onClose={onClose} tone="settings"
        onBack={openMenu === "sort" ? onOpenFilter : undefined} data={{ "data-toolbar-menu-input-modality": inputModality }}>
        {content}
      </PhoneSheet>
    );
  }
  // A touch screen wider than a phone opens its filter and sort lists in a window in its middle, as Notion's iPad (DatabaseTouchMenuWindow).
  if (wideSheets && (openMenu === "filter" || openMenu === "sort")) {
    return (
      <DatabaseTouchMenuWindow title={openMenu === "filter" ? labels.addFilter : viewSorts(activeView).length ? labels.sorts : labels.sheetNewSort}
        label={dialogLabels[openMenu]} menuRef={menuRef} layer={menuStyle?.zIndex} shadeLayer={menuBackdropStyle?.zIndex} onClose={onClose}
        data={{ "data-toolbar-menu-input-modality": inputModality }}>
        {content}
      </DatabaseTouchMenuWindow>
    );
  }
  return createPortal(
    <>
      <button type="button" className={styles.menuBackdrop} style={menuBackdropStyle} tabIndex={-1}
        aria-label={t("closeMenu")} onClick={onClose} data-toolbar-menu-backdrop />
      <div ref={menuRef} className={`${styles.toolbarMenu} ${openMenu === "layout" ? styles.layoutMenu : ""}`}
        style={menuStyle} role="dialog" aria-label={dialogLabels[openMenu]}
        data-toolbar-menu-input-modality={inputModality} data-property-picker-window={(openMenu === "filter" && !widgetFilters) || openMenu === "sort" ? "" : undefined}>
        {content}
      </div>
    </>,
    document.body,
  );
}

export type { AutomationSurface };
