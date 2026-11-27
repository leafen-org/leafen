"use client";

import { useRef, useState, type CSSProperties, type KeyboardEvent, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { CheckIcon, ChevronDown, ChevronRight, Plus } from "@/icons/leafen";
import { NativeMenuItem, useNativeMenu, useNativeSubmenu } from "@/lib/nativeMenu";
import { placeAround, placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { CARD_PREVIEW_NONE, CARD_PREVIEW_PAGE, CARD_PREVIEW_PAGE_CONTENT } from "./cardPreviewModePolicy";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { propertyTypeLabel } from "./propertyTypes";
import layoutStyles from "./databaseSheet";
import styles from "./NotionSelect.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type Item = typeof CARD_PREVIEW_NONE | typeof CARD_PREVIEW_PAGE | "properties" | typeof CARD_PREVIEW_PAGE_CONTENT;
const ITEMS: Item[] = [CARD_PREVIEW_NONE, CARD_PREVIEW_PAGE, "properties", CARD_PREVIEW_PAGE_CONTENT];
const NEW = "\u0000new";
const fixed: CSSProperties = { position: "fixed", right: "auto", bottom: "auto", width: 250 };

/**
 * A board's or gallery's "카드 미리보기", as Notion's (seen 2026-11-11): none,
 * the page's cover, "페이지 속성 ›" — its files properties by name, then one
 * made there by name, which the cards then preview — and the page's content,
 * "페이지에서 First Block을 사용하세요" under it. The row names what is chosen.
 */
export function CardPreviewChoice({ label, value, properties, disabled, onChange, onCreateFilesProperty }: {
  label: string; value: string; properties: readonly GraphProperty[]; disabled?: boolean;
  onChange(value: string): void;
  onCreateFilesProperty?(name: string): Promise<GraphProperty | null>;
}) {
  const { t } = useTranslation("databaseView");
  const row = useRef<HTMLDivElement>(null), button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const chosen = properties.find((property) => property.id === value);
  const named = (property: GraphProperty) => property.name || t("untitled");
  const shown = value === CARD_PREVIEW_NONE ? t("noCardPreview") : value === CARD_PREVIEW_PAGE ? t("pageCover")
    : value === CARD_PREVIEW_PAGE_CONTENT ? t("pageContent") : chosen ? named(chosen) : t("noCardPreview");
  // Its files properties, and one chosen before of another kind, by name as Notion lists them.
  const listed = properties.filter((property) => property.type === "files" || property.id === value)
    .sort((left, right) => named(left).localeCompare(named(right)));
  return <div ref={row} className={`${layoutStyles.layoutRow} ${layoutStyles.layoutChoiceRow}`} data-layout-option="coverProperty"
    onClick={(event) => {
      // Anywhere on the row opens the list, as Notion's; the list's own presses come through the portal and are not the row's.
      const target = event.target as Node;
      if (!disabled && row.current?.contains(target) && !button.current?.contains(target)) setOpen(true);
    }}>
    <span>{label}</span>
    <span className={`${styles.notionSelect} ${layoutStyles.layoutChoice}`} data-notion-select="">
      <button ref={button} type="button" className={`${styles.notionSelectButton} ${layoutStyles.layoutChoiceButton}`} data-notion-select-button="" aria-label={label}
        aria-haspopup="menu" aria-expanded={open} disabled={disabled} onClick={() => setOpen(!open)}>
        <span className={styles.notionSelectLabel}>{shown}</span><ChevronDown size="tiny" aria-hidden="true" />
      </button>
    </span>
    {open ? createPortal(<PreviewMenu row={row} button={button} label={label} value={value} listed={listed} named={named}
      onClose={() => setOpen(false)} onChange={(next) => { setOpen(false); if (next !== value) onChange(next); }}
      onCreate={onCreateFilesProperty} />, document.body) : null}
  </div>;
}

function PreviewMenu({ row, button, label, value, listed, named, onClose, onChange, onCreate }: {
  row: RefObject<HTMLDivElement | null>; button: RefObject<HTMLButtonElement | null>; label: string; value: string;
  listed: readonly GraphProperty[]; named(property: GraphProperty): string;
  onClose(): void; onChange(value: string): void; onCreate?(name: string): Promise<GraphProperty | null>;
}) {
  const { t } = useTranslation("databaseView");
  const menuRef = useRef<HTMLDivElement>(null), propertiesRef = useRef<HTMLButtonElement>(null), subRef = useRef<HTMLDivElement>(null);
  const close = () => { onClose(); button.current?.focus({ preventScroll: true }); };
  const text = (item: Item) => item === CARD_PREVIEW_NONE ? t("noCardPreview") : item === CARD_PREVIEW_PAGE ? t("pageCover")
    : item === "properties" ? t("cardPreviewProperties") : t("pageContent");
  const menu = useNativeMenu({ items: ITEMS, open: true, menuRef, label, onClose: close, keyOf: (item) => item, textOf: text,
    highlightOnOpen: true, firstKey: CARD_PREVIEW_NONE, onAction: (item) => { if (item !== "properties") onChange(item); } });
  const sub = useNativeSubmenu({ triggerKey: "properties", rootTrigger: menu.rootTrigger, parentMenuRef: menuRef, submenuRef: subRef, triggerRef: propertiesRef });
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(popoverRectOf(row.current)!, size, viewport,
    { gap: 0, align: "end", keepSide: true, minHeight: Math.ceil(size.height) }), [], undefined,
  { kind: "menu", restoreFocus: false, onEscape: close, onOutsidePress: onClose, inside: () => [row.current] });
  // Beside the list, its middle by the row's, over the list's edge as Notion's.
  const subPlace = usePopoverPlacement(subRef, sub.state.isOpen ? (size, viewport) => {
    const item = popoverRectOf(propertiesRef.current)!, list = popoverRectOf(menuRef.current)!;
    return placeBeside({ ...list, top: item.top, bottom: item.bottom }, size, viewport, { gap: -4, offsetTop: (item.bottom - item.top - size.height) / 2 });
  } : null, [sub.state.isOpen, place?.left, place?.top], undefined,
  { kind: "menu", restoreFocus: false, inside: () => [menuRef.current], onEscape: sub.state.close, onOutsidePress: onClose });
  const isProperty = ![CARD_PREVIEW_NONE, CARD_PREVIEW_PAGE, CARD_PREVIEW_PAGE_CONTENT].includes(value);
  return <>
    <div {...menu.menuProps} ref={menuRef} className={`${styles.notionSelectMenu} ${styles.settingsRowMenu}`} data-notion-select-menu
      style={{ ...popoverPlacementStyle(place), ...fixed }}>
      <div className={styles.notionSelectOptions}>
        {ITEMS.map((item) => <NativeMenuItem key={item} state={menu.state} itemKey={item} className={styles.notionSelectOption}
          buttonRef={item === "properties" ? propertiesRef : undefined} itemOptions={item === "properties" ? sub.submenuTriggerProps : undefined}
          role={item === "properties" ? undefined : "menuitemradio"} aria-checked={item === "properties" ? undefined : item === value}
          data-option-value={item} data-described={item === CARD_PREVIEW_PAGE_CONTENT ? "true" : undefined}>
          {({ labelProps, descriptionProps }) => <>
            <span className={styles.notionSelectLabel} data-described={item === CARD_PREVIEW_PAGE_CONTENT ? "true" : undefined}>
              <span {...labelProps}>{text(item)}</span>
              {item === CARD_PREVIEW_PAGE_CONTENT ? <small {...descriptionProps} className={styles.notionSelectDescription}>{t("cardPreviewContentNote")}</small> : null}
            </span>
            {item === "properties" ? <ChevronRight size="small" aria-hidden="true" />
              : item === value ? <CheckIcon size="tiny" aria-hidden="true" /> : <span aria-hidden="true" />}
          </>}
        </NativeMenuItem>)}
      </div>
    </div>
    {sub.state.isOpen ? <PropertiesMenu menuRef={subRef} menuOptions={sub.submenuProps} focusOnOpen={sub.state.focusStrategy !== null}
      style={{ ...popoverPlacementStyle(subPlace), ...fixed }} value={isProperty ? value : ""} listed={listed} named={named}
      onPick={onChange} onCreate={onCreate} /> : null}
  </>;
}

function PropertiesMenu({ menuRef, menuOptions, focusOnOpen, style, value, listed, named, onPick, onCreate }: {
  menuRef: RefObject<HTMLDivElement | null>; menuOptions: NonNullable<Parameters<typeof useNativeMenu>[0]["menuOptions"]>;
  focusOnOpen: boolean; style: CSSProperties; value: string; listed: readonly GraphProperty[]; named(property: GraphProperty): string;
  onPick(value: string): void; onCreate?(name: string): Promise<GraphProperty | null>;
}) {
  const { t } = useTranslation("databaseView");
  const nameRef = useRef<HTMLInputElement>(null);
  const [naming, setNaming] = useState(false), [name, setName] = useState(""), [making, setMaking] = useState(false);
  const items = [...listed.map(({ id }) => id), ...(onCreate && !naming ? [NEW] : [])];
  const picker = useNativeMenu({ items, open: true, menuRef, label: t("cardPreviewProperties"), menuOptions, focusOnOpen,
    keyOf: (item) => item, textOf: (item) => item === NEW ? t("newFilesProperty") : named(listed.find(({ id }) => id === item)!),
    onAction: (item) => { if (item === NEW) setNaming(true); else onPick(item); } });
  // A name typed and Enter make the property, which the cards preview from then on.
  async function make(event: KeyboardEvent<HTMLInputElement>) {
    event.stopPropagation();
    if (event.key === "Escape") { event.preventDefault(); setNaming(false); setName(""); return; }
    if (!fieldEnter(event) || making || !onCreate) return;
    event.preventDefault();
    setMaking(true);
    const made = await onCreate(name.trim() || propertyTypeLabel("files"));
    setMaking(false);
    if (made) onPick(made.id);
  }
  return <div {...picker.menuProps} ref={menuRef} className={`${styles.notionSelectMenu} ${styles.settingsRowMenu} ${styles.propertyChoiceMenu}`}
    data-notion-select-menu data-card-preview-properties style={style}>
    <div className={styles.notionSelectOptions}>
      {listed.map((property) => <NativeMenuItem key={property.id} state={picker.state} itemKey={property.id} className={styles.notionSelectOption}
        role="menuitemradio" aria-checked={property.id === value} data-option-value={property.id}>
        {({ labelProps }) => <>
          <span className={styles.notionSelectIcon} aria-hidden="true"><PropertyTypeIcon type={property.type} size="regular" /></span>
          <span className={styles.notionSelectLabel}><span {...labelProps}>{named(property)}</span></span>
          {property.id === value ? <CheckIcon size="tiny" aria-hidden="true" /> : <span aria-hidden="true" />}
        </>}
      </NativeMenuItem>)}
      {listed.length > 0 && onCreate ? <div role="presentation" className={styles.notionSelectDivider} /> : null}
      {onCreate && !naming ? <NativeMenuItem state={picker.state} itemKey={NEW} className={styles.notionSelectOption} data-option-value="new">
        {({ labelProps }) => <>
          <span className={styles.notionSelectIcon} aria-hidden="true"><Plus size="small" /></span>
          <span className={styles.notionSelectLabel}><span {...labelProps}>{t("newFilesProperty")}</span></span><span aria-hidden="true" />
        </>}
      </NativeMenuItem> : null}
      {naming ? <>
        <div role="presentation" className={styles.notionSelectOption} data-card-preview-new-heading>
          <span className={styles.notionSelectIcon} aria-hidden="true"><Plus size="small" /></span>
          <span className={styles.notionSelectLabel}>{t("pageLayout:newProperty")}</span><span aria-hidden="true" />
        </div>
        <div className={styles.notionSelectSearch}><input ref={nameRef} autoFocus aria-label={t("tableView:propertyName")}
          placeholder={t("tableView:propertyName")} value={name} disabled={making} onChange={(event) => setName(event.target.value)} onKeyDown={make} /></div>
      </> : null}
    </div>
  </div>;
}
