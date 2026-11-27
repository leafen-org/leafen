import { Fragment, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { ArrowLeft, CheckIcon, ChevronRight, CommentIcon, Copy, EyeIcon, EyeSlashIcon, LayoutIcon, PageLayoutIcon, Pencil, PropertiesIcon, Trash } from "@/icons/leafen";
import { RowMenuChoiceSubmenu } from "../RowMenuSubmenus";
import type { DbProperty, PropertyConfig } from "@/lib/types";
import { keyboardPropsEvent, useOverlayLayer } from "@/lib/overlayLayer";
import { keyCameLast, openerOf } from "@/lib/lastPress";
import { placeAround, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { DatabasePropertyEditPanel, type DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { duplicatedProperty } from "./propertyEditModel";
import { canDuplicateProperty, canHideWhenEmpty, propertiesAfterDuplicate } from "./rowPropertiesModel";
import { tableConfigAfterDuplicate } from "./tablePropertyMenuModel";
import styles from "./RowPagePropertyMenu.module.css";
import { RowPagePropertyPhoneSheet } from "./RowPagePropertyPhoneSheet";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { confirmPropertyDeletion } from "./confirmPropertyDeletion";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { plainTitleOf } from "@/lib/closedMentions";

type Panel = "main" | "rename" | "edit" | "new" | "rollupForm";

/** How far a database page's property menus keep from the window's top and
 * foot, its "속성 추가" menu too: Notion's move up by whole pixels till their
 * foot is 11 over the window's, which leaves 10 to 11 (seen 2026-11-14 at
 * 1440×723: feet at 712.9, 712.5 and 712, where most popovers keep 12) —
 * here the middle of that. */
export const ROW_PAGE_MENU_EDGE = 10.5;

type Visibility = "always" | "whenEmpty" | "hidden";

function visibilityOf(property: GraphProperty): Visibility {
  if (property.config.hideInPagePanel) return "hidden";
  return property.config.hideWhenEmpty ? "whenEmpty" : "always";
}

function presentation(property: GraphProperty): DbProperty {
  return { ...property, databaseId: property.dataSourceId, position: 0, config: property.config as PropertyConfig } as DbProperty;
}

/**
 * A property's menu on a database page, as Notion's (seen 2026-10-01):
 * "이름 바꾸기" and "속성 편집" open its settings, the first with its whole
 * name chosen (Notion's, measured in Chrome with no script before the press
 * and one letter typed after it, which took the name's place: 2026-11-15,
 * worker w1139 and w1249 in another tab at another time, a side peek);
 * "댓글" starts a thread on its value; "속성 표시 여부 ›" chooses,
 * in a 180px list beside the menu (seen 2026-10-11), when the page shows it
 * (always, not when empty, never); then "속성 복제" and "속성 삭제". Who
 * can't change the database's properties has "댓글" alone. Escape from a
 * row's settings goes back to this menu with no row lit, ↓ lighting the
 * first, pressed or keyed open (Notion's, seen 2026-11-14 at 1440); a
 * property just made opens on its settings, its whole name chosen, and
 * Escape goes back the same way (seen 2026-11-16 twice, a full page and a side peek). A relation in the layout
 * builder's relations group has Notion's menu for it instead (seen
 * 2026-11-07): "표시 옵션 ›", its name and settings, "레이아웃에 추가" and
 * "속성 삭제", parted by lines.
 */
export function RowPagePropertyMenu({ property, anchor, editing, opened, extraActions = [], layoutRelation, pinned = false, onCustomizeLayout, onComment, onClose }: {
  property: GraphProperty;
  /** Opened from its name among the page's pinned properties: no showing or copying rows (usePinnedPropertyMenu). */
  pinned?: boolean;
  /** Opens the page layout builder, the page the menu is on previewed first. */
  onCustomizeLayout?(): void;
  /** The property's name, which the menu opens under, or over where only that side has room. */
  anchor: PopoverRect;
  editing?: DatabasePropertyEditing;
  /**
   * "new": the property was just made, and the menu opens on its settings, 1px from its row;
   * "rollupForm": a rollup's value was pressed, and its small form opens by the value, as Notion's (seen 2026-11-06).
   */
  /** "redo": opened by a ⌘⇧Z making the property again, no row lit (Notion's, seen 2026-11-11). */
  opened?: "new" | "rollupForm" | "redo";
  /** Rows after "속성 편집" where the menu is open: the layout builder's "레이아웃에 추가". */
  extraActions?: readonly { key: string; label: string; icon: ReactNode; run(): void }[];
  /** A relation in a page layout's relations group: how it shows there, and its group's own "레이아웃에 추가" and "속성 삭제". */
  layoutRelation?: {
    display: "minimized" | "section";
    onDisplay(display: "minimized" | "section"): void;
    onAddToLayout(): void;
    onDelete?(): void;
  };
  /** Starts a thread on the property's value, where its value takes one. */
  onComment?(): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["rowProperties", "tableView", "pageLayout"]);
  const [panel, setPanel] = useState<Panel>(opened === "new" || opened === "rollupForm" ? opened : "main");
  // "속성 표시 여부 ›" opens its choices beside the row.
  const [visibilityRow, setVisibilityRow] = useState<HTMLElement | null>(null);
  // "표시 옵션 ›" of a layout's relation opens its two ways beside the row.
  const [displayRow, setDisplayRow] = useState<HTMLElement | null>(null);
  // Back by Escape from a row's panel: no row lit, the keys staying in the menu.
  const [returned, setReturned] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  // Opened from its name, pressed or keyed, the name keeps the focus while
  // the menu is open, the keys moving the menu's light from it, as Notion's.
  // Measured 2026-11-14 at 1440 dark, right after each action
  // (document.activeElement, getSelection(), then one typed letter):
  // pressed — the name, not :focus-visible, no selection; ↓ — still the
  // name, now ringed, "이름 바꾸기" lit; "x" — went nowhere, name and menu as
  // they were; Escape — the menu gone, the name keeping focus and ring. A
  // property just made, a rollup's form and one made again by ⌘⇧Z keep the
  // keys in the menu, as does the menu come back to from a row's settings.
  const [name] = useState(() => {
    if (opened || typeof document === "undefined") return null;
    const pressed = openerOf(document.activeElement instanceof HTMLElement ? document.activeElement : null);
    return pressed?.matches("[data-row-property-name], [data-row-pinned-name]") ? pressed : null;
  });
  const nameRef = useRef(name);
  const shown = presentation(property);
  const fixed = property.type === "title";
  // On a phone it is Notion's sheet (RowPagePropertyPhoneSheet); a rollup's small form stays by its value.
  const phoneSheet = useSidebarLayout(true).phone && opened !== "rollupForm";

  // The menu is a layer over the page: Escape goes back to its first panel,
  // then closes it, and a press outside it closes it. A property just made
  // opens on its settings, and Escape there goes back to the menu as from
  // "속성 편집" (Notion's, seen 2026-11-14 on a row page); a rollup's small
  // form closes at once.
  const { zIndex } = useOverlayLayer(!phoneSheet, {
    kind: "menu", ref: menuRef,
    onEscape: () => {
      if (panel === "main" || panel === "rollupForm") onClose();
      else { setReturned(true); setPanel("main"); }
    },
    onOutsidePress: onClose,
  });
  // 1px under the name, or over it where only that side has room for it
  // whole (1px over it); with room on neither, under it moved up into the
  // window over it, as its panels change size — its settings opened from a
  // name low in the window (Notion's, seen 2026-11-14 at 1440×723: under a
  // name at 329..363 the menu at 364, its 455-high settings at 258..713;
  // over a name at 628 the menu and its settings with their foot at 627).
  // A rollup's small form covers its value from the value's top, or stands
  // right over it near the foot, as Notion's (seen 2026-11-11 in a side
  // peek: form top, or form foot, 1px above the value). The menu and its
  // panels are at most 80% of the window high, scrolling past that (Notion's,
  // seen 2026-11-14 in a 723 window: "속성 편집 › 유형" 578 high at 134..712).
  const rollupForm = opened === "rollupForm";
  const placed = popoverPlacementStyle(usePopoverPlacement(menuRef,
    (size, viewport) => rollupForm
      ? placeAround({ ...anchor, top: anchor.top - 1, bottom: anchor.top - 1 }, size, viewport, { gap: 0, minHeight: 160 })
      : placeAround(anchor, size, viewport, { gap: 1, minHeight: 160, whole: true, marginY: ROW_PAGE_MENU_EDGE, cap: viewport.height * 0.8 }), [anchor, panel], undefined, false));
  // Settings opened by a press keep the keys in the menu, as Notion's do, so
  // Escape goes back to its first panel: the list closing hands the focus
  // back to the name under it, which is outside the menu. Back on the list,
  // the keys stay in it for the arrows. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440, a side peek:
  // "속성 편집" pressed, the name not focused and "z" went nowhere, Escape back to the first panel; Notion's
  // focus on <body>, Leafen's on the menu (next key the same).
  useEffect(() => {
    if (panel !== "edit" && !(panel === "main" && returned)) return;
    const frame = window.requestAnimationFrame(() => {
      const root = menuRef.current;
      if (root && !root.contains(document.activeElement)) root.focus({ preventScroll: true });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [panel, returned]);
  // A rollup's form rings none of its choices, as Notion's (seen 2026-11-11
  // in a side peek, pressed or opened with Enter). Pressed open, the keys stay
  // on the pressed value: a typed letter lands nowhere and rings it, Escape
  // closes the form and Tab closes it too, going on past the value, as
  // Notion's (a side peek, measured twice on 2026-11-15 in Chrome 1440 in
  // different tabs, no script before the press, the screen and one typed
  // letter after it). Opened with Enter it takes the keys on itself, Tab
  // going on to its choices (seen 2026-11-11).
  const [rollupKeyed] = useState(() => opened === "rollupForm" && keyCameLast());
  useLayoutEffect(() => {
    if (rollupKeyed) menuRef.current?.focus({ preventScroll: true });
  }, [rollupKeyed]);
  useEffect(() => {
    if (opened !== "rollupForm" || rollupKeyed) return undefined;
    const tabAway = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Tab" && !menuRef.current?.contains(document.activeElement)) onClose();
    };
    document.addEventListener("keydown", tabAway, true);
    return () => document.removeEventListener("keydown", tabAway, true);
  }, [opened, rollupKeyed, onClose]);


  function setVisibility(next: Visibility) {
    if (!editing) return;
    editing.onCommitProperties([{
      ...property,
      config: { ...property.config, hideInPagePanel: next === "hidden", hideWhenEmpty: next === "whenEmpty" },
    }]);
    onClose();
  }

  function duplicate() {
    if (!editing) return;
    const copy = duplicatedProperty(property, editing.properties, t);
    // After its source on the page too, the database taking the page's order if it has none.
    editing.onCommitProperties(propertiesAfterDuplicate(editing.properties, property.id, copy));
    if (editing.view && editing.onUpdateView) {
      editing.onUpdateView({
        ...editing.view,
        config: tableConfigAfterDuplicate(editing.view.config, [...editing.properties, copy], property.id, copy.id),
      });
    }
    onClose();
  }

  // The menu closes and the window asks, as Notion's does (seen 2026-10-06).
  async function remove() {
    const deleteProperty = editing?.onDeleteProperty;
    if (!editing || !deleteProperty) return;
    const database = plainTitleOf(editing.dataSource) || inlineDatabasePlaceholderTitle();
    onClose();
    if (!await confirmPropertyDeletion({
      property: { id: property.id, type: property.type, dataSourceId: editing.dataSource.id, config: property.config },
      database,
      relatedDatabase: (id) => editing.dataSources.find((source) => source.id === id)?.title ?? editing.pagesById?.[id]?.title,
    })) return;
    deleteProperty(property.id);
  }

  type Row = { key: string; label: string; icon?: ReactNode; next?: boolean; disabled?: boolean; danger?: boolean;
    checked?: boolean; back?: boolean; data: Record<string, string>; run(): void };
  // Notion's groups (seen 2026-11-06, and 2026-11-16 at 1440 on a full page
  // and in a side peek): naming, settings and "댓글", then showing, copying
  // and deleting, then the layout builder, parted by lines.
  const GROUP_STARTS = new Set(["visibility", "duplicate", "delete", "layout"]);
  const visibility = visibilityOf(property);
  const action = (key: string, label: string, icon: ReactNode, run: () => void, extra: Partial<Row> = {}): Row =>
    ({ key, label, icon, run, data: { "data-row-property-menu-item": key }, ...extra });
  const rowOf = (key: string) => menuRef.current?.querySelector<HTMLElement>(`[data-row-property-menu-item="${key}"]`) ?? null;
  const rows: Row[] = panel !== "main" && !phoneSheet ? [] : layoutRelation ? [
    action("display", t("pageLayout:relationDisplayOptions"), <EyeIcon size="regular" />, () => setDisplayRow(rowOf("display")), { next: true }),
    ...(editing ? [action("rename", t("rowProperties:renameProperty"), <Pencil size="regular" />, () => setPanel("rename"), { disabled: !editing.writable }),
      action("edit", t("tableView:editProperty"), <PropertiesIcon size="regular" />, () => setPanel("edit"), { disabled: !editing.writable })] : []),
    action("addToLayout", t("pageLayout:addToLayout"), <LayoutIcon size="regular" />, () => { onClose(); layoutRelation.onAddToLayout(); }),
    ...(layoutRelation.onDelete ? [action("delete", t("rowProperties:deleteProperty"), <Trash size="regular" />, () => { onClose(); layoutRelation.onDelete?.(); },
      { danger: true })] : []),
  ] : [
    ...(editing ? [action("rename", t("rowProperties:renameProperty"), <Pencil size="regular" />, () => setPanel("rename"), { disabled: !editing.writable }),
      action("edit", t("tableView:editProperty"), <PropertiesIcon size="regular" />, () => setPanel("edit"), { disabled: !editing.writable }),
      ...extraActions.map((extra) => action(extra.key, extra.label, extra.icon, () => { onClose(); extra.run(); }))] : []),
    ...(onComment ? [action("comment", t("rowProperties:comment"), <CommentIcon size="regular" />, () => { onClose(); onComment(); })] : []),
    ...(editing && !fixed && !pinned ? [action("visibility", t("rowProperties:propertyVisibility"), <EyeSlashIcon size="regular" weight="regular" />,
      () => setVisibilityRow(rowOf("visibility")),
      { next: true, disabled: !editing.writable })] : []),
    ...(editing && !fixed && !pinned && canDuplicateProperty(shown) ? [action("duplicate", t("rowProperties:duplicateProperty"), <Copy size="regular" />, duplicate,
      { disabled: !editing.writable })] : []),
    ...(editing && !fixed && editing.onDeleteProperty ? [action("delete", t("rowProperties:deleteProperty"), <Trash size="regular" />, remove,
      { danger: true, disabled: !editing.writable })] : []),
    // Notion's last row (seen 2026-10-01): the database's page layout builder.
    ...(editing?.writable && onCustomizeLayout ? [action("layout", t("rowProperties:customizeLayout"), <PageLayoutIcon size="regular" />, () => {
      onClose();
      onCustomizeLayout();
    })] : []),
  ];
  // A layout's relation (Notion's, seen 2026-11-07): a line under "표시 옵션", before "레이아웃에 추가" and before "속성 삭제".
  const parted = (row: Row, index: number) => index > 0 && (layoutRelation ? ["rename", "addToLayout", "delete"].includes(row.key)
    : row.key === "layout" || (GROUP_STARTS.has(row.key) && !GROUP_STARTS.has(rows[index - 1].key)));
  const menuPanel = panel === "main";
  const fromName = Boolean(name) && menuPanel && !returned && !phoneSheet;
  const menu = useNativeMenu({ items: rows, open: menuPanel && !phoneSheet, menuRef, label: t("rowProperties:propertyOptions", { name: property.name }),
    ...(fromName ? { focusOwnerRef: nameRef, ownerSearches: false } : {}),
    // Back with no row lit, the menu taking the focus from its name's field lights none either.
    highlightOnOpen: opened === "redo" || returned ? false : undefined, quietOnPointer: returned,
    disabledKeys: rows.filter((row) => row.disabled).map((row) => row.key),
    keyOf: (row) => row.key, textOf: (row) => row.label, onAction: (row) => row.run() });
  // Back with no row lit, the keys still move through the rows: ↓ lights the
  // first (the menu, holding the focus still, isn't entered afresh).
  const selection = menu.state.selectionManager;
  useLayoutEffect(() => {
    if (menuPanel && returned) selection.setFocused(true);
  }, [menuPanel, returned, selection]);
  // The name holding the focus names the lit row and hands the menu its keys.
  const owner = useRef(menu.ownerProps);
  owner.current = menu.ownerProps;
  const lit = menu.ownerProps["aria-activedescendant"];
  useLayoutEffect(() => {
    if (!fromName || !name) return;
    if (lit) name.setAttribute("aria-activedescendant", lit); else name.removeAttribute("aria-activedescendant");
    name.setAttribute("aria-controls", owner.current["aria-controls"] ?? "");
  }, [fromName, lit, name]);
  useLayoutEffect(() => {
    if (!fromName || !name) return;
    const down = (event: KeyboardEvent) => { owner.current.onKeyDown(keyboardPropsEvent(event, name)); };
    const up = (event: KeyboardEvent) => { owner.current.onKeyUp(keyboardPropsEvent(event, name)); };
    name.addEventListener("keydown", down);
    name.addEventListener("keyup", up);
    return () => {
      name.removeEventListener("keydown", down);
      name.removeEventListener("keyup", up);
      name.removeAttribute("aria-activedescendant");
      name.removeAttribute("aria-controls");
    };
  }, [fromName, name]);

  if (phoneSheet) {
    // Notion's phone groups (seen 2026-11-11): naming, settings and "댓글"; showing, copying and deleting; the layout builder.
    const starts = new Set(["visibility", "duplicate", "delete", "layout"]);
    const visibilityChoices = [
      { value: "always", label: t("rowProperties:visibilityAlways") },
      { value: "whenEmpty", label: t("rowProperties:visibilityWhenEmpty"), disabled: !canHideWhenEmpty(shown) },
      { value: "hidden", label: t("rowProperties:visibilityHidden") },
    ];
    return (
      <RowPagePropertyPhoneSheet propertyId={property.id} title={property.name} settingsTitle={t("tableView:editProperty")}
        rows={rows.map((row, index) => ({ ...row,
          groupStart: layoutRelation ? parted(row, index) : index > 0 && (row.key === "layout" || (starts.has(row.key) && !starts.has(rows[index - 1].key))),
          opens: row.key === "rename" || row.key === "edit" ? "settings" as const : row.key === "visibility" || row.key === "display" ? "choices" as const : undefined }))}
        choices={[
          { key: "visibility", title: t("rowProperties:propertyVisibility"), value: visibility, choices: visibilityChoices, onChoose: (next) => setVisibility(next as Visibility) },
          ...(layoutRelation ? [{ key: "display", title: t("pageLayout:relationDisplayOptions"), value: layoutRelation.display,
            choices: [{ value: "section", label: t("pageLayout:relationShowSection") }, { value: "minimized", label: t("pageLayout:relationShowMinimized") }],
            onChoose: (display: string) => { layoutRelation.onDisplay(display as "section" | "minimized"); onClose(); } }] : []),
        ]}
        settingsOpen={panel === "rename" || panel === "edit" || panel === "new"} onSettings={(open) => setPanel(open ? "edit" : "main")}
        // Its name isn't focused, so no keyboard rises over the settings (Notion's "이름 바꾸기", seen 2026-11-11); a new one's is chosen whole.
        settings={editing ? <DatabasePropertyEditPanel property={property} editing={editing} rowPage focusName={panel === "new" ? "select" : false} onClose={onClose} /> : null}
        onClose={onClose} />
    );
  }
  return createPortal(
    <div {...(menuPanel ? menu.menuProps : { role: "dialog", "aria-label": t("rowProperties:propertyOptions", { name: property.name }) })}
      ref={menuRef} className={styles.menu} data-row-property-menu={property.id} data-row-menu-variant="property" data-panel={panel}
      // Its rows the keys light ring none, as Notion's keeps the focus on the
      // page, not in the menu (seen 2026-11-14 at 1440: ↓ shaded "이름 바꾸기"
      // and "속성 편집", opened or come back to); the menu itself keeps the
      // focus here, which a row's settings handing it back left on the page.
      data-ringless={menuPanel || undefined}
      tabIndex={panel === "rollupForm" || panel === "edit" || menuPanel ? -1 : undefined}
      style={{ ...placed, zIndex }}>
      {rows.map((row, index) => <Fragment key={row.key}>
        {parted(row, index) ? <div className={styles.separator} role="separator" /> : null}
        {row.back ? (
        <NativeMenuItem key={row.key} state={menu.state} itemKey={row.key} className={styles.back}>
          {({ labelProps }) => <><ArrowLeft size="small" aria-hidden="true" /><span {...labelProps}>{row.label}</span></>}
        </NativeMenuItem>
      ) : (
        <NativeMenuItem key={row.key} state={menu.state} itemKey={row.key} className={styles.item}
          role={row.checked === undefined ? undefined : "menuitemradio"} aria-checked={row.checked}
          aria-haspopup={row.next ? "menu" : undefined} data-danger={row.danger ? "true" : undefined} {...row.data}>
          {({ labelProps }) => <>
            {row.icon ? <span className={styles.icon} aria-hidden="true">{row.icon}</span> : null}
            <span {...labelProps} className={styles.label}>{row.label}</span>
            {row.next ? <ChevronRight className={styles.next} size="small" aria-hidden="true" /> : null}
            {row.checked ? <CheckIcon className={styles.next} size="small" aria-hidden="true" /> : null}
          </>}
        </NativeMenuItem>
      )}</Fragment>)}
      {!menuPanel && editing ? (
        <DatabasePropertyEditPanel property={property} editing={editing} rowPage focusName={panel === "new" || panel === "rename" ? "select" : false}
          rollupForm={panel === "rollupForm"} onClose={onClose} />
      ) : null}
      {visibilityRow && menuPanel ? (
        <RowMenuChoiceSubmenu row={visibilityRow} width={180} label={t("rowProperties:propertyVisibility")} marker="row-property-visibility"
          value={visibility} onChoose={setVisibility} onClose={() => setVisibilityRow(null)} choices={[
            { value: "always", label: t("rowProperties:visibilityAlways") },
            { value: "whenEmpty", label: t("rowProperties:visibilityWhenEmpty"), disabled: !canHideWhenEmpty(shown) },
            { value: "hidden", label: t("rowProperties:visibilityHidden") },
          ]} />
      ) : null}
      {displayRow && menuPanel && layoutRelation ? (
        <RowMenuChoiceSubmenu row={displayRow} width={180} label={t("pageLayout:relationDisplayOptions")} marker="layout-relation-display"
          value={layoutRelation.display} onChoose={(display) => { layoutRelation.onDisplay(display); onClose(); }} onClose={() => setDisplayRow(null)} choices={[
            { value: "section", label: t("pageLayout:relationShowSection") },
            { value: "minimized", label: t("pageLayout:relationShowMinimized") },
          ]} />
      ) : null}
    </div>,
    document.body,
  );
}
