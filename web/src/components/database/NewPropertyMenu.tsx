import { useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { createPortal, flushSync } from "react-dom";
import { useTranslation } from "react-i18next";
import { placeAtPoint, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { PropertyType } from "@/lib/types";
import { useDataSourceHasUniqueId, type UniqueIdSource } from "@/lib/databaseUniqueIdState";
import { PropertyIcon } from "./PropertyIcon";
import { propertyNameButtonClass, PropertyTypeIcon } from "./PropertyTypeIcon";
import { ROW_PAGE_MENU_EDGE } from "./RowPagePropertyMenu";
import { propertyTypeFound, propertyTypeLabel } from "./propertyTypes";
import styles from "./PageLayoutAddMenu.module.css";
import { inlineStartX } from "@/lib/readingDirection";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import type { IconPickerAnchor } from "../page/AnchoredEmojiPicker";
import { menuIconButtonProps } from "./menuIconButton";
import { PropertyIconPicker, propertyIconPickerAnchor } from "./PropertyIconPicker";
import { firstNewPropertyOpening, markNewPropertyOpened } from "./newPropertyOpening";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { useTypeListKeys } from "./typeListKeys";
import { PropertyTypeInfo, usePropertyTypeTip } from "./PropertyTypeInfo";
import { PropertyTypeSearchToggle } from "./PropertyTypeSearchToggle";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * Notion's new property in one column (seen 2026-10-25 in a layout's "속성
 * 추가", 2026-11-06 under a database page's properties at 1280px): a "속성
 * 이름" field beside a 28px tile showing the text type, then "유형" with its
 * 🔍 over the types. Nothing is marked until the keys or the pointer mark
 * one: the arrows move the mark from the name or the tile, which keep the
 * keys, and from the search, which hands them to the list (typeListKeys,
 * Notion's seen 2026-11-21); Enter makes the one marked, or the type a name
 * typed is the name of; the pointer marks the type it moves onto, after the
 * arrows only once it moves into another type (Notion's, seen 2026-11-14,
 * `pointerHover`). A type picked makes a property named as typed, else for
 * its type, with the icon its tile picked. Escape or a press outside makes
 * nothing.
 *
 * It is one window wherever it opens — under a database page's properties
 * and in a layout's title or property group (Notion's, seen 2026-11-17 at
 * 1416×838, the group's menu left open under it): from its button's corner,
 * running to the window's foot, its tile a button that picks the icon as the
 * table's new column's does, named "아이콘 바꾸기" as Notion's
 * (collectionSettingsPropertyType.iconButton.ariaLabel, "Change icon"), with
 * no hover tip. Where the keys go is one rule, by when it opens
 * (newPropertyOpening): the first new property since the document loaded
 * leaves the name unfocused, so the tile holds the focus and a key typed
 * opens the types' search with it, the list narrowing and the window placed
 * again at its new height ("d" in "유형 검색 중…" leaving 날짜 …); every later
 * one puts the keys in the name.
 */
export function NewPropertyMenu({ anchor, label, types, idSource, onCreate, onClose, data }: {
  anchor: RefObject<HTMLElement | null>;
  label: string;
  types: readonly PropertyType[];
  /** The data source, whose ID property dims "ID", as Notion's "+" (seen 2026-11-11). */
  idSource?: UniqueIdSource;
  onCreate(type: PropertyType, name: string, icon?: string): void;
  onClose(): void;
  data?: Record<string, string>;
}) {
  const { t } = useTranslation(["pageLayout", "tableView"]);
  // The first new property since the document loaded keeps the keys for the types' search.
  const [typeKeys] = useState(firstNewPropertyOpening);
  useEffect(() => markNewPropertyOpened(), []);
  const [name, setName] = useState("");
  const [icon, setIcon] = useState<string | null>(null);
  const [iconAnchor, setIconAnchor] = useState<IconPickerAnchor | null>(null);
  const [typeQuery, setTypeQuery] = useState<string | null>(null);
  const menu = useRef<HTMLDivElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const iconRef = useRef<HTMLButtonElement>(null);
  const typeSearchRef = useRef<HTMLInputElement>(null);
  const typesRef = useRef<HTMLDivElement>(null);
  useEffect(() => { if (typeKeys) iconRef.current?.focus({ preventScroll: true }); }, [typeKeys]);
  const place = usePopoverPlacement(menu, (size, viewport) => {
    const button = popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 };
    // From the button's foot, whole up to 80% of the window and slid up to
    // fit over the window's foot as the page's property menus are, scrolling
    // past that (Notion's max-height 80vh, seen 2026-11-06: 698px at 873,
    // its top over the properties above the button; 2026-11-14: a narrowed
    // list 1px under the button, a full one 578 high at 134.5..712.9 in a 723 window;
    // 2026-11-17 in a layout's group at 838: 670.4 high at 157.4..827.8, at the button's start).
    const cap = Math.round(viewport.height * 0.8);
    return { ...placeAtPoint({ x: inlineStartX(button, 0, anchor.current), y: button.bottom + 1 }, size, viewport, { cap, marginY: ROW_PAGE_MENU_EDGE }),
      minHeight: Math.min(cap, Math.ceil(size.height)) };
  }, [], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose,
    inside: () => [anchor.current, document.querySelector<HTMLElement>("[data-new-property-icon-picker]")] });
  const shown = types.filter((type) => !typeQuery || propertyTypeFound(type, typeQuery));
  const idDimmed = useDataSourceHasUniqueId(idSource?.dataSourceId, idSource?.has ?? false, Boolean(idSource));
  // Nothing is marked as it opens; the keys move the mark as Notion's (typeListKeys).
  const keys = useTypeListKeys({ keys: shown, searchOpen: typeQuery !== null, searchWords: Boolean(typeQuery?.trim()),
    listRef: typesRef, menuRef: menu, onChoose: (type) => create(type as PropertyType) });
  // Each type's "ⓘ" on the row under the pointer, its words in a tip beside it (PropertyTypeInfo).
  const typeTip = usePropertyTypeTip();

  function create(type: PropertyType) {
    if (idDimmed && type === "unique_id") return;
    onCreate(type, name, icon ?? undefined);
  }

  // Words typed in the search mark the first type they find.
  function search(value: string) {
    setTypeQuery(value);
    keys.setMarked(types.find((type) => !value || propertyTypeFound(type, value)) ?? null);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>, field: "name" | "search") {
    if (imeComposing(event)) return;
    if (field === "name" ? keys.onNameKeyDown(event) : keys.onSearchKeyDown(event)) return;
    if (fieldEnter(event)) {
      event.preventDefault();
      const typed = name.trim();
      const type = (typed ? types.find((candidate) => propertyTypeLabel(candidate) === typed) : undefined)
        ?? (keys.at === null ? undefined : shown[keys.at]);
      if (type) create(type);
    }
  }

  // The keys the list holds once the search hands them over (the menu itself
  // focused); the tile's arrows move the mark, the tile keeping the keys; a
  // key typed with no field focused goes to the types' search, opened for it
  // before the key lands, so a composition starts there too.
  function onMenuKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.defaultPrevented || event.target instanceof HTMLInputElement) return;
    if (keys.onListKeyDown(event) || keys.onNameKeyDown(event)) return;
    if (!typeKeys || event.metaKey || event.ctrlKey || event.altKey) return;
    if (event.key.length !== 1 && !imeComposing(event)) return;
    if (event.key === " " && event.target instanceof HTMLButtonElement) return;
    flushSync(() => { setTypeQuery((current) => current ?? ""); keys.setMarked(types[0] ?? null); });
    typeSearchRef.current?.focus({ preventScroll: true });
  }

  function closeIconPicker() {
    setIconAnchor(null);
    focusOpenedField(() => nameRef.current);
  }

  return createPortal(
    <>
      <div ref={menu} className={`${styles.menu} ${styles.newProperty}`} role="dialog" aria-label={label} style={popoverPlacementStyle(place)}
        tabIndex={-1} onKeyDown={onMenuKeyDown} data-new-property-menu {...data}>
        <div className={styles.nameRow}>
          {/* The new property's mark, the text type's or the icon picked, drawn
              as by any property's name field (propertyNameButtonClass). */}
          <button ref={iconRef} type="button" className={`${styles.nameType} ${propertyNameButtonClass}`}
            {...menuIconButtonProps(t("tableView:changePropertyIcon"), iconAnchor !== null)} data-new-property-icon-button
            onClick={(event) => {
              setIconAnchor(propertyIconPickerAnchor(event.currentTarget));
            }}>
            <PropertyIcon property={{ type: "rich_text", config: icon ? { icon } : undefined }} size="regular" />
          </button>
          <input ref={nameRef} autoFocus={!typeKeys} className={styles.nameField} value={name} placeholder={t("propertyName")} aria-label={t("propertyName")}
            onChange={(event) => setName(event.target.value)} onKeyDown={(event) => onKeyDown(event, "name")} data-layout-new-property-name />
        </div>
        <div className={styles.typeHead}>
          <span>{t("propertyType")}</span>
          <PropertyTypeSearchToggle label={t("searchTypes")} open={typeQuery !== null}
            onClick={() => setTypeQuery((current) => current === null ? "" : null)} data={{ "data-layout-type-search-button": "" }} />
        </div>
        {typeQuery === null ? null : (
          <input ref={typeSearchRef} autoFocus={fieldFocusOnOpen()} className={styles.typeSearch} value={typeQuery} placeholder={t("searchingTypes")} aria-label={t("searchTypes")}
            onChange={(event) => search(event.target.value)} onKeyDown={(event) => onKeyDown(event, "search")} data-layout-type-search />
        )}
        <div ref={typesRef} className={styles.typeRows} onPointerMove={keys.onPointerMove}>
          {shown.map((type) => (
            <button key={type} type="button" className={styles.typeRow} data-active={type === keys.marked || undefined}
              aria-disabled={(idDimmed && type === "unique_id") || undefined}
              onClick={() => create(type)} data-key={type} data-layout-add-type={type}>
              <PropertyTypeIcon type={type} size="regular" /><span>{propertyTypeLabel(type)}</span>
              <PropertyTypeInfo type={type} dimmed={idDimmed && type === "unique_id"} tip={typeTip} />
            </button>
          ))}
        </div>
      </div>
      {typeTip.tip}
      {iconAnchor ? (
        <PropertyIconPicker anchor={iconAnchor} marker="new-property-icon-picker" onClose={closeIconPicker}
          picker={{
            label: t("tableView:changePropertyIcon"),
            onPickBuiltInIcon: (picked) => { setIcon(picked); closeIconPicker(); },
            onRemove: () => { setIcon(null); closeIconPicker(); },
          }} />
      ) : null}
    </>,
    document.body,
  );
}
