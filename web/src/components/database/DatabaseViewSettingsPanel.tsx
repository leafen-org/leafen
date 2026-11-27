import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { PhoneSettingsListContext } from "./PropertySettingRow";
import { SwitchInput } from "@/components/Switch";
import { i18next } from "@/i18n";
import { useContext, useEffect, useRef, useState, type CSSProperties, type MouseEvent, type ReactNode, type RefObject } from "react";
import type { GraphDataSource, GraphView } from "@/contentGraph";
import {
  ArchiveIcon, ArrowLeft, ArrowUpRightIcon, BoardIcon, BulletedListIcon, CheckIcon, ChevronDown, ChevronRight, ConvertIcon, DatabaseCylinderIcon, DataPathIcon, DotsHorizontal, ExpandDiagonalIcon,
  DoubleChevronRight, EyeIcon, FilterIcon, GroupIcon, LinkIcon, LockIcon, PaintRollerIcon, LightningIcon, SortIcon, UnlockIcon, X, Copy, Trash,
  AddCircleFillIcon, ArrowsLeftRight, FileText, Plus, PaintBrushIcon,
} from "@/icons/leafen";
import { PageIconGlyph } from "../page/PageIcon";
import { DatabaseDataSourcesContext, surfaceDataSourceName, viewSourceIsLinked, viewSourceSwitchable } from "./databaseDataSourcesContext";
import { SettingsScreenHeadContext, SettingsScreenOpenContext, useSettingsScreenHeads } from "./settingsScreenHead";
import { PanelHeadBox, PanelHeading } from "./panelHead";
import { DatabaseArchivedRowsContext } from "./databaseArchivedRowsContext";
import { ChartSaveAsRow } from "./ChartSaveAs";
import {
  DatabaseMoreSettingsMenu, DatabaseMoreSettingsSheet, DatabaseTypeSwitchMenu, DatabaseTypeSwitchSheet, type DatabaseSettingsStart,
} from "./DatabaseMoreSettingsMenu";
import { PhoneSheet } from "../shell/PhoneSheet";
import { usePhoneSettingsSheetSize } from "./phoneSettingsSheetSize";
import { DatabaseViewNameField } from "./DatabaseViewNameField";
import { FormSubmitSettings } from "./FormSubmitSettings";
import { useFormSettings } from "@/lib/formSettingsBridge";
import { requestViewDeletion, sourceChosenView, sourceFirstView } from "@/lib/viewTabRequests";
import { isDashboardWidget } from "@/lib/dashboardLayout";
import { DatabaseViewSourceScreen } from "./DatabaseViewSourceScreen";
import { DatabaseViewSourceViews } from "./DatabaseViewSourceViews";
import { useSettingsScreenAcrossViews } from "./settingsScreenAcrossViews";
import { settingsPanelFitsContent } from "./settingsPanelWidth";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { useDatabaseLock } from "./databaseStructureLock";
import { useDatabaseFullPageLink, useDatabasePageTitleNamesSource } from "./databaseFullPageLink";
import { useRouter } from "@/lib/router";
import { databaseViewLabels } from "./databaseViewLabels";
import { ViewTypeIcon } from "./databaseViewShared";
import type { ViewType } from "@/lib/types";
import { conditionalColorSettings } from "./conditionalColorModel";
import { DatabaseSettingsFootContext, DatabaseSourcePropertiesOnlyContext, DatabaseSourceSettingsContext } from "./databaseNewRowControl";
import { useDockedPanelTop } from "./useDockedPanelTop";
import { useDatabaseAutomationCount } from "./databaseAutomationCount";
import dockColumn from "./dockedPanelColumn.module.css";
import { LayerBox } from "@/lib/layerBox";
import { scrollStartBeforePaint } from "@/lib/scrollStartBeforePaint";
import type { OverlayLayerOptions } from "@/lib/overlayLayer";
import { PageLayoutTabActionsContext, usePageLayoutTabMode } from "./pageLayoutTabEnvironment";
import styles from "./viewSettingsSheet";
import { setCreatingView } from "./creatingView";
import { useWikiSource } from "./wikiSourceShape";
import { DatabaseTemplateMenuBeside, type DatabaseTemplateMenuActions } from "./DatabaseTemplateMenu";

// A database's settings as Notion's (seen 2026-09-30): the toolbar's
// "Settings" opens a panel at the window's right with the view's name, its
// layout, shown properties, filters, sorts, grouping, conditional color and
// link, then its data source's properties, automations, more settings and
// sources. An item opens its settings in the panel, with a way back. An
// inline database's open as a 290px popover under the button instead, the
// page staying as it is; either widens to its sorts' rules (settingsPanelWidth). On a phone
// they are a bottom sheet (`phone`, Notion's of 2026-10-21 at 375px): "설정"
// over the view's name in a group of its own, then white groups of 45px rows
// — the view's options to "보기 링크 복사", "데이터 소스 설정" in gray over
// its source's, and the sources and lock — each screen in the same sheet
// under its name with "‹", "추가 설정" and "유형 전환" over it as half sheets.

export type DatabaseViewSettingsScreen =
  | "layout" | "properties" | "filter" | "sort" | "group" | "subGroup" | "color" | "editProperties" | "settings" | "dataSources" | "database"
  | "databaseViews" | "mapHeight" | "compactCards" | "formSubmit" | "timelineTable";

// A map's heights, "보통" the one it starts at.
const MAP_HEIGHTS = ["small", "medium", "large", "extra_large"] as const;

// Screens that draw their own heading and way back.
const OWN_HEADING = new Set<DatabaseViewSettingsScreen>(["settings", "dataSources"]);
// On a phone the properties screen heads itself too: its list and each property's settings.
const PHONE_OWN_HEADING = new Set<DatabaseViewSettingsScreen>([...OWN_HEADING, "editProperties"]);

export function DatabaseViewSettingsItem({ icon, label, meta, metaIcon, metaLinked = false, next = true, disabled, marker, expanded, onClick }: {
  /** A style row (a map's "높이") has none, as Notion's. */
  icon?: ReactNode;
  label: string;
  meta?: string;
  /** Its value's own icon before it: "데이터베이스"'s source icon, as Notion's (✅ 작업 트래커, seen 2026-11-09). */
  metaIcon?: ReactNode;
  /** Its value is another database's source, "↗" before it (Notion's "데이터베이스 ↗ 〈source〉", seen 2026-11-08). */
  metaLinked?: boolean;
  next?: boolean;
  disabled?: boolean;
  marker?: string; /** Its menu is open: the row stays lit and its "›" turns down, as Notion's "유형 전환" (seen 2026-11-17). */ expanded?: boolean;
  onClick?(event: MouseEvent<HTMLButtonElement>): void;
}) {
  return (
    <button type="button" className={styles.item} disabled={disabled} data-view-setting={marker} aria-expanded={expanded} onClick={onClick}>
      {icon ? <span className={styles.itemIcon} aria-hidden="true">{icon}</span> : null}
      <span className={styles.itemLabel} data-view-setting-name={meta ? marker === "database" ? "one-line" : "beside-value" : undefined}>{label}</span>
      {meta ? <span className={styles.itemMeta} data-sheet-value={next ? "" : undefined}>
        {metaLinked ? <ArrowUpRightIcon size="page" className={styles.itemMetaArrow} aria-hidden="true" /> : null}
        {metaIcon ? <span className={styles.itemMetaIcon} aria-hidden="true">{metaIcon}</span> : null}{meta}</span> : null}
      {next && !disabled /* a dimmed row has no "›", as Notion's locked database and read-only guest (2026-11-15) */ ? expanded ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" /> : null}
    </button>
  );
}

const Item = DatabaseViewSettingsItem;

export function DatabaseViewSettingsPanel({
  panelRef,
  newView = false,
  onDoneNewView,
  locked = false,
  sourceLocked = locked,
  reader = false,
  view,
  dataSource,
  shownCount,
  filterCount,
  filterNames = [],
  sortNames = [],
  groupName,
  subGroupName,
  colorAvailable,
  taskDatabase = false,
  subitems = false,
  dependencies = false,
  onRename,
  onUpdateConfig,
  onCopyLink,
  onAutomations,
  onEditLayout,
  renderScreen,
  renderTaskConversion,
  onRevertTaskDatabase,
  startPropertyId,
  startScreen,
  sourceViews,
  onSourceViews,
  chartSettings,
  popoverStyle,
  newPage,
  layerStyle,
  layer,
  dockRef,
  phone = false,
  onClose,
}: {
  panelRef: RefObject<HTMLDivElement | null>;
  /** The database's toolbar, which the panel at the window's right hangs under. */
  dockRef?: RefObject<HTMLElement | null>;
  newView?: boolean;
  onDoneNewView?(picked?: boolean): void;
  /**
   * The database is locked, as Notion's (seen 2026-10-01): its view's name,
   * grouping, colors, properties and more settings are off; its layout and
   * shown properties show without changing, and its filters and sorts apply
   * for the viewer alone.
   */
  locked?: boolean;
  /**
   * Its source's properties, type and more settings are off too; a linked
   * database's "보기 잠금" leaves them on (Notion's, seen 2026-10-13).
   */
  sourceLocked?: boolean;
  /** A signed-in reader's settings: locked, its sub-items still read (Notion's, seen 2026-10-12). */
  reader?: boolean;
  view: GraphView;
  dataSource: GraphDataSource;
  shownCount: number;
  filterCount: number;
  /** The properties the view's filters filter by, which Notion shows beside "필터" (seen 2026-09-30). */
  /** The filters' properties, null for the advanced filter (appliedFilterItems). */
  filterNames?: readonly (string | null)[];
  /** The properties the view sorts by. */
  sortNames?: readonly string[];
  groupName?: string;
  /** A board's sub-group, which Notion sets right after its grouping (seen 2026-10-10). */
  subGroupName?: string;
  colorAvailable: boolean;
  /** The database is a task database, which "유형 전환" shows. */
  taskDatabase?: boolean;
  /** Its sub-items are on: "하위 항목" opens their view options from the list, as Notion's (seen 2026-10-11). */
  subitems?: boolean;
  /** Its dependencies are on: "추가 설정" says "켜짐" beside them, as Notion's (seen 2026-11-09). */
  dependencies?: boolean;
  onRename(name: string): void;
  /** Changes the view's own settings, as its icon and description. */
  onUpdateConfig?(patch: Record<string, unknown>): void;
  onCopyLink?(): void;
  onAutomations?(): void;
  /** "페이지 레이아웃 사용자 지정" under "추가 설정": opens the pages' layout editor. */
  onEditLayout?(): void;
  renderScreen(screen: DatabaseViewSettingsScreen, back: () => void, start: DatabaseSettingsStart, propertyId?: string): ReactNode;
  /** "작업 데이터베이스로 전환"'s window in the middle, which `close` puts away (Notion's, seen 2026-11-02). */
  renderTaskConversion?(close: () => void): ReactNode;
  /** A task database's "작업 데이터베이스 되돌리기" in place of "유형 전환 ›". */
  onRevertTaskDatabase?(): void;
  /** Opens at this property's "속성 편집", whose way back leads to the settings, as Notion's from a status cell (seen 2026-10-10). */
  startPropertyId?: string;
  /** Opens at the layout or the properties, as a board or gallery card's menu does (Notion's, seen 2026-10-11). */
  /** "sprints" opens More settings at the sprints. */
  startScreen?: "layout" | "properties" | "sprints" | "subitems" | "group" | "subGroup" | "filter";
  /** The source a new view just turned to: the panel opens, or stays, at its views (DatabaseViewSourceViews). */
  sourceViews?: DatabaseContainerSource;
  onSourceViews?(candidate: DatabaseContainerSource | null): void;
  /**
   * A chart's own settings, which take the place of its properties, sorts,
   * groups and colors, as Notion's chart panel (seen 2026-10-05); given the
   * database's "하위 항목 ›", it places it among them.
   */
  chartSettings?: ReactNode | ((rows: { subitems: ReactNode }) => ReactNode);
  /** "스킬 데이터베이스로 전환", after the automations, as Notion's (seen 2026-10-11). */
  /** "스킬 데이터베이스로 전환" in "유형 전환 ›", and its dialog. */
  /** Where it opens as a popover under its button; the panel at the window's right without it. */
  popoverStyle?: CSSProperties;
  /**
   * On a toolbar too narrow for "새로 만들기", Notion's menu opens with "새
   * 페이지" and "페이지 템플릿 ›" in its place, with no head or "×" (seen
   * 2026-11-02 at 768px).
   */
  newPage?: DatabaseTemplateMenuActions;
  /** An unanchored creation rail still sits above its overlay backdrop. */
  layerStyle?: CSSProperties;
  /** Docked, the layer its own box takes (useDatabaseToolbarController); a new view's stands at its height. */
  layer?: Omit<OverlayLayerOptions, "ref"> | null;
  /** A phone's bottom sheet in place of the panel or popover. */
  phone?: boolean;
  onClose(): void;
}) {
  // "자동화" says how many it has, none saying nothing, on a desktop and a phone (Notion's, seen 2026-11-15).
  const labels = databaseViewLabels(), automations = useDatabaseAutomationCount(dataSource.id, dataSource.automationCount) || "";
  const pageTitleNamesSource = useDatabasePageTitleNamesSource();
  // A layout tab is one view kept by the page layout, as Notion's (seen 2026-10-12): no link, automations, type, sources or lock of its own.
  const layoutTab = usePageLayoutTabMode() !== null, tabActions = useContext(PageLayoutTabActionsContext);
  const sourcesSurface = useContext(DatabaseDataSourcesContext), archived = useContext(DatabaseArchivedRowsContext);
  const dataSources = layoutTab ? null : sourcesSurface;
  // A new view of a database with more than one source, of a linked one showing another database's source, or
  // made by "데이터 소스 관리"'s "기존 데이터 소스 연결하기" (sourceFirstView, seen 2026-11-15), asks for its
  // source first, "기존 데이터 소스 연결하기" with no way back, as Notion's (seen 2026-11-02; a
  // linked one's, its one source "추천", 2026-11-12); its source's views come
  // next. A dashboard's new widget opens at its layout, its source then
  // chosen from "데이터베이스", which takes other databases' too (the widget's
  // "새 보기" picked its layout). A linked view gone on from its "새 보기" has its source (sourceChosenView).
  const sourceFirst = newView && !!dataSources && !dataSources.readOnly && !isDashboardWidget(view) && !sourceChosenView(view.id)
    && (dataSources.own.length > 1 || dataSources.linked.length > 0 || sourceFirstView(view.id));
  const [screen, setScreenShown] = useState<DatabaseViewSettingsScreen | null>(
    startPropertyId ? "editProperties" : newView && sourceViews ? "databaseViews" : sourceFirst ? "database"
      : startScreen === "sprints" || startScreen === "subitems" ? "settings" : startScreen ?? null);
  const [startProperty, setStartProperty] = useState(startPropertyId);
  // Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: the view
  // settings' "레이아웃" pressed, "z" typed went nowhere and the layout shown stayed ringed (Leafen the same).
  // Each screen opens at its top, not where the one before was scrolled to (Notion's, seen 2026-11-06), put
  // there before the frame drawing it paints (scrollStartBeforePaint); the first is there as it is drawn. The
  // focus goes to the body the screens are drawn in (the popover or sheet itself) first when what had it, the
  // row or "←" pressed, goes with the screen, Tab going on into the screen shown: taking out the focused
  // control made the browser work out the panel's styles and layout as it went (2026-11-14: 54 and 8
  // elements at "레이아웃" and "←", NAS condition, CPU ×2).
  const bodyRef = useRef<HTMLDivElement>(null);
  const scrollReset = useRef<(() => void) | null>(null);
  useEffect(() => () => scrollReset.current?.(), []);
  const setScreen = (next: DatabaseViewSettingsScreen | null) => {
    if (next !== screen) {
      const panel = panelRef.current, holder = bodyRef.current ?? panel, active = document.activeElement;
      if (panel && holder && active instanceof HTMLElement && active !== holder && panel.contains(active)) holder.focus({ preventScroll: true });
      scrollReset.current?.();
      scrollReset.current = scrollStartBeforePaint([panel, bodyRef.current]);
    }
    setScreenShown(next);
  };
  // "추가 설정" opens a menu under its row, whose choice opens its setting.
  const [moreMenu, setMoreMenu] = useState<DOMRect | null>(null), [templatesAt, setTemplatesAt] = useState<DOMRect | null>(null);
  const [typeMenu, setTypeMenu] = useState<DOMRect | null>(null), [taskConverting, setTaskConverting] = useState(false);
  const [settingsStart, setSettingsStart] = useState<DatabaseSettingsStart>(startScreen === "sprints" ? "sprints" : "subitems");
  // A screen may name the heading while it shows (settingsScreenHead.ts), as grouping's "그룹화 기준".
  const [screenHead, registerScreenHead] = useSettingsScreenHeads();
  // Manage data sources is there when the editor can change the sources.
  // Last, an inline database's "데이터베이스 잠금", or its "잠금 해제" once locked (seen 2026-10-01).
  const databaseLock = useDatabaseLock(), lock = layoutTab ? null : databaseLock;
  const toggleLock = lock?.locked ? lock.unlockForEveryone : lock?.lock;
  // On a phone an inline database opens alone from here, its toolbar's button
  // gone, as Notion's sheet (seen 2026-10-23).
  const fullPageHref = useDatabaseFullPageLink(), router = useRouter();
  const back = () => { setScreen(null); setStartProperty(undefined); onSourceViews?.(null); };
  useSettingsScreenAcrossViews(view, newView ? null : screen, setScreenShown, () => setStartProperty(undefined));
  // Grouping opened from the layout's "그룹화 기준" goes back there (Notion's, seen 2026-11-06).
  const [groupFromLayout, setGroupFromLayout] = useState(false);
  const openFromScreen = (next: "compactCards" | "properties" | "layout" | "group" | "timelineTable") => { setGroupFromLayout(next === "group" && screen === "layout"); setScreen(next); };
  // Where "←" goes: nowhere from a source-first view's sources, to them from its source's views.
  // Opened right at its filters (a group's "필터 편집하기") or its groups ("그룹 편집하기"), it has nowhere to go back to, as Notion's (seen 2026-11-05, 2026-11-12).
  const backOf = (at: DatabaseViewSettingsScreen) => (at === "filter" || at === "group") && at === startScreen ? undefined : at === "compactCards" || at === "timelineTable" || (at === "group" && groupFromLayout) ? () => { setGroupFromLayout(false); setScreen("layout"); } : !sourceFirst ? back
    : at === "database" ? undefined : at === "databaseViews" ? () => { setScreen("database"); onSourceViews?.(null); } : back;
  // Escape closes the whole panel from any of its screens, however deep and
  // whether the focus is in a search field or not: its layer's Escape
  // (useDatabaseToolbarController), as Notion's (seen 2026-11-11: "레이아웃",
  // "그룹화" and "속성 편집"'s searches, a property, its "유형" and the
  // types' search, in a full-page and an inline database).
  const popover = Boolean(popoverStyle) || phone, sheetSize = usePhoneSettingsSheetSize(view.type);
  const sheetLayer = Number((popoverStyle ?? layerStyle)?.zIndex ?? 1001);
  // Its tab reads "새 보기" once its source is chosen; before, its layout's name, as Notion's (seen 2026-11-02).
  const choosingSource = sourceFirst && screen === "database";
  useEffect(() => {
    if (!newView || choosingSource) return;
    setCreatingView(view.id);
    return () => setCreatingView(null);
  }, [newView, view.id, choosingSource]);
  // A database's own page narrows beside its panel rather than going under
  // it; an inline database's new view's panel lies over the page, which
  // keeps its place, as Notion's (seen 2026-10-30).
  useEffect(() => {
    if (popover || !dockRef) return;
    const root = document.documentElement;
    root.dataset.databaseViewSettingsOpen = "true";
    return () => { delete root.dataset.databaseViewSettingsOpen; };
  }, [dockRef, popover]);
  const dockTop = useDockedPanelTop(dockRef, !popover, panelRef, true);
  const fits = settingsPanelFitsContent(screen, screenHead !== null);
  const placed = popoverStyle ?? (dockTop === null ? layerStyle : { ...layerStyle, top: `max(var(--page-panel-top, var(--topbar-height)), ${dockTop}px)` });
  const style = fits ? { ...placed, width: "max-content" } : placed;
  const titles: Record<DatabaseViewSettingsScreen, string> = {
    layout: labels.layout,
    properties: labels.propertyVisibility,
    filter: labels.filter,
    sort: labels.sort,
    group: labels.group,
    subGroup: labels.subGroup,
    color: labels.conditionalColor,
    editProperties: labels.editProperties,
    settings: labels.moreSettings,
    dataSources: labels.manageDataSources,
    // Notion's new view names the screen "기존 데이터 소스 연결하기" (seen 2026-10-11).
    database: newView ? labels.linkExistingDataSource : labels.database,
    databaseViews: labels.newView,
    mapHeight: labels.mapHeight,
    compactCards: labels.compactCardSettings,
    // A timeline's "표 속성" heads its list "속성" (Notion's, seen 2026-11-12).
    timelineTable: labels.properties,
    formSubmit: i18next.t("formView:submitScreen"),
  };
  // "데이터베이스 ›" turns the view to another source (viewSourceSwitchable).
  const switchable = viewSourceSwitchable(dataSources, dataSource.id, locked);
  // "조건부 색상" counts its settings, as Notion's map list does (seen 2026-10-12),
  // by the paint roller, as Notion's desktop and phone lists (seen 2026-10-24).
  const colorCount = conditionalColorSettings(view.config as Record<string, unknown>).length;
  const sourceSettings = useContext(DatabaseSourceSettingsContext);
  const settingsFoot = useContext(DatabaseSettingsFootContext), propertiesOnly = useContext(DatabaseSourcePropertiesOnlyContext);
  const colorRow = colorAvailable && !sourceSettings && (
    <Item icon={<PaintRollerIcon size="regular" />} label={labels.conditionalColor} meta={colorCount ? String(colorCount) : ""} marker="color"
      disabled={locked} onClick={() => setScreen("color")} />
  );
  // A form view's builder lends its form to "폼 설정" (lib/formSettingsBridge).
  const formSource = useFormSettings(view.id);
  const wiki = useWikiSource(dataSource.id);
  // A layout tab's view is linked to its source, "↗ 〈source〉" whichever database's, the open page's own too (Notion's, seen 2026-11-12).
  const databaseRow = <Item icon={<DataPathIcon size="regular" />} label={labels.database} meta={surfaceDataSourceName(sourcesSurface, dataSource)}
    metaIcon={dataSource.icon && dataSource.iconType !== "none"
      ? <PageIconGlyph page={{ icon: dataSource.icon, iconType: dataSource.iconType ?? "none" }} size={16} box={18} fallback="database" /> : undefined}
    metaLinked={layoutTab || viewSourceIsLinked(dataSources, dataSource.id)} next={switchable} disabled={!switchable} marker="database" onClick={() => setScreen("database")} />;
  const subitemsRow = subitems ? <Item icon={<DoubleChevronRight size="regular" />}
    label={taskDatabase ? i18next.t("databaseView:subtasks") : wiki ? i18next.t("databaseView:subpages") : labels.subitems} marker="subitems" disabled={locked && !reader}
    onClick={() => { setSettingsStart("subitems"); setScreen("settings"); }} /> : null;
  // "보관된 페이지 보기" turns the view to its archived rows and closes the
  // panel; once on, it reads "보관된 페이지 보기에서 나가기", as Notion's (seen
  // 2026-10-11/12). A reader has none, as Notion's read-only guest (2026-11-15).
  const archivedRow = archived && !reader ? <Item icon={<ArchiveIcon size="regular" />}
    label={archived.viewing ? labels.exitViewingArchivedPages : labels.viewArchivedPages} next={false} marker="archived"
    onClick={() => { if (archived.viewing) archived.exit(); else archived.enter(); onClose(); }} /> : null;
  // Viewing archived pages, the list keeps the view's own options and the way
  // out, without the data source's settings, sources or lock, as Notion's
  // (seen 2026-10-13 in table, list, gallery, calendar and timeline).
  const archiveViewing = Boolean(archived?.viewing) && !newView;
  // "필터" and "정렬" name their one property, or count two or more, as
  // Notion's (seen 2026-10-24); a filter waiting for its value isn't one, and
  // the advanced filter alone, with no property to name, reads "1".
  const queryMeta = (names: readonly (string | null)[]) => names.length === 1 && names[0] !== null ? names[0]
    : names.length ? String(names.length) : "";
  const filterRow = <Item icon={<FilterIcon size="regular" />} label={labels.filter}
    meta={queryMeta(filterNames)}
    marker="filter" onClick={() => setScreen("filter")} />;
  const copyLinkRow = onCopyLink && !layoutTab ? <Item icon={<LinkIcon size="regular" />} label={labels.copyViewLink} next={false} marker="link" onClick={onCopyLink} /> : null;
  const mapHeight = typeof view.config.mapHeight === "string" ? view.config.mapHeight : "medium";
  const mapHeightLabel = (option: string) => option === "small" ? labels.small
    : option === "large" ? labels.large : option === "extra_large" ? labels.extraLarge : labels.mapHeightMedium;
  // A layout builder's tab duplicated or removed from the foot of each of
  // its settings' screens, as Notion's builder (seen 2026-11-12: under its
  // list, its layout's options and its sorts alike).
  const tabFoot = tabActions && (
    <>
      <div className={styles.divider} />
      <Item icon={<Copy size="regular" />} label={labels.duplicateView} next={false} marker="duplicateTab"
        onClick={() => { tabActions.duplicate(); onClose(); }} />
      <Item icon={<Trash size="regular" />} label={labels.deleteView} next={false} marker="deleteTab"
        onClick={() => { tabActions.remove(); onClose(); }} />
    </>
  );
  const main = (
    <>
      {newPage ? <div className={styles.newPageRows}>
        <button type="button" className={`${styles.item} ${styles.newPageRow}`} data-settings-new-page
          disabled={!newPage.writable} onClick={() => {
            onClose();
            newPage.onCreateRow(newPage.defaultTemplateId);
          }}>
          <span className={styles.itemIcon} aria-hidden="true"><AddCircleFillIcon size="regular" /></span>
          <span className={styles.itemLabel}>{i18next.t("databaseView:newPage")}</span>
        </button>
        <button type="button" className={styles.item} data-settings-page-templates aria-haspopup="dialog" aria-expanded={templatesAt !== null}
          onClick={(event) => {
            // Its list opens beside the menu at this row, as Notion's.
            const row = popoverAnchorOf(event.currentTarget), menu = event.currentTarget.closest('[role="dialog"]')?.getBoundingClientRect();
            setTemplatesAt(menu ? new DOMRect(menu.left, row.top, menu.width, row.height) : row);
          }}>
          <span className={styles.itemIcon} aria-hidden="true"><FileText size="regular" /></span>
          <span className={styles.itemLabel}>{i18next.t("databaseView:pageTemplates")}</span>
          <ChevronRight size="small" aria-hidden="true" />
        </button>
        {templatesAt ? <DatabaseTemplateMenuBeside anchor={templatesAt} {...newPage}
          onCreateRow={(templateId) => { onClose(); newPage.onCreateRow(templateId); }}
          onCreateTemplate={() => { onClose(); newPage.onCreateTemplate(); }}
          onEditTemplate={(templateId) => { onClose(); newPage.onEditTemplate(templateId); }}
          onClose={() => setTemplatesAt(null)} /> : null}
      </div> : <PanelHeading><div className={`${styles.heading} ${styles.mainHeading} ${newView ? styles.newViewHeading : ""}`}><span className={styles.label}>{newView ? labels.newView
        : view.type === "form" ? i18next.t("formView:formSettings") : labels.viewSettingsLabel}</span></div></PanelHeading>}
      {/* My tasks' start at "레이아웃", no name row (Notion's, 2026-11-16). */ sourceSettings ? null : <DatabaseViewNameField view={view} newView={newView} locked={locked} describable={view.type !== "form"} onRename={onRename} onDone={onDoneNewView ?? onClose} onUpdateConfig={onUpdateConfig} />}
      {newView ? <>
        {/* Its "그룹화 기준 ›" goes to the grouping screen, "←" back here, as Notion's new view (seen 2026-11-07). */}
        <div className={`${styles.screen} ${styles.newViewScreen}`}><SettingsScreenOpenContext.Provider value={openFromScreen}>{renderScreen("layout", onClose, settingsStart)}</SettingsScreenOpenContext.Provider></div>
        <div className={styles.newViewDatabase}>{databaseRow}</div>
        {/* A new chart ends in a blue "차트 편집" in place of "완료", which opens its settings, as Notion's (seen 2026-11-02); a phone's sheet ends at "데이터베이스", its name's Enter being its "완료" (Notion's A90 and iPhone, seen 2026-11-18). */}
        {phone ? null : <button type="button" className={styles.done} data-new-view-done onClick={event => {
          event.stopPropagation(); (onDoneNewView ?? onClose)();
        }}>{view.type === "chart" ? i18next.t("chartView:editChart") : labels.done}</button>}
      </> : view.type === "form" ? <>
      {/* A form's own "폼 설정", as Notion's (seen 2026-11-04): a new question, its
          automations, its "제출 화면", its database, and deleting it. */}
      <Item icon={<Plus size="regular" />} label={i18next.t("formView:newQuestion")} next={false} marker="formNewQuestion"
        disabled={locked || !formSource} onClick={() => { onClose(); formSource?.addQuestion(); }} />
      {onAutomations && <Item icon={<LightningIcon size="regular" />} label={labels.automations} meta={String(automations)} marker="automations" onClick={onAutomations} />}
      <Item icon={<PaintBrushIcon size="regular" />} label={i18next.t("formView:submitScreen")} marker="formSubmit" onClick={() => setScreen("formSubmit")} />
      <div className={styles.divider} />
      {/* A form keeps its database: the row is greyed, without "›" (Notion's, seen 2026-11-08). */}
      <Item icon={<DataPathIcon size="regular" />} label={labels.database} meta={surfaceDataSourceName(sourcesSurface, dataSource)}
        next={false} disabled marker="database" />
      <div className={styles.divider} />
      <Item icon={<Trash size="regular" />} label={i18next.t("formView:deleteForm")} next={false} marker="formDelete" disabled={locked}
        onClick={() => { if (requestViewDeletion(view.id)) onClose(); }} />
      </> : <>
      {/* The rows' marks as Notion's (seen 2026-10-24): the layout's own, an
          open eye, a board's grouping as a board, the properties as a list,
          "유형 전환" as two arrows round a square. */}
      <Item icon={<ViewTypeIcon type={view.type as ViewType} size="regular" />} label={labels.layout}
        meta={labels.viewTypes[view.type as keyof typeof labels.viewTypes]} marker="layout" onClick={() => setScreen("layout")} />
      {chartSettings ? <>
        {typeof chartSettings === "function" ? chartSettings({ subitems: subitemsRow }) : chartSettings}
        <div className={styles.divider} />
        {databaseRow}
        {filterRow}
        {archivedRow}
        <div className={styles.divider} />
        {view.config.chartType !== "number" && <ChartSaveAsRow viewId={view.id} />}
        {copyLinkRow}
      </> : view.type === "dashboard" ? <>
        {/* A dashboard's list is short, its widgets keeping their own filters
            and sorts, as Notion's (seen 2026-11-07): whether its title shows —
            "새 대시보드", on its own page (seen 2026-11-09) — and whether its
            widgets' names show their views' icons, then its link; its sources
            and lock follow. */}
        {!pageTitleNamesSource && <label className={styles.item} data-view-setting="showDataSourceTitle">
          <span className={styles.itemIcon} aria-hidden="true"><ArrowsLeftRight size="regular" /></span>
          <span className={styles.itemLabel}>{labels.showDataSourceTitle}</span>
          <SwitchInput checked={view.config.showDataSourceTitle !== false} disabled={locked || !onUpdateConfig}
            onChange={(event) => onUpdateConfig?.({ showDataSourceTitle: event.currentTarget.checked ? undefined : false })} />
        </label>}
        <label className={styles.item} data-view-setting="dashboardTitleIcons">
          <span className={styles.itemIcon} aria-hidden="true"><EyeIcon size="regular" weight="regular" /></span>
          <span className={styles.itemLabel}>{labels.dashboardTitleIcons}</span>
          <SwitchInput checked={view.config.dashboardTitleIcons === true} disabled={locked || !onUpdateConfig}
            onChange={(event) => onUpdateConfig?.({ dashboardTitleIcons: event.currentTarget.checked || undefined })} />
        </label>
        <div className={styles.divider} />
        {copyLinkRow}
      </> : view.type === "map" ? <>
        {/* A map's list is short, as Notion's (seen 2026-10-11): its height
            under "스타일", then its database, filters, colors and archived
            pages, and its link. */}
        <div className={styles.divider} />
        <div className={styles.section}>{labels.mapStyle}</div>
        <Item label={labels.mapHeight} meta={mapHeightLabel(mapHeight)} marker="mapHeight"
          disabled={locked || !onUpdateConfig} onClick={() => setScreen("mapHeight")} />
        <div className={styles.divider} />
        {databaseRow}
        {filterRow}
        {colorRow}
        {archivedRow}
        <div className={styles.divider} />
        {copyLinkRow}
      </> : <>
        <Item icon={<EyeIcon size="regular" weight="regular" />} label={labels.propertyVisibility} meta={String(shownCount)} marker="properties"
          onClick={() => setScreen("properties")} />
        {filterRow}
        <Item icon={<SortIcon size="regular" />} label={labels.sort} meta={queryMeta(sortNames)}
          marker="sort" onClick={() => setScreen("sort")} />
        {/* Notion's calendar and feed don't group (seen 2026-11-03). */}
        {view.type !== "calendar" && view.type !== "feed" && (
          <Item icon={view.type === "board" ? <BoardIcon size="regular" /> : <GroupIcon size="regular" />} label={labels.group} meta={groupName} marker="group" disabled={locked}
            onClick={() => { setGroupFromLayout(false); setScreen("group"); }} />
        )}
        {view.type === "board" && (
          <Item icon={<GroupIcon size="regular" />} label={labels.subGroup} meta={subGroupName} marker="subGroup" disabled={locked}
            onClick={() => setScreen("subGroup")} />
        )}
        {colorRow}
        {archiveViewing ? <>
          {copyLinkRow}
          <div className={styles.divider} />
          {archivedRow}
        </> : <>
        {subitemsRow}
        {copyLinkRow}
        <div className={styles.divider} />
        <div className={styles.section}>{labels.dataSourceSettings}</div>
        {sourceSettings ?? <>
        {!propertiesOnly && databaseRow}
        <Item icon={<BulletedListIcon size="regular" />} label={labels.editProperties} marker="editProperties" disabled={sourceLocked}
          onClick={() => setScreen("editProperties")} />
        {onAutomations && !layoutTab && !propertiesOnly && <Item icon={<LightningIcon size="regular" />} label={labels.automations} meta={String(automations)} marker="automations" onClick={onAutomations} />}
        {/* A task database's row turns it back, as Notion's (seen 2026-11-02). */}
        {!layoutTab && !propertiesOnly && (taskDatabase && onRevertTaskDatabase ? (
          <Item icon={<ConvertIcon size="regular" />} label={i18next.t("databaseTasks:revertTaskDatabase")} marker="revertTaskDatabase"
            next={false} disabled={sourceLocked} onClick={onRevertTaskDatabase} />
        ) : (
          <Item icon={<ConvertIcon size="regular" />} label={labels.convertType} marker="convertType" disabled={sourceLocked} expanded={typeMenu !== null}
            onClick={(event) => setTypeMenu(popoverAnchorOf(event.currentTarget))} />
        ))}
        {taskConverting && renderTaskConversion ? renderTaskConversion(() => setTaskConverting(false)) : null}
        {typeMenu && (phone ? (
          <DatabaseTypeSwitchSheet taskDatabase={taskDatabase} zIndex={sheetLayer + 1} onClose={() => setTypeMenu(null)}
            onPick={(start) => {
              setTypeMenu(null);
              setSettingsStart(start);
              setScreen("settings");
            }} />
        ) : (
          <DatabaseTypeSwitchMenu anchor={typeMenu} taskDatabase={taskDatabase} onClose={() => setTypeMenu(null)}
            onPick={(start) => {
              setTypeMenu(null);
              // Its window in the middle, over these settings (Notion's, seen 2026-11-02).
              if (start === "taskDatabase" && !taskDatabase && renderTaskConversion) { setTaskConverting(true); return; }
              setSettingsStart(start);
              setScreen("settings");
            }} />
        ))}
        {/* "보관된 페이지 보기" sits here, before "추가 설정", as Notion's (seen 2026-10-11). */}
        {!propertiesOnly && archivedRow}
        {!propertiesOnly && <Item icon={<DotsHorizontal size="regular" />} label={labels.moreSettings} marker="settings" disabled={sourceLocked} expanded={moreMenu !== null}
          onClick={(event) => setMoreMenu(popoverAnchorOf(event.currentTarget))} />}
        {moreMenu && (phone ? (
          <DatabaseMoreSettingsSheet subitemsOn={subitems} taskDatabase={taskDatabase} dependenciesOn={dependencies} wiki={wiki} zIndex={sheetLayer + 1} onClose={() => setMoreMenu(null)}
            onLayout={onEditLayout && (() => { setMoreMenu(null); onClose(); onEditLayout(); })}
            onPick={(start) => {
              setMoreMenu(null);
              setSettingsStart(start);
              setScreen("settings");
            }} />
        ) : (
          <DatabaseMoreSettingsMenu anchor={moreMenu} subitemsOn={subitems} taskDatabase={taskDatabase} dependenciesOn={dependencies} wiki={wiki} onClose={() => setMoreMenu(null)}
            onLayout={onEditLayout && (() => { setMoreMenu(null); onClose(); onEditLayout(); })}
            onPick={(start) => {
              setMoreMenu(null);
              setSettingsStart(start);
              setScreen("settings");
            }} />
        ))}
        </>}
        </>}
      </>}
      {phone && fullPageHref && !layoutTab && !archiveViewing && !newView && (
        <>
          <div className={styles.divider} />
          <Item icon={<ExpandDiagonalIcon size="regular" />} label={labels.openAsFullPage} next={false} marker="fullPage"
            onClick={() => { onClose(); router.push(fullPageHref); }} />
        </>
      )}
      {dataSources && !archiveViewing && !propertiesOnly && (
        <>
          <div className={styles.divider} />
          <button type="button" className={styles.item} data-view-setting="dataSources" data-manage-data-sources
            onClick={() => setScreen("dataSources")}>
            <span className={styles.itemIcon} aria-hidden="true"><DatabaseCylinderIcon size="regular" /></span>
            <span className={styles.itemLabel}>{labels.manageDataSources}</span>
            <ChevronRight size="small" aria-hidden="true" />
          </button>
        </>
      )}
      {/* "데이터베이스 잠금" closes the first screen, after "데이터 소스 관리", as Notion's (seen 2026-10-11). */}
      {lock && toggleLock && !archiveViewing && (
        <>
          {!dataSources && <div className={styles.divider} />}
          <button type="button" className={styles.item} data-view-setting="lock" onClick={toggleLock}>
            <span className={styles.itemIcon} aria-hidden="true">{lock.locked ? <UnlockIcon size="regular" /> : <LockIcon size="regular" />}</span>
            <span className={styles.itemLabel}>{lock.views
              ? lock.locked ? labels.unlockViews : labels.lockViews
              : lock.locked ? labels.unlockDatabase : labels.lockDatabase}</span>
          </button>
        </>
      )}
      {settingsFoot}
      {tabFoot}
      </>}
    </>
  );
  const screenContent = screen === null ? null : (
    <div className={styles.screen} data-view-settings-screen={screen}>
      {screen === "formSubmit"
        ? <FormSubmitSettings viewId={view.id} locked={locked} />
        : screen === "database" && dataSources
        ? <DatabaseViewSourceScreen surface={dataSources} viewId={view.id} currentSourceId={dataSource.id} newView={newView} onDone={back}
          onSwitched={newView && onSourceViews ? (candidate) => { onSourceViews(candidate); setScreen("databaseViews"); } : undefined} />
        : screen === "databaseViews" && dataSources && sourceViews
        ? <DatabaseViewSourceViews surface={dataSources} viewId={view.id} viewType={view.type} candidate={sourceViews}
          onNew={() => { setScreen(null); onSourceViews?.(null); }} onAdopted={() => { onSourceViews?.(null); if (onDoneNewView) onDoneNewView(true); else back(); }} />
        : screen === "mapHeight"
        ? <div role="radiogroup" aria-label={labels.mapHeight} data-map-height-choices>
          {MAP_HEIGHTS.map((option) => (
            <button key={option} type="button" role="radio" aria-checked={mapHeight === option} className={styles.item}
              data-map-height={option} disabled={locked || !onUpdateConfig}
              onClick={() => { onUpdateConfig?.({ mapHeight: option === "medium" ? undefined : option }); back(); }}>
              <span className={styles.itemLabel}>{mapHeightLabel(option)}</span>
              {mapHeight === option ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </button>
          ))}
        </div>
        : <SettingsScreenOpenContext.Provider value={openFromScreen}>{renderScreen(screen, back, settingsStart, startProperty)}</SettingsScreenOpenContext.Provider>}
      {tabFoot}
    </div>
  );
  if (phone) {
    // A screen that heads itself names the sheet's head and its "‹" (DatabaseTaskSettingsHeader); the others go back by the sheet's own.
    return (
      <PhoneSheet title={screen === null ? (newView ? labels.newView : labels.settings)
        : screen === "filter" ? labels.sheetFilters(filterCount)
        // Ungrouped, Notion's grouping screen is its "그룹화 기준" list (seen 2026-10-23).
        : screen === "group" && !groupName ? labels.groupBy : titles[screen]}
        label={newView ? labels.newView : labels.viewSettingsLabel} sheetRef={panelRef} zIndex={sheetLayer} onClose={onClose} tone="settings" size={sheetSize}
        onBack={screen !== null && !PHONE_OWN_HEADING.has(screen) ? backOf(screen) : undefined}
        data={{ "data-new-view-settings": newView ? "true" : undefined, "data-database-view-settings": screen ?? "main", "data-popover": "true" }}>
        <div className={styles.phoneList} data-phone-settings>
          <PhoneSettingsListContext.Provider value={true}>{screen === null ? main : screenContent}</PhoneSettingsListContext.Provider>
        </div>
      </PhoneSheet>
    );
  }
  const content = screen === null ? main : (
    <>
      {!OWN_HEADING.has(screen) && (
        <PanelHeading><div className={styles.heading}>
          {!screenHead?.onBack && !backOf(screen) ? null : (
            <button type="button" className={styles.back} aria-label={labels.backToViewSettings}
              onClick={screenHead?.onBack ?? backOf(screen)}>
              <ArrowLeft size="small" aria-hidden="true" />
            </button>
          )}
          <strong>{screenHead?.title ?? titles[screen]}</strong>
        </div></PanelHeading>
      )}
      <SettingsScreenHeadContext.Provider value={registerScreenHead}>{screenContent}</SettingsScreenHeadContext.Provider>
    </>
  );
  return (
    <LayerBox boxRef={panelRef} layer={popover ? null : layer} raised={newView} className={`${styles.panel} ${popover ? "" : dockColumn.column}`} tabIndex={popover ? -1 : undefined} data-form-settings={view.type === "form" && !newView ? "true" : undefined} data-dashboard-settings={view.type === "dashboard" && !newView ? "true" : undefined} role="dialog" aria-label={newView ? labels.newView : labels.viewSettingsLabel}
      data-new-view-settings={newView ? "true" : undefined} data-database-view-settings={screen ?? "main"} data-popover={popover ? "true" : undefined}
      data-docked-panel={popover ? undefined : "true"} data-layout-tab-settings={layoutTab ? "true" : undefined} data-fits-content={fits ? "true" : undefined} style={style}>
      {/* A layout builder tab's have no "×", as Notion's builder (seen 2026-11-12); a view's own keep it. */}
      {newPage || layoutTab ? null : <button type="button" className={styles.close} aria-label={labels.closeViewSettings} onClick={onClose}>
        <X size="tiny" aria-hidden="true" />
      </button>}
      {/* Docked or a popover, a 42px head over a body that scrolls alone (panelHead.tsx), as Notion's (popover seen 2026-11-15). */}
      <PanelHeadBox>
        <div ref={bodyRef} className={styles.panelBody} tabIndex={-1} data-panel-body data-edge-fade=""><div className={styles.panelContent}>{content}</div></div>
      </PanelHeadBox>
    </LayerBox>
  );
}
