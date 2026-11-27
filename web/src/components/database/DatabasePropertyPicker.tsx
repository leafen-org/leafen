import { useRef, useState, type ReactNode } from "react";
import type { DbProperty } from "@/lib/types";
import { propertyPickerOrder } from "./databaseToolbarModel";
import { propertyTypeLabel } from "./propertyTypes";
import { databaseViewLabels } from "./databaseViewLabels";
import { PropertyIcon } from "./PropertyIcon";
import styles from "./DatabasePropertyPicker.module.css";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { CheckIcon, ChevronRight, FilterIcon } from "@/icons/leafen";
import { PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { useEdgeFadeAsSized } from "@/lib/edgeFade";
import { SearchClearButton } from "@/components/PageActionSearch";

type PickerProperty = Pick<DbProperty, "id" | "name" | "type" | "config">;

/**
 * Whether what is typed finds a row: its letters, in order, case aside, in
 * the row's name followed by its type's name ("점수 숫자"), as Notion's
 * property picker searches (its code, read 2026-11-17: the fuzzy package's
 * test over the name and the type's name; seen in Chrome the same day in
 * Korean: "숫자" finds a number property, "prt" finds "Priority", "tpr" and
 * "number" nothing). A row stays in its place among the others.
 */
function pickerFound(text: string, query: string) {
  const line = text.toLocaleLowerCase();
  let at = 0;
  for (let index = 0; index < line.length && at < query.length; index++) if (line[index] === query[at]) at++;
  return at === query.length;
}

/** A sheet row's state, as Notion's filter list marks a filter it has (seen 2026-10-24). */
export type SheetRowMark = { applied?: boolean; unsaved?: boolean; value?: ReactNode };

/**
 * Notion's first screen for a filter, sort or grouping (seen 2026-09-30): a
 * focused search ("필터 기준", "정렬 기준"), then the properties as 28px rows
 * of icon and name, and below them an action such as "고급 필터 추가". The
 * arrow keys move through the rows from the search and Enter picks one.
 */
export function DatabasePropertyPicker({ placeholder, properties, keepOrder, leading, footer, sheet, sheetOpensMore = true, leadingApart = true, emptyApart, checked, mark, onPick }: {
  placeholder: string;
  properties: readonly PickerProperty[];
  /**
   * The properties in the order given: a phone's filter list puts those it
   * filters first and then all by name, the name property among them, as
   * Notion's phone (seen 2026-11-11; its sorting and grouping lists keep the
   * name property first, as every desktop list).
   */
  keepOrder?: boolean;
  /** A row before the properties, such as grouping's "없음". */
  leading?: { label: string; icon?: ReactNode; onPick(): void };
  /** An action below the properties, such as "고급 필터 추가", or "고급 필터" with its rules' count grey at its end ("규칙 2개"). */
  footer?: { icon: ReactNode; label: string; sheetLabel?: string; hint?: string; onClick(): void };
  /**
   * In a phone's sheet, as Notion's "필터 및 정렬" (seen 2026-10-21): the
   * leading row and the footer in groups of their own, the search and the
   * properties in one between, each row opening more ("›").
   */
  sheet?: boolean;
  /** In a sheet, a property opens more ("›"), as a filter's does; a sort's is picked at once. */
  sheetOpensMore?: boolean;
  /**
   * In a sheet, the leading row in a group of its own, as a filter's; a
   * grouping's "없음" heads the properties under the search, its choice or
   * the property chosen checked (Notion's "그룹화 기준", seen 2026-10-23).
   */
  leadingApart?: boolean;
  /** In a sheet, its "결과 없음" a group's gap under the search rather than just under it, as a dashboard's "속성 필터링", whose search stands apart from its sources' groups (Notion's, seen 2026-11-17). */
  emptyApart?: boolean;
  /** The chosen row in a sheet: a property's ID, or "" for the leading row. */
  checked?: string;
  /** In a sheet, a row's mark: blue for a filter it has, an orange dot while the viewer's own, its value grey at its end. */
  mark?(property: PickerProperty): SheetRowMark | undefined;
  onPick(property: PickerProperty): void;
}) {
  const labels = databaseViewLabels();
  const inputRef = useRef<HTMLInputElement>(null), listRef = useRef<HTMLDivElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  // In a window that lends it its inside, its search and rows scroll in a box of
  // their own, fading 1.7px a pixel scrolled up to 34px (Notion's mask, useEdgeFade;
  // DatabasePropertyPicker.module.css `.scroll`).
  const scrollRef = useRef<HTMLDivElement>(null);
  useEdgeFadeAsSized(scrollRef, 1.7);
  const [search, setSearch] = useState("");
  const query = search.trim().toLocaleLowerCase();
  // The name property first, then the others by name, as every Notion list
  // of a database's properties (seen 2026-11-03: grouping, sorting,
  // filtering, conditional color) — not the table's column order.
  const found = properties.filter((property) => !query || pickerFound(`${property.name} ${propertyTypeLabel(property.type)}`, query));
  const matches = keepOrder ? found : propertyPickerOrder(found);
  const rows: Array<{ id: string; label: string; property?: PickerProperty; pick(): void }> = [
    ...(leading && (!query || pickerFound(leading.label, query)) && !sheet ? [{ id: "__leading", label: leading.label, pick: leading.onPick }] : []),
    ...matches.map((property) => ({ id: property.id, label: property.name, property, pick: () => onPick(property) })),
  ];
  const picker = useNativeAutocomplete({ items: rows, query: search, onQuery: setSearch, inputRef, listRef, popoverRef,
    keyOf: row => row.id, textOf: row => row.label, onPick: row => row.pick(),
    label: placeholder, commitTab: false });
  if (sheet) {
    const check = <CheckIcon size="small" className={styles.sheetCheck} aria-hidden="true" />;
    const leadingRow = leading ? <PhoneSheetRow icon={leading.icon} label={leading.label} more={leadingApart}
      value={checked === "" ? check : undefined} onPress={leading.onPick} /> : null;
    const leadingShown = Boolean(leadingRow) && !leadingApart && (!query || Boolean(leading && pickerFound(leading.label, query)));
    return (
      <div ref={popoverRef} className={`${sheetStyles.stack} ${sheetStyles.propertyRows}`} data-property-picker data-property-picker-sheet>
        {leadingApart && leadingRow ? <PhoneSheetGroup>{leadingRow}</PhoneSheetGroup> : null}
        <PhoneSheetGroup>
          {/* A filter's search row carries the filter's grey lines; a sort's starts at its edge (Notion's, seen 2026-10-21). */}
          <label className={`${sheetStyles.row} ${styles.sheetSearchRow} ${sheetOpensMore ? "" : styles.sheetSearchBare}`}>
            {sheetOpensMore ? <span className={`${sheetStyles.rowIcon} ${styles.sheetSearchIcon}`} aria-hidden="true" data-icon-own=""><FilterIcon size="wide" /></span> : null}
            <input {...picker.inputProps} ref={inputRef} className={styles.sheetSearch} placeholder={placeholder} />
            {/* "입력 값 지우기" once it holds words, as Notion's sheet search (seen 2026-11-17 in Android Chrome at 411). */}
            <SearchClearButton value={search} inputRef={inputRef} onClear={() => setSearch("")} />
          </label>
          {leadingShown ? leadingRow : null}
          <div {...picker.listBoxProps} ref={listRef}>
            {rows.map(row => {
              const state = row.property ? mark?.(row.property) : undefined;
              return (
              <NativePickerOption key={row.id} itemKey={row.id} state={picker.state} className={sheetStyles.row}
                data-picker-property={row.property?.id} data-applied={state?.applied ? "true" : undefined}>{({ labelProps }) => <>
                {row.property && <span className={`${sheetStyles.rowIcon} ${styles.sheetMarkIcon}`} aria-hidden="true" data-icon-own={sheetOpensMore ? "gray" : ""}>
                  <PropertyIcon property={row.property} size="wide" />
                  {state?.unsaved ? <span className={styles.sheetUnsaved} data-unsaved /> : null}
                </span>}
                <span {...labelProps} className={sheetStyles.rowLabel}>{row.label}</span>
                {state?.value ? <span className={sheetStyles.rowValue}>{state.value}</span> : null}
                {checked !== undefined && checked === row.property?.id ? check : null}
                {sheetOpensMore ? <ChevronRight size="page" className={sheetStyles.rowMore} aria-hidden="true" /> : null}
              </>}</NativePickerOption>
              );
            })}
          </div>
        </PhoneSheetGroup>
        {/* Finding none, the search's group holds the search alone and "결과 없음" stands under it in small grey words,
            as Notion's (seen 2026-11-17 in Android Chrome at 411: a filter's search, a dashboard's "속성 필터링"). */}
        {!rows.length && !leadingShown ? <div className={styles.sheetEmpty} data-picker-empty data-apart={emptyApart ? "true" : undefined}>{labels.noSearchResults}</div> : null}
        {footer ? <PhoneSheetGroup><PhoneSheetRow icon={footer.icon} label={footer.sheetLabel ?? footer.label} onPress={footer.onClick} /></PhoneSheetGroup> : null}
      </div>
    );
  }
  return (
    <div ref={popoverRef} className={styles.picker} data-property-picker>
      <div ref={scrollRef} className={styles.scroll} data-edge-fade="">
        {/* A touch screen's search waits for a tap, so no keyboard rises over the list, as Notion's iPad (seen 2026-11-12). */}
        <input {...picker.inputProps} ref={inputRef} className={styles.search} placeholder={placeholder} autoFocus={fieldFocusOnOpen()} />
        <div {...picker.listBoxProps} ref={listRef} className={styles.list}>
          {rows.map(row => (
            <NativePickerOption key={row.id} itemKey={row.id} state={picker.state} className={styles.option}
              data-picker-property={row.property?.id}>{({ labelProps }) => <>
              {row.property && <span className={styles.icon} aria-hidden="true"><PropertyIcon property={row.property} size="regular" /></span>}
              <span {...labelProps} className={styles.name}>{row.label}</span>
              {/* The chosen one ticked, as Notion's "그룹화 기준" (seen 2026-10-24): "없음" while nothing is. */}
              {checked !== undefined && (row.property ? checked === row.property.id : checked === "")
                ? <CheckIcon size="small" className={styles.check} aria-hidden="true" /> : null}
            </>}</NativePickerOption>
          ))}
          {!rows.length && <div className={styles.empty}>{labels.noSearchResults}</div>}
        </div>
      </div>
      {footer && (
        <button type="button" className={styles.footer} onClick={footer.onClick}>
          <span className={styles.footerIcon} aria-hidden="true">{footer.icon}</span>
          <span>{footer.label}</span>
          {footer.hint ? <span className={styles.footerHint}>{footer.hint}</span> : null}
        </button>
      )}
    </div>
  );
}
