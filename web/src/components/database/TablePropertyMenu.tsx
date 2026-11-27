import { DatabaseFixedPropertiesContext, DatabaseTaskRolePropertiesContext } from "./untitledRowName";
import { sprintManagedProperty } from "./databaseSprintContext";
import { SwitchMark } from "@/components/Switch";
import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode, useContext } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphValueType } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { placeAround, placeAtPoint, placeBeside, popoverBackdropStyle, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import { relationTargetConfig, relationTwoWayConfig } from "@/lib/relationPairConfig";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { organizationModule } from "@/lib/organizationModule";
import { propertyAccessRestrictable, requestPropertyAccessDialog } from "@/lib/propertyAccess";
import {
  ArrowsExpandHorizontal, ChevronDown, ChevronRight, Copy, EyeIcon, EyeSlashIcon, FilterIcon, FormulaIcon, GroupIcon, InsertLeftIcon, InsertRightIcon, LockIcon, PinIcon, ConvertIcon,
  SlidersIcon, SmileIcon, SortIcon, Trash, UnwrapIcon, WrapIcon,
} from "../icons";
import { Pencil } from "@/icons/leafen";
import { DatabaseButtonAutomationDialog } from "./DatabaseButtonAutomationDialog";
import { DatabaseButtonActionsEditor } from "./DatabaseButtonActionsEditor";
import { usePropertyTypeChangeConfirm } from "./PropertyTypeChangeConfirm";
import { PropertyAppEditorChoice, usePropertyAppEditors } from "./PropertyAppEditorChoice";
import { PropertyTypeConfig, propertyTypeHasConfig } from "./PropertyTypeConfig";
import { configWithDescription, propertyDescription } from "./propertyDescription";
import { TablePropertyMenuHead, type TablePropertyHeadChange } from "./TablePropertyMenuHead";
import { TablePropertyPhoneSheet } from "./TablePropertyPhoneSheet";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PropertyTypeMenu } from "./PropertyTypeChoices";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { isMenuMoveKey, isTextField } from "@/lib/menuKeyboard";
import { duplicatedProperty, propertyAfterTypeChange } from "./propertyEditModel";
import {
  RelationPropertyCreationDialog,
  type RelationPropertyCreationContext,
  type RelationPropertyCreationSnapshot,
} from "./RelationPropertyCreationDialog";
import type { FormulaBounds } from "./formulaBudget";
import { canDuplicateProperty } from "./rowPropertiesModel";
import { offeredGroupProperty, sortableProperty } from "./arrangeChoices";
import {
  tableColumnFrozenEdge,
  tableColumnWrapped,
  tableConfigAfterCalculation,
  tableColumnFreezable,
  tableConfigAfterFreeze,
  tableConfigAfterGroup,
  tableConfigAfterHide,
  tableConfigAfterPageIcons,
  tableConfigAfterSort,
  tableConfigAfterWrap,
  tableConfigAfterStatusDisplay,
  tableShowsPageIcons,
  propertySortLabels,
  propertyAfterFullUrl,
  propertyShowsFullUrl,
  tableStatusCheckboxIds,
} from "./tablePropertyMenuModel";
import { TableStatusDisplayMenu } from "./TableStatusDisplay";
import { TablePropertySide, TableSortSideMenu, useTablePropertySideHover } from "./TablePropertySide";
import { submenuRowMarks } from "../PageMenu";
import { TableCalculationMenu } from "./TableCalculationMenu";
import { tableCalculation } from "./tableCalculationModel";
import styles from "./databaseSheet";
import menuStyles from "./TablePropertyMenu.module.css";
import { confirmPropertyDeletion } from "./confirmPropertyDeletion";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";
import { plainTitleOf } from "@/lib/closedMentions";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

function presentationProperty(property: GraphProperty, index = 0): DbProperty {
  return {
    id: property.id,
    databaseId: property.dataSourceId,
    name: property.name,
    type: property.type as DbProperty["type"],
    config: property.config as DbProperty["config"],
    position: index,
    sprintManaged: property.sprintManaged,
  };
}

export type TableRelationCreation = {
  context: RelationPropertyCreationContext;
  snapshot: RelationPropertyCreationSnapshot;
};

type TablePropertyMenuProps = {
  /** Where it opens: under its header (`top`), or over it (`headerTop`) where the window ends. */
  anchor: { left: number; top: number; headerTop?: number };
  config: Readonly<Record<string, unknown>>;
  /** Opens with "edit property" beside the menu, as for a relation just made. */
  editing?: boolean;
  /** Shows only the property's settings, as Notion's empty rollup cell opens them (seen 2026-10-17). */
  settingsOnly?: boolean;
  readOnly?: boolean;
  shownPropertyIds: readonly string[];
  /** The width of the frame the table is in, past which less 100px no columns freeze. */
  frameWidth?: number;
  contextIdentity: string;
  dataSource: GraphDataSource;
  formulaBounds: FormulaBounds;
  /** The pages loaded with the table, which the formula editor previews on and a relation names its database from. */
  pagesById?: Record<string, Page>;
  /** The properties of the databases the table's relations reach, which name a relation's pair. */
  propertiesByDataSourceId?: Record<string, DbProperty[]>;
  property: GraphProperty;
  properties: readonly GraphProperty[];
  relationCreation?: TableRelationCreation;
  snapshotIdentity: string;
  workspaceId: string;
  onClose(): void;
  onDelete(propertyId: string): void;
  onDuplicate(duplicate: GraphProperty): void;
  /** Filters the view by the column in its filter bar. */
  onFilter?(): void;
  /** Gives way to a new property's name and type, left or right of the column. */
  onInsert(side: "left" | "right"): void;
  onUpdate(property: GraphProperty): void;
  onViewConfig(config: Record<string, unknown>): void;
};

type Submenu = "edit" | "type" | "sort" | "calculate" | "statusDisplay";
/** "유형 변경" rolls past this, as Notion's (seen 2026-11-04, 200×470). */
const TYPE_MENU_MAX_HEIGHT = 470;
const SUBMENU_WIDTH: Record<Submenu, number> = { edit: 300, type: 200, sort: 200, calculate: 200, statusDisplay: 250 }; // 정렬, 계산: 200 for every type (Notion's, 2026-11-16)

// A table column's menu, as Notion's (seen 2026-09-30): the type icon and
// the name, focused, then "edit property" and "change type", which open
// beside the menu; filter, sort, group, calculate, freeze, hide and wrap;
// insert left or right, duplicate and delete. The title column keeps its
// type and place in the table, so it has neither of the first two nor
// hide, duplicate or delete; it starts with the view's "show page icon"
// switch instead. Sorting, grouping, calculating, freezing and the rest
// close the menu; renaming, settings and the switch leave it open.
export function TablePropertyMenu({
  anchor,
  config,
  editing = false,
  settingsOnly = false,
  readOnly = false,
  shownPropertyIds,
  frameWidth,
  contextIdentity,
  dataSource,
  formulaBounds,
  pagesById,
  propertiesByDataSourceId,
  property,
  properties,
  relationCreation,
  snapshotIdentity,
  workspaceId,
  onClose,
  onDelete,
  onDuplicate,
  onFilter,
  onInsert,
  onUpdate,
  onViewConfig,
}: TablePropertyMenuProps) {
  const { t } = useTranslation(["tableView", "databaseView"]);
  // A touch screen's menu is Notion's bottom sheet (TablePropertyPhoneSheet),
  // an iPad's as a phone's "속성 편집" (seen 2026-11-14 at 834).
  const layout = useSidebarLayout(true), phone = layout.phone || layout.touch;
  const SheetLayer = layout.phone ? Fragment : TouchSheetLayout;
  const [submenu, setSubmenu] = useState<{ kind: Submenu; item: DOMRect; menu: DOMRect; keyboard: boolean } | null>(null);
  // A button's "자동화 편집" opens Notion's dialog in the middle in place of the menu (seen 2026-11-02).
  const [buttonDialog, setButtonDialog] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const sideRef = useRef<HTMLDivElement>(null);
  // Under its column's header, 18px in from the window's sides (Notion's
  // column menu, seen 2026-11-07: 761–981 in a 999 window, where its cell
  // pickers keep 12) and 12 from its foot; shown whole, over the header where
  // only there it fits, else under it moved up into the window (seen
  // 2026-11-08 in a 723 window: 498 high, 152..650 over a header at 650,
  // 213..711 under one at 400 or 299); a side menu beside it on the right,
  // or on the left where the window ends.
  const backdropRef = useRef<HTMLButtonElement>(null);
  const menuPlace = usePopoverPlacement(
    menuRef, (size, viewport) => anchor.headerTop === undefined ? placeAtPoint({ x: anchor.left, y: anchor.top }, size, viewport)
      : placeAround(popoverRectAt({ top: anchor.headerTop, bottom: anchor.top, left: anchor.left }), size, viewport, { gap: 0, align: "start", minHeight: 0, margin: 18, marginY: 12, whole: true }),
    // Its shade closes it over a modal too, as over a layout builder's tab (overlayLayer modalOwned); Escape with the focus on the page closes its side menu, then it, as from inside.
    [anchor.left, anchor.top, anchor.headerTop], undefined, { kind: "menu", restoreFocus: false, modalOwned: () => [backdropRef.current], onEscape: () => { if (submenu) closeSide(); else close(); } },
  );
  // A side menu sits 4px into the menu: a short list centred on its row, as
  // Notion's sort (seen 2026-11-02: 65px, its middle on the row's), the
  // property's settings level with its row; "다음과 같이 표시:" drops under
  // its row, at its right end. A list is centred at the height it is drawn,
  // the types' at their 470 (seen 2026-11-14: 54..524 by a row at 275..303,
  // where centred at their whole 645 they rose to the window's top).
  const sidePlace = usePopoverPlacement(sideRef, submenu ? (size, viewport) => {
    if (submenu.kind === "statusDisplay") {
      return placeAround({ top: submenu.item.top, bottom: submenu.item.bottom, left: submenu.item.left, right: submenu.item.right },
        size, viewport, { gap: 0, align: "end", minHeight: 0 });
    }
    const cap = submenu.kind === "type" ? TYPE_MENU_MAX_HEIGHT : undefined;
    const height = Math.min(size.height, cap ?? Infinity);
    return placeBeside(
      { top: submenu.item.top, bottom: submenu.item.bottom, left: submenu.menu.left, right: submenu.menu.right },
      size, viewport, { gap: -4, offsetTop: submenu.kind === "edit" ? 0 : (submenu.item.bottom - submenu.item.top - height) / 2,
        ...(cap ? { cap } : {}) },
    );
  } : null, [submenu], undefined, { kind: "submenu", restoreFocus: false, closesHost: false, onEscape: () => closeSide() });
  const nameRef = useRef<HTMLInputElement>(null);
  const descriptionRef = useRef<HTMLTextAreaElement>(null);
  // A property turned into a relation here picks the database it relates
  // to; a relation's menu opens as any other's.
  const [relationTarget, setRelationTarget] = useState(false);
  // Type changes share the row page's guard: a change the server can't make
  // or one that ends a two-way relation asks first.
  const { confirmPropertyTypeChange, typeChangeConfirmDialog } = usePropertyTypeChangeConfirm();
  const presentationProperties = useMemo(
    () => properties.map(presentationProperty),
    [properties],
  );
  // The name takes focus as the menu opens, the caret after it, no row lit; the menus of the title and the ID,
  // the two whose type can't change, open with the name unfocused, the menu itself holding the focus for its arrows
  // and Escape. Measured after the press only (no script in the page before it), Notion in Chrome 1440: a person
  // column's header pressed, "z" typed went to the end of its name; the title column's (2026-11-15) and the ID
  // column's (2026-11-15 twice, two tabs at two times, the second in a page made by hand), "z" went nowhere and
  // the name stayed, no ring on it. A phone's sheet leaves its name unfocused, so no keyboard comes up over its
  // rows (Notion's, seen 2026-11-11).
  useEffect(() => {
    const input = nameRef.current;
    if (phone) return;
    if (property.type === "title" || property.type === "unique_id") { menuRef.current?.focus({ preventScroll: true }); return; }
    if (!input) { menuRef.current?.focus({ preventScroll: true }); return; }
    input.focus();
    input.setSelectionRange(input.value.length, input.value.length);
  }, [property.id]);
  // A relation just made opens its settings beside the menu once the menu
  // stands where it shows, not where it was drawn first.
  const menuPlaced = Boolean(menuPlace);
  useLayoutEffect(() => {
    if (!editing || !menuPlaced) return;
    const menu = menuRef.current;
    const item = menu?.querySelector<HTMLElement>("[data-menu-item='edit']");
    if (menu && item) setSubmenu({ kind: "edit", item: item.getBoundingClientRect(), menu: menu.getBoundingClientRect(), keyboard: false });
  }, [editing, menuPlaced, property.id]);
  // A rollup cell's settings alone take the keys at once on themselves,
  // ringing none of their choices, as Notion's rings none (seen 2026-11-11,
  // pressed or opened with Enter); Escape closes them, Tab goes on to a choice.
  // After the press that opened them is done, which leaves the page's caret by the table.
  // Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: a rollup cell pressed, no ring, "z" went
  // nowhere (Notion's focus on <body>, Leafen's on the menu — next key the same).
  useEffect(() => {
    if (!settingsOnly || !menuPlaced) return;
    const frame = window.requestAnimationFrame(() => menuRef.current?.focus({ preventScroll: true }));
    return () => window.cancelAnimationFrame(frame);
  }, [settingsOnly, menuPlaced, property.id]);
  const presentation = presentationProperty(property);
  const database = dataSourcePage(dataSource, workspaceId);
  // A button's actions, kept in the versioned document the server runs, are
  // saved onto the property as it is by then (DatabaseButtonActionsEditor).
  const liveProperty = useRef(property);
  liveProperty.current = property;
  const currentProperty = () => liveProperty.current;
  const configContext = {
    contextIdentity,
    ops: {
      loadDatabase: () => {},
      setRelationDatabase: (_operation: unknown, propertyId: string, targetDatabaseId: string) => {
        const target = properties.find((candidate) => candidate.id === propertyId);
        if (target) onUpdate({ ...target, config: relationTargetConfig(target.config, target.dataSourceId, targetDatabaseId) });
      },
      setRelationTwoWay: (_operation: unknown, propertyId: string, enabled: boolean, reciprocalName?: string) => {
        const target = properties.find((candidate) => candidate.id === propertyId);
        if (target) onUpdate({ ...target, config: relationTwoWayConfig(target.config, target.dataSourceId, enabled, reciprocalName) });
      },
      updateProperty: (_operation: unknown, propertyId: string, patch: Partial<DbProperty>) => {
        const target = properties.find((candidate) => candidate.id === propertyId);
        if (!target) return;
        onUpdate({
          ...target,
          name: patch.name ?? target.name,
          type: (patch.type ?? target.type) as GraphValueType,
          config: patch.config ? { ...patch.config } : target.config,
        });
      },
    },
  };
  // A relation's settings name the database it reaches and its pair there.
  const configSnapshot = {
    databases: [database, ...Object.values(pagesById ?? {}).filter((page) => page.kind === "database" && page.id !== database.id)],
    formulaBounds,
    propertiesByDatabase: { ...(propertiesByDataSourceId ?? {}), [dataSource.id]: presentationProperties },
    snapshotIdentity,
    pagesById,
  };
  const appEditors = usePropertyAppEditors(property);
  let advanced: ReactNode = (
    <>
      <PropertyTypeConfig
        context={configContext}
        onClose={close}
        prop={presentation}
        snapshot={configSnapshot}
        cellForm={settingsOnly}
      />
      <PropertyAppEditorChoice property={property} writable={!readOnly} onUpdate={onUpdate} />
    </>
  );
  if (property.type === "button") {
    advanced = (
      <DatabaseButtonActionsEditor key={property.id} property={property} current={currentProperty} onSave={onUpdate}
        properties={presentationProperties} pages={[database]} />
    );
  }
  const title = property.type === "title";
  // Every property but the title changes type, an automatic one too, as
  // Notion's (seen 2026-10-05).
  // A title, and an ID, keep their type, as Notion's (seen 2026-10-11).
  const fixedType = title || property.type === "unique_id";
  const taskRole = useContext(DatabaseTaskRolePropertiesContext).has(property.id), fixed = useContext(DatabaseFixedPropertiesContext).has(property.id);
  // A locked rollup stays one, its menu without the row, as Notion's (seen 2026-11-04); so do a task database's own
  // property and one its sprints manage.
  const typeRow = !fixed && !fixedType && !taskRole && !sprintManagedProperty(property) && property.config.rollupLocked !== true;
  const editable = !fixed && (property.type === "button" || propertyTypeHasConfig(property.type)
    || appEditors.editors.length > 0 || Boolean(appEditors.current));
  // The name and description go in one change, so neither undoes the other.
  const sent = useRef({ name: property.name, description: propertyDescription(property) });
  function changeHead(change: TablePropertyHeadChange) {
    let next = property;
    const name = change.name?.trim();
    if (name && name !== property.name && name !== sent.current.name) {
      sent.current.name = name;
      next = { ...next, name };
    }
    const description = change.description?.trim();
    if (description !== undefined && description !== propertyDescription(property) && description !== sent.current.description) {
      sent.current.description = description;
      next = { ...next, config: configWithDescription(next.config, description) };
    }
    if (next !== property) onUpdate(next);
  }
  // Closing the menu leaves its fields, which keep what was typed.
  function keepHead() {
    changeHead({ name: nameRef.current?.value, description: descriptionRef.current?.value });
  }
  function close() {
    keepHead();
    onClose();
  }
  const wrapped = tableColumnWrapped(config, property.id);
  const statusCheckbox = tableStatusCheckboxIds(config).has(property.id);
  const grouped = config.groupBy === property.id;
  const frozenEdge = tableColumnFrozenEdge(config, shownPropertyIds, property.id);
  const freezable = tableColumnFreezable(config, shownPropertyIds.flatMap((id) => properties.find((candidate) => candidate.id === id) ?? []), property.id, frameWidth);
  const calculations = (config.calculations && typeof config.calculations === "object" ? config.calculations : {}) as Record<string, unknown>;
  const [ascending, descending] = propertySortLabels(presentation, presentationProperties);
  function view(next: Record<string, unknown> | null) {
    if (next) onViewConfig(next);
    close();
  }
  // A press, or the pointer resting on the row (useTablePropertySideHover), opens a side menu and the side takes the
  // focus, the name left without its ring and the side's first row (or its current choice) lit without a ring, so the
  // first ↓ goes on to the second; the keyboard opens it lit too, with its ring. A press on the row of the side open
  // keeps it. Measured after the action only (no script in the page before it), Notion in Chrome 1440: 2026-11-15, "A →
  // Z로 정렬", "텍스트" and "계산 안함" lit as 정렬, 유형 변경 and 계산 opened from the pointer, the next ↓ lighting the
  // second row (the desktop app's "A → Z로 정렬" the same); 2026-11-27 at dpr 1 in two tabs, "정렬" and "계산" pressed
  // again with their lists open kept them.
  function open(kind: Submenu, item: HTMLElement, keyboard = false) {
    const menu = menuRef.current?.getBoundingClientRect();
    if (!menu) return;
    setSubmenu((current) => current?.kind === kind && !keyboard ? current : { kind, item: item.getBoundingClientRect(), menu, keyboard });
  }
  const sideHover = useTablePropertySideHover({ open: submenu?.kind ?? null, menuRef, sideRef, onOpen: (kind, row) => open(kind, row),
    onClose: () => setSubmenu(null), light: (key) => { list.state.selectionManager.setFocused(true); list.state.selectionManager.setFocusedKey(key); } });
  const pointerSide = submenu && !submenu.keyboard ? submenu.kind : null;
  useLayoutEffect(() => {
    if (pointerSide) sideRef.current?.focus({ preventScroll: true });
  }, [pointerSide]);
  // Closing a side menu from its keys gives them back to its row: the menu
  // holds the focus, the name without its ring, the row lit with no keyboard
  // ring until an arrow moves on, as Notion's (2026-11-15, Chrome, no script
  // before the action; judged by the screen and one typed letter: 유형 변경
  // pressed, Escape — the row lit with no ring, the letter landing nowhere).
  function closeSide() {
    const kind = submenu?.kind;
    setSubmenu(null);
    if (!kind || !list.state.collection.getItem(kind)) return;
    if (listRef.current) listRef.current.dataset.searchLit = kind;
    list.state.selectionManager.setFocused(true); list.state.selectionManager.setFocusedKey(kind);
    menuRef.current?.focus({ preventScroll: true });
  }
  // Escape closes a side menu, then the menu; a field or picker inside that
  // handles Escape itself keeps it.
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Escape" || event.defaultPrevented) return;
    event.preventDefault();
    event.stopPropagation();
    if (submenu) closeSide();
    else close();
  }
  function onSideKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === inlineStartKey(event.currentTarget) && submenu?.kind !== "edit" && !event.defaultPrevented && !isTextField(event.target)) {
      event.preventDefault(); event.stopPropagation(); closeSide(); return;
    }
    // A side the pointer opened holds the focus itself; an arrow enters its
    // list on the first row, or moves on from the row a list lit as it
    // opened (the types' first: Notion's first ↓ there lights the second
    // type — 2026-11-15, Chrome, no script before the press).
    const inner = event.target === event.currentTarget && isMenuMoveKey(event.key)
      ? event.currentTarget.querySelector<HTMLElement>("[data-native-menu]") : null;
    if (inner) {
      event.preventDefault(); event.stopPropagation(); inner.focus({ preventScroll: true });
      if (inner.querySelector("[data-focused='true']")) inner.dispatchEvent(new globalThis.KeyboardEvent("keydown", { key: event.key, bubbles: true, cancelable: true }));
      return;
    }
    onKeyDown(event);
  }
  function duplicate() {
    onDuplicate(duplicatedProperty(property, properties, t));
    close();
  }
  const accessRow = !readOnly && !fixed && Boolean(dataSource.manageAccess) && Boolean(organizationModule()?.PropertyAccessDialog)
    && propertyAccessRestrictable(property);
  type Row = { key: string; label: string; icon: ReactNode; disabled?: boolean; danger?: boolean; opens?: Submenu;
    meta?: string; dividerBefore?: boolean; tip?: string; run?: () => void; on?: boolean };
  const button = property.type === "button";
  const fullUrl = propertyShowsFullUrl(property, config);
  const rows: Row[] = [
    // The title column's "페이지 아이콘 표시" and a URL column's "전체 URL
    // 표시" are their menus' first rows, switch rows that leave the menu open
    // (the URL's with ↔), as Notion's (seen 2026-11-07).
    ...(title ? [{ key: "pageIcons", icon: <SmileIcon size="regular" aria-hidden="true" />, label: t("databaseView:showPageIcons"),
      on: tableShowsPageIcons(config), disabled: readOnly,
      run: () => onViewConfig(tableConfigAfterPageIcons(config, !tableShowsPageIcons(config))) }] : []),
    ...(property.type === "url" ? [{ key: "fullUrl", icon: <ArrowsExpandHorizontal size="regular" aria-hidden="true" />,
      label: t("databaseView:showFullUrl"), on: fullUrl, disabled: readOnly,
      run: () => onUpdate(propertyAfterFullUrl(property, !fullUrl)) }] : []),
    // A button's "자동화 편집" opens its window, Notion's ✎ with no "›" (seen 2026-11-06).
    ...(editable ? [{ key: "edit", icon: button ? <Pencil size="regular" aria-hidden="true" /> : <SlidersIcon size="regular" aria-hidden="true" />,
      label: t(button ? "editAutomation" : "editProperty"), disabled: readOnly, opens: "edit" as const }] : []),
    // "속성 사용 권한" after "속성 편집", for whoever has full access to the
    // database, where the organization module offers it (seen 2026-10-12).
    ...(accessRow ? [{ key: "access", icon: <LockIcon size="regular" aria-hidden="true" />, label: t("propertyAccess"),
      run: () => { close(); requestPropertyAccessDialog({ dataSourceId: dataSource.id, propertyId: property.id }); } }] : []),
    ...(typeRow ? [{ key: "type", icon: <ConvertIcon size="regular" aria-hidden="true" />, label: t("changeType"),
      disabled: readOnly, opens: "type" as const }] : []),
    // A button holds no value to filter, sort, count or wrap: its menu goes
    // from "유형 변경" to "고정" (Notion's, seen 2026-11-02).
    ...(!button ? [{ key: "filter", icon: <FilterIcon size="regular" aria-hidden="true" />, label: t("filter"), disabled: readOnly || !onFilter,
      dividerBefore: !fixed && (title || editable || !fixedType), run: () => { onFilter?.(); close(); } },
    // "정렬" dim (no "›", as any dim row) where no sort list offers the column, "그룹화" where a grouping list does (arrangeChoices.ts; Notion's, 2026-11-19).
    { key: "sort", icon: <SortIcon size="regular" aria-hidden="true" />, label: t("sort"), disabled: readOnly || !sortableProperty(property), opens: "sort" as const }] : []),
    ...(offeredGroupProperty(property, config.groupBy) ? [{ key: "group", icon: <GroupIcon size="regular" aria-hidden="true" />, label: t(grouped ? "ungroup" : "group"),
      disabled: readOnly, run: () => view(tableConfigAfterGroup(config, property.id, properties)) }] : []),
    ...(!button ? [{ key: "calculate", icon: <FormulaIcon size="regular" aria-hidden="true" />, label: t("calculate"), disabled: readOnly,
      opens: "calculate" as const }] : []),
    { key: "freeze", icon: <PinIcon size="regular" aria-hidden="true" />, label: t(frozenEdge ? "unfreeze" : "freeze"), disabled: readOnly || (!frozenEdge && !freezable),
      dividerBefore: button, run: () => view(tableConfigAfterFreeze(config, shownPropertyIds, property.id)) },
    ...(!title ? [{ key: "hide", icon: <EyeSlashIcon size="regular" weight="regular" aria-hidden="true" />, label: t("hide"), disabled: readOnly,
      run: () => view(tableConfigAfterHide(config, properties, property.id)) }] : []),
    // A checkbox has nothing to wrap, and Notion's menu leaves it out there.
    ...(property.type !== "checkbox" && !button ? [{ key: "wrap", icon: wrapped ? <UnwrapIcon size="regular" aria-hidden="true" /> : <WrapIcon size="regular" aria-hidden="true" />,
      label: t(wrapped ? "unwrapText" : "wrapText"), disabled: readOnly,
      run: () => view(tableConfigAfterWrap(config, property.id, properties)) }] : []),
    // A status shows as its option or as a checkbox, as Notion's (seen 2026-10-11).
    ...(property.type === "status" ? [{ key: "statusDisplay", icon: <EyeIcon size="regular" aria-hidden="true" />, label: t("statusShowAs"),
      meta: t(statusCheckbox ? "statusShowAsCheckbox" : "statusShowAsSelect"), disabled: readOnly, opens: "statusDisplay" as const }] : []),
    { key: "insert-left", icon: <InsertLeftIcon size="regular" aria-hidden="true" />, label: t("insertLeft"), disabled: readOnly, dividerBefore: true,
      run: () => { keepHead(); onInsert("left"); } },
    { key: "insert-right", icon: <InsertRightIcon size="regular" aria-hidden="true" />, label: t("insertRight"), disabled: readOnly,
      run: () => { keepHead(); onInsert("right"); } },
    // A relation can't be duplicated and its menu leaves the row out, as Notion's (seen 2026-10-11).
    ...(!title && !fixed && canDuplicateProperty(presentation) ? [
      { key: "duplicate", icon: <Copy size="regular" aria-hidden="true" />, label: t("duplicateProperty"),
        disabled: readOnly || fixedType, run: duplicate },
    ] : []),
    ...(!title && !fixed ? [
      { key: "delete", icon: taskRole ? <LockIcon size="regular" aria-hidden="true" /> : <Trash size="regular" aria-hidden="true" />, label: t("deleteProperty"), danger: !taskRole, disabled: readOnly || taskRole,
        tip: taskRole ? t("databaseView:taskPropertyRequired") : undefined,
        // The menu closes and the window asks, as Notion's does (seen 2026-10-06).
        run: () => {
          close();
          void confirmPropertyDeletion({
            property: { id: property.id, type: property.type, dataSourceId: dataSource.id, config: property.config },
            database: plainTitleOf(dataSource) || inlineDatabasePlaceholderTitle(),
            relatedDatabase: (id) => pagesById?.[id]?.title,
          }).then((confirmed) => { if (confirmed) onDelete(property.id); });
        } },
    ] : []),
  ];
  // The name keeps focus as Notion's does; arrows move the rows' highlight,
  // the name a stop of its own at their head.
  const keyboardAction = useRef(false);
  const list = useNativeMenu({ items: rows, open: menuPlace !== null, menuRef: listRef, label: t("propertyOptionsFor", { name: property.name }),
    focusOnOpen: false, focusOwnerRef: nameRef, ownerKeepsTab: true, ownerSearches: false, nameStop: true, disabledKeys: rows.filter((row) => row.disabled).map((row) => row.key),
    keyOf: (row) => row.key, textOf: (row) => row.label, onAction: (row) => {
      const item = listRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(row.key)}"]`);
      if (row.opens === "edit" && property.type === "button") setButtonDialog(true);
      else if (row.opens) { if (item) open(row.opens, item, keyboardAction.current); }
      else row.run?.();
    } });
  const highlighted = list.ownerProps["aria-activedescendant"];
  useLayoutEffect(() => {
    const name = nameRef.current;
    if (!name) return;
    if (highlighted) name.setAttribute("aria-activedescendant", highlighted); else name.removeAttribute("aria-activedescendant");
    name.setAttribute("aria-controls", list.ownerProps["aria-controls"] ?? "");
  }, [highlighted, list.ownerProps]);
  function onDialogKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    // ArrowRight opens the highlighted row's side menu with the keys.
    const focused = list.state.selectionManager.focusedKey;
    const opens = focused === null ? undefined : rows.find((row) => row.key === focused && !row.disabled)?.opens;
    const item = opens ? listRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(String(focused))}"]`) : null;
    // The title column's menu holds the keys itself, its name unfocused.
    const owner = event.target === nameRef.current || event.target === event.currentTarget;
    if (event.key === inlineEndKey(event.currentTarget) && owner && opens && item && !imeComposing(event)) {
      event.preventDefault(); event.stopPropagation(); open(opens, item, true); return;
    }
    if (event.target === event.currentTarget && fieldEnter(event) && focused !== null) {
      const row = listRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(String(focused))}"]`);
      if (row) { event.preventDefault(); keyboardAction.current = true; try { row.click(); } finally { keyboardAction.current = false; } return; }
    }
    if (owner) {
      keyboardAction.current = true; try { if (list.ownerProps.onKeyDown(event)) return; } finally { keyboardAction.current = false; }
    }
    onKeyDown(event);
  }
  if (typeof document === "undefined") return null;
  if (buttonDialog) {
    return <DatabaseButtonAutomationDialog property={property} current={currentProperty} properties={presentationProperties} pages={[database]}
      readOnly={readOnly} onRename={(name) => onUpdate({ ...property, name })} onSave={onUpdate} onClose={close} />;
  }
  let side: ReactNode = null;
  if (submenu) {
    const width = SUBMENU_WIDTH[submenu.kind];
    const label = t(submenu.kind === "edit" ? (property.type === "button" ? "editAutomation" : "editProperty")
      : submenu.kind === "type" ? "changeType" : submenu.kind === "sort" ? "sort" : submenu.kind === "statusDisplay" ? "statusShowAs" : "calculate");
    const kind = submenu.kind;
    side = (
      <TablePropertySide key={kind} className={menuStyles.submenu} data-submenu={kind} role={kind === "edit" ? "dialog" : kind === "calculate" ? "group" : undefined}
        aria-label={kind === "edit" || kind === "calculate" ? label : undefined} onKeyDown={onSideKeyDown} onPointerMove={sideHover.onSidePointerMove}
        sideRef={sideRef} row={() => menuRef.current?.querySelector(`[data-key="${CSS.escape(kind)}"]`) ?? null} style={{ width, ...popoverPlacementStyle(sidePlace) }}>
        {submenu.kind === "edit" && advanced}
        {/* The same types, names and new-type defaults as the row page's
            property menu. */}
        {submenu.kind === "type" && (
          <PropertyTypeMenu property={property} label={label} focusOnOpen={submenu.keyboard}
            onChoose={(type) => confirmPropertyTypeChange(presentation, type, () => {
              onUpdate(propertyAfterTypeChange(property, type, dataSource.id));
              // A relation asks next which database; any other type closes the
              // menu, as Notion's does (seen 2026-10-13).
              if (type === "relation" && property.type !== "relation") setRelationTarget(true);
              else close();
            }, close)} />
        )}
        {submenu.kind === "sort" && (
          <TableSortSideMenu label={label} focusOnOpen={submenu.keyboard} ascending={t(ascending)} descending={t(descending)}
            onSort={(direction) => view(tableConfigAfterSort(config, property.id, direction))} />
        )}
        {submenu.kind === "statusDisplay" && (
          <TableStatusDisplayMenu label={label} focusOnOpen={submenu.keyboard} checkbox={statusCheckbox}
            choices={{ checkbox: t("statusShowAsCheckbox"), select: t("statusShowAsSelect") }}
            onPick={(checkbox) => view(tableConfigAfterStatusDisplay(config, property.id, checkbox, properties))} />
        )}
        {submenu.kind === "calculate" && (
          <TableCalculationMenu label={t("calculate")} type={property.type} calculation={tableCalculation(calculations[property.id])}
            focusOnOpen={submenu.keyboard} litOnOpen limitLargeCounts={config.limitLargeCounts === true}
            onLimitLargeCounts={(limit) => onViewConfig({ ...config, limitLargeCounts: limit || undefined })}
            onPick={(next) => view(tableConfigAfterCalculation(config, property.id, next))} />
        )}
      </TablePropertySide>
    );
  }
  const relationDialog = relationCreation && relationTarget && property.type === "relation" ? (
    <RelationPropertyCreationDialog
      context={relationCreation.context}
      databaseId={dataSource.id}
      existingProperty={property}
      snapshot={relationCreation.snapshot}
      onCancel={close}
      onCreated={close}
    />
  ) : null;
  if (phone && !settingsOnly) {
    return createPortal(
      <SheetLayer>
        <TablePropertyPhoneSheet property={property} title={t("editProperty")} opened={editing ? "edit" : undefined}
          // A button's "자동화 편집" opens its window, as on a desktop.
          rows={rows.map((row) => button && row.opens === "edit" ? { ...row, opens: undefined, run: () => setButtonDialog(true) } : row)}
          head={fixed ? null : <TablePropertyMenuHead phone property={property} readOnly={readOnly} nameRef={nameRef} descriptionRef={descriptionRef}
            onChange={changeHead} onUpdate={onUpdate} />}
          edit={{ title: t("editProperty"), content: advanced }}
          sort={{ ascending: t(ascending), descending: t(descending), onSort: (direction) => view(tableConfigAfterSort(config, property.id, direction)) }}
          calculation={{ title: t("calculateSheet"), type: property.type, picked: tableCalculation(calculations[property.id]),
            limitLargeCounts: { on: config.limitLargeCounts === true, set: (limit) => onViewConfig({ ...config, limitLargeCounts: limit || undefined }) },
            onPick: (next) => view(tableConfigAfterCalculation(config, property.id, next)) }}
          status={{ title: t("statusShowAs"), checkbox: statusCheckbox, choices: { checkbox: t("statusShowAsCheckbox"), select: t("statusShowAsSelect") },
            onPick: (checkbox) => view(tableConfigAfterStatusDisplay(config, property.id, checkbox, properties)) }}
          onType={(type) => confirmPropertyTypeChange(presentation, type, () => {
            onUpdate(propertyAfterTypeChange(property, type, dataSource.id));
            if (type === "relation" && property.type !== "relation") setRelationTarget(true);
            else close();
          })}
          onClose={close} />
        {typeChangeConfirmDialog}
        {relationDialog}
      </SheetLayer>,
      document.body,
    );
  }
  if (settingsOnly) {
    return createPortal(
      <>
        <button ref={backdropRef} type="button" className={styles.propertyMenuBackdrop} style={popoverBackdropStyle(menuPlace)} aria-label={t("closePropertyMenu")} onClick={close} />
        <div ref={menuRef} className={menuStyles.menu} role="dialog" tabIndex={-1} aria-label={t("editProperty")} data-table-property-settings={property.id}
          style={popoverPlacementStyle(menuPlace)} onKeyDown={onDialogKeyDown}>
          {advanced}
        </div>
      </>,
      document.body,
    );
  }
  return createPortal(
    <>
      <button ref={backdropRef} type="button" className={styles.propertyMenuBackdrop} style={popoverBackdropStyle(menuPlace)} aria-label={t("closePropertyMenu")} onClick={close} />
      <div
        ref={menuRef}
        className={menuStyles.menu}
        role="dialog"
        tabIndex={-1}
        aria-label={t("propertyOptionsFor", { name: property.name })}
        data-table-property-menu={property.id}
        // As wide as its longest line (TablePropertyMenu.module.css .menu); the
        // name column's head, its name at a field's own width, is one of them.
        style={popoverPlacementStyle(menuPlace)} data-title-column={title || undefined}
        onKeyDown={onDialogKeyDown} onPointerMove={sideHover.onMenuPointerMove} onPointerOver={sideHover.onMenuPointerOver}
        onKeyUp={(event) => { if (event.target === nameRef.current) list.ownerProps.onKeyUp(event); }}
      >
        {fixed ? null : <TablePropertyMenuHead property={property} readOnly={readOnly} nameRef={nameRef} descriptionRef={descriptionRef}
          onChange={changeHead} onUpdate={onUpdate} />}
        <div {...list.menuProps} ref={listRef} style={{ display: "contents" }}>
          {rows.map((row) => (
            <Fragment key={row.key}>
              {row.dividerBefore ? <div className={styles.propertyHeaderDivider} /> : null}
              <NativeMenuItem state={list.state} itemKey={row.key} className={styles.propertyHeaderItem} title={row.tip}
                role={row.on === undefined ? undefined : "menuitemcheckbox"} aria-checked={row.on}
                aria-haspopup={row.opens ? (row.opens === "edit" ? "dialog" : "menu") : undefined}
                aria-expanded={row.opens ? submenu?.kind === row.opens : undefined}
                data-danger={row.danger ? "true" : undefined} data-menu-item={row.key === "edit" ? "edit" : ""}
                data-active={row.opens && submenu?.kind === row.opens ? "true" : undefined}
                {...submenuRowMarks(row.opens === "statusDisplay" || (button && row.opens === "edit") ? undefined : row.opens)}>
                {({ labelProps }) => <>
                  {row.icon}
                  <span {...labelProps} className={styles.propertyHeaderItemText}>{row.label}</span>
                  {row.meta ? <span className={styles.propertyHeaderItemMeta}>{row.meta}
                    {submenu?.kind === row.opens ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}</span>
                    : row.opens && !row.disabled && !(button && row.opens === "edit") && <ChevronRight size="small" aria-hidden="true" />}
                  {row.on === undefined ? null : <SwitchMark on={row.on} className={styles.propertyHeaderItemMeta} />}
                </>}
              </NativeMenuItem>
            </Fragment>
          ))}
        </div>
      </div>
      {side}
      {typeChangeConfirmDialog}
      {relationDialog}
    </>,
    document.body,
  );
}
