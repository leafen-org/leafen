import { useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { useTranslation } from "react-i18next";
import type { PropertyType } from "@/lib/types";
import { useDataSourceHasUniqueId, type UniqueIdSource } from "@/lib/databaseUniqueIdState";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { propertyTypeFound, propertyTypeLabel } from "./propertyTypes";
import {
  TABLE_ADD_PROPERTY_GROUPS,
  tableAddPropertyRows,
  tableAddPropertyStep,
  type TableAddPropertySpot,
} from "./tableAddPropertyModel";
import { keysMovedHighlight, pointerTakenRow } from "@/lib/pointerHover";
import { scrollKeyWalkedRow } from "@/lib/menuKeyScroll";
import styles from "./TableAddProperty.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { PropertyTypeInfo, usePropertyTypeTip } from "./PropertyTypeInfo";
import { PropertyTypeSearchToggle } from "./PropertyTypeSearchToggle";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// The types a new table property is chosen from, as Notion's header "+" and
// its column menu's "insert left" and "insert right" offer them (seen
// 2026-09-30): two columns by group, a search, and the arrow keys moving the
// one Enter chooses.

const NONE: readonly PropertyType[] = [];
export const TABLE_PROPERTY_TYPE_MENU_MAX_HEIGHT = 388;

export type TablePropertyTypeChoice = ReturnType<typeof useTablePropertyTypeChoice>;

const ID_DIMMED: readonly PropertyType[] = ["unique_id"];

/** The types to choose from, with "ID" dimmed where the data source has one already. */
export function useTablePropertyTypeChoice(omit: readonly PropertyType[] = NONE, idSource?: UniqueIdSource, open = true) {
  const dimmed = useDataSourceHasUniqueId(idSource?.dataSourceId, idSource?.has ?? false, open && Boolean(idSource)) ? ID_DIMMED : NONE;
  const [search, setSearch] = useState<string | null>(null);
  // The type marked, or none: the open search's own stop among the keys.
  const [at, setAt] = useState<TableAddPropertySpot | null>({ row: 0, column: 0 });
  // Words in the search keep the types they find in their groups, each group
  // after the first still under its line, as Notion's ("자" finds 숫자 over a
  // line and 생성자 · 최종 편집자 under it, seen 2026-11-21) — so the keys walk
  // the rows the list shows. They find by the names shown only (Notion's
  // "u" finds URL there, no type by its English name).
  const { groups, firsts } = useMemo(() => {
    const words = (search ?? "").trim();
    const shown = TABLE_ADD_PROPERTY_GROUPS.map((group) => group.filter((type) => !omit.includes(type)
      && (!words || propertyTypeFound(type, words, { shownOnly: true }))));
    const kept = shown.flatMap((group, index) => group.length ? [{ group, first: index === 0 }] : []);
    return { groups: kept.map(({ group }) => group), firsts: kept.map(({ first }) => first) };
  }, [omit, search]);
  const rows = useMemo(() => tableAddPropertyRows(groups), [groups]);
  const flatRows = useMemo(() => rows.flat(), [rows]);
  const chosen = at ? flatRows[at.row]?.[at.column] ?? null : null;
  const listRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  function reset() {
    setSearch(null);
    setAt({ row: 0, column: 0 });
  }

  // The keys move the mark (`tableAddPropertyStep`), which the pointer then
  // takes only as it moves into another type (`pointerHover`, Notion's seen
  // 2026-11-14), the list scrolling as Notion's menus do to keep it in sight;
  // `takeKeys` hands the keys to the list, out of the field typed in.
  function mark(next: TableAddPropertySpot | null, takeKeys: boolean) {
    keysMovedHighlight(listRef.current);
    setAt(next);
    if (takeKeys) listRef.current?.focus({ preventScroll: true });
    window.requestAnimationFrame(() => {
      const list = listRef.current;
      scrollKeyWalkedRow(list?.querySelector<HTMLElement>('[data-active="true"]'), list?.parentElement ?? null);
    });
  }

  // In the name or the search, as Notion's (seen 2026-11-21 at 1440 in
  // Chrome, without its AI rows, in four tabs at different times, judged by
  // the screen and a letter typed after each key): Escape chooses nothing.
  // Enter chooses the type a name typed is the name of ("숫자" makes a
  // number, seen 2026-11-02 and 11-03), else the type marked — the search's
  // first find once words are in it, nothing while it is empty. ← and → are
  // the field's own. In the search, empty at its stop, ↓ marks the first type
  // and ↑ the last; with words in it the arrows move on from the find marked;
  // either way the keys leave it for the list, its ring falling to its faint
  // line. In the name ↓ moves the mark on and hands the keys to the list
  // too, while ↑ moves it back and the name keeps them.
  function onFieldKeyDown(event: KeyboardEvent<HTMLInputElement>, finish: (type: PropertyType | null) => void,
    field: "name" | "search", name = "") {
    if (imeComposing(event)) return;
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      finish(null);
    } else if (fieldEnter(event)) {
      event.preventDefault();
      if (field === "search" && !(search ?? "").trim()) return;
      const typed = name.trim();
      const named = typed ? TABLE_ADD_PROPERTY_GROUPS.flat().find((type) => !omit.includes(type)
        && propertyTypeLabel(type) === typed) : undefined;
      const type = named ?? chosen;
      if (type && !dimmed.includes(type)) finish(type);
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      // The search's words have marked a find, which ↓ moves on from (URL to
      // the next find, seen 2026-11-21 in both accounts); empty, it sits at its stop.
      const from = field === "search" && !(search ?? "").trim() ? null : at;
      mark(tableAddPropertyStep(flatRows, from, event.key, search !== null),
        field === "search" || event.key === "ArrowDown");
    }
  }

  // The keys in the list, as Notion's (seen the same day the same way): the
  // arrows move the mark, Enter chooses it, Escape chooses nothing; a letter
  // opens the search with it while the search is closed, and goes nowhere
  // while it is open.
  function onListKeyDown(event: KeyboardEvent<HTMLDivElement>, finish: (type: PropertyType | null) => void) {
    if (event.target !== event.currentTarget || imeComposing(event)) return;
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      finish(null);
    } else if (fieldEnter(event)) {
      event.preventDefault();
      if (chosen && !dimmed.includes(chosen)) finish(chosen);
    } else if (["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight"].includes(event.key)) {
      event.preventDefault();
      mark(tableAddPropertyStep(flatRows, at, event.key, search !== null), false);
    } else if (search === null && event.key.length === 1 && event.key.trim() && !event.metaKey && !event.ctrlKey && !event.altKey) {
      event.preventDefault();
      setSearch(event.key);
      setAt({ row: 0, column: 0 });
      focusOpenedField(() => searchRef.current);
    }
  }

  // The pointer marks the type it moves onto, after the keys only once it
  // moves into another type.
  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    const type = pointerTakenRow(event);
    const row = type === null ? -1 : flatRows.findIndex((types) => types.includes(type as PropertyType));
    if (row < 0) return;
    const column = flatRows[row].indexOf(type as PropertyType);
    setAt((current) => current !== null && current.row === row && current.column === column ? current : { row, column });
  }

  return { search, setSearch, at, setAt, rows, firsts, reset, onFieldKeyDown, onListKeyDown, dimmed, listRef, searchRef, onPointerMove };
}

export function TablePropertyTypeList({ choice, onFinish }: {
  choice: TablePropertyTypeChoice;
  onFinish(type: PropertyType | null): void;
}) {
  const { t } = useTranslation(["tableView", "rowProperties", "pageLayout"]);
  const { searchRef } = choice;
  const typeTip = usePropertyTypeTip();
  // A type the data source can't take more of shows dimmed, its "?" telling
  // why, as Notion's "ID" where the database has one (seen 2026-11-11).

  // The 🔍 hands the keys to its search. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440:
  // "+ 속성 추가" opened on its name field ("z" went there), the 🔍 pressed, "q" went into the search (Leafen the same).
  function openSearch() {
    choice.setSearch("");
    focusOpenedField(() => searchRef.current);
  }

  let rowIndex = -1;
  return (
    <>
      <div className={styles.label}>
        <span>{t("chooseType")}</span>
        <PropertyTypeSearchToggle label={t("rowProperties:searchPropertyTypes")} open={choice.search !== null} onClick={openSearch} />
      </div>
      {choice.search !== null && (
        <input ref={searchRef} className={styles.search} value={choice.search} placeholder={t("pageLayout:searchingTypes")}
          data-alone={choice.rows.length === 0 || undefined}
          aria-label={t("rowProperties:searchPropertyTypes")}
          onChange={(event) => { choice.setSearch(event.target.value); choice.setAt({ row: 0, column: 0 }); }}
          onKeyDown={(event) => choice.onFieldKeyDown(event, onFinish, "search")} />
      )}
      {/* The groups as one list the pointer moves over, which takes the keys
          from the fields as Notion's menu does (Notion's go to the page). */}
      <div ref={choice.listRef} className={styles.groups} tabIndex={-1} onPointerMove={choice.onPointerMove}
        onKeyDown={(event) => choice.onListKeyDown(event, onFinish)}>
      {choice.rows.map((group, groupIndex) => (
        <div key={groupIndex} className={styles.group} role="group" data-later-group={!choice.firsts[groupIndex] || undefined}>
          {group.map((row) => {
            rowIndex += 1;
            const current = rowIndex;
            return row.map((type, column) => (
              <button key={type} type="button" className={styles.type} role="menuitem" aria-disabled={choice.dimmed.includes(type) || undefined}
                data-key={type} data-property-type={type} data-active={(choice.at !== null && current === choice.at.row && column === choice.at.column) || undefined}
                onClick={() => { if (!choice.dimmed.includes(type)) onFinish(type); }}>
                <PropertyTypeIcon type={type} size="regular" />
                <span>{propertyTypeLabel(type)}</span>
                <PropertyTypeInfo type={type} dimmed={choice.dimmed.includes(type)} tip={typeTip} />
              </button>
            ));
          })}
        </div>
      ))}
      </div>
      {typeTip.tip}
    </>
  );
}
