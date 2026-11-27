import { useLayoutEffect, useRef, useState, type KeyboardEvent, type MutableRefObject, type PointerEvent } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { CheckIcon } from "@/icons/leafen";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { useOpenedFromKeyboard } from "@/lib/nativeMenu";
import { pointerMoved } from "@/lib/pointerHover";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import type { PropertyType } from "@/lib/types";
import { editablePropertyTypes, propertyTypeFound } from "./propertyTypes";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { PropertyTypeInfo, usePropertyTypeTip } from "./PropertyTypeInfo";
import { PropertyTypeSearchToggle } from "./PropertyTypeSearchToggle";
import { useTypeListKeys } from "./typeListKeys";
import styles from "./databaseSheet";
import own from "./PropertyTypeHeadedList.module.css";

/** The arrows of a name over the list, which keep the keys, and the type they mark. */
export type TypeListNameKeys = { onNameKeyDown(event: KeyboardEvent<HTMLInputElement>): boolean; marked(): PropertyType | null };

/**
 * A property's types under "유형" and its 🔍 — the view settings' "새 속성"
 * and a property's own "유형" screen — as Notion's (seen 2026-11-06,
 * 2026-11-11): the current type marked at its line's end, each row's "?"
 * telling what the type holds as the pointer moves onto it, none marked as
 * it opens from the pointer (seen 2026-11-14), the first from the keys. Its
 * keys are the one-column type lists' (typeListKeys, seen 2026-11-21): the
 * list holds them, its search handing them back to it, and a name over it
 * (`nameKeysRef`) moves the mark keeping them.
 */
export function PropertyTypeHeadedList({ property, choices, label, onChoose, heading, className, focusOnOpen = true, litOnOpen = true,
  attributes, nameKeysRef }: {
  property?: GraphProperty;
  choices?: readonly { type: PropertyType; label: string; disabled?: boolean }[];
  label: string; onChoose(type: PropertyType): void; heading: string;
  className?: string; focusOnOpen?: boolean; litOnOpen?: boolean; attributes?: Record<string, string>;
  nameKeysRef?: MutableRefObject<TypeListNameKeys | null>;
}) {
  const { t } = useTranslation(["rowProperties", "pageLayout"]);
  const listRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState<string | null>(null);
  const [pointerRow, setPointerRow] = useState<string | null>(null);
  const typeTip = usePropertyTypeTip();
  const all = choices ?? editablePropertyTypes((property?.type ?? "rich_text") as PropertyType);
  const rows = all.filter(({ type }) => !search || propertyTypeFound(type, search));
  const keyboardNow = useOpenedFromKeyboard();
  const [first] = useState(() => litOnOpen || keyboardNow ? rows.find((row) => !row.disabled)?.type ?? null : null);
  const choose = (type: string) => { if (!rows.find((row) => row.type === type)?.disabled) onChoose(type as PropertyType); };
  const keys = useTypeListKeys({ keys: rows.map((row) => row.type), first, searchOpen: search !== null,
    searchWords: Boolean(search?.trim()), listRef, menuRef: listRef, onChoose: choose });
  const markedType = () => keys.at === null ? null : rows[keys.at].type;
  if (nameKeysRef) nameKeysRef.current = { onNameKeyDown: keys.onNameKeyDown, marked: markedType };
  useLayoutEffect(() => { if (focusOnOpen) listRef.current?.focus({ preventScroll: true }); }, [focusOnOpen]);

  function openSearch() {
    if (search === null) setSearch("");
    else searchRef.current?.focus();
  }
  // Words typed mark the first type they find; Enter chooses the mark with
  // words in the search, nothing while it is empty.
  function typed(value: string) {
    setSearch(value);
    keys.setMarked(all.find(({ type }) => !value || propertyTypeFound(type, value))?.type ?? null);
  }
  function onSearchKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (imeComposing(event) || keys.onSearchKeyDown(event) || !fieldEnter(event)) return;
    event.preventDefault();
    const type = markedType();
    if (type && search?.trim()) choose(type);
  }
  // The row the pointer moved onto shows its "?" (not one a list came up under).
  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    keys.onPointerMove(event);
    if (!pointerMoved(event)) return;
    const row = event.target instanceof Element ? event.target.closest<HTMLElement>("[data-key]") : null;
    const key = row?.dataset.key ?? null;
    if (key !== pointerRow) setPointerRow(key);
  }

  return (
    <div ref={listRef} className={`${className ?? ""} ${own.list}`} role="menu" aria-label={label} tabIndex={-1} data-type-heading=""
      onKeyDown={keys.onListKeyDown} onPointerMove={onPointerMove} onPointerLeave={() => setPointerRow(null)} {...attributes}>
      <div className={styles.propertyTypeHead}>
        <span>{heading}</span>
        <PropertyTypeSearchToggle label={t("rowProperties:searchPropertyTypes")} open={search !== null} onClick={openSearch}
          data={{ "data-property-type-search-button": "" }} />
      </div>
      {search === null ? null : (
        <input ref={searchRef} autoFocus={fieldFocusOnOpen()} className={styles.propertyTypeSearch} value={search} placeholder={t("pageLayout:searchingTypes")}
          aria-label={t("rowProperties:searchPropertyTypes")} data-property-type-search data-alone={rows.length === 0 || undefined}
          onChange={(event) => typed(event.target.value)} onKeyDown={onSearchKeyDown} />
      )}
      {rows.map((row) => (
        <button key={row.type} type="button" role="menuitemradio" aria-checked={property?.type === row.type} disabled={row.disabled}
          className={styles.propertyHeaderTypeOption} data-key={row.type} data-property-type={row.type}
          data-active={keys.marked === row.type ? "true" : undefined} data-pointer-row={pointerRow === row.type ? "true" : undefined}
          onClick={() => choose(row.type)}>
          <PropertyTypeIcon type={row.type} size="regular" /> <span>{row.label}</span>
          <PropertyTypeInfo type={row.type} dimmed={row.disabled} tip={typeTip} />
          {property?.type === row.type ? <CheckIcon size="small" aria-hidden="true" className={styles.propertyHeaderTypeCheck} /> : null}
        </button>
      ))}
      {typeTip.tip}
    </div>
  );
}
