import { SwitchInput } from "@/components/Switch";
import { useState } from "react";
import type { GraphView } from "@/contentGraph";
import { CheckIcon, PaintRollerIcon, Plus, Trash } from "@/icons/leafen";
import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { COLOR_NAMES } from "./colors";
import { conditionalColorName, Swatch } from "./ConditionalColorCard";
import { PhoneAdvancedFilter } from "./DatabasePhoneAdvancedFilter";
import { DatabasePropertyPicker } from "./DatabasePropertyPicker";
import { PhoneChoiceSheet } from "./DatabasePhonePickers";
import { PhoneFilterRuleRows } from "./PhoneFilterRuleRows";
import { PropertyIcon } from "./PropertyIcon";
import { NO_VALUE_FILTERS } from "./databaseFilterLabels";
import { effectiveFilterOperator, filterOperatorLabel, filterPropertyForComparison } from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { filterValueText } from "./databaseFilterValueText";
import { filterGroupTerms } from "./filterGroupTerms";
import { databaseViewLabels } from "./databaseViewLabels";
import {
  conditionalColorHasOneCondition,
  conditionalColorHasOptionColors,
  type ConditionalColor,
  type ConditionalColorSetting,
} from "./conditionalColorModel";
import styles from "./ConditionalColorSheet.module.css";

type Picking = { kind: "condition" | "color" | "apply"; id: string } | { kind: "add" } | null;

/** A setting's conditions in a line, without its property: "비어 있지 않음". */
function conditionSummary(group: FilterGroup, properties: DbProperty[], and: string, or: string): string {
  const join = group.conjunction === "or" ? or : and;
  return filterGroupTerms(group).map((term) => {
    if (term.kind === "group") return `(${conditionSummary(term.group, properties, and, or)})`;
    const selected = properties.find(({ id }) => id === term.filter.propertyId);
    if (!selected) return "";
    const property = filterPropertyForComparison(selected, properties);
    const operator = effectiveFilterOperator(property, term.filter.operator);
    const value = NO_VALUE_FILTERS.has(operator) ? "" : filterValueText(term.filter.value, property);
    return [filterOperatorLabel(operator, property), value ? `"${value}"` : ""].filter(Boolean).join(" ");
  }).filter(Boolean).join(` ${join} `);
}

/** A color row's name: "일치 옵션", or "초록색 배경" and "기본 배경" (none). */
function colorLabel(color: ConditionalColor) {
  const labels = databaseViewLabels();
  return color === "auto" ? labels.conditionalColorAuto : labels.conditionalColorBackgroundOf(conditionalColorName(color));
}

/**
 * A view's conditional colors in a phone's settings sheet, as Notion's (seen
 * 2026-10-24 at 375px): each setting under its gray "색상 설정 1" in a white
 * group — its property, fixed; its condition ("값과 동일한 데이터 ›", picked in
 * the "비교 연산자" half sheet) and its value ("Won ›" on a screen of options,
 * "15 편집 ⊗" typed in the row), as a filter rule's rows; its color (the paint
 * roller's "일치 옵션 ›", or a swatch's "초록색 배경 ›"), picked in the "색상
 * 선택" half sheet; in a table "적용 대상: 전체 행 ›"; and red "색상 설정
 * 삭제" — then "+ 더 추가하기" in a white button, asking first for the property
 * a new setting colors by. A setting with several
 * conditions, or one on another property, has them in a line, on a screen of
 * their own as the advanced filter's.
 */
export function ConditionalColorSheet({ view, settings, dbProperties, addable, filterSnapshot, onCommit, onAdd }: {
  view: GraphView;
  settings: ConditionalColorSetting[];
  dbProperties: DbProperty[];
  /** The properties a new setting may color by. */
  addable: DbProperty[];
  filterSnapshot: DatabaseFilterSnapshot;
  onCommit(next: ConditionalColorSetting[]): void;
  onAdd(property: DbProperty): void;
}) {
  const labels = databaseViewLabels();
  const [picking, setPicking] = useState<Picking>(null);
  const close = () => setPicking(null);
  const byId = new Map(dbProperties.map((property) => [property.id, property]));
  const change = (id: string, patch: (setting: ConditionalColorSetting) => ConditionalColorSetting) =>
    onCommit(settings.map((setting) => setting.id === id ? patch(setting) : setting));
  const setFilter = (id: string, patch: Partial<ViewFilter>) => change(id, (current) => ({
    ...current, filterGroup: { ...current.filterGroup, filters: current.filterGroup.filters.map((item, at) => at === 0 ? { ...item, ...patch } as ViewFilter : item) },
  }));
  const picked = picking && "id" in picking ? settings.find(({ id }) => id === picking.id) : undefined;
  const pickedIndex = picked ? settings.indexOf(picked) : -1;
  return (
    <div className={styles.stack} data-conditional-color-sheet>
      {settings.map((setting, index) => {
        const property = byId.get(setting.propertyId);
        const applyLabel = setting.applyTo === "property"
          ? labels.conditionalColorApplyProperty : labels.conditionalColorApplyRow;
        return (
          <PhoneSheetGroup key={setting.id} label={labels.conditionalColorSetting(index + 1)}>
            <div className={sheetStyles.row} data-conditional-color-property={setting.propertyId}>
              <span className={sheetStyles.rowIcon} aria-hidden="true">{property ? <PropertyIcon property={property} size="wide" /> : null}</span>
              <span className={sheetStyles.rowLabel}>{property?.name ?? labels.untitled}</span>
            </div>
            {property && conditionalColorHasOneCondition(setting) ? (
              <PhoneFilterRuleRows filter={setting.filterGroup.filters[0]} properties={dbProperties} snapshot={filterSnapshot} showProperty={false}
                onChange={(patch) => setFilter(setting.id, patch)} />
            ) : (
              <PhoneSheetRow indent label={conditionSummary(setting.filterGroup, dbProperties, labels.sheetAnd, labels.sheetOr) || labels.sheetFilters(0)}
                data={{ "data-conditional-color-condition": "" }} onPress={() => setPicking({ kind: "condition", id: setting.id })} />
            )}
            <PhoneSheetRow icon={setting.color === "auto" ? <PaintRollerIcon size="regular" /> : <Swatch color={setting.color} phone />}
              label={colorLabel(setting.color)} data={{ "data-conditional-color-color": setting.color }}
              onPress={() => setPicking({ kind: "color", id: setting.id })} />
            {view.type === "table" && (
              <PhoneSheetRow label={labels.conditionalColorApplyTo} value={<span className={styles.applyValue}>{applyLabel}</span>}
                data={{ "data-conditional-color-apply": setting.applyTo ?? "row" }}
                onPress={() => setPicking({ kind: "apply", id: setting.id })} />
            )}
            <PhoneSheetRow icon={<Trash size="regular" />} label={labels.conditionalColorDeleteSetting} danger more={false}
              data={{ "data-conditional-color-delete": "" }} onPress={() => onCommit(settings.filter(({ id }) => id !== setting.id))} />
          </PhoneSheetGroup>
        );
      })}
      <button type="button" className={styles.addMore} data-conditional-color-add disabled={!addable.length} onClick={() => setPicking({ kind: "add" })}>
        <Plus size="regular" aria-hidden="true" />{labels.conditionalColorAddMore}
      </button>
      {picking?.kind === "add" && <ConditionalColorPropertySheet properties={addable} onPick={onAdd} onClose={close} />}
      {picked && picking?.kind === "condition" && (
        <PhoneSheet title={labels.conditionalColorSetting(pickedIndex + 1)} onClose={close} onBack={close}
          data={{ "data-conditional-color-conditions": "" }}>
          <PhoneAdvancedFilter root={picked.filterGroup} properties={dbProperties} snapshot={filterSnapshot}
            onCommit={(next) => change(picked.id, (current) => ({ ...current, filterGroup: next }))} />
        </PhoneSheet>
      )}
      {picked && picking?.kind === "color" && (
        <ColorSheet setting={picked} property={byId.get(picked.propertyId)} onClose={close}
          onPick={(color) => change(picked.id, (current) => ({ ...current, color }))} />
      )}
      {picked && picking?.kind === "apply" && (
        <PhoneChoiceSheet title={labels.conditionalColorApplyTo} onClose={close}
          choices={[
            { key: "row", label: labels.conditionalColorApplyRow },
            { key: "property", label: labels.conditionalColorApplyProperty },
          ]}
          onPick={(applyTo) => change(picked.id, (current) => applyTo === "property" ? { ...current, applyTo: "property" } : { ...current, applyTo: undefined })} />
      )}
    </div>
  );
}

/**
 * A setting's color in Notion's "색상 선택" half sheet (seen 2026-10-24 at
 * 375px): for a property with option colors, "일치 옵션" with the paint roller
 * and a switch — off, the pages take none; then each color's swatch and name,
 * "기본 배경" (none) first, the chosen one checked; picking one closes it.
 */
function ColorSheet({ setting, property, onPick, onClose }: {
  setting: ConditionalColorSetting;
  property: DbProperty | undefined;
  onPick(color: ConditionalColor): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet size="half" title={labels.conditionalColorPickColor} onClose={onClose} data={{ "data-conditional-color-colors": "" }}>
      <div className={sheetStyles.stack}>
        {conditionalColorHasOptionColors(property) ? (
          <PhoneSheetGroup>
            <label className={`${sheetStyles.row} ${styles.switchRow}`}>
              <span className={sheetStyles.rowIcon} aria-hidden="true"><PaintRollerIcon size="wide" /></span>
              <span className={sheetStyles.rowLabel}>{labels.conditionalColorAuto}</span>
              <SwitchInput checked={setting.color === "auto"} data-conditional-color-auto
                onChange={(event) => onPick(event.target.checked ? "auto" : "default")} />
            </label>
          </PhoneSheetGroup>
        ) : null}
        <PhoneSheetGroup>
          {COLOR_NAMES.map((color) => (
            <PhoneSheetRow key={color} icon={<Swatch color={color} phone />} label={colorLabel(color)} more={false}
              value={setting.color === color ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
              onPress={() => { onPick(color); onClose(); }}
              data={{ "data-phone-choice": color, "data-chosen": setting.color === color ? "true" : undefined }} />
          ))}
        </PhoneSheetGroup>
      </div>
    </PhoneSheet>
  );
}

/**
 * The property a new setting colors by, in Notion's "속성 선택" half sheet
 * (seen 2026-10-24 at 375px): a search field, then each property's icon and
 * name; picking one adds the setting.
 */
export function ConditionalColorPropertySheet({ properties, onPick, onClose }: {
  properties: DbProperty[];
  onPick(property: DbProperty): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  return (
    <PhoneSheet size="half" title={labels.conditionalColorPickProperty} onClose={onClose} data={{ "data-conditional-color-properties": "" }}>
      <DatabasePropertyPicker placeholder={labels.searchProperties} properties={properties} sheet sheetOpensMore={false} leadingApart={false}
        onPick={(picked) => {
          const property = properties.find(({ id }) => id === picked.id);
          if (property) onPick(property);
          onClose();
        }} />
    </PhoneSheet>
  );
}
