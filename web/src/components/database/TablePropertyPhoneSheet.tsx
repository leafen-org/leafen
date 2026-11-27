import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { CheckIcon } from "@/icons/leafen";
import type { PropertyType } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, PhoneSheetSwitch } from "../shell/PhoneSheet";
import { PhoneChoiceSheet } from "./DatabasePhonePickers";
import { PhonePropertyTypeGroup } from "./PhonePropertySettings";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { editablePropertyTypes } from "./propertyTypes";
import { tableCalculationGroups, type TableCalculation, type TableCalculationGroup } from "./tableCalculationModel";
import styles from "./TablePropertyPhoneSheet.module.css";

/** A chosen row's dark tick. */
export function PhoneSheetTick() {
  return <CheckIcon size="page" aria-hidden="true" className={styles.tick} />;
}

/** A column menu's row as the sheet draws it: what opens a sheet over it, or what it does. */
export type PhonePropertyRow = {
  key: string; label: string; icon: ReactNode; disabled?: boolean; danger?: boolean; meta?: string;
  /** A line before it in the desktop menu: a new group here. */
  dividerBefore?: boolean;
  /** Its switch, for a row that turns a setting on and off. */
  on?: boolean;
  opens?: "edit" | "type" | "sort" | "calculate" | "statusDisplay";
  run?(): void;
};

type Side = NonNullable<PhonePropertyRow["opens"]>;

/** Rows in white groups, a new group where the desktop menu has a line. */
export function phoneRowGroups<Row extends { dividerBefore?: boolean }>(rows: readonly Row[]): Row[][] {
  const groups: Row[][] = [];
  for (const row of rows) {
    if (!groups.length || row.dividerBefore) groups.push([row]);
    else groups[groups.length - 1].push(row);
  }
  return groups;
}

/**
 * A table column's menu on a phone, as Notion's sheet "속성 편집" (seen
 * 2026-11-11 at 402pt in iOS Safari): it rises half the screen high over a
 * shade, the property's icon, name and "i" in a group of their own, the name
 * not focused so no keyboard comes up; then the menu's rows in white groups,
 * "고정" left out as Notion's phone has none. "유형 변경" and "정렬" open a
 * whole sheet over it; "속성 편집" a half sheet with its title alone, as
 * high as a half one even over this one pushed up (Notion's, seen 2026-11-14
 * at 402pt: both at 367 of 714, the menu at 10 under it once pushed up);
 * "계산" a half sheet "표 집계" whose groups open their calculations over
 * it, "필터" its rule's sheet.
 */
export function TablePropertyPhoneSheet({ property, title, opened, head, rows, edit, sort, calculation, status, onType, onClose }: {
  property: GraphProperty;
  title: string;
  /** The sheet open over it at first: a relation just made opens on its settings. */
  opened?: Side;
  head: ReactNode;
  rows: readonly PhonePropertyRow[];
  /** "속성 편집"'s settings. */
  edit: { title: string; content: ReactNode };
  sort: { ascending: string; descending: string; onSort(direction: "asc" | "desc"): void };
  calculation: { title: string; type: string; picked?: TableCalculation; limitLargeCounts?: { on: boolean; set(limit: boolean): void };
    onPick(next: TableCalculation | undefined): void };
  status: { title: string; checkbox: boolean; choices: { checkbox: string; select: string }; onPick(checkbox: boolean): void };
  onType(type: PropertyType): void;
  onClose(): void;
}) {
  const { t } = useTranslation("tableView");
  const [side, setSide] = useState<Side | null>(opened ?? null);
  // "고정" has no row on a phone; its line goes to the row after it.
  const shown: PhonePropertyRow[] = [];
  let carried = false;
  for (const row of rows) {
    if (row.key === "freeze") { carried ||= Boolean(row.dividerBefore); continue; }
    shown.push(carried ? { ...row, dividerBefore: true } : row);
    carried = false;
  }
  return (
    <>
      <PhoneSheet size="half" title={title} onClose={onClose} data={{ "data-table-property-menu": property.id, "data-phone-property-sheet": "" }}>
        <PhoneSheetGroup>{head}</PhoneSheetGroup>
        {phoneRowGroups(shown).map((group) => (
          <PhoneSheetGroup key={group[0].key} className={styles.rows}>
            {group.map((row) => (
              <PhoneSheetRow key={row.key} icon={row.icon} label={row.label} disabled={row.disabled} danger={row.danger}
                // "필터 ›" opens its rule's sheet, as the rows that open one; a dim row ("정렬" of a rollup with no
                // relation) opens nothing and draws no "›", as the column menu's (TablePropertyMenu) and Notion's
                // phone sheet (seen 2026-11-19 on the A90's Chrome at 411).
                more={!row.disabled && (Boolean(row.opens) || row.key === "filter")}
                value={row.on !== undefined ? <PhoneSheetSwitch on={row.on} /> : row.meta}
                onPress={() => { if (row.opens) setSide(row.opens); else row.run?.(); }}
                data={{ "data-menu-item": row.key, "data-phone-property-row": row.key }} />
            ))}
          </PhoneSheetGroup>
        ))}
      </PhoneSheet>
      {side === "edit" ? (
        // Its title alone, no "‹": dragged down it goes back to the menu; its shade pressed beside both closes both on an
        // iPhone, on the menu the menu stays and the press is its, an Android phone's closing it alone (Notion's, seen
        // 2026-11-14 at 402pt and on a Galaxy A90; PhoneSheet useShadePress).
        <PhoneSheet size="half" title={edit.title} onClose={() => setSide(null)} data={{ "data-submenu": "edit" }}>
          {/* The type's settings alone, rows of one group, as Notion's (seen 2026-11-11: "관계형 대상", "제한 ›", "양방향 관계형 ›"). */}
          <PhonePropertyTypeGroup settings={edit.content} />
        </PhoneSheet>
      ) : null}
      {side === "type" ? (
        <PhoneSheet title={t("changeType")} onClose={() => setSide(null)} data={{ "data-submenu": "type" }}>
          <PhoneSheetGroup>
            {editablePropertyTypes(property.type as PropertyType).map(({ type, label, disabled }) => (
              <PhoneSheetRow key={type} icon={<PropertyTypeIcon type={type} size="regular" />} label={label} disabled={disabled} more={false}
                value={property.type === type ? <PhoneSheetTick /> : undefined}
                onPress={() => { setSide(null); onType(type); }} data={{ "data-property-type": type }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {side === "sort" ? (
        // Notion's has no title, its two directions without icons.
        <PhoneSheet title="" label={t("sort")} onClose={() => setSide(null)} data={{ "data-submenu": "sort" }}>
          <PhoneSheetGroup>
            {(["asc", "desc"] as const).map((direction) => (
              <PhoneSheetRow key={direction} label={direction === "asc" ? sort.ascending : sort.descending} more={false}
                onPress={() => sort.onSort(direction)} data={{ "data-sort-direction": direction }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {side === "calculate" ? <PhoneCalculationSheet {...calculation} onClose={() => setSide(null)} /> : null}
      {side === "statusDisplay" ? (
        <PhoneChoiceSheet title={status.title} onClose={() => setSide(null)}
          choices={[{ key: "select", label: status.choices.select }, { key: "checkbox", label: status.choices.checkbox }]}
          onPick={(key) => status.onPick(key === "checkbox")} />
      ) : null}
    </>
  );
}

/**
 * "계산" on a phone, as Notion's half sheet "표 집계" (seen 2026-11-11):
 * "계산 안함" ticked where nothing is counted, then "수", "비율(%)" and the
 * type's other groups with "›", each opening its calculations over it.
 */
function PhoneCalculationSheet({ title, type, picked, limitLargeCounts, onPick, onClose }: {
  title: string; type: string; picked?: TableCalculation; limitLargeCounts?: { on: boolean; set(limit: boolean): void };
  onPick(next: TableCalculation | undefined): void; onClose(): void;
}) {
  const { t } = useTranslation("tableView");
  const [open, setOpen] = useState<TableCalculationGroup | null>(null);
  const groups = tableCalculationGroups(type);
  const tick = (on: boolean) => on ? <PhoneSheetTick /> : undefined;
  return (
    <>
      <PhoneSheet size="half" title={title} onClose={onClose} data={{ "data-submenu": "calculate" }}>
        <PhoneSheetGroup>
          <PhoneSheetRow label={t("calculations.none")} more={false} value={tick(!picked)} onPress={() => onPick(undefined)}
            data={{ "data-calculation": "none" }} />
          {groups.map((group) => (
            <PhoneSheetRow key={group.key} label={t(`calculationGroups.${group.key}`)} onPress={() => setOpen(group)}
              data={{ "data-calculation-group": group.key }} />
          ))}
        </PhoneSheetGroup>
      </PhoneSheet>
      {open ? (
        <PhoneSheet size="half" title={t(`calculationGroups.${open.key}`)} onClose={() => setOpen(null)} onBack={() => setOpen(null)}
          data={{ "data-calculation-items": open.key }}>
          {open.key === "count" && limitLargeCounts ? (
            <PhoneSheetGroup>
              <PhoneSheetRow label={t("calculations.limitLargeCounts")} description={t("calculations.limitLargeCountsHint")} more={false}
                value={<PhoneSheetSwitch on={limitLargeCounts.on} />} onPress={() => limitLargeCounts.set(!limitLargeCounts.on)} />
            </PhoneSheetGroup>
          ) : null}
          <PhoneSheetGroup>
            {open.items.map((item) => (
              <PhoneSheetRow key={item} label={t(`calculations.${item}`)} more={false} value={tick(picked === item)}
                onPress={() => onPick(item)} data={{ "data-calculation": item }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
    </>
  );
}
