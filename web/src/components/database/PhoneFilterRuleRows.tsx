import { useRef, useState } from "react";
import { XCircleIcon } from "@/icons/leafen";
import type { DbProperty, FilterOperator, ViewFilter } from "@/lib/types";
import { usePageNames } from "@/lib/pageNames";
import { PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { DatabaseFilterValueInput } from "./DatabaseFilterValueInput";
import { filterOperatorChange } from "./DatabaseFilterEditors";
import { NumberTextInput } from "./NumberTextInput";
import { chosenFilterOptionList, PhoneChoiceSheet, PhoneFilterOptionsScreen, PhoneOptionPill, PhonePropertyScreen } from "./DatabasePhonePickers";
import { PhoneDateFilterValue, PhoneFilterValueScreen, PhonePagesFilterValue, PhonePeopleFilterValue } from "./DatabasePhoneFilterValues";
import { PropertyIcon } from "./PropertyIcon";
import { isRelativeFilterDay, RELATIVE_FILTER_DAYS } from "@/shared/database/queryRelativeDate.mjs";
import { DateFilterCalendar, dateFilterShowsPart, isDateFilterProperty } from "./DateFilterEditor";
import { relativeFilterDayText } from "./DateFilterDayChoice";
import { DateTextInput } from "./DateTextInput";
import { dateRangeFilterText } from "./DateRangeFilterField";
import { activeDateLocale } from "@/lib/i18n";
import { dateKey, parseDate } from "./dateUtils";
import { NO_VALUE_FILTERS, dateFilterLabels, relativeDateUnitLabels, rollupMatchChoices, rollupMatchPatch } from "./databaseFilterLabels";
import {
  dateFilterRelativeValue,
  defaultOperator,
  defaultValue,
  effectiveFilterOperator,
  filterAsksRollupMatch,
  filterOperatorLabel,
  filterOperatorsFor,
  filterPropertyForComparison,
} from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { filterValueText } from "./databaseFilterValueText";
import { databaseViewLabels } from "./databaseViewLabels";
import styles from "./DatabasePhoneAdvancedFilter.module.css";

// A value picked from the property's options, on a screen of its own.
const OPTION_TYPES = new Set(["select", "multi_select", "status"]);
// A value typed in the row, with "편집" to start and a clear button once set.
const TEXT_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);
const NUMBER_TYPES = new Set(["number", "unique_id"]);
// A value chosen on a screen of its own: a checkbox's state, people, pages.
const PEOPLE_TYPES = new Set(["person", "created_by", "last_edited_by"]);
const SCREEN_TYPES = new Set(["checkbox", "relation", ...PEOPLE_TYPES]);
const DIRECTIONS = ["past", "next", "this"] as const;
const UNITS = ["day", "week", "month", "year"] as const;
// A date's conditions on a day, whose day may be named from today.
const DAY_OPERATORS = new Set<FilterOperator>(["equals", "before", "after", "on_or_before", "on_or_after"]);
const CUSTOM_DAY = "custom";

type Picking = "property" | "operator" | "options" | "value" | "part" | "match" | "direction" | "unit" | "day" | "date" | "range" | null;

/** A custom day as a rule's row writes it, "2026년 11월 18일" (Notion's phone, seen 2026-11-11). */
function ruleDayText(key: string) {
  return parseDate(key)?.toLocaleDateString(activeDateLocale(), { year: "numeric", month: "long", day: "numeric" }) ?? key;
}

/** A rule's condition and value in 16px under its 17px property, as Notion's (seen 2026-10-24). */
function ruleText(text: string) {
  return <span className={styles.ruleText}>{text}</span>;
}

/**
 * One filter rule's rows in a phone sheet, as Notion draws a rule (seen
 * 2026-10-21 in the advanced filter, 2026-10-24 in a conditional color's
 * setting): its property ("Aa Name ›", picked on a screen of properties —
 * left out where the rule's property is fixed), a date's part, its condition
 * ("값을 포함하는 데이터 ›", picked in the "비교 연산자" half sheet), a
 * relative date's parts and its value (typed in the row with "편집", or "옵션
 * 선택 ›" on a screen of options), these in 16px under the property's 17px —
 * each with the picker it opens. A rule's day condition picked from "오늘
 * 기준" or "범위 내" starts "오늘", in both as Notion's phone (seen
 * 2026-11-11, dateFilterOperatorChange).
 */
export function PhoneFilterRuleRows({ filter, properties, snapshot, showProperty = true, onChange }: {
  filter: ViewFilter;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  showProperty?: boolean;
  onChange(patch: Partial<ViewFilter>): void;
}) {
  const labels = databaseViewLabels();
  const [picking, setPicking] = useState<Picking>(null);
  const close = () => setPicking(null);
  const selected = properties.find(({ id }) => id === filter.propertyId) ?? properties[0];
  if (!selected) return null;
  const property = filterPropertyForComparison(selected, properties);
  const operator = effectiveFilterOperator(property, filter.operator);
  const dates = isDateFilterProperty(property);
  // A date's part, condition and relative parts each a row of its own ("시작일 ›", "오늘 기준 ›", "이번 ›", "주 ›").
  const relative = dates && operator === "relative_to_today" ? dateFilterRelativeValue(filter) : null;
  // A day's condition names its day from today ("오늘 ›") or, for "사용자 지정
  // 날짜 ›", picks it on a screen of its own from "날짜 선택 ›" (Notion's phone,
  // seen 2026-11-11 at 402pt).
  const dayRule = dates && DAY_OPERATORS.has(operator);
  const namedDay = dayRule && isRelativeFilterDay(filter.value);
  const customDay = dayRule && !namedDay && typeof filter.value === "string" ? dateKey(filter.value) : "";
  // "범위 내" picks its days on a screen of its own from "범위 선택 ›", as a custom day's.
  const rangeText = dates && operator === "is_between" ? dateRangeFilterText(filter.value) : null;
  return (
    <>
      {showProperty ? (
        <PhoneSheetRow icon={<PropertyIcon property={selected} size="regular" />} indent label={selected.name}
          onPress={() => setPicking("property")} data={{ "data-filter-rule-property": selected.id }} />
      ) : null}
      {filterAsksRollupMatch(selected) ? (
        <PhoneSheetRow indent label={ruleText(rollupMatchChoices().find(({ key }) => key === (filter.rollupMatch ?? "any"))?.label ?? "")}
          onPress={() => setPicking("match")} data={{ "data-filter-rule-rollup-match": filter.rollupMatch ?? "any" }} />
      ) : dates && dateFilterShowsPart(property, operator) ? (
        <PhoneSheetRow indent label={ruleText(dateFilterLabels().parts[filter.datePart === "end" ? "end" : "start"])}
          onPress={() => setPicking("part")} data={{ "data-filter-rule-part": filter.datePart ?? "start" }} />
      ) : null}
      <PhoneSheetRow indent label={ruleText(filterOperatorLabel(operator, property))} onPress={() => setPicking("operator")}
        data={{ "data-filter-rule-operator": operator }} />
      {relative ? (
        <>
          <PhoneSheetRow indent label={ruleText(labels.relativeDateDirections[relative.direction])} onPress={() => setPicking("direction")}
            data={{ "data-filter-rule-direction": relative.direction }} />
          {relative.direction !== "this" ? (
            <PhoneRuleValue filter={{ ...filter, value: relative.count }} property={{ ...property, type: "number" } as DbProperty} operator="equals"
              properties={properties} snapshot={snapshot} onOptions={() => undefined}
              onChange={(count) => {
                if (typeof count === "number" && Number.isSafeInteger(count) && count > 0) onChange({ operator, value: { ...relative, count } });
              }} />
          ) : null}
          <PhoneSheetRow indent label={ruleText(relativeDateUnitLabels(relative)[relative.unit])}
            onPress={() => setPicking("unit")} data={{ "data-filter-rule-unit": relative.unit }} />
        </>
      ) : dayRule ? (
        <>
          <PhoneSheetRow indent label={ruleText(relativeFilterDayText(filter.value) || dateFilterLabels().customDay)}
            onPress={() => setPicking("day")} data={{ "data-filter-rule-day": namedDay ? String(filter.value) : CUSTOM_DAY }} />
          {namedDay ? null : (
            <PhoneSheetRow indent muted={!customDay} onPress={() => setPicking("date")} data={{ "data-filter-rule-value": "" }}
              label={ruleText(customDay ? ruleDayText(customDay) : dateFilterLabels().pickDay)} />
          )}
        </>
      ) : rangeText !== null ? (
        <PhoneSheetRow indent muted={!rangeText} onPress={() => setPicking("range")} data={{ "data-filter-rule-value": "" }}
          label={ruleText(rangeText || dateFilterLabels().pickRange)} />
      ) : NO_VALUE_FILTERS.has(operator) ? null : (
        <PhoneRuleValue filter={filter} property={property} operator={operator} properties={properties} snapshot={snapshot}
          onChange={(value) => onChange({ operator, value })}
          onOptions={() => setPicking(SCREEN_TYPES.has(property.type) ? "value" : "options")} />
      )}
      {picking === "property" ? (
        <PhonePropertyScreen properties={properties} onClose={close} onPick={(choice) => {
          const next = filterPropertyForComparison(properties.find(({ id }) => id === choice.id) ?? choice as DbProperty, properties);
          onChange({ propertyId: next.id, operator: defaultOperator(next), value: defaultValue(next), rollupMatch: undefined });
          close();
        }} />
      ) : null}
      {picking === "operator" ? (
        <PhoneChoiceSheet title={labels.sheetCondition} onClose={close} data={{ "data-filter-operators": "" }}
          choices={filterOperatorsFor(property).map((key) => ({ key, label: filterOperatorLabel(key, property) }))}
          onPick={(key) => onChange(filterOperatorChange(filter, property, key as FilterOperator, true))} />
      ) : null}
      {picking === "value" ? (
        <PhoneFilterValueScreen property={property} onClose={close}>
          {property.type === "checkbox" ? (
            <PhoneSheetGroup>
              {[false, true].map((state) => (
                <PhoneSheetRow key={String(state)} label={state ? labels.checked : labels.unchecked} more={false}
                  onPress={() => { onChange({ operator, value: state }); close(); }}
                  data={{ "data-filter-value-checkbox": String(state) }} />
              ))}
            </PhoneSheetGroup>
          ) : PEOPLE_TYPES.has(property.type) ? (
            <PhonePeopleFilterValue property={property} value={filter.value} snapshot={snapshot} onChange={(value) => onChange({ operator, value })}
              onClear={() => { onChange({ operator, value: undefined }); close(); }} />
          ) : (
            <PhonePagesFilterValue property={property} value={filter.value} snapshot={snapshot} onChange={(value) => onChange({ operator, value })}
              onClear={() => { onChange({ operator, value: undefined }); close(); }} />
          )}
        </PhoneFilterValueScreen>
      ) : null}
      {picking === "match" ? (
        <PhoneChoiceSheet title="" onClose={close} data={{ "data-filter-rollup-matches": "" }}
          choices={rollupMatchChoices()} onPick={(key) => onChange(rollupMatchPatch(key))} />
      ) : null}
      {picking === "part" ? (
        <PhoneChoiceSheet title="" onClose={close} data={{ "data-filter-parts": "" }}
          choices={(["start", "end"] as const).map((key) => ({ key, label: dateFilterLabels().parts[key] }))}
          onPick={(key) => onChange({ datePart: key === "end" ? "end" : undefined })} />
      ) : null}
      {picking === "direction" || picking === "unit" ? (() => {
        // A fixed "이번 주" becomes the relative date it names once its parts change.
        const current = dateFilterRelativeValue(filter);
        const relativeOperator = "relative_to_today" as const;
        return picking === "direction" ? (
          <PhoneChoiceSheet title="" onClose={close} data={{ "data-filter-directions": "" }}
            choices={DIRECTIONS.map((key) => ({ key, label: labels.relativeDateDirections[key] }))}
            onPick={(direction) => onChange({ operator: relativeOperator, value: direction === "this" ? { direction, unit: current.unit }
              : { direction, count: current.direction === "this" ? 1 : current.count, unit: current.unit } })} />
        ) : (
          <PhoneChoiceSheet title="" onClose={close} data={{ "data-filter-units": "" }}
            choices={UNITS.map((key) => ({ key, label: relativeDateUnitLabels(current)[key] }))}
            onPick={(unit) => onChange({ operator: relativeOperator, value: { ...current, unit } })} />
        );
      })() : null}
      {picking === "day" ? (
        <PhoneChoiceSheet title={property.name} onClose={close} data={{ "data-filter-days": "" }}
          choices={[...RELATIVE_FILTER_DAYS.map((key) => ({ key: key as string, label: dateFilterLabels().relativeDays(key) })),
            { key: CUSTOM_DAY, label: dateFilterLabels().customDay }]}
          onPick={(key) => {
            if (key === (namedDay ? filter.value : CUSTOM_DAY)) return;
            onChange({ operator, value: key === CUSTOM_DAY ? undefined : key });
          }} />
      ) : null}
      {picking === "date" ? (
        <PhoneFilterValueScreen property={property} onClose={close}>
          <PhoneSheetGroup>
            <label className={sheetStyles.row}>
              <DateTextInput className={styles.typedInput} value={customDay} placeholder={dateFilterLabels().dayPlaceholder}
                ariaLabel={labels.filterValueFor(property.name)} onChange={(next) => onChange({ operator, value: next ?? undefined })} />
            </label>
          </PhoneSheetGroup>
          <PhoneSheetGroup>
            <DateFilterCalendar phone start={customDay || undefined} end={customDay || undefined}
              onPick={(day) => onChange({ operator, value: day })} />
          </PhoneSheetGroup>
        </PhoneFilterValueScreen>
      ) : null}
      {picking === "range" ? (
        <PhoneFilterValueScreen property={property} onClose={close}>
          <PhoneDateFilterValue filter={{ ...filter, operator }} property={property} onChange={onChange} />
        </PhoneFilterValueScreen>
      ) : null}
      {picking === "options" ? (
        <PhoneFilterOptionsScreen property={property} value={filter.value} onClose={close}
          onChange={(value) => onChange({ operator, value })} />
      ) : null}
    </>
  );
}

/**
 * A rule's value row: a typed value in the row itself, "편집" at its end to
 * start and a clear button once set, as Notion's (seen 2026-10-21); a
 * select's "옵션 선택 ›", its chosen options' pills once set (seen
 * 2026-10-24); a checkbox's "값 선택 ›", people's "원하는 만큼 선택 ›" and
 * pages' "페이지 선택 ›" (seen 2026-10-23), each the chosen ones once set;
 * any other value's own control in the row.
 */
function PhoneRuleValue({ filter, property, operator, properties, snapshot, onChange, onOptions }: {
  filter: ViewFilter;
  property: DbProperty;
  operator: FilterOperator;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onChange(value: unknown): void;
  onOptions(): void;
}) {
  const pageNames = usePageNames();
  const labels = databaseViewLabels();
  const rowRef = useRef<HTMLDivElement>(null);
  const [editing, setEditing] = useState(false);
  if (OPTION_TYPES.has(property.type)) {
    const chosen = chosenFilterOptionList(property, filter.value);
    return (
      <PhoneSheetRow indent muted={!chosen.length} onPress={onOptions} data={{ "data-filter-rule-value": "" }}
        label={chosen.length ? (
          <span className={styles.pills}>{chosen.map((option) => <PhoneOptionPill key={option.id ?? option.name} option={option} />)}</span>
        ) : ruleText(labels.sheetPickOptions)} />
    );
  }
  if (SCREEN_TYPES.has(property.type)) {
    const chosen = property.type === "checkbox"
      ? typeof filter.value === "boolean" ? filter.value ? labels.checked : labels.unchecked : ""
      : filterValueText(filter.value, property, (id) => {
        const page = snapshot.pagesById[id];
        return page ? pageNames.title(page) : "";
      });
    const pick = property.type === "checkbox" ? labels.sheetPickValue : property.type === "relation" ? labels.sheetPickPage
      : labels.sheetPickPeople;
    return <PhoneSheetRow indent muted={!chosen} label={ruleText(chosen || pick)} onPress={onOptions} data={{ "data-filter-rule-value": "" }} />;
  }
  const typed = TEXT_TYPES.has(property.type) || NUMBER_TYPES.has(property.type);
  const value = operator === "relative_to_today" && isDateFilterProperty(property) ? dateFilterRelativeValue(filter) : filter.value;
  const empty = value === undefined || value === null || value === "";
  return (
    <div ref={rowRef} className={`${sheetStyles.row} ${styles.value}`} data-filter-rule-value="" data-typed={typed ? "true" : undefined}
      onFocus={(event) => { if (event.target instanceof HTMLInputElement) setEditing(true); }}
      onBlur={(event) => { if (event.target instanceof HTMLInputElement) setEditing(false); }}>
      <span className={sheetStyles.rowIcon} aria-hidden="true" />
      <span className={styles.control}>
        {TEXT_TYPES.has(property.type) ? (
          <input type="text" className={styles.typedInput} aria-label={labels.filterValueFor(property.name)} value={value == null ? "" : String(value)} placeholder={labels.sheetValue}
            onChange={(event) => onChange(event.target.value)} />
        ) : NUMBER_TYPES.has(property.type) ? (
          <NumberTextInput className={styles.typedInput} ariaLabel={labels.filterValueFor(property.name)} value={value} placeholder={labels.sheetValue} live onChange={onChange} />
        ) : (
          <DatabaseFilterValueInput prop={property} operator={operator} value={value} databaseProperties={properties} snapshot={snapshot} onChange={onChange} />
        )}
      </span>
      {typed && !editing ? (
        <button type="button" className={styles.edit} data-filter-value-edit onMouseDown={(event) => event.preventDefault()}
          onClick={() => rowRef.current?.querySelector("input")?.focus()}>
          {labels.sheetEditValue}
        </button>
      ) : null}
      {typed && !editing && !empty ? (
        <button type="button" className={styles.clear} aria-label={labels.sheetClearValue} data-filter-value-clear onMouseDown={(event) => event.preventDefault()}
          onClick={() => onChange(undefined)}>
          <XCircleIcon size="regular" aria-hidden="true" />
        </button>
      ) : null}
    </div>
  );
}
