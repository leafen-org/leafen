"use client";

import { markDashboardMade } from "./dashboardJustMade";
import { NativeDatabaseViewTab, useSprintTabName } from "./DatabaseViewTab";
import { useCommittedOperationScope } from "../product/useCommittedOperationScope";
import { usePageLayoutTabMode } from "./pageLayoutTabEnvironment";

import { useContext, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { holdMeasure } from "./heldStyleWrites";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import { watchPageSize } from "../page/pageSizeWatch";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { usePressShowsWithin } from "@/lib/pressShowsWithin";
import { createPortal, flushSync } from "react-dom";
import { useTabList } from "react-aria";
import { Item, useTabListState } from "react-stately";
import type { GraphView, GraphViewType } from "@/contentGraph";
import { ArrowUpRightIcon, ChevronDown, Copy, DataPathIcon, DotsHorizontal, EyeIcon, EyeSlashIcon, FileText, LinkIcon, MenuIcon, PageLayoutIcon, PaintBrushIcon, Pencil, Plus, SlidersIcon, SmileIcon, Trash } from "@/icons/leafen";
import { requestPageLayoutBuilder } from "@/lib/databasePageLayout";
import { databaseLinksSource } from "@/lib/nativeDataSourceManagement";
import { requestInlineDatabase } from "@/lib/inlineDatabaseRequests";
import { useProductToast } from "@/lib/productToast";
import { useRouter } from "@/lib/router";
import { confirmAction } from "../ConfirmActionDialog";
import { useDatabaseFullPageLink, useDatabaseOwnPage, useDatabasePageTitleNamesSource } from "./databaseFullPageLink";
import {
  placeAround, popoverBackdropStyle, popoverPlacementStyle, type PopoverRect, popoverRectOf, usePopoverPlacement,
} from "@/lib/popoverPlacement";
import { databaseViewLabels } from "./databaseViewLabels";
import { useCurrentSprintViewNames, useSprintBoardPage } from "./databaseSprintContext";
import { NOTION_2023_VIEW_TYPES, ViewTabGlyph, viewDisplayName, viewOwnName } from "./databaseViewShared";
import { DatabaseAddViewChoices, DatabaseViewActionChoices, DatabaseViewActionsBeside, DatabaseViewAddBeside, DatabaseViewTabDisplayChoices, type DatabaseViewAction } from "./DatabaseTabMenus";
import { useViewTabDisplay } from "./databaseViewTabDisplay";
import { DatabaseViewList, type ViewListLead } from "./DatabaseViewList";
import { WIKI_ADD_VIEW_TYPES, type WikiViews } from "./wikiViewsContext";
import { WIKI_HOME_VIEW_ID } from "@/lib/wikiHome";
import { PhoneViewMenus } from "./DatabaseViewPhoneSheets";
import { useProductPages } from "@/components/product/productPagesContext";
import { sidebarRefusalLabel, sidebarViewRefusal, useViewToSidebar } from "@/components/sidebar/useViewToSidebar";
import { DatabaseDataSourcesContext, surfaceDataSourceName, viewSourceSwitchable } from "./databaseDataSourcesContext";
import { visibleDatabaseViewTabs } from "./databaseViewModel";
import { textWidth, useTextWidthsVersion } from "@/lib/textWidth";
import type {
  DatabaseViewOperationContext,
  DatabaseViewOperations,
  DatabaseViewSnapshot,
} from "./DatabaseView";
import styles from "./databaseSheet";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { useDragReorder } from "@/lib/useDragReorder";
import { useViewTabRequests } from "./useViewTabRequests";
import { useCreatingView, viewTabName } from "./creatingView";
import { viewLimitRefusal } from "./databaseViewLimit";

type MenuState =
  | { kind: "add" | "overflow" | "titleAdd"; anchor: PopoverRect }
  | { kind: "view" | "title"; viewId: string; anchor: PopoverRect }
  | null;

// The layouts a view is added with under "새 보기 추가", from the tab row and
// beside a database's title alike, four to a row and the form last, as
// Notion's (seen 2026-10-30; it had five to a row and no form on 2026-10-11).
const ADD_VIEW_TYPES = NOTION_2023_VIEW_TYPES.map(({ type }) => type);
// A phone's sheet as it was seen (2026-10-11), without the form.
const PHONE_ADD_VIEW_TYPES = ADD_VIEW_TYPES.filter((type) => type !== "form");
// A database that links its source offers no form view, its ten layouts five
// to a row (Notion's, seen 2026-11-11 beside a linked database's title).
const LINKED_ADD_VIEW_TYPES = PHONE_ADD_VIEW_TYPES;
const WIKI_VIEW_ACTIONS = new Set(["rename", "edit", "link", "duplicate", "delete"]);
// How soon after a resize a new drawing of the tabs is taken as that resize's.
const RESIZE_DRAWN_MS = 250;

export function DatabaseViewTabs({
  snapshot,
  operations,
  panelId,
  onNewViewSettings,
  onViewSettings,
  onCopyViewLink,
  titleRow = false,
  titleActionsSlot = null,
  wiki,
}: {
  snapshot: DatabaseViewSnapshot;
  operations: DatabaseViewOperations;
  panelId: string;
  onNewViewSettings?(trigger: HTMLElement): void;
  /** Opens the active view's settings, as a duplicated view's open. */
  onViewSettings?(trigger: HTMLElement): void;
  onCopyViewLink?(viewId: string): void;
  /** The one view's tools sit beside the database's title instead of its tab. */
  titleRow?: boolean;
  /** On a phone with more views, where the title row's "⌄" goes. */
  titleActionsSlot?: HTMLElement | null;
  /** A wiki's database: its views in one picker with the wiki's Home first. */
  wiki?: WikiViews;
}) {
  const labels = databaseViewLabels();
  // At its limit "새 보기", "새 데이터 소스" and "보기 복제" are dim, saying why (Notion's, seen 2026-11-12).
  const atViewLimit = viewLimitRefusal(snapshot.views.length);
  const creating = useCreatingView(), sprintTabName = useSprintTabName();
  const tabName = (view: GraphView) => sprintTabName(view, viewTabName(view, creating, labels.newView));
  const dataSources = useContext(DatabaseDataSourcesContext);
  const newViewSettings = useRef<string | null>(null);
  const newSettingsCallback = useRef(onNewViewSettings); newSettingsCallback.current = onNewViewSettings;
  const { notify } = useProductToast();
  const router = useRouter();
  const fullPageHref = useDatabaseFullPageLink();
  const ownPage = useDatabaseOwnPage();
  // A locked database's views stay as they are.
  const structureLocked = useDatabaseStructureLocked();
  // A signed-in reader sets how the tabs show for themselves; a site's visitor doesn't.
  const signedIn = Boolean(useProductPages()?.session);
  const addToSidebar = useViewToSidebar(snapshot.holderPageId), sprintBoard = useSprintBoardPage(snapshot.holderPageId);
  const sprintNames = useCurrentSprintViewNames();
  // Its title row shows unless the page's title names its one source (InlineDatabase).
  const pageTitleNamesSource = useDatabasePageTitleNamesSource();
  const viewsReadOnly = snapshot.readOnly || structureLocked;
  // A layout tab is one view, named by its tab.
  const layoutTab = usePageLayoutTabMode() !== null;
  const rootRef = useRef<HTMLDivElement>(null);
  // A menu's opener, and the tab of a view its menu deleted, to which it gives no focus back.
  const menuTriggerRef = useRef<HTMLElement>(null), goneTabRef = useRef<HTMLElement | null>(null);
  const tabRefs = useRef(new Map<string, HTMLButtonElement>());
  const createdViewFocusRef = useRef<string | null>(null);
  const [availableWidth, setAvailableWidth] = useState(900);
  // The tabs' type, read as the room is (textWidth).
  const [typeFamily, setTypeFamily] = useState<string | null>(null);
  const [menu, setMenu] = useState<MenuState>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  // How this person shows this database's tabs, and the row whose choices of it are open.
  const [tabDisplay, setTabDisplay] = useViewTabDisplay(snapshot.dataSource.id);
  const [displayRow, setDisplayRow] = useState<HTMLElement | null>(null);
  // The views list's "새 보기" row while the view types are open beside it.
  const [addRow, setAddRow] = useState<HTMLElement | null>(null);
  // A listed view's "⋯" while its actions are open over the list.
  const [listActions, setListActions] = useState<{ viewId: string; anchor: HTMLElement } | null>(null);
  useEffect(() => { if (menu?.kind !== "overflow") { setAddRow(null); setListActions(null); } }, [menu?.kind]);
  useEffect(() => { if (!menu) setDisplayRow(null); }, [menu]);
  const lifecycle = useCommittedOperationScope([snapshot.contextIdentity]);
  // Notion's line holds what is up to its tools (seen 2026-11-02); the
  // line's gap before the tools is not the tabs' to take.
  const measureRoom = () => {
    const root = rootRef.current;
    if (!root) return;
    const tools = root.parentElement?.querySelector<HTMLElement>(":scope > [data-database-toolbar]");
    const left = root.getBoundingClientRect().left, toolsLeft = tools?.getBoundingClientRect().left ?? 0;
    const gap = parseFloat(getComputedStyle(root.parentElement ?? root).columnGap) || 0;
    const room = toolsLeft > left ? toolsLeft - left - gap : root.clientWidth;
    setAvailableWidth((current) => Math.abs(current - room) > 0.5 ? room : current);
    setTypeFamily(getComputedStyle(root).fontFamily);
  };
  // As they resize, with the page's other measures, before their writes
  // (pageSizeWatch): with an observer of its own it measured after them, as
  // a side peek slid in or out, laying the page out again in that frame
  // (some 33 ms on a table of 60 rows, NAS condition, CPU ×2, 2026-11-13).
  const resizedAt = useRef(-Infinity);
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const tools = root.parentElement?.querySelector<HTMLElement>(":scope > [data-database-toolbar]");
    const resized = () => { resizedAt.current = performance.now(); measureRoom(); };
    const stopRoot = watchPageSize(root, resized), stopTools = tools ? watchPageSize(tools, resized) : () => {};
    return () => { stopRoot(); stopTools(); };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [titleRow]);
  // Tools moved by the tabs drawn before (none resized) are measured again once other tabs are drawn, or the
  // room first guessed stays; with the view's other first measures once the page is drawn (holdMeasure), and
  // not while the same tabs stand. Tabs a resize drew are measured as the next frame starts, laid out by then:
  // measured at once, the page was laid out again in the frame of each fold as a side peek slid in (9 ms
  // each, three a slide, a table of 60 rows, NAS condition, CPU ×2, 2026-11-13).
  const measuredTabs = useRef<string | null>(null);
  const nextFrame = useRef(0);
  useEffect(() => () => cancelAnimationFrame(nextFrame.current), []);
  useLayoutEffect(() => {
    const root = rootRef.current;
    const drawn = root ? `${root.childElementCount}|${root.textContent}` : null;
    if (drawn === measuredTabs.current) return;
    const first = measuredTabs.current === null;
    measuredTabs.current = drawn;
    const measure = () => { measureRoom(); measureOverflow(); };
    cancelAnimationFrame(nextFrame.current);
    if (!first && performance.now() - resizedAt.current < RESIZE_DRAWN_MS) {
      nextFrame.current = requestAnimationFrame(() => flushSync(measure));
    } else holdMeasure(measuredTabs, measure);
  });
  // Moving to another view or source closes the menus; a view just made
  // still opens its settings once it shows, through the moves on the way.
  useEffect(() => {
    setMenu(null);
    if (createdViewFocusRef.current !== newViewSettings.current) newViewSettings.current = null;
  }, [snapshot.contextIdentity]);
  useEffect(() => {
    const viewId = createdViewFocusRef.current;
    if (!viewId || snapshot.activeView.id !== viewId) return;
    createdViewFocusRef.current = null;
    const frame = window.requestAnimationFrame(() => {
      const tab = tabRefs.current.get(viewId);
      tab?.focus();
      if (newViewSettings.current === viewId && !viewsReadOnly && rootRef.current) {
        newViewSettings.current = null; newSettingsCallback.current?.(rootRef.current);
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, [snapshot.activeView.id, viewsReadOnly]);

  const context = (requestKey: string): DatabaseViewOperationContext => ({
    contextIdentity: snapshot.contextIdentity,
    dataSourceId: snapshot.dataSource.id,
    requestGeneration: snapshot.requestGeneration,
    requestKey,
    signal: lifecycle.signal,
    snapshotIdentity: snapshot.snapshotIdentity,
    viewId: snapshot.activeView.id,
  });
  // A tab as Notion draws it: its name at 14px/500, 12px in at each side,
  // the icon 20 and 6 before the name; 45 as an icon alone; 220 at most.
  // A view being made is reckoned by its own name, not "새 보기": Notion's
  // row folds no more tabs while one is made (seen 2026-11-06: 줄 stayed).
  // Reckoned again once the product's fonts load (textWidth.ts).
  useTextWidthsVersion();
  const tabWidth = (view: GraphView) => {
    if (tabDisplay === "icon") return 45;
    const text = typeFamily ? textWidth(sprintTabName(view, viewDisplayName(view)), "500 14px", typeFamily) : null;
    return text === null ? undefined : Math.min(220, (tabDisplay === "text" ? 24 : 50) + text);
  };
  // On a phone the row is Notion's view picker alone (seen 2026-10-17, 390px);
  // a touch screen wider than one keeps the tabs and takes the title's "⌄",
  // as Notion's iPad (seen 2026-11-14 at 834): the "⌄" and its "새 보기 추가"
  // are sheets there, the tabs' menus the desktop's popovers.
  const { phone: mobile, touch } = useSidebarLayout(true);
  // Its tabs' "+" shows on a tablet once the row is pressed, as under the pointer (CSS).
  usePressShowsWithin(rootRef, touch && !mobile);
  // "N개 더 보기" keeps all of its room past Notion's 80, a longer count
  // folding one more tab rather than running under the tools. Where it is not
  // drawn the width last measured stays, or a line folded to its picker would
  // lose the room, unfold and fold again without end.
  const overflowRef = useRef<HTMLDivElement>(null);
  const [overflowWidth, setOverflowWidth] = useState(0);
  // Measured with the room (above).
  const measureOverflow = () => {
    const width = overflowRef.current?.getBoundingClientRect().width ?? 0;
    if (width > 0) setOverflowWidth((current) => Math.abs(width - current) > 0.5 ? width : current);
  };
  // A wiki's views are its picker alone, on the toolbar's row (Notion's, seen 2026-11-11).
  const projected = mobile || wiki ? { visible: [], overflow: snapshot.views.map(({ id }) => id), collapsed: true }
    : visibleDatabaseViewTabs(snapshot.views, snapshot.activeView.id, availableWidth, Math.max(0, overflowWidth + 1 - 80), tabDisplay, tabWidth);
  const visible = projected.visible.flatMap((id) => snapshot.views.find((view) => view.id === id) ?? []);
  const overflow = projected.overflow.flatMap((id) => snapshot.views.find((view) => view.id === id) ?? []);

  const tabsState = useTabListState({ items: visible, selectedKey: snapshot.activeView.id,
    onSelectionChange: key => { const view = snapshot.views.find(view => view.id === key); if (view) selectView(view); },
    children: view => <Item key={view.id} textValue={viewDisplayName(view)}>{viewDisplayName(view)}</Item> });
  const { tabListProps } = useTabList({ "aria-label": labels.viewsFor(snapshot.displayTitle), keyboardActivation: "automatic" }, tabsState, rootRef);

  async function run<Result>(
    requestKey: string,
    perform: (owner: DatabaseViewOperationContext) => Result | Promise<Result>,
  ) {
    const owner = context(requestKey);
    try {
      return await perform(owner);
    } catch (error) {
      if (operations.isCurrent(owner) && !owner.signal.aborted) operations.onError(error, owner);
      return undefined;
    }
  }

  function openMenu(kind: NonNullable<MenuState>["kind"], element: HTMLElement, viewId?: string) {
    menuTriggerRef.current = element; goneTabRef.current = null;
    const anchor = popoverRectOf(element)!;
    setMenu((kind === "view" || kind === "title") && viewId ? { kind, viewId, anchor } : { kind: kind as "add" | "overflow" | "titleAdd", anchor });
  }

  function selectView(view: GraphView) {
    setMenu(null);
    void run(`view:select:${view.id}`, (owner) => operations.selectView(view, owner));
  }

  // A view made shows at once, chosen on the press's own frame, and is saved
  // behind it (user decision 2026-11-12); `made` follows it once, from the
  // adapter as it shows it, else (a list that shows it only once saved) then.
  function makeView(requestKey: string, type: GraphViewType, made: (created: GraphView) => void) {
    let shown = false;
    const show = (created: GraphView) => { if (!shown) { shown = true; made(created); } };
    void run(requestKey, async (owner) => {
      const created = await operations.createView(type, owner, show);
      if (created) show(created);
      return created;
    });
  }

  function createView(type: GraphViewType) {
    setMenu(null);
    makeView(`view:create:${type}`, type, (created) => {
      createdViewFocusRef.current = created.id;
      // A new form asks about its questions instead, with no settings beside it, as Notion's (seen 2026-11-02);
      // a new dashboard opens in Edit mode, ready for its first widget (seen 2026-11-03).
      newViewSettings.current = type === "form" || type === "dashboard" ? null : created.id;
      if (type === "dashboard") markDashboardMade(created.id);
      selectView(created);
    });
  }

  // The view's settings, from its tab's "⋯": "보기 편집" opens them, "이름
  // 바꾸기" with its name chosen to type over, and "데이터베이스 ›" at its
  // "데이터베이스" screen, as Notion's (seen 2026-10-11).
  function openSettings(view: GraphView, nameFirst: boolean, database = false) {
    setMenu(null);
    if (view.id !== snapshot.activeView.id) selectView(view);
    // The name chosen whole, as Notion's (seen 2026-11-03), what is typed from the press going in (focusOpenedField): the settings mount a few frames later, and the closing menu hands focus back to the tab first — the name takes it back for a few frames. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: the tab's 이름 바꾸기, "zboard" chosen whole and "q" typed over it (Leafen the same).
    const nameField = () => document.querySelector<HTMLInputElement>("[data-view-settings-name]");
    if (nameFirst) focusOpenedField(nameField, { select: true, from: 2, frames: 30 });
    window.requestAnimationFrame(() => {
      if (!rootRef.current) return;
      onViewSettings?.(rootRef.current);
      const keepName = (tries: number, held: number) => {
        const name = nameField(), active = document.activeElement; if (name && (!active || active === document.body || rootRef.current?.contains(active))) name.focus();
        if (tries > 0 && held < 6) window.requestAnimationFrame(() => keepName(tries - 1, name ? held + 1 : held));
      };
      if (nameFirst) keepName(30, 0);
      // The settings mount a few frames later; their "데이터베이스" row opens its screen.
      const openDatabase = (tries: number) => {
        const row = document.querySelector<HTMLButtonElement>('[data-view-setting="database"]');
        if (row) row.click(); else if (tries > 0) window.requestAnimationFrame(() => openDatabase(tries - 1));
      };
      if (database) openDatabase(20);
    });
  }

  async function moveView(view: GraphView, delta: -1 | 1) {
    const ordered = [...snapshot.views];
    const index = ordered.findIndex(({ id }) => id === view.id);
    const target = index + delta;
    if (index < 0 || target < 0 || target >= ordered.length) return;
    [ordered[index], ordered[target]] = [ordered[target], ordered[index]];
    const changed = ordered.map((item, position) => ({ ...item, position: String(position).padStart(6, "0") }));
    await run(`view:reorder:${view.id}`, (owner) => operations.updateViews(changed, owner, true));
  }

  // The copy, "<name> (1)", shows at once with its settings open and its name
  // chosen, as Notion's does (seen 2026-10-11).
  // Its name and settings are written right after it and show with it.
  function duplicateView(view: GraphView) {
    setMenu(null);
    const taken = new Set(snapshot.views.map(viewDisplayName));
    let copies = 1;
    while (taken.has(`${viewDisplayName(view)} (${copies})`)) copies += 1;
    makeView(`view:duplicate:${view.id}`, view.type, (created) => {
      createdViewFocusRef.current = created.id;
      void run(`view:duplicate-config:${created.id}`, (owner) => operations.updateViews(
        [{ ...created, name: `${viewDisplayName(view)} (${copies})`, config: { ...view.config } }], owner));
      if (viewsReadOnly) selectView(created);
      else openSettings(created, true);
    });
  }

  // Notion asks before a view goes, and says it went (seen 2026-10-11). Its menu stays open under the question:
  // kept when it is cancelled, and a views list stays open when its view is gone (seen 2026-11-05). A
  // dashboard goes without asking, empty or with widgets (seen 2026-11-07).
  async function deleteView(view: GraphView) {
    // A view that took no name of its own, its layout's, goes unnamed (Notion's "보기 삭제됨", seen 2026-11-06).
    const name = viewOwnName(view).trim();
    const asked = view.type === "form"
      ? { message: labels.deleteFormViewConfirm(viewDisplayName(view)), detail: labels.deleteFormViewDetail, confirmLabel: labels.deleteFormView, headline: true }
      : { message: labels.deleteViewConfirm, confirmLabel: labels.removeView };
    if (view.type !== "dashboard" && !await confirmAction({ ...asked, danger: true })) return;
    // Its tab's menu leaves the focus on the page, not on the tab going (the tab list would hand it to the
    // one beside it, ringed by the next menu closed by a key), as Notion's (seen 2026-11-17 by a press).
    goneTabRef.current = tabRefs.current.get(view.id) ?? null;
    // Gone from the views list, the list stays open with nothing in it focused, its search unringed, and the
    // next letter typed goes nowhere, as Notion's (2026-11-15 in Chrome after the press only, by "⋯" › "보기
    // 삭제" › "보기 제거": with no search, the row now in its place lit; with a search left without a result,
    // a letter typed after it not reaching it; the 11-12 audit read the focus outside the field too).
    if (listActions?.viewId === view.id) setListActions(null);
    else setMenu(current => current?.kind === "overflow" ? current : null);
    const deleted = await run(`view:delete:${view.id}`, async (owner) => { await operations.deleteView(view, owner); return true; });
    if (deleted) notify(labels.viewDeleted(name), "success");
  }

  // What a wiki's Home asked of these views as it brought them up (WikiHomeViewPicker).
  useEffect(() => {
    const request = wiki?.takeRequest();
    if (!request) return;
    if ("create" in request) { void createView(request.create); return; }
    const view = snapshot.views.find(({ id }) => id === request.viewId);
    if (view) tabActions(view).find(({ id }) => id === request.action)?.act();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // A form's "양식 삭제" and "기존 데이터 소스 연결하기" ask here (lib/viewTabRequests).
  useViewTabRequests({ databaseId: dataSources?.databaseId, views: snapshot.views, readOnly: viewsReadOnly, atLimit: Boolean(atViewLimit), deleteView, createView });

  // A view tab's "⋯", as Notion's (seen 2026-10-11): rename, how the tabs
  // show, the view's settings and its database; its link, the view as a
  // sidebar section (2026-11-03) — dimmed on the Sprint board, as is
  // renaming its current sprint's view —, a linked source's own database or
  // an inline database's own page, and its title hidden or shown wherever it
  // has one (seen 2026-11-04); duplicate, and delete
  // while the database has another view. Its "데이터베이스 ›" points the view
  // at another data source, which Leafen's views don't do: it names the
  // database, as the view settings do.
  function tabActions(view: GraphView): DatabaseViewAction[] {
    const inline = snapshot.placement === "inline" && !ownPage, titleHidden = view.config.showDataSourceTitle === false;
    const linked = dataSources?.linked.find(({ dataSourceId }) => dataSourceId === view.dataSourceId);
    const source = view.dataSourceId === snapshot.dataSource.id ? snapshot.dataSource.title
      : dataSources?.own.find(({ id }) => id === view.dataSourceId)?.title ?? linked?.title;
    // A dashboard's opens alone from its toolbar's "⤢" only (Notion's, seen 2026-11-09).
    const opens = linked ? linked.href : inline && view.type !== "dashboard" ? fullPageHref : null;
    const shared: DatabaseViewAction[] = [
      ...(onCopyViewLink ? [{ id: "link", label: labels.copyViewLink, icon: <LinkIcon size="regular" />, act: () => { setMenu(null); onCopyViewLink(view.id); } }] : []),
      // A form's, a chart's or a dashboard's view stays off the sidebar, its row shown faded (Notion's, seen 2026-11-06),
      // as does a grouped view, a calendar or a map, its tip saying why (sidebarViewRefusal).
      ...(addToSidebar ? [{ id: "sidebar", label: labels.addViewToSidebar, icon: <MenuIcon size="regular" />,
        disabled: sprintBoard || view.type === "form" || view.type === "chart" || view.type === "dashboard" || sidebarViewRefusal(view) !== null,
        reason: sprintBoard ? undefined : sidebarRefusalLabel(sidebarViewRefusal(view), labels),
        act: () => { setMenu(null); addToSidebar(view); } }] : []),
      ...(opens ? [{ id: "fullPage", label: linked ? labels.openSourceDatabase : labels.openAsFullPage, icon: <ArrowUpRightIcon size="regular" />,
        act: () => { setMenu(null); router.push(opens); } }] : []),
      // A dashboard's title shows or hides from its settings alone (Notion's, seen 2026-11-09, inline and on its own page).
      ...(!pageTitleNamesSource && view.type !== "dashboard" ? [{ id: "title", label: titleHidden ? labels.showDataSourceTitle : labels.hideDataSourceTitle, icon: <EyeIcon size="regular" weight="regular" />,
        act: () => {
          setMenu(null);
          void run(`view:title:${view.id}`, (owner) => operations.updateViews([{ ...view, config: { ...view.config, showDataSourceTitle: titleHidden ? undefined : false } }], owner, true));
        } }] : []),
    ];
    const others = snapshot.views.filter(({ dataSourceId }) => dataSourceId === view.dataSourceId).length > 1;
    // As in its settings (viewSourceSwitchable).
    const switchable = viewSourceSwitchable(dataSources, view.dataSourceId, structureLocked || viewsReadOnly);
    const actions: DatabaseViewAction[] = [
      { id: "rename", label: labels.renameView, icon: <Pencil size="regular" />, disabled: sprintNames.has(view.id), act: () => openSettings(view, true) },
      { id: "display", label: labels.showTabsAs, icon: <PaintBrushIcon size="regular" />, next: true, open: Boolean(displayRow),
        act: () => setDisplayRow(menuRef.current?.querySelector<HTMLElement>('[data-view-action="display"]') ?? null) },
      { id: "edit", label: labels.editView, icon: <SlidersIcon size="regular" />, act: () => openSettings(view, false) },
      // A dashboard draws from its widgets' sources, naming none of its own (Notion's, seen 2026-11-06).
      ...(view.type === "dashboard" ? [] : [{ id: "database", label: labels.database, meta: surfaceDataSourceName(dataSources, { id: view.dataSourceId, title: source }), metaLinked: Boolean(linked), icon: <DataPathIcon size="regular" />,
        next: switchable, disabled: !switchable, act: () => openSettings(view, false, true) }]),
      ...shared.map((action, index) => index === 0 ? { ...action, grouped: true } : action),
      { id: "duplicate", label: labels.duplicateView, icon: <Copy size="regular" />, grouped: true, disabled: Boolean(atViewLimit), reason: atViewLimit, act: () => void duplicateView(view) },
      ...(others ? [{ id: "delete", label: labels.deleteView, icon: <Trash size="regular" />, danger: true, act: () => void deleteView(view) }] : []),
    ];
    // A wiki's view keeps its name, settings, link, copy and deleting (Notion's, seen 2026-11-11): no tabs to show, no other database.
    return wiki ? actions.filter(({ id }) => WIKI_VIEW_ACTIONS.has(id)) : actions;
  }

  // The chosen tab's menu for someone who can't edit the views, as Notion's
  // (seen 2026-10-11): "다음과 같이 표시 ›" (theirs alone) and "보기 링크 복사";
  // a published site's visitor has the link alone.
  function readerActions(view: GraphView): DatabaseViewAction[] {
    return [
      ...(signedIn ? [{ id: "display", label: labels.showTabsAs, icon: <PaintBrushIcon size="regular" />, next: true, open: Boolean(displayRow),
        act: () => setDisplayRow(menuRef.current?.querySelector<HTMLElement>('[data-view-action="display"]') ?? null) }] : []),
      ...(onCopyViewLink ? [{ id: "link", label: labels.copyViewLink, icon: <LinkIcon size="regular" />,
        act: () => { setMenu(null); onCopyViewLink(view.id); } }] : []),
    ];
  }

  // The actions "⋯" beside the title gives a database's one view, as Notion's (seen 2026-10-11):
  // its link and copy; the database — a linked source's own, "데이터 소스 보기" (seen 2026-11-04) —,
  // its title, its icon and its pages' layout; hiding the title.
  function titleActions(view: GraphView) {
    const blockId = snapshot.block.id;
    const layoutPageId = snapshot.dataSource.pageId ?? snapshot.layoutPageId;
    const linked = dataSources?.linked.find(({ dataSourceId }) => dataSourceId === view.dataSourceId);
    const opens = linked ? linked.href : fullPageHref;
    return [
      ...(onCopyViewLink ? [{ id: "link", label: labels.copyViewLink, icon: <LinkIcon size="small" />,
        act: () => { setMenu(null); onCopyViewLink(view.id); } }] : []),
      { id: "duplicate", label: labels.duplicateView, icon: <Copy size="small" />, disabled: Boolean(atViewLimit), reason: atViewLimit, act: () => void duplicateView(view) },
      ...(opens ? [{ id: "database", label: linked ? labels.viewDataSource : labels.viewDatabase, icon: <ArrowUpRightIcon size="small" />, grouped: true,
        act: () => { setMenu(null); router.push(opens); } }] : []),
      ...(blockId ? [
        { id: "title", label: labels.editTitle, icon: <Pencil size="small" />, grouped: !opens,
          act: () => { setMenu(null); requestInlineDatabase(blockId, "rename"); } },
        { id: "icon", label: labels.editIcon, icon: <SmileIcon size="small" />,
          act: () => { setMenu(null); requestInlineDatabase(blockId, "icon"); } },
      ] : []),
      // An inline database's pages too, from the page it sits on (Notion's, seen 2026-10-24).
      ...(signedIn && !viewsReadOnly && layoutPageId ? [{ id: "layout", label: labels.editLayout, icon: <PageLayoutIcon size="small" />,
        act: () => { setMenu(null); requestPageLayoutBuilder({ pageId: layoutPageId, dataSourceId: snapshot.dataSource.id }); } }] : []),
      { id: "hide", label: labels.hideTitle, icon: <EyeSlashIcon size="small" />, grouped: true,
        act: () => {
          setMenu(null);
          void run(`view:hide-title:${view.id}`, (owner) => operations.updateViews([{ ...view, config: { ...view.config, showDataSourceTitle: false } }], owner, true));
        } },
    ];
  }

  // A dragged tab takes the place of the tab it is dropped on, a step of ⌘Z as Notion's (seen 2026-11-12).
  // A view tab drags along this database's tabs only and lands on the side
  // of the tab it is dropped on.
  const tabsId = useId();
  const viewReorder = useDragReorder(`database-view-tab:${tabsId}`,
    (sourceId, targetId, side) => { void dropView(sourceId, targetId, side); }, { axis: "horizontal" });
  async function dropView(sourceId: string, targetId: string, side: "before" | "after") {
    if (!sourceId || sourceId === targetId) return;
    const ordered = [...snapshot.views];
    const sourceIndex = ordered.findIndex(({ id }) => id === sourceId);
    if (sourceIndex < 0) return;
    const [moved] = ordered.splice(sourceIndex, 1);
    const targetIndex = ordered.findIndex(({ id }) => id === targetId);
    if (targetIndex < 0) return;
    ordered.splice(targetIndex + (side === "after" ? 1 : 0), 0, moved);
    if (ordered.every((view, index) => view.id === snapshot.views[index]?.id)) return;
    await run(`view:drop:${sourceId}:${targetId}`, (owner) => operations.updateViews(
      ordered.map((item, position) => ({ ...item, position: String(position).padStart(6, "0") })),
      owner, true,
    ));
  }

  function tab(view: GraphView) {
    const active = view.id === snapshot.activeView.id;
    const reorder = viewReorder.row(view.id, () => true, !viewsReadOnly);
    return (
      <div key={view.id} className={styles.viewTabWrap} data-view-tab-wrap data-active={active ? "true" : undefined}
        {...reorder} data-drag-over={reorder["data-drop-side"] ? "true" : undefined}
        // Alt+←/→ moves a tab along, as dragging does: Notion's menu has no move rows.
        onKeyDownCapture={event => {
          if (viewsReadOnly || !event.altKey || event.metaKey || event.ctrlKey || (event.key !== "ArrowLeft" && event.key !== "ArrowRight")) return;
          event.preventDefault(); event.stopPropagation();
          void moveView(view, event.key === "ArrowLeft" ? -1 : 1);
        }}>
        <NativeDatabaseViewTab view={view} state={tabsState} panelId={panelId} active={active} display={tabDisplay} movable={!viewsReadOnly}
          menuOpen={menu?.kind === "view" && menu.viewId === view.id}
          onOpenMenu={!viewsReadOnly || (snapshot.readOnly && readerActions(view).length > 0)
            ? (element) => openMenu("view", element, view.id) : undefined}
          register={element => { if (element) tabRefs.current.set(view.id, element); else tabRefs.current.delete(view.id); }} />
      </div>
    );
  }

  // Under what opened it, or over it where only that side has room. A touch
  // screen wider than a phone opens a view's own menu as the phone's "보기
  // 옵션" sheet in its middle, and the row's "+" as "새 보기 추가", as
  // Notion's iPad (seen 2026-11-14 at 834: the views' list a popover, a
  // view's "⋯", its chosen tab and the "+" sheets).
  const sheetMenu = Boolean(menu) && (mobile || (touch && menu?.kind !== "overflow"));
  const menuPlace = usePopoverPlacement(
    menuRef, menu && !sheetMenu ? (size, viewport) => placeAround(menu.anchor, size, viewport, { gap: 4 }) : null, [menu], undefined,
    { kind: "menu", onEscape: () => setMenu(null), onOutsidePress: () => setMenu(null), returnFocusRef: menuTriggerRef, inside: () => [menuTriggerRef.current],
      shouldRestoreFocus: () => !goneTabRef.current || goneTabRef.current !== menuTriggerRef.current },
  );
  const listView = menu?.kind === "overflow" && listActions ? snapshot.views.find(({ id }) => id === listActions.viewId) : undefined;
  // A wiki's database takes no other data source; its Home leads its views.
  const addSource = !wiki && !viewsReadOnly && dataSources && !dataSources.readOnly ? () => { setMenu(null); void run("source:add", () => dataSources.add()); } : undefined;
  // A touch screen offers no form, as its sheets (Notion's iPad, seen 2026-11-14 at 834: ten layouts five to a row under "새 보기").
  const addTypes = wiki ? WIKI_ADD_VIEW_TYPES : touch ? PHONE_ADD_VIEW_TYPES
    : databaseLinksSource(snapshot.block, snapshot.dataSource.id) ? LINKED_ADD_VIEW_TYPES : ADD_VIEW_TYPES;
  // Ten layouts, without the form, go five to a row; eleven four (Notion's, seen 2026-10-30 and 2026-11-11).
  const addWide = !addTypes.includes("form");
  const lead: ViewListLead | undefined = wiki && { id: WIKI_HOME_VIEW_ID, label: wiki.homeLabel, icon: <FileText pixels={20} aria-hidden="true" />,
    current: false, index: wiki.homeIndex, onSelect: () => { setMenu(null); wiki.chooseHome(); }, actionsOpen: listActions?.viewId === WIKI_HOME_VIEW_ID,
    onActions: (element) => setListActions(current => current?.anchor === element ? null : { viewId: WIKI_HOME_VIEW_ID, anchor: element }) };
  const activeMenuView = menu?.kind === "view" || menu?.kind === "title"
    ? snapshot.views.find(({ id }) => id === menu.viewId)
    : undefined;
  // A phone's are Notion's sheets (seen 2026-10-24 at 375px): the title's
  // "⌄" holds "새 보기 추가" first; a reader keeps its link and database; a
  // visitor's list has the views alone, no "⋯" (Notion's published database
  // in iOS Safari, seen 2026-11-11).
  const phoneTitleActions = (view: GraphView) => viewsReadOnly
    ? titleActions(view).filter(({ id }) => id === "link" || id === "database")
    : [{ id: "add", label: labels.addNewView, icon: <Plus size="small" />, disabled: Boolean(atViewLimit), reason: atViewLimit,
      act: () => setMenu((current) => current && { kind: "titleAdd", anchor: current.anchor }) },
      ...titleActions(view)];
  const phoneMenus = menu && sheetMenu ? (
    <PhoneViewMenus kind={menu.kind} view={activeMenuView} views={snapshot.views} activeId={snapshot.activeView.id} addTypes={PHONE_ADD_VIEW_TYPES} lead={lead} wide={!mobile}
      titleActions={phoneTitleActions} viewActions={(view) => !viewsReadOnly ? tabActions(view) : signedIn ? readerActions(view) : []}
      onDisplay={setTabDisplay} onSelect={selectView} onCreate={viewsReadOnly ? undefined : (type) => void createView(type)}
      onAddSource={addSource} addDisabled={Boolean(atViewLimit)}
      onOpen={(kind, viewId) => setMenu((current) => current && ((kind === "view" || kind === "title") && viewId
        ? { kind, viewId, anchor: current.anchor } : { kind: kind as "add" | "overflow" | "titleAdd", anchor: current.anchor }))}
      onClose={() => setMenu(null)} />
  ) : null;
  const layer = phoneMenus ? (mobile ? phoneMenus : <TouchSheetLayout>{phoneMenus}</TouchSheetLayout>)
    : menu && typeof document !== "undefined" ? createPortal(
    <>
      <button
        type="button"
        className={styles.menuBackdrop}
        style={popoverBackdropStyle(menuPlace)}
        aria-label={labels.close}
        tabIndex={-1}
      />
      <div
        className={`${styles.viewMenu} ${menu.kind === "add" || menu.kind === "titleAdd" ? styles.titleAddViewMenuPanel : styles.viewTabMenu}`}
        data-wide={(menu.kind === "add" || menu.kind === "titleAdd") && addWide ? "true" : undefined}
        data-view-overflow-menu={menu.kind === "overflow" ? "true" : undefined}
        data-view-tab-menu={menu.kind === "view" ? "true" : undefined}
        data-add-view-menu={menu.kind === "add" || menu.kind === "titleAdd" ? "true" : undefined}
        ref={menuRef}
        style={{ position: "fixed", ...popoverPlacementStyle(menuPlace) }}
      >
        {menu.kind === "overflow" && <DatabaseViewList views={[...snapshot.views]} activeId={snapshot.activeView.id} lead={lead}
          readOnly={viewsReadOnly} actionsOpen={listActions?.viewId} onReorder={wiki?.reorder ?? ((source, target, side) => void dropView(source, target, side))}
          onActions={(view, element) => setListActions(current => current?.anchor === element ? null : { viewId: view.id, anchor: element })}
          onAddSource={addSource} addRefusal={atViewLimit}
          label={projected.collapsed ? labels.viewsFor(snapshot.displayTitle) : labels.moreViews(overflow.length)}
          open={Boolean(menuPlace)} onSelect={selectView}
          onAddView={!viewsReadOnly ? setAddRow : undefined} />}
        {menu.kind === "overflow" && addRow && <DatabaseViewAddBeside row={addRow} types={addTypes} wide={addWide}
          onCreate={type => void createView(type)} onClose={() => {
            setAddRow(null);
            (addRow.closest("[data-view-overflow-menu]")?.querySelector<HTMLElement>("input") ?? addRow).focus();
          }} />}
        {(menu.kind === "add" || menu.kind === "titleAdd") && <DatabaseAddViewChoices types={addTypes} heading={labels.addNewView}
          className={styles.titleAddViewGrid} itemClassName={styles.inlineAddViewType}
          open={Boolean(menuPlace)} onCreate={type => void createView(type)}
          onAddSource={addSource} />}
        {menu.kind === "title" && activeMenuView && <DatabaseViewActionChoices key={activeMenuView.id}
          open={Boolean(menuPlace)} label={labels.openActions(viewDisplayName(activeMenuView))} actions={titleActions(activeMenuView)} />}
        {menu.kind === "view" && activeMenuView && <DatabaseViewActionChoices key={activeMenuView.id}
          open={Boolean(menuPlace)} label={labels.openActions(viewDisplayName(activeMenuView))}
          actions={viewsReadOnly ? readerActions(activeMenuView) : tabActions(activeMenuView)} />}
        {menu.kind === "overflow" && listActions && listView && <DatabaseViewActionsBeside key={listView.id} anchor={listActions.anchor}
          onClose={() => { setDisplayRow(null); setListActions(null); }}>
          {open => <DatabaseViewActionChoices open={open} label={labels.openActions(viewDisplayName(listView))}
            actions={viewsReadOnly ? readerActions(listView) : tabActions(listView)} />}
        </DatabaseViewActionsBeside>}
        {/* Home's "⋯" holds "이름 바꾸기" alone (Notion's, seen 2026-11-11). */}
        {menu.kind === "overflow" && wiki && lead && listActions?.viewId === WIKI_HOME_VIEW_ID && <DatabaseViewActionsBeside anchor={listActions.anchor}
          onClose={() => setListActions(null)}>
          {open => <DatabaseViewActionChoices open={open} label={labels.openActions(lead.label)} actions={[{ id: "rename", label: labels.renameView,
            icon: <Pencil size="regular" />, act: () => { setListActions(null); setMenu(null); wiki.renameHome(); } }]} />}
        </DatabaseViewActionsBeside>}
        {(menu.kind === "view" || listView) && displayRow && <DatabaseViewTabDisplayChoices row={displayRow} value={tabDisplay}
          onChoose={display => { setTabDisplay(display); setMenu(null); }} onClose={() => setDisplayRow(null)} />}
      </div>
    </>,
    document.body,
  ) : null;

  const titleChevron = (mobile || touch) && !layoutTab && phoneTitleActions(snapshot.activeView).length ? (
    <div className={styles.viewTitleTools} data-view-title-tools data-phone="true">
      <button type="button" className={styles.viewTitleTool} aria-label={labels.collectionActions} aria-haspopup="dialog"
        aria-expanded={menu?.kind === "title"} onClick={(event) => openMenu("title", event.currentTarget, snapshot.activeView.id)}>
        <ChevronDown size="small" />
      </button>
    </div>
  ) : null;

  // Over the title, "+" adds a view and "⋯" opens the view's actions, shown
  // while the pointer is on the row, as Notion's (seen 2026-10-10); a row
  // narrowed to its few tools keeps "+" alone (CSS, seen 2026-11-11).
  if (titleRow) {
    const view = snapshot.activeView;
    // A touch screen's title row has one "⌄" after the title, as Notion's phone and iPad.
    if (mobile || touch) return <>{titleChevron}{layer}</>;
    return (
      <>
        {!viewsReadOnly && !layoutTab && (
          <div className={styles.viewTitleTools} data-view-title-tools>
            <button type="button" className={styles.viewTitleTool} aria-label={labels.addNewView} aria-haspopup="dialog"
              aria-expanded={menu?.kind === "titleAdd"} data-add-view-opener data-press-keeps-focus onClick={(event) => openMenu("titleAdd", event.currentTarget)}>
              <Plus size="small" />
            </button>
            <button type="button" className={styles.viewTitleTool} aria-label={labels.openActions(viewDisplayName(view))} aria-haspopup="dialog"
              aria-expanded={menu?.kind === "title"} data-view-title-actions
              onClick={(event) => openMenu("title", event.currentTarget, view.id)}>
              <DotsHorizontal size="small" />
            </button>
          </div>
        )}
        {layer}
      </>
    );
  }

  return (
    <>
      <div {...tabListProps} ref={rootRef} className={styles.viewTabs} data-view-tabs
        onFocus={event => {
          if ((event.target as Element).closest("[data-view-tab]")) tabListProps.onFocus?.(event);
          else tabsState.selectionManager.setFocused(false);
        }}
        // The arrow keys move between no tabs and choose no view, as Notion's (seen 2026-11-15 twice: → on a tab).
        onKeyDown={undefined}>
        {visible.map(tab)}
        {projected.collapsed ? (
          <div className={`${styles.viewTabWrap}`} data-view-overflow-wrap data-active="true" data-phone={mobile ? "true" : undefined}>
            <button type="button" className={`${styles.viewTab} ${styles.viewOverflowButton}`} data-view-picker aria-haspopup="menu"
              data-phone={mobile ? "true" : undefined} data-wiki-view-menu-trigger={wiki ? "" : undefined} aria-expanded={menu?.kind === "overflow"}
              onClick={(event) => { if (menu?.kind !== "overflow" && wiki?.onTrigger?.()) return; openMenu("overflow", event.currentTarget); }}>
              <ViewTabGlyph view={snapshot.activeView} className={styles.viewGlyph} />
              <span>{tabName(snapshot.activeView)}</span><ChevronDown size="small" />
            </button>
          </div>
        ) : overflow.length > 0 && (
          <div ref={overflowRef} className={`${styles.viewTabWrap}`} data-view-overflow-wrap>
            <button type="button" className={`${styles.viewTab} ${styles.viewOverflowButton}`} aria-haspopup="menu" aria-expanded={menu?.kind === "overflow"}
              data-press-keeps-focus onClick={(event) => openMenu("overflow", event.currentTarget)}>
              <span>{labels.moreViews(overflow.length)}</span>
            </button>
          </div>
        )}
        {/* With "N개 더 보기" the list holds "+ 새 보기" instead, as Notion's (seen 2026-10-11);
            otherwise a round "+" after the tabs while the pointer is over them (seen 2026-11-01). */}
        {!viewsReadOnly && !projected.collapsed && overflow.length === 0 && (
          <div className={styles.addViewWrap}>
            <button type="button" className={styles.addView} aria-label={labels.addView} title={labels.addView} aria-haspopup="dialog"
              aria-expanded={menu?.kind === "add"} data-add-view-opener data-press-keeps-focus onClick={(event) => openMenu("add", event.currentTarget)}>
              <Plus size="small" />
            </button>
          </div>
        )}
      </div>
      {titleActionsSlot && titleChevron ? createPortal(titleChevron, titleActionsSlot) : null}
      {layer}
    </>
  );
}
