import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode, type Ref } from "react";
import { DotsHorizontal, FilterIcon, Settings, SidebarChevronDown, Trash } from "@/icons/leafen";
import { i18next } from "@/i18n";
import type { DbProperty, FilterOperator, ViewFilter } from "@/lib/types";
import { filterAwaitsValue } from "@/shared/database/queryFilterPolicy";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { DatabaseFilterValueInput } from "./DatabaseFilterValueInput";
import { filterOperatorChange } from "./DatabaseFilterEditors";
import { PhoneChoiceSheet, PhoneFilterOptions } from "./DatabasePhonePickers";
import { PhoneDateFilterValue, PhoneFilterValueClear, PhoneNumberFilterValue, PhonePagesFilterValue, PhonePeopleFilterValue } from "./DatabasePhoneFilterValues";
import { dateFilterShowsPart, isDateFilterProperty } from "./DateFilterEditor";
import { NO_VALUE_FILTERS, dateFilterLabels, rollupMatchChoices, rollupMatchPatch } from "./databaseFilterLabels";
import {
  effectiveFilterOperator, filterAsksRollupMatch, filterOperatorLabel, filterOperatorsFor, filterPropertyForComparison, filterUsesNumberSigns,
} from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { databaseViewLabels } from "./databaseViewLabels";
import { useMultiSourceEdit } from "./dashboardMultiSourceEdit";
import styles from "./DatabaseQuickFilterSheet.module.css";

const PEOPLE_TYPES = new Set(["person", "created_by", "last_edited_by"]);

/**
 * A quick filter's editor on a phone, as Notion's (seen 2026-10-21 at
 * 375px): a sheet over the filters' sheet whose head holds "⋯" at its left
 * ("필터 제거" and "고급 필터에 추가" rising from the bottom), its condition
 * in the middle ("값과 동일한 데이터 ▾", whose choices rise in a half sheet;
 * a number's after its name, "Count = ▾"; a date's "오늘 기준" and "범위 내"
 * after its part, "시작일 오늘 기준 ▾", and a day's alone, "~이전(당일 불포함) ▾";
 * "비어 있음" and "비어 있지 않음" after the name whatever the type, "마감일 비어
 * 있음 ▾", "선택 비어 있음 ▾", while a value's condition stands alone, as Notion's
 * phone, seen 2026-11-11, 2026-11-12 and 2026-11-15) and "완료"; under it the
 * value — a select's field and options
 * to tick, a checkbox's two states, unchecked first, choosing one closing
 * it, a date's parts and month, people or pages to choose any of, a
 * number's band or a field to type in (seen 2026-10-23).
 */
export function QuickFilterSheet({ filter, property: sourceProperty, properties, snapshot, sheetRef, zIndex, onKeyDown, onChange, onRemove, onToAdvanced, onClose }: {
  filter: ViewFilter;
  property: DbProperty;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  sheetRef: Ref<HTMLDivElement>;
  zIndex?: number;
  onKeyDown?(event: KeyboardEvent<HTMLDivElement>): void;
  onChange(patch: Partial<ViewFilter>): void;
  onRemove(): void;
  onToAdvanced?(): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const property = filterPropertyForComparison(sourceProperty, properties);
  const operator = effectiveFilterOperator(property, filter.operator);
  // A filter left without its value goes when its sheet closes — "완료", the
  // shade, a drag or Escape — as Notion's phone (seen 2026-11-11: a select,
  // multi-select, status or text filter with nothing chosen leaves "필터 없음").
  const close = () => (filterAwaitsValue(filter) ? onRemove() : onClose());
  const [more, setMore] = useState(false);
  const multiSource = useMultiSourceEdit();
  const [conditions, setConditions] = useState(false);
  const [parts, setParts] = useState(false);
  const [matches, setMatches] = useState(false);
  const dates = isDateFilterProperty(property);
  const signs = filterUsesNumberSigns(property);
  const matching = filterAsksRollupMatch(sourceProperty);
  const span = operator === "relative_to_today" || operator === "is_between";
  const part = !matching && dates && span && dateFilterShowsPart(property, operator);
  // A number's name is already in its condition; a rollup's other than a date's is left bare (not seen in Notion's phone).
  const named = !matching && !signs && (dates || sourceProperty.type !== "rollup") && (operator === "is_empty" || operator === "is_not_empty");
  let value: ReactNode = null;
  // Whether the value is typed — a word's or a number's field — rather than chosen.
  let typed = false;
  if (NO_VALUE_FILTERS.has(operator) && !dates) value = null;
  else if (dates) value = <PhoneDateFilterValue filter={filter} property={property} onChange={onChange} />;
  else if (property.type === "checkbox") {
    value = (
      <PhoneSheetGroup>
        {[false, true].map((state) => (
          <PhoneSheetRow key={String(state)} label={state ? labels.checked : labels.unchecked} more={false}
            onPress={() => { onChange({ operator, value: state }); onClose(); }} data={{ "data-quick-filter-checkbox": String(state) }} />
        ))}
      </PhoneSheetGroup>
    );
  } else if (property.type === "select" || property.type === "multi_select" || property.type === "status") {
    // A chosen value's field "ⓧ" empties it and closes the sheet, so the filter goes and "필터 없음" shows (Notion's phone, seen 2026-11-17).
    value = <PhoneFilterOptions property={property} value={filter.value} onChange={(next) => onChange({ operator, value: next })} onClear={onRemove} />;
  } else if (PEOPLE_TYPES.has(property.type)) {
    value = <PhonePeopleFilterValue property={property} value={filter.value} snapshot={snapshot} onChange={(next) => onChange({ operator, value: next })}
      onClear={onRemove} />;
  } else if (property.type === "relation") {
    value = <PhonePagesFilterValue property={property} value={filter.value} snapshot={snapshot} onChange={(next) => onChange({ operator, value: next })}
      onClear={onRemove} />;
  } else if (signs) {
    typed = true;
    value = <PhoneNumberFilterValue property={property} operator={operator} value={filter.value} onChange={(next) => onChange({ operator, value: next })} />;
  } else {
    typed = true;
    value = (
      <PhoneSheetGroup>
        <div className={`${sheetStyles.row} ${styles.field}`}>
          <DatabaseFilterValueInput prop={property} operator={operator} value={filter.value} databaseProperties={properties}
            snapshot={snapshot} inputClassName={styles.fieldInput} onChange={(next) => onChange({ operator, value: next })} />
          {(typeof filter.value === "string" && filter.value) || typeof filter.value === "number"
            ? <PhoneFilterValueClear onClear={() => onChange({ operator, value: "" })} /> : null}
        </div>
      </PhoneSheetGroup>
    );
  }
  // A typed value's field takes the focus as the sheet opens, the keyboard
  // rising with it and the caret after what it holds; a value chosen from a
  // list, a date's or a checkbox's leaves it where it was. Notion's phone
  // (Galaxy A90 Chrome 411, 2026-11-15, judged after the press alone by the
  // screen and one letter typed: a new and a kept "이름" filter, "글" and
  // "수" — "q" went into "값 입력"; "선택" and "날짜" raised no keyboard).
  const valueRef = useRef<HTMLDivElement>(null);
  const typedOnOpen = useRef(typed);
  useEffect(() => {
    if (typedOnOpen.current) valueRef.current?.querySelector<HTMLInputElement>("input[type='text']")?.focus({ preventScroll: true });
  }, []);
  return (
    <PhoneSheet label={labels.editQuickFilter(sourceProperty.name)} sheetRef={sheetRef} zIndex={zIndex} onClose={close} onKeyDown={onKeyDown}
      title={(
        <span className={styles.head}>
          {matching ? (
            <button type="button" className={styles.part} aria-label={labels.filterRollupMatch} data-quick-filter-rollup-match
              onClick={() => setMatches(true)}>
              {rollupMatchChoices().find(({ key }) => key === (filter.rollupMatch ?? "any"))?.label}
            </button>
          ) : null}
          {part ? (
            <button type="button" className={styles.part} aria-label={dateFilterLabels().datePartFor(property.name)} data-quick-filter-part
              onClick={() => setParts(true)}>
              {dateFilterLabels().parts[filter.datePart === "end" ? "end" : "start"]}
            </button>
          ) : null}
          {named ? <span className={styles.lead} data-quick-filter-name>{sourceProperty.name}</span> : null}
          <button type="button" className={styles.condition} aria-label={labels.filterCondition} data-quick-filter-condition
            onClick={() => setConditions(true)}>
            {signs ? <span className={styles.name}>{property.name}</span> : null}
            <span>{filterOperatorLabel(operator, property)}</span>
            {/* Notion's chevron at 14px, 4px after the condition (its phone, Galaxy A90 Chrome 411, 2026-11-15: "수 = ⌄"). */}
            <span className={styles.chevron} aria-hidden="true"><SidebarChevronDown size="tiny" /></span>
          </button>
        </span>
      )}
      start={(
        <button type="button" className={styles.more} aria-label={labels.filterMoreActions} aria-haspopup="menu" data-quick-filter-more
          onClick={() => setMore(true)}>
          <DotsHorizontal size="small" aria-hidden="true" />
        </button>
      )}
      action={{ label: labels.sheetDone, onPress: close }} data={{ "data-quick-filter-editor": "" }}>
      <div ref={valueRef} className={sheetStyles.stack}>{value}</div>
      {more ? (
        <PhoneSheetMenu label={labels.filterMoreActions} onClose={() => setMore(false)}>
          <PhoneSheetRow icon={<Trash size="regular" />} label={labels.removeFilter} danger more={false} onPress={onRemove}
            data={{ "data-quick-filter-remove": "" }} />
          {/* A dashboard's filter goes on to "⚙ 다중 출처 필터링" in its place, as on a computer (MultiSourceEditContext). */}
          {multiSource ? (
            <PhoneSheetRow icon={<Settings size="regular" />} label={i18next.t("databaseView:multiSourceFiltering")} more={false}
              onPress={() => { setMore(false); multiSource(); }} data={{ "data-quick-filter-multi-source": "" }} />
          ) : onToAdvanced ? (
            <PhoneSheetRow icon={<FilterIcon size="regular" />} label={labels.addToAdvancedFilter} more={false} onPress={onToAdvanced}
              data={{ "data-quick-filter-to-advanced": "" }} />
          ) : null}
        </PhoneSheetMenu>
      ) : null}
      {matches ? (
        <PhoneChoiceSheet title="" onClose={() => setMatches(false)} data={{ "data-quick-filter-rollup-matches": "" }}
          choices={rollupMatchChoices()} onPick={(key) => onChange(rollupMatchPatch(key))} />
      ) : null}
      {parts ? (
        <PhoneChoiceSheet title="" onClose={() => setParts(false)} data={{ "data-quick-filter-parts": "" }}
          choices={(["start", "end"] as const).map((key) => ({ key, label: dateFilterLabels().parts[key] }))}
          onPick={(key) => onChange({ datePart: key === "end" ? "end" : undefined })} />
      ) : null}
      {conditions ? (
        <PhoneChoiceSheet title="" onClose={() => setConditions(false)} data={{ "data-quick-filter-conditions": "" }}
          choices={filterOperatorsFor(property).map((key) => ({ key, label: filterOperatorLabel(key, property) }))}
          onPick={(key) => onChange(filterOperatorChange(filter, property, key as FilterOperator))} />
      ) : null}
    </PhoneSheet>
  );
}
