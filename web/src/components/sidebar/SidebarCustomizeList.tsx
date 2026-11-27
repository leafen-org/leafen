import { placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { menuTakesFocus } from "@/lib/menuTakesFocus";
import { useDragReorder } from "@/lib/useDragReorder";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { TableIcon, DragHandleIcon, LayoutIcon, Plus, X, type LeafenIconSize } from "@/icons/leafen";
import { BuiltInIconGlyph } from "@/components/page/PageIcon";
import { parseNativeIconValue } from "@/components/page/nativeIcons";
import type { KnownSidebarSectionId } from "./sidebarLayoutModel";
import { SidebarSubmenuRow, type SidebarBesidePlace, type SidebarMenuChoice, type SidebarMenuSubmenu } from "./SidebarActionMenu";
import { sidebarViewPickerAnchor } from "./SidebarViewPicker";
import { SidebarCustomizingContext } from "./sidebarCustomizing";
import { useMenuEscape } from "./sidebarMenuEscape";
import { styles } from "./sidebarStyles";
import own from "./SidebarCustomizeList.module.css";

/**
 * The sidebar's customize mode, as Notion's (seen 2026-10-01 and
 * 2026-11-09): its shortcuts and "단축키 추가" over a line (seen 2026-11-18),
 * then the list's sections as 30px rows to drag by ⠿ into order,
 * each with its ⚙ menu and × "사이드바에서 제거" under the pointer, then
 * "섹션 추가" — the sections off it, "팀스페이스 ›" with "모든 팀스페이스"
 * while that is off it and each teamspace without a section of its own
 * (seen 2026-11-11), "데이터베이스 보기 ›", the databases beside it to choose
 * from (seen 2026-11-11), "고급"'s developer links, and the workspace's optional presets
 * under "워크스페이스 사전 설정", which Notion's "탭 추가" offers
 * (sidebar_presets.go) — and a blue "완료". In the sidebar the list fills
 * its body and scrolls there, "완료" under it at the body's foot, outside the
 * scroll, as Notion's (seen 2026-11-18 in Chrome at 1440, dpr 1, computed, the
 * window 423, 723 and 873 high: the button 28px high 8px in from the
 * sidebar's edges and 8px over the body's end, the list scrolling over it
 * with no line between). A preset's editor in the settings holds the same
 * list without the presets and "완료".
 */
export function SidebarCustomizeList({
  sections,
  addable,
  name,
  label = name,
  icon,
  mark,
  menu,
  viewPicker,
  viewsFull,
  shortcuts,
  presets = [],
  onAddPreset,
  teamspaces,
  onMove,
  onRemove,
  onAdd,
  onDone,
  scrollRef,
}: {
  /** The sections on the sidebar, in order, by key. */
  sections: readonly string[];
  /** The sections "섹션 추가" can put on it. */
  addable: readonly KnownSidebarSectionId[];
  name: (key: string) => string;
  /** What a row shows for its section, where it reads more than its name (a view's "〈보기〉 · 〈데이터베이스〉"). */
  label?: (key: string) => ReactNode;
  /** A section's icon, before its name, as Notion's rows show (seen 2026-10-08), at the size the row draws. */
  icon?: (key: string, size: LeafenIconSize) => ReactNode;
  /** What a row marks after its name, at its end (a preset's ⚠). */
  mark?: (key: string) => ReactNode;
  menu: (key: string) => ReactNode;
  /**
   * The databases whose view becomes a section, beside "섹션 추가"'s "데이터베이스 보기 ›" at `anchor`,
   * if a view can be added: `done` closes the menu once one is chosen, `back` the databases alone.
   */
  viewPicker?: (done: () => void, anchor: PopoverRect, back: () => void, byKeys: boolean) => ReactNode;
  /** Why no database view can be added, when the tab holds as many as it can. */
  viewsFull?: string;
  /** The list's shortcuts and "단축키 추가", above its sections. */
  shortcuts?: ReactNode;
  /** The workspace's optional presets, whose sections and shortcuts one adds. */
  presets?: readonly { id: string; name: string; icon?: string }[];
  onAddPreset?: (id: string) => void;
  /** The teamspaces "팀스페이스 ›" can make sections of their own, and making one; without it the row adds the teamspaces section. */
  teamspaces?: { choices: readonly { id: string; name: string; icon: ReactNode }[]; onAdd: (teamspaceId: string) => void };
  onMove: (key: string, before: string | null) => void;
  onRemove: (key: string) => void;
  onAdd: (id: KnownSidebarSectionId) => void;
  /** The sidebar's "완료"; a preset's editor has none. */
  onDone?: () => void;
  /** In the sidebar, its body's own scroll (a drag held near its ends scrolls it). */
  scrollRef?: (element: HTMLElement | null) => void;
}) {
  const { t } = useTranslation("sidebar");
  const [adding, setAdding] = useState<"menu" | null>(null);
  const [query, setQuery] = useState("");
  const [teamspaceQuery, setTeamspaceQuery] = useState("");
  const addRef = useRef<HTMLButtonElement>(null);
  const addMenuRef = useRef<HTMLDivElement>(null), shadeRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  type AddChoice = { key: string; label: string; data: Record<string, string>; icon?: ReactNode; run(): void; submenu?: SidebarMenuSubmenu };
  const close = () => {
    setAdding(null);
    setQuery("");
    setTeamspaceQuery("");
  };
  const sectionChoice = (id: KnownSidebarSectionId): AddChoice => ({ key: id, label: name(id), icon: icon?.(id, "regular"),
    data: { "data-sidebar-customize-add-section": id }, run: () => {
      close();
      onAdd(id);
    } });
  // "팀스페이스 ›": "모든 팀스페이스" while the teamspaces section is off the
  // list, then each teamspace without a section of its own, found by a search
  // of their names (Notion's, seen 2026-11-11).
  const teamspaceChoice = (): AddChoice[] => {
    if (!teamspaces) return [sectionChoice("teamspaces")];
    const all = !sections.includes("teamspaces");
    if (!all && teamspaces.choices.length === 0) return [];
    const needle = teamspaceQuery.trim().toLocaleLowerCase();
    const choices: SidebarMenuChoice[] = [
      ...(all ? [{ key: "all", label: t("allTeamspaces"), data: { "data-sidebar-customize-add-section": "teamspaces" }, run: () => {
        close();
        onAdd("teamspaces");
      } }] : []),
      ...teamspaces.choices.map((choice) => ({ key: choice.id, label: choice.name, icon: choice.icon,
        data: { "data-sidebar-customize-add-teamspace": choice.id }, run: () => {
          close();
          teamspaces.onAdd(choice.id);
        } })),
    ].filter((choice) => !needle || choice.label.toLocaleLowerCase().includes(needle));
    return [{ key: "teamspaces", label: name("teamspaces"), icon: icon?.("teamspaces", "regular"), data: { "data-sidebar-customize-add-teamspaces": "" },
      run: () => undefined, submenu: { label: name("teamspaces"), width: 280, data: { "data-sidebar-customize-teamspaces": "" },
        search: { label: t("searchTeamspaces"), query: teamspaceQuery, onQuery: setTeamspaceQuery, bare: true, remembers: "teamspace" }, choices } }];
  };
  const allChoices: AddChoice[] = [
    ...addable.filter((id) => id !== "developer").flatMap((id) => (id === "teamspaces" ? teamspaceChoice() : [sectionChoice(id)])),
    ...(viewPicker ? [{ key: "view", label: t("databaseView"), icon: <TableIcon size="regular" aria-hidden="true" />,
      data: { "data-sidebar-customize-add-view": "" }, run: () => undefined, submenu: { label: t("databaseView"),
        custom: ({ main, trigger, back, byKeys }: SidebarBesidePlace) => viewPicker(close,
          sidebarViewPickerAnchor({ main, trigger }), back, byKeys) } }] : []),
  ];
  // "고급" holds the developer links, after the pages, as Notion's menu (seen 2026-10-30).
  const advancedChoices: AddChoice[] = addable.filter((id) => id === "developer").map(sectionChoice);
  const presetIcon = (value?: string) => {
    const own = parseNativeIconValue(value);
    return own ? <BuiltInIconGlyph icon={own} size={20} color={own.color === "lightgray" ? "currentColor" : undefined} />
      : <LayoutIcon size="regular" aria-hidden="true" />;
  };
  const presetChoices: AddChoice[] = onAddPreset ? presets.map((preset) => ({ key: `preset:${preset.id}`, label: preset.name,
    icon: presetIcon(preset.icon), data: { "data-sidebar-customize-add-preset": preset.id }, run: () => {
      close();
      onAddPreset(preset.id);
    } })) : [];
  // "섹션 검색…" finds the sections by name, as Notion's menu does (seen 2026-10-08).
  const needle = query.trim().toLocaleLowerCase();
  const matching = (choices: AddChoice[]) => (needle ? choices.filter((choice) => choice.label.toLocaleLowerCase().includes(needle)) : choices);
  const addChoices = matching(allChoices);
  const addAdvanced = matching(advancedChoices);
  const addPresets = matching(presetChoices);
  // Its shade is its own: a press outside a list beside it closes it too (sidebarPickerBesideRow).
  const addLayer = useOverlayLayer(adding === "menu", { kind: "menu", floats: false, ref: addMenuRef, onEscape: close, inside: () => [addRef.current],
    modalOwned: () => [shadeRef.current], returnFocusRef: addRef });
  const addPlace = usePopoverPlacement(addMenuRef, adding === "menu"
    ? (size, viewport) => placeBeside(popoverRectOf(addRef.current)!, size, viewport, { gap: 4 }) : null, [adding], undefined, false);
  const addMenu = useNativeMenu({ items: [...addChoices, ...addAdvanced, ...addPresets], open: adding === "menu", menuRef: addMenuRef, label: t("addSection"),
    // Its search takes the focus and its first row is lit as it opens, pressed open too, as Notion's "섹션 추가" and
    // "단축키 추가" (seen 2026-11-18 at 1440 in Chrome in two tabs: the row grey, no ring, ↓ lighting the second row).
    focusOnOpen: true, highlightOnOpen: true, focusOwnerRef: searchRef,
    keyOf: (choice) => choice.key, textOf: (choice) => choice.label, onAction: (choice) => choice.run() });
  // Escape closes the open "팀스페이스 ›" before the menu, which takes the focus itself.
  const expandedChoice = addMenu.rootTrigger.expandedKeysStack[0];
  useMenuEscape(adding === "menu" && expandedChoice !== undefined, () => {
    menuTakesFocus(addMenuRef.current);
    addMenu.rootTrigger.closeSubmenu(expandedChoice!, 0);
  });

  const item = (choice: AddChoice) => (choice.submenu ? <SidebarSubmenuRow key={choice.key} row={choice} picker={addMenu} main={addMenuRef} /> : (
    <NativeMenuItem key={choice.key} state={addMenu.state} itemKey={choice.key} className={styles.sectionMenuItem} {...choice.data}>
      {({ labelProps }) => <>{choice.icon}<span {...labelProps} className={styles.sectionMenuLabel}>{choice.label}</span></>}
    </NativeMenuItem>
  ));

  // A section lands on the side of the row it is dropped on.
  const reorder = useDragReorder("sidebar-customize-section", (dragged, target, side) => {
    const rest = sections.filter((id) => id !== dragged);
    onMove(dragged, rest[rest.indexOf(target) + (side === "after" ? 1 : 0)] ?? null);
  }, { live: true });

  const list = (
    <div className={`${styles.customizeList} ${own.list}`} data-sidebar-customize data-in-sidebar={onDone ? "" : undefined}>
      {shortcuts}
      {shortcuts ? <div className={own.shortcutsDivider} role="separator" data-sidebar-customize-shortcuts-divider="" /> : null}
      {sections.map((id) => {
        return <div key={id} className={styles.customizeRow} data-sidebar-customize-section={id} {...reorder.row(id, () => true, true)}>
          <span className={styles.customizeHandle} aria-hidden="true"><DragHandleIcon size="small" /></span>
          {icon ? <span className={styles.customizeIcon} aria-hidden="true">{icon(id, "wide")}</span> : null}
          <span className={styles.customizeName}>{label(id)}</span>
          {mark?.(id)}
          <span className={styles.customizeActions}>
            {menu(id)}
            <button type="button" className={styles.sectionAction} data-sidebar-customize-remove={id}
              aria-label={t("removeFromSidebar")} title={t("removeFromSidebar")} onClick={() => onRemove(id)}>
              <X size="small" aria-hidden="true" />
            </button>
          </span>
        </div>;
      })}
      <div className={styles.customizeAddWrap}>
        <button ref={addRef} type="button" className={styles.customizeAdd} data-sidebar-customize-add aria-haspopup="menu"
          aria-expanded={adding !== null} onClick={() => (adding ? close() : setAdding("menu"))}>
          <Plus size="small" aria-hidden="true" />
          <span>{t("addSection")}</span>
        </button>
        {adding ? (
          <>
            <button ref={shadeRef} type="button" className={styles.moreMenuBackdrop} tabIndex={-1} aria-label={t("addSection")}
              onClick={close} />
            {createPortal(
              <div {...addMenu.menuProps} ref={addMenuRef} className={`${styles.customizeMenu} ${styles.sectionAddMenu}`} data-ringless="true"
                style={{ ...popoverPlacementStyle(addPlace), zIndex: addLayer.zIndex }}
                onKeyDown={(event) => { addMenu.menuProps.onKeyDown(event); addLayer.onKeyDown(event); }}>
                <label className={styles.viewChoiceSearch}>
                  <input ref={searchRef} value={query} placeholder={t("searchSections")} aria-label={t("searchSections")}
                    data-sidebar-customize-add-search {...addMenu.ownerProps} onKeyDown={addMenu.ownerProps.onKeyDown}
                    onChange={(event) => setQuery(event.target.value)} />
                </label>
                {allChoices.length > 0 && addChoices.length > 0 ? <div className={styles.shortcutMenuLabel}>{t("sectionGroupPages")}</div> : null}
                {allChoices.length + advancedChoices.length + presetChoices.length === 0 ? <p className={styles.customizeEmpty}>{t("noSectionsToAdd")}</p>
                  : addChoices.length + addAdvanced.length + addPresets.length === 0 ? <p className={styles.customizeEmpty}>{t("noMatchingSections")}</p> : null}
                {addChoices.map(item)}
                {addAdvanced.length ? <div className={styles.shortcutMenuLabel}>{t("sectionGroupAdvanced")}</div> : null}
                {addAdvanced.map(item)}
                {addPresets.length ? <div className={styles.shortcutMenuLabel}>{t("tabPresets")}</div> : null}
                {addPresets.map(item)}
                {viewsFull ? <p className={styles.customizeEmpty} data-sidebar-views-full>{viewsFull}</p> : null}
              </div>, document.body,
            )}
          </>
        ) : null}
      </div>
    </div>
  );
  return (
    <SidebarCustomizingContext.Provider value>
      {onDone ? <>
        <div ref={scrollRef} className={`${styles.scroll} nscroll`} data-sidebar-customize-scroll="">{list}</div>
        <div className={own.doneBar}>
          <button type="button" className={styles.customizeDone} data-sidebar-customize-done onClick={onDone}>
            {t("customizeDone")}
          </button>
        </div>
      </> : list}
    </SidebarCustomizingContext.Provider>
  );
}
