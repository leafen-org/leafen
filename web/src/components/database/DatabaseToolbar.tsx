import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { keepDatabaseHeadInView } from "./databaseHeadInView";
import { holdMeasure } from "./heldStyleWrites"; import { watchPageSize } from "../page/pageSizeWatch";
import type { DatabaseDeletedPropertiesSurface } from "@/lib/databaseDeletedProperties";
import { useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type MutableRefObject } from "react";
import { useProductPages } from "@/components/product/productPagesContext";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphView } from "@/contentGraph";
import { newId } from "@/lib/ids";
import { holdTypingWhile } from "@/lib/pageTitleFocus";
import {
  listenForDatabaseTemplateRequests, offerDatabaseTemplateActions, offerDatabaseTemplates,
  type DatabaseTemplateActions, type DatabaseTemplateChoice,
} from "@/lib/databaseTemplateRequests";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import type { Session } from "@/types";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { DatabaseSprintCompleteAction } from "./DatabaseSprintCompleteAction";
import { closeSettingsPlaceWindow, closeShownDatabaseStartPanel, linkedFromStartPanel, requestDatabaseStartPanel, useDatabaseStartPanelShown, useSettingsPlaceWindowShown } from "@/lib/databaseStartPanel";
import { useShownBlockCopy } from "@/lib/shownBlockCopy";
import { forgetLinkedViewWindowPlace, linkedViewWindowPlace } from "@/lib/linkedViewWindowPlace";
import { leftLinkedView, makingLinkedView, takeLeftLinkedView } from "@/lib/databaseStarterLinked";
import { DatabaseLinkedStarter } from "./DatabaseLinkedStarter";
import { focusPickedViewName } from "./pickedViewName";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { addDatabaseShortcut } from "@/components/sidebar/SidebarDatabaseShortcut";
import { useOptionalProductToast } from "@/lib/productToast";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import { confirmAction, confirmActions } from "@/components/ConfirmActionDialog";
import { DatabaseToolbarControls } from "./DatabaseToolbarControls";
import { DatabaseTemplateEditor, templateBandDatabaseIcon } from "./DatabaseTemplateEditor";
import { DatabaseTemplateMenu, type DatabaseTemplateMenuActions } from "./DatabaseTemplateMenu";
import { DatabaseToolbarMenus, settingsDocked, type AutomationSurface } from "./DatabaseToolbarMenus";
import { useDatabaseSourceLocked, useDatabaseStructureLocked } from "./databaseStructureLock";
import { useDatabaseFilterBar } from "./databaseFilterBar";
import { dashboardLayoutOf } from "@/lib/dashboardLayout";
import {
  useDatabaseToolbarController,
  type DatabaseToolbarOperations,
  type DatabaseToolbarSnapshot,
} from "./useDatabaseToolbarController";
import { appliedFilterItems, type DatabaseToolbarCommands, type DatabaseToolbarMenu } from "./databaseToolbarModel";
import { useTemplateDisplayName } from "./untitledRowName";
import { keptPageTemplate, newPageTemplate, VIEW_DEFAULT_TEMPLATE, viewDefaultTemplate, withViewDefaultTemplate } from "./viewDefaultTemplate";
import { viewDisplayName } from "./databaseViewShared";
import { ownQueryChanged, type PhoneOwnQuery } from "./DatabasePhoneFilterList";
import { sameViewQuery, type PersonalQuery } from "./databasePersonalQuery";
import { CONDITIONAL_COLOR_VIEW_TYPES } from "./conditionalColorModel";
import { tableColumnSettings } from "./tablePropertyMenuModel";
import { projectVisibleProperties } from "./tableViewModel";
import styles from "./databaseSheet";
import { boardGroupPropertyId } from "./viewLayoutDefaults";
import { cardSubitemFilter } from "./subitemFilterScope";
import { useViewingArchivedRows } from "./databaseArchivedRowsContext";
import { DatabaseNewRowControlContext, DatabaseRowsAddableContext } from "./databaseNewRowControl";
import { useTemplateAddress } from "./useTemplateAddress";
import { usePageLayoutTabFilter, usePageLayoutTabMode } from "./pageLayoutTabEnvironment";
import { templateRepeats } from "./templateRepeatModel";
import { isFreshDatabase, leaveFreshDatabase, settleFreshDatabase } from "@/lib/freshDatabases";
import { useDatabaseAutomationCount } from "./databaseAutomationCount";
import { CRAMPED_DATABASE_ROW, NARROW_DATABASE_ROW, databaseToolsRowWidth } from "./databaseToolbarRowWidth";
import { revealColumnBesidePanel } from "./revealColumnBesidePanel";
import { openTemplateAtItsTitle, openTemplateCopy } from "@/lib/templatesMadeHere";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// A view's filters, quick and advanced, apart from its sorts.
const filterPart = (config: GraphView["config"]): PersonalQuery => ({ quickFilters: config.quickFilters,
  filterGroup: config.filterGroup, filters: config.filters, filterConjunction: config.filterConjunction });

function filterCount(value: unknown): number {
  if (!value || typeof value !== "object") return 0;
  const group = value as { filters?: unknown[]; groups?: unknown[] };
  const groups = Array.isArray(group.groups) ? group.groups : [];
  return (group.filters?.length ?? 0) + groups.reduce<number>((total, child) => total + filterCount(child), 0);
}

function activeFilterCount(snapshot: DatabaseToolbarSnapshot) {
  const tree = snapshot.activeView.config.filterGroup;
  return tree ? filterCount(tree) : Array.isArray(snapshot.activeView.config.filters)
    ? snapshot.activeView.config.filters.length
    : 0;
}

const TOOLBAR_MINIMIZED = "database-toolbar-minimized";

export function DatabaseToolbar({
  widget,
  automation,
  dataSources,
  operations,
  search,
  snapshot,
  onCreateProperty,
  onSearchChange,
  onOwnQueryChange,
  onResetOwnQuery,
  ownQuery,
  searchRelationDatabases,
  deletedProperties,
  session = null,
  commandsRef,
  blockId,
  onCopyViewLink,
  onEditLayout,
  formBarSlotRef,
  viewControlsSlotRef,
}: {
  /** A widget's tools in its head: a dashboard's (DatabaseDashboardWidgets) in "view" or "edit" mode, Home's "내 작업" ("home"). */
  widget?: "view" | "edit" | "home";
  automation?: AutomationSurface | null;
  dataSources: readonly GraphDataSource[];
  operations: DatabaseToolbarOperations;
  search: string;
  snapshot: DatabaseToolbarSnapshot;
  onCreateProperty?(type: GraphProperty["type"], name: string, config?: Record<string, unknown>): Promise<GraphProperty | null>;
  onSearchChange(value: string): void;
  /** Applies, for this viewer alone, the filters and sorts of a view they can't edit. */
  onOwnQueryChange?(view: GraphView): void;
  /** A locked database's "필터 초기화", while the viewer's own filters and sorts apply. */
  onResetOwnQuery?(): void;
  /** The viewer's own filters and sorts while they apply, for a phone's filter sheet. */
  ownQuery?: PhoneOwnQuery | null;
  searchRelationDatabases?: (query: string, signal: AbortSignal) => Promise<RelationDatabaseOption[]>;
  deletedProperties?: DatabaseDeletedPropertiesSurface;
  /** Opens a template's body, its page, for editing. */
  session?: Session | null;
  /** Takes the toolbar's commands for the rest of its database view. */
  commandsRef?: MutableRefObject<DatabaseToolbarCommands | null>;
  /** The database block: inline, "»" folds its tools for this person (see `minimized`); a linked view's "←" opens its start panel. */
  blockId?: string;
  /** Copies a link that opens the page on this view. */
  onCopyViewLink?(viewId: string): void;
  /** Opens the layout editor of the database's pages ("추가 설정 › 페이지 레이아웃 사용자 지정"). */
  onEditLayout?(): void;
  /** Takes the place in a form view's row where its builder puts "미리보기" and "양식 공유". */
  formBarSlotRef?(element: HTMLElement | null): void;
  viewControlsSlotRef?(element: HTMLElement | null): void;
}) {
  // The database's own page, whose icon the template band shows (Notion's).
  const databasePage = useProductPages()?.pages.find(({ id }) => id === snapshot.pageId);
  const { t } = useTranslation("databaseView");
  const templateName = useTemplateDisplayName(snapshot.templates.filter((template) => !template.deleted));
  // A view showing its archived rows makes no page: no "새로 만들기", as Notion's.
  const viewingArchived = useViewingArchivedRows();
  const toolbarRef = useRef<HTMLDivElement>(null);
  // The width the row's tools go by, by its step (databaseToolsRowWidth).
  const [rowWidth, setRowWidth] = useState(0);
  // A table or a feed just made keeps the same three until it first changes (freshDatabases.ts);
  // a board or a list made from "/" shows all its tools at once (Notion's, seen 2026-11-06).
  const justMade = (snapshot.activeView.type === "table" || snapshot.activeView.type === "feed")
    && isFreshDatabase(snapshot.dataSource.id, snapshot.properties.length, snapshot.rows.length);
  useEffect(() => {
    if (!justMade) settleFreshDatabase(snapshot.dataSource.id);
  }, [justMade, snapshot.dataSource.id]);
  // A widget's head keeps its few tools however narrow (DatabaseDashboardWidgets). A
  // database just made, on its own page too, shows only settings and "새로 만들기"
  // (and "⤢" in a page) while its start panel is open and till it first changes;
  // drawn again — come back to its page through its parent's link, or its block
  // moved in or out — it shows them all (Notion's, seen 2026-11-07: s190 and
  // s192 for the first, s163 and s192 for the rest); back from its turn into a
  // simple table with ⌘Z it is as it was made (leaveFreshDatabase).
  const ownPage = useDatabaseOwnPage();
  useEffect(() => {
    const dataSourceId = snapshot.dataSource.id, shownAt = window.location.pathname;
    return () => leaveFreshDatabase(dataSourceId, window.location.pathname !== shownAt);
  }, [snapshot.dataSource.id]);
  const narrow = !widget && (justMade || rowWidth > 0 && rowWidth < NARROW_DATABASE_ROW);
  // A phone keeps search, "필터 및 정렬" and settings however narrow the row (Notion's, seen 2026-10-20).
  // A touch screen wider than one keeps the desktop's tools, its "⌄" by "새로 만들기" and its settings opening
  // the phone's sheets, as Notion's iPad (seen 2026-11-14 at 834).
  const { phone, touch } = useSidebarLayout(true);
  useLayoutEffect(() => {
    const row = toolbarRef.current?.parentElement;
    if (!row || typeof ResizeObserver === "undefined") return undefined;
    const { column, read } = databaseToolsRowWidth(row), update = () => setRowWidth(read());
    // Under a side peek the row goes by the part of it still in view.
    const stopHeadInView = toolbarRef.current ? keepDatabaseHeadInView(row, toolbarRef.current) : () => {};
    // First measured with the view's other first measures (holdMeasure).
    const dropFirst = holdMeasure(toolbarRef, update);
    // As they resize, with the page's other measures, before their writes (pageSizeWatch).
    const stopRow = watchPageSize(row, update), stopColumn = column ? watchPageSize(column, update) : () => {};
    return () => { dropFirst(); stopRow(); stopColumn(); stopHeadInView(); };
  }, []);
  const searchRef = useRef<HTMLInputElement>(null);
  // Each of its buttons is a stop of Tab and ⇧Tab, in the order they stand, and the arrow keys move
  // nowhere, as Notion's (measured 2026-11-15 in Chrome 1440 after the key only: 필터 → 정렬 → 자동화,
  // 설정 ⇧Tab → 전체 페이지로 열기, 필터 ⇧Tab → "»", "⌄" Tab → the first column's head, → on 필터 stays).
  const [searchOpen, setSearchOpen] = useState(Boolean(search));
  // Another view shows with its search put away, as Notion's (seen 2026-11-05).
  const searchedView = useRef(snapshot.activeView.id);
  useEffect(() => {
    if (searchedView.current === snapshot.activeView.id) return;
    searchedView.current = snapshot.activeView.id;
    setSearchOpen(false);
  }, [snapshot.activeView.id]);
  // "»" folds the tools away for this person on this browser, by inline database block, as Notion keeps it (seen
  // 2026-10-30, its local preference store); a row page's data tab too (2026-11-24, two tabs: in its tools' share,
  // folded to "«" alone); a widget's head and a layout builder's data tab keep theirs unfolded (no "»", 2026-11-24).
  const tabMode = usePageLayoutTabMode(), builderTab = tabMode === "builder", foldBlockId = ownPage || widget || builderTab ? undefined : blockId;
  const [minimized, setMinimized] = useState(() => Boolean(foldBlockId) && personValue(TOOLBAR_MINIMIZED, { part: foldBlockId }) === "1");
  // A touch screen has no "»": Notion's iPad and phone draw none (seen
  // 2026-11-14 at 834 and 1194), so a database folded on a desktop shows its
  // tools there.
  const changeMinimized = foldBlockId && !touch ? (next: boolean) => {
    setMinimized(next);
    storePersonValue(TOOLBAR_MINIMIZED, next ? "1" : null, { part: foldBlockId });
  } : undefined;
  const [editingTemplateId, setEditingTemplateId] = useState<string | null>(null);
  // A template just made opens in its editor once it is there, as Notion's
  // "새 템플릿" does (seen 2026-10-06), with the caret in its title; so does a
  // copy just made, as its "복제" (seen 2026-10-30), without it.
  const [madeTemplate, setMadeTemplate] = useState<{ id: string; focusTitle: boolean } | null>(null);
  // Whether a template's body holds a block, as its window last told, for its
  // icon before the menu reads the templates again.
  const [templateContent, setTemplateContent] = useState<Record<string, boolean>>({});
  const [editingMade, setEditingMade] = useState(false);
  // Which menus open docked at the page's end, read as they open (set below, with what decides it).
  const docksRef = useRef<((menu: DatabaseToolbarMenu) => boolean) | null>(null);
  const controller = useDatabaseToolbarController(snapshot, operations, docksRef);
  const toggleMenu = controller.toggleMenu;
  useTemplateAddress(useMemo(() => snapshot.templates.map((template) => template.id), [snapshot.templates]), editingTemplateId,
    (templateId) => { setEditingMade(false); setEditingTemplateId(templateId); });
  const completedStarter = useRef<{ viewId: string; context: string; settings: false | "new" | "picked" } | null>(null);
  useEffect(() => {
    const completed = completedStarter.current;
    if (!completed || snapshot.activeView.config.linkedStarterPending === true) return;
    completedStarter.current = null;
    // Where its "새 보기" stood, the page left where it is (linkedViewWindowPlace): "+ 새 표 보기" goes on to the new view's window there, one of its source's views to its settings.
    const at = linkedViewWindowPlace() ?? undefined;
    forgetLinkedViewWindowPlace();
    if (!completed.settings || completed.viewId !== snapshot.activeView.id || completed.context !== snapshot.contextIdentity) return;
    const trigger = toolbarRef.current?.querySelector<HTMLElement>("[data-database-settings-trigger]");
    if (!trigger) return;
    toggleMenu(completed.settings === "new" ? "newView" : "view", trigger, "pointer", at);
    if (completed.settings === "picked") focusPickedViewName(() => controller.menuRef.current);
  }, [snapshot.activeView, snapshot.contextIdentity, toggleMenu]);

  // A cell's "속성 편집" opens the view settings at that property, from the settings button; a card's menu at the layout or properties.
  const [propertyStart, setPropertyStart] = useState<{ id?: string; screen?: "layout" | "properties" | "sprints" | "subitems" | "group" | "subGroup" | "filter"; choosePagesIn?: boolean; request: number } | null>(null);
  const viewSettingsOpen = controller.openMenu === "view";
  // A new view turned to another database's source goes on to that source's views, kept here so a redraw of the panel keeps it.
  const [sourceViews, setSourceViews] = useState<{ viewId: string; candidate: DatabaseContainerSource } | null>(null);
  useEffect(() => { if (controller.openMenu !== "newView") setSourceViews(null); }, [controller.openMenu]);
  useEffect(() => { if (!viewSettingsOpen) setPropertyStart(null); }, [viewSettingsOpen]);
  // On a phone a column's "필터" opens the filter sheet at that property's filter, as there is no bar.
  const [filterStart, setFilterStart] = useState<{ propertyId: string; request: number } | null>(null);
  const filterOpen = controller.openMenu === "filter";
  useEffect(() => { if (!filterOpen) setFilterStart(null); }, [filterOpen]);
  // An empty relation tab's "필터 편집하기" shows its bar (tabFiltered, below).
  const showTabFilters = useRef<(() => void) | null>(null);
  useEffect(() => {
    if (!commandsRef) return;
    const openSettingsAt = (start: { id?: string; screen?: "layout" | "properties" | "subitems" | "group" | "subGroup" | "filter"; choosePagesIn?: boolean }) => {
      const trigger = toolbarRef.current?.querySelector<HTMLElement>("[data-database-settings-trigger]");
      if (!trigger) return;
      setPropertyStart((current) => ({ ...start, request: (current?.request ?? 0) + 1 }));
      // Opened for a property, a column or a cell, not by its button: closed, it leaves the focus on the page.
      if (!viewSettingsOpen) toggleMenu("view", trigger, "pointer", undefined, true);
      if (start.id) revealColumnBesidePanel(trigger, start.id, () => controller.menuRef.current);
    };
    commandsRef.current = {
      openMenu: (menu, trigger) => toggleMenu(menu, trigger, "pointer"),
      openPropertySettings: (propertyId) => openSettingsAt({ id: propertyId }),
      openViewSettings: (screen, choosePagesIn) => screen === "filter" && showTabFilters.current ? showTabFilters.current()
        : openSettingsAt({ screen, choosePagesIn }),
      openFilter: (propertyId) => {
        const trigger = toolbarRef.current?.querySelector<HTMLElement>('[data-database-toolbar-menu="filter"]');
        if (!phone || !trigger) return false;
        setFilterStart((current) => ({ propertyId, request: (current?.request ?? 0) + 1 }));
        if (!filterOpen) toggleMenu("filter", trigger, "pointer");
        return true;
      },
    };
    return () => { commandsRef.current = null; };
  }, [commandsRef, filterOpen, phone, toggleMenu, viewSettingsOpen]);
  // A locked database keeps its rows and templates editable but not its views
  // and properties; a viewer's filters and sorts then apply to them alone.
  const structureLocked = useDatabaseStructureLocked();
  const structureWritable = snapshot.writable && !structureLocked;
  // A linked view waiting for its view asks in "새 보기" (DatabaseLinkedStarter), its "⚙" pressed meanwhile as Notion's (seen 2026-11-08),
  // where it was just linked alone (makingLinkedView); one whose window was left as its page went is the table it waited as, unnamed.
  // Drawn twice, in a peek and on the page under it, it asks in the copy that shows (shownBlockCopy); its "←" goes back
  // where its source was picked (linkedFromStartPanel), none for one made with its page from a source card (seen 2026-11-09).
  const starterWaiting = snapshot.activeView.config.linkedStarterPending === true, shownCopy = useShownBlockCopy(blockId, ownPage);
  const starterOpen = Boolean(session) && structureWritable && starterWaiting && shownCopy && makingLinkedView(snapshot.activeView.id);
  const pickedIn = blockId ? linkedFromStartPanel(blockId) : null;
  // A database's start panel, or "관계형 대상" opened where they open, is its settings' window: "⚙" pressed while it shows, and pressed again it puts it away (databaseStartPanel.ts).
  const placeWindowShown = useSettingsPlaceWindowShown(toolbarRef), startPanelShown = useDatabaseStartPanelShown(blockId) && shownCopy || placeWindowShown;
  const leaveStarter = controller.actions.updateView;
  useEffect(() => {
    if (structureWritable && starterWaiting && takeLeftLinkedView(snapshot.activeView.id)) void leaveStarter(leftLinkedView(snapshot.activeView));
  }, [leaveStarter, snapshot.activeView, starterWaiting, structureWritable]);
  // A linked database's "보기 잠금" leaves its source's properties and settings editable.
  const sourceLocked = useDatabaseSourceLocked();
  const sourceWritable = snapshot.writable && !sourceLocked;
  // A signed-in viewer who can't edit sees the settings as a locked database's, the rows they can't change dimmed.
  const readerSettings = !snapshot.writable && Boolean(session);
  const toast = useOptionalProductToast();
  // A form view's row keeps its settings (a phone's its search and filter), then the builder's
  // "미리보기" and "양식 공유", as Notion's (seen 2026-10-08, the phone's 2026-11-15): no New.
  const form = snapshot.activeView.type === "form";
  // The tools each kind of view keeps, as Notion's (seen 2026-11-03): a chart filters alone, but a phone's searches too (iPhone
  // 402, A90 411, seen 2026-11-18), a map does not sort, a dashboard keeps its settings and a filter of its own (dashboardFilter.ts) —
  // on a computer once it has widgets, on a phone always, its sheet "필터 없음" with no "정렬" (Notion's iPhone Safari at 402, seen 2026-11-16).
  const viewType = snapshot.activeView.type;
  const kindTools = viewType === "chart" ? { sort: false, automations: false, search: phone }
    : viewType === "map" ? { sort: false }
    : viewType === "dashboard" ? { filter: phone || dashboardLayoutOf(snapshot.activeView).length > 0, sort: false, automations: false, search: false } : undefined;
  // A widget's head, as Notion's dashboard (seen 2026-11-05, 2026-11-15): outside Edit mode its filter, sort, search and New; in Edit
  // mode its filter, sort and settings; never "전체 페이지로 열기"; Home's "내 작업" all five and New (seen 2026-11-24, two tabs).
  // A layout builder's data tab, as Notion's (seen 2026-11-12): its filter and sort alone, at its tabs' end.
  const viewTools = widget === "home" ? { ...kindTools, automations: false } : widget ? { ...kindTools, automations: false, openFullPage: false, ...(widget === "view" ? { settings: false } : { search: false }) }
    : builderTab ? { ...kindTools, search: false } : kindTools;
  const automationCount = useDatabaseAutomationCount(snapshot.dataSource.id, snapshot.dataSource.automationCount, automation?.service);
  const folded = minimized && Boolean(changeMinimized) && !phone && !form;
  // A row page's data tab has no settings to take "새로 만들기" in: it keeps it however narrow (its narrowest seen, "» 새로 만들기 ⌄").
  const cramped = !widget && !phone && tabMode !== "page" && rowWidth > 0 && rowWidth < CRAMPED_DATABASE_ROW;
  const newRowControl = useContext(DatabaseNewRowControlContext), rowsAddable = useContext(DatabaseRowsAddableContext);
  const newRow = snapshot.writable && rowsAddable && !viewingArchived && !folded && !form && viewType !== "dashboard" && widget !== "edit" && !builderTab;
  docksRef.current = (menu) => !phone && settingsDocked(menu, { ownPage, narrowNew: newRow && cramped, viewType });
  const editingTemplate = snapshot.templates.find((template) => template.id === editingTemplateId);
  const sorts = Array.isArray(snapshot.activeView.config.sorts) ? snapshot.activeView.config.sorts : [];
  // While the view has quick or advanced filters, "필터" shows or hides their
  // bar, as Notion's does (seen 2026-09-30); otherwise it opens its property list.
  // A viewer who can't edit the view, signed in or visiting its published
  // page, has the same button and bar, its pills their own (Notion's
  // visitor's "필터" opens "필터 기준" too, seen 2026-11-12); a locked
  // database's stay in its settings. A phone has no bar: its "필터 및 정렬"
  // always opens the sheet (seen 2026-10-24).
  const filterBarSurface = useDatabaseFilterBar();
  const filterBar = structureWritable || (!structureLocked && Boolean(onOwnQueryChange)) ? filterBarSurface : null;
  // A board's, gallery's or calendar's sub-item option colors the filter icon alone (cardSubitemFilter).
  const subitemFilter = cardSubitemFilter(snapshot.activeView, snapshot.taskConfiguration.subitems);
  const quickFilterCount = Array.isArray(snapshot.activeView.config.quickFilters) ? snapshot.activeView.config.quickFilters.length : 0;
  // A relation tab's pages, which the page's layout sets, are its filter, as
  // Notion's (seen 2026-11-12): "필터" lit, it shows the bar with that pill, as
  // does an empty tab's "필터 편집하기" (PageLayoutTabFilterContext).
  const tabFilter = usePageLayoutTabFilter();
  const tabFiltered = tabFilter?.viewId === snapshot.activeView.id;
  showTabFilters.current = tabFiltered && filterBar && !phone ? () => { if (!filterBar.shown) filterBar.toggle(); } : null;
  // So does "정렬" while the view has sorts, whose pill leads the bar (seen 2026-10-01).
  const toggleToolbarMenu: typeof controller.toggleMenu = (menu, trigger, modality) => {
    if (menu === "view" && (blockId && closeShownDatabaseStartPanel(blockId) || closeSettingsPlaceWindow(toolbarRef.current))) return;
    if (!phone && (menu === "filter" && (quickFilterCount > 0 || activeFilterCount(snapshot) > 0 || tabFiltered || filterBar?.keptEmpty) || menu === "sort" && sorts.length > 0) && filterBar) {
      controller.closeMenu(false);
      filterBar.toggle();
      return;
    }
    controller.toggleMenu(menu, trigger, modality);
  };

  useEffect(() => {
    if (!searchOpen) return;
    return focusOpenedField(() => searchRef.current);
  }, [searchOpen]);

  function editTemplate(templateId: string, made = false) {
    const template = snapshot.templates.find((candidate) => candidate.id === templateId);
    if (!template) return;
    setEditingMade(made);
    setEditingTemplateId(templateId);
    controller.closeMenu(false);
  }
  useEffect(() => {
    if (!madeTemplate || !snapshot.templates.some((template) => template.id === madeTemplate.id)) return;
    setMadeTemplate(null);
    editTemplate(madeTemplate.id, madeTemplate.focusTitle);
  });

  const menuHandlers = useMemo(() => ({
    createTemplate() {
      const position = `${snapshot.templates.at(-1)?.position ?? "m"}\u8000`;
      const id = newId();
      setMadeTemplate({ id, focusTitle: true });
      // Unnamed, as Notion makes it: a template is a page, "새 페이지" until titled; what is typed next is its name.
      void openTemplateAtItsTitle(id, () => controller.actions.createTemplate({ id, name: "", position }));
    },
    // The copy opens with its title's caret at its end, as Notion's (seen 2026-11-04), but a phone's (openTemplateCopy).
    duplicateTemplate(templateId: string) {
      const id = newId();
      setMadeTemplate({ id, focusTitle: openTemplateCopy(id, () => controller.actions.duplicateTemplate(templateId, id)) });
    },
    // Notion asks first (seen 2026-10-30), its "삭제" filled red (2026-11-08);
    // the template goes to the trash.
    async deleteTemplate(templateId: string) {
      if (!await confirmAction({ message: t("templateMenu.confirmDelete"), confirmLabel: t("templateMenu.delete"), danger: "filled" })) return;
      void controller.actions.deleteTemplate(templateId);
    },
  }), [controller.actions, snapshot.templates, t]);
  // A database page's empty body asks here for a new template, and lists the live ones.
  const templatesWritable = snapshot.writable && !viewingArchived;
  // Offered by what they are, so a view drawn again offers nothing new (its pages' lists don't redraw it).
  const templateKey = JSON.stringify(snapshot.templates.filter((template) => !template.deleted)
    .map((template) => ({ id: template.id, name: template.title.trim() || template.name, titled: Boolean(template.title.trim()), icon: template.icon,
      repeating: templateRepeats(template), isDefault: template.isDefault, hasContent: template.id in templateContent ? templateContent[template.id] : template.hasContent })));
  const templateChoices = useMemo(() => JSON.parse(templateKey) as DatabaseTemplateChoice[], [templateKey]);
  useEffect(() => templatesWritable ? offerDatabaseTemplates(snapshot.dataSource.id, templateChoices) : undefined,
    [snapshot.dataSource.id, templateChoices, templatesWritable]);
  useEffect(() => templatesWritable ? listenForDatabaseTemplateRequests(snapshot.dataSource.id, menuHandlers.createTemplate) : undefined,
    [menuHandlers, snapshot.dataSource.id, templatesWritable]);
  // Its pages' templates "···" acts here, on the templates as they are when chosen.
  const templateActions = useRef<DatabaseTemplateActions | null>(null);
  templateActions.current = {
    template: (templateId) => snapshot.templates.find((template) => template.id === templateId && !template.deleted),
    edit: (templateId) => editTemplate(templateId),
    duplicate: menuHandlers.duplicateTemplate,
    remove: (templateId) => void menuHandlers.deleteTemplate(templateId),
    setRepeat: (templateId, repeat) => void controller.actions.setTemplateRepeat(templateId, repeat),
  };
  const templatesNow = useRef<typeof snapshot.templates | null>(null); // Null till read (newPageTemplate).
  templatesNow.current = snapshot.templatesRead === false ? null : snapshot.templates;
  const offeredTemplateActions = useMemo<DatabaseTemplateActions>(() => ({
    template: (templateId) => templateActions.current?.template(templateId),
    defaultFor: (view) => newPageTemplate(templatesNow.current, view),
    edit: (templateId) => templateActions.current?.edit(templateId),
    duplicate: (templateId) => templateActions.current?.duplicate(templateId),
    remove: (templateId) => templateActions.current?.remove(templateId),
    setRepeat: (templateId, repeat) => templateActions.current?.setRepeat(templateId, repeat),
  }), []);
  useEffect(() => templatesWritable ? offerDatabaseTemplateActions(snapshot.dataSource.id, offeredTemplateActions) : undefined,
    [offeredTemplateActions, snapshot.dataSource.id, templatesWritable]);

  const databaseTitle = snapshot.dataSource.title.trim() || inlineDatabasePlaceholderTitle();
  const viewDefault = viewDefaultTemplate(snapshot.templates, snapshot.activeView);
  // "기본으로 설정" asks how far it reaches, as Notion's (seen 2026-11-03): the
  // database's views, which leaves this view no default of its own (others
  // keep theirs), or this view alone, whose own default wins.
  async function chooseDefaultTemplate(templateId: string | null) {
    const template = templateId ? snapshot.templates.find((candidate) => candidate.id === templateId) : undefined;
    const view = snapshot.activeView;
    // The database named by its name alone, as Notion's answer does whatever
    // its icon (seen 2026-11-06: an emoji's and a built-in icon's database alike).
    const scope = await confirmActions({
      message: t("templateMenu.defaultAsk", { name: template ? templateName(template) : t("templateMenu.empty") }),
      actions: [{ value: "all", label: t("templateMenu.defaultAllViews", { database: databaseTitle }) },
        ...(structureWritable ? [{ value: "view", label: t("templateMenu.defaultThisView", { view: viewDisplayName(view) }) }] : [])],
    });
    if (scope === "view") return void controller.actions.updateView(withViewDefaultTemplate(view, templateId ?? ""));
    if (scope !== "all") return;
    const current = snapshot.templates.find((candidate) => candidate.isDefault && !candidate.deleted);
    if (template && !template.isDefault) void controller.actions.setDefaultTemplate(template.id, true);
    else if (!template && current) void controller.actions.setDefaultTemplate(current.id, false);
    if (structureWritable && VIEW_DEFAULT_TEMPLATE in view.config) void controller.actions.updateView(withViewDefaultTemplate(view, null));
  }

  const templateMenu: DatabaseTemplateMenuActions = {
    databaseTitle,
    defaultTemplateId: viewDefault?.id,
    templates: snapshot.templates.map((template) => template.id in templateContent
      ? { ...template, hasContent: templateContent[template.id] } : template),
    writable: snapshot.writable, templatesWritable,
    // The menu goes as the row comes: what is typed next goes to the new page.
    onCreateRow: (templateId) => {
      controller.closeMenu(false);
      void holdTypingWhile(() => controller.actions.createRow(templateId));
    },
    onCreateTemplate: menuHandlers.createTemplate,
    onDeleteTemplate: (templateId) => void menuHandlers.deleteTemplate(templateId),
    onDuplicateTemplate: menuHandlers.duplicateTemplate,
    onEditTemplate: editTemplate,
    onMoveTemplate: (templateId, position) => void controller.actions.updateTemplate(templateId, { position }),
    onAddShortcut: session ? () => void addDatabaseShortcut(session, snapshot.workspaceId, {
      pageId: snapshot.pageId, dataSourceId: snapshot.dataSource.id, view: snapshot.activeView,
      templateId: keptPageTemplate(templatesNow.current, snapshot.activeView), // Null till read: read then.
    }, (message) => toast?.notify(message, "error")).catch(() => undefined) : undefined,
    onChooseDefault: (templateId) => void chooseDefaultTemplate(templateId),
    onSetRepeat: (templateId, repeat) => void controller.actions.setTemplateRepeat(templateId, repeat),
  };
  return (
    <>
      <div
        ref={toolbarRef}
        className={styles.dbToolbar}
        role="toolbar"
        aria-label={t("databaseToolbar")}
        data-database-toolbar={snapshot.dataSource.id}
        data-narrow-row={!widget && rowWidth > 0 && rowWidth < NARROW_DATABASE_ROW ? "true" : undefined}
        data-toolbar-menu-input-modality={controller.inputModality}
        tabIndex={-1}
      >
        <DatabaseSprintCompleteAction activeViewId={snapshot.activeView.id} writable={structureWritable} rows={snapshot.rows}
          properties={snapshot.properties} statusPropertyId={snapshot.taskConfiguration.tasks?.statusPropertyId}
          onSettings={() => {
            const trigger = toolbarRef.current?.querySelector<HTMLElement>("[data-database-settings-trigger]");
            if (!trigger) return;
            setPropertyStart((current) => ({ screen: "sprints", request: (current?.request ?? 0) + 1 }));
            if (!viewSettingsOpen) toggleMenu("view", trigger, "pointer");
          }} />
        <DatabaseToolbarControls filterCount={activeFilterCount(snapshot) + quickFilterCount + (subitemFilter ? 1 : 0) + (tabFiltered ? 1 : 0)} filtersApplied={appliedFilterItems(snapshot.activeView.config).length > 0 || Boolean(subitemFilter) || tabFiltered}
          openMenu={starterOpen || startPanelShown ? "view" : controller.openMenu} readOnly={!structureWritable} ownQuery={Boolean(onOwnQueryChange)} unsavedQuery={ownQueryChanged(snapshot.activeView, ownQuery)}
          unsavedFilters={Boolean(ownQuery) && !sameViewQuery(filterPart(snapshot.activeView.config), filterPart(ownQuery!.saved.config))}
          unsavedSorts={Boolean(ownQuery) && !sameViewQuery({ sorts: snapshot.activeView.config.sorts }, { sorts: ownQuery!.saved.config.sorts })}
          settingsWritable={snapshot.writable} readerSettings={readerSettings}
          search={search} searchOpen={searchOpen} searchRef={searchRef}
          sortCount={sorts.length} onSearchChange={onSearchChange}
          onSearchOpenChange={setSearchOpen} onToggleMenu={toggleToolbarMenu} narrow={narrow} cramped={cramped} phone={phone} touch={touch} form={form} viewTools={viewTools} automations={Boolean(automation)} automated={automationCount > 0} trashCheck={{ pageId: snapshot.pageId, source: snapshot.dataSource }}
          minimized={minimized} onMinimizedChange={changeMinimized} viewControlsSlotRef={viewControlsSlotRef} />
        {/* A form's "미리보기" and "양식 공유", a dashboard's "편집하기" / "완료", come after the settings; a phone's search, open, leaves no "+ ⌄" (DatabaseToolbarControls). */}
        {form || viewType === "dashboard" ? <div ref={formBarSlotRef} className={styles.dbToolbarGroup} data-form-toolbar-slot />
          : newRowControl ? (cramped || phone && searchOpen ? null : newRowControl) : (newRow && !cramped && !(phone && searchOpen) ? <DatabaseTemplateMenu open={controller.openMenu === "templates"} {...templateMenu}
          onToggle={(trigger, modality) => controller.toggleMenu("templates", trigger, modality)}
          onClose={() => controller.closeMenu()} phone={phone || touch} /> : null)}
      </div>
      {/* Mount after the toolbar host so its anchor ref exists in the child layout effect. */}
      {starterOpen && session ? <DatabaseLinkedStarter key={snapshot.activeView.id}
        session={session} view={snapshot.activeView} sourceTitle={databaseTitle} toolbarRef={toolbarRef} alone={ownPage && !phone}
        onCommit={(view, settings) => {
          completedStarter.current = { viewId: view.id, context: snapshot.contextIdentity, settings };
          return controller.actions.updateView(view);
        }} onBack={blockId && pickedIn ? () => requestDatabaseStartPanel(blockId, { relink: true, sources: pickedIn === "sources" }) : undefined} /> : null}
      <DatabaseToolbarMenus activeView={snapshot.activeView} automation={automation}
        contextIdentity={snapshot.contextIdentity} menuIdentity={snapshot.settingsIdentity ?? snapshot.menuIdentity ?? snapshot.contextIdentity} dataSource={snapshot.dataSource}
        dataSources={dataSources} filterSnapshot={snapshot.filterSnapshot}
        inputModality={controller.inputModality}
        menuRef={controller.menuRef} toolbarRef={toolbarRef} menuStyle={controller.menuStyle} menuBackdropStyle={controller.menuBackdropStyle}
        dockedLayer={controller.dockedLayer}
        narrowNew={newRow && cramped ? templateMenu : undefined}
        openMenu={controller.openMenu} properties={snapshot.properties}
        snapshotIdentity={snapshot.snapshotIdentity} taskConfiguration={snapshot.taskConfiguration}
        writable={structureWritable} sourceWritable={sourceWritable} onClose={controller.closeMenu}
        onBackToSettings={phone || controller.menuFrom === "view" ? () => controller.switchMenu("view") : undefined}
        onShowSettings={() => controller.switchMenu("view")}
        onOpenSort={phone && kindTools?.sort !== false ? () => {
          const trigger = toolbarRef.current?.querySelector<HTMLElement>('[data-database-toolbar-menu="filter"]');
          if (trigger) controller.toggleMenu("sort", trigger, "pointer");
        } : undefined}
        phone={phone}
        wideSheets={touch && !phone}
        onOpenFilter={phone ? () => {
          const trigger = toolbarRef.current?.querySelector<HTMLElement>('[data-database-toolbar-menu="filter"]');
          if (trigger) controller.toggleMenu("filter", trigger, "pointer");
        } : undefined}
        onDoneNewView={(picked) => {
          // Leaving the creation rail widens the tabs. Anchor the normal settings to their stable toolbar
          // button, after that layout turn; a linked view's stay where its "새 보기" stood (menuAt).
          window.requestAnimationFrame(() => {
            const trigger = toolbarRef.current?.querySelector<HTMLElement>("[data-database-settings-trigger]");
            if (trigger) controller.toggleMenu("view", trigger, "pointer", controller.menuAt);
            // One of its source's views picked: its name takes the keys, a touch screen's keyboard rising too (focusPickedViewName).
            if (trigger && picked === true) focusPickedViewName(() => controller.menuRef.current);
          });
        }}
        onCommitProperties={(properties) => void controller.actions.commitProperties(properties)}
        onConfigureTaskFeatures={(configuration, made) => controller.actions.configureTaskFeatures(configuration, made)}
        onCreateProperty={onCreateProperty}
        onUpdateView={(view) => structureWritable ? void controller.actions.updateView(view) : onOwnQueryChange?.(view)}
        onTurnIntoDashboard={structureWritable && operations.turnIntoDashboard ? () => void controller.actions.turnIntoDashboard(snapshot.activeView) : undefined}
        onLeaveDashboard={structureWritable && operations.leaveDashboard ? (next) => void controller.actions.leaveDashboard(snapshot.activeView, next) : undefined}
        searchRelationDatabases={searchRelationDatabases} deletedProperties={deletedProperties}
        pagesById={snapshot.templatePropertyCells.environment?.snapshot.pagesById}
        propertiesByDataSourceId={snapshot.templatePropertyCells.environment?.snapshot.propertiesByDataSourceId}
        onAddQuickFilter={filterBar ? (propertyId) => { controller.closeMenu(false); filterBar.add(propertyId, controller.menuTrigger.current); } : undefined}
        onAdvancedAdded={filterBar ? () => { controller.closeMenu(false); filterBar.editAdvanced(controller.menuTrigger.current); } : undefined}
        onSortPicked={filterBar ? () => { controller.closeMenu(false); filterBar.editSorts(controller.menuTrigger.current); } : undefined}
        onResetOwnQuery={onResetOwnQuery} ownQuery={ownQuery} filterStart={filterStart}
        queryEditable={structureWritable || Boolean(onOwnQueryChange)} widgetFilters={Boolean(widget)}
        rows={snapshot.rows}
        onDeleteProperty={sourceWritable ? (propertyId) => void controller.actions.deleteProperty(propertyId) : undefined}
        viewSettings={{
          locked: (snapshot.writable && structureLocked) || readerSettings,
          sourceLocked: (snapshot.writable && sourceLocked) || readerSettings,
          reader: readerSettings,
          shownCount: projectVisibleProperties({ properties: snapshot.properties, ...tableColumnSettings(snapshot.activeView.config) }).length,
          // A relation tab's filter first, named as its pill names it, as Notion's (seen 2026-11-12).
          filterCount: activeFilterCount(snapshot) + quickFilterCount + (tabFiltered ? 1 : 0),
          filterNames: [...(tabFiltered && tabFilter ? [tabFilter.label] : []), ...appliedFilterItems(snapshot.activeView.config)
            .flatMap((id) => id === null ? [null] : snapshot.properties.find((property) => property.id === id)?.name ?? [])],
          sortNames: sorts.flatMap((sort) => snapshot.properties.find((property) => property.id === (sort as { propertyId?: unknown })?.propertyId)?.name ?? []),
          groupName: snapshot.properties.find((property) => property.id === (snapshot.activeView.type === "board"
            ? boardGroupPropertyId(snapshot.activeView.config, snapshot.properties) : snapshot.activeView.config.groupBy))?.name,
          subGroupName: snapshot.properties.find((property) => property.id === snapshot.activeView.config.subGroupBy)?.name,
          colorAvailable: CONDITIONAL_COLOR_VIEW_TYPES.has(snapshot.activeView.type),
          onCopyLink: onCopyViewLink ? () => onCopyViewLink(snapshot.activeView.id) : undefined,
          onAutomations: automation ? () => controller.switchMenu("automations") : undefined,
          onEditLayout: session && structureWritable ? onEditLayout : undefined,
          startProperty: propertyStart ?? undefined,
          sourceViews: sourceViews?.viewId === snapshot.activeView.id ? sourceViews.candidate : undefined,
          onSourceViews: (candidate) => setSourceViews(candidate ? { viewId: snapshot.activeView.id, candidate } : null),
        }} />
      {editingTemplate && (
        <DatabaseTemplateEditor session={session} template={editingTemplate} focusTitle={editingMade}
          databaseTitle={snapshot.dataSource.title.trim() || inlineDatabasePlaceholderTitle()}
          databaseIcon={templateBandDatabaseIcon(databasePage, snapshot.dataSource, snapshot.workspaceId)}
          properties={snapshot.properties} propertyCells={snapshot.templatePropertyCells}
          writable={snapshot.writable} workspaceId={snapshot.workspaceId}
          pageId={snapshot.pageId} dataSourceId={snapshot.dataSource.id}
          onClose={() => setEditingTemplateId(null)}
          onSetRepeat={(repeat) => void controller.actions.setTemplateRepeat(editingTemplate.id, repeat)}
          onDuplicate={() => menuHandlers.duplicateTemplate(editingTemplate.id)}
          onBodyContentChange={(hasContent) => setTemplateContent((current) => current[editingTemplate.id] === hasContent
            ? current : { ...current, [editingTemplate.id]: hasContent })}
          onRestore={() => controller.actions.restoreTemplate(editingTemplate.id)}
          onDeleteForever={() => controller.actions.deleteTemplateForever(editingTemplate.id)}
          onDelete={() => { void controller.actions.deleteTemplate(editingTemplate.id); setEditingTemplateId(null); }}
          onCommitProperties={structureWritable ? (properties) => controller.actions.commitProperties(properties) : undefined} />
      )}
    </>
  );
}

export type { DatabaseToolbarOperations, DatabaseToolbarSnapshot };
