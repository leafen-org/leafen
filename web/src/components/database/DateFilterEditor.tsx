"use client";

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { activeDateLocale } from "@/lib/i18n";
import type { DbProperty, FilterOperator, RelativeDateFilterValue, ViewFilter } from "@/lib/types";
import { dateRangeFilterValue } from "@/shared/database/queryFilterPolicy";
import { isRelativeFilterDay, relativeDateFilterDays, relativeFilterDay } from "@/shared/database/queryRelativeDate.mjs";
import { DateFilterDayChoice, relativeFilterDayText } from "./DateFilterDayChoice";
import { DateTextInput } from "./DateTextInput";
import { RelativeDateFilterValueInput } from "./RelativeDateFilterValueInput";
import { dateFilterLabels, NO_VALUE_FILTERS } from "./databaseFilterLabels";
import { dateFilterRelativeValue, effectiveFilterOperator, filterOperatorLabel } from "./databaseFilterPolicy";
import { databaseViewLabels } from "./databaseViewLabels";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { dateKey, parseDate } from "./dateUtils";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { civilDate, readCivilDate } from "@/lib/nativeDate";
import styles from "./DateFilterEditor.module.css";

const DAY_OPERATORS = new Set<FilterOperator>(["equals", "before", "after", "on_or_before", "on_or_after"]);

/** The field a date filter's operator shows for its value: a day's, a range's two, or none. */
export function dateFilterField(operator: FilterOperator) {
  return DAY_OPERATORS.has(operator) ? "day" as const : operator === "is_between" ? "range" as const : null;
}

/** Whether a filter's property takes the date filter editor. */
export function isDateFilterProperty(property: DbProperty) {
  return property.type === "date" || property.type === "created_time" || property.type === "last_edited_time";
}

/**
 * Whether a date filter's head names its date part ("시작일 ˅"): a date's, a
 * formula's date or a rollup's date range, with a day to compare; a rollup's
 * earliest or latest day is one day and shows its name (Notion's, seen 2026-11-11).
 */
export function dateFilterShowsPart(property: DbProperty, operator: FilterOperator) {
  return property.type === "date" && !(property.config as { dateFilterOneDay?: boolean } | undefined)?.dateFilterOneDay
    && operator !== "is_empty" && operator !== "is_not_empty";
}

/**
 * The change a date filter's new operator makes, as Notion's (seen
 * 2026-10-01 and 2026-11-11, desktop and phone): a day stays between the day
 * operators, "오늘 기준" starts at this week, and "범위 내" starts empty. Into
 * a day from "오늘 기준" or "범위 내" a pill's day starts empty, as a desktop
 * conditional colour's, and a rule's (`rule`: an advanced filter's, and on a
 * phone a conditional colour's too) is "오늘"; from "비어 있음" either starts
 * empty.
 */
export function dateFilterOperatorChange(filter: ViewFilter, next: FilterOperator, rule = false): Partial<ViewFilter> {
  if (next === "relative_to_today") return { operator: next, value: { direction: "this", unit: "week" } };
  if (next === "is_between") return { operator: next, value: {} };
  if (DAY_OPERATORS.has(next)) {
    if (DAY_OPERATORS.has(filter.operator)) return { operator: next, value: typeof filter.value === "string" ? filter.value : undefined };
    const fromSpan = filter.operator !== "is_empty" && filter.operator !== "is_not_empty";
    return { operator: next, value: rule && fromSpan ? "today" : undefined };
  }
  return { operator: next, value: undefined };
}

function today() {
  return dateKey(new Date());
}

function dayOf(key: string | undefined) { return key ? parseDate(key) : null; }

/** A day as a pill writes it: "10월 7일", with its year when not this year's. */
function dateFilterDayText(key: unknown) {
  const day = dayOf(typeof key === "string" ? dateKey(key) : undefined);
  if (!day) return "";
  const thisYear = day.getFullYear() === new Date().getFullYear();
  return day.toLocaleDateString(activeDateLocale(), { month: "long", day: "numeric", ...(thisYear ? {} : { year: "numeric" }) });
}

/**
 * What a date filter's pill says after the colon, as Notion's (seen
 * 2026-10-01): "10월 7일", "이전 10월 7일", "10월 5일 → 10월 9일", "시작일
 * 10월 5일", "이번 주", "지난 3 주", "비어 있음".
 */
export function dateFilterSummary(filter: ViewFilter, property: DbProperty): string {
  const operator = effectiveFilterOperator(property, filter.operator);
  const labels = dateFilterLabels();
  if (operator === "relative_to_today") {
    const value = dateFilterRelativeValue(filter);
    const direction = databaseViewLabels().relativeDateDirections[value.direction];
    return labels.pillRelative(direction, value.direction === "this" ? 1 : value.count, value.unit);
  }
  if (operator === "is_between") {
    const range = dateRangeFilterValue(filter.value);
    const start = dateFilterDayText(range.start);
    const end = dateFilterDayText(range.end);
    if (start && end) return labels.pillRange(start, end);
    return start ? labels.pillRangeStart(start) : end ? labels.pillRangeEnd(end) : "";
  }
  if (operator === "is_empty" || operator === "is_not_empty") return filterOperatorLabel(operator, property);
  const day = relativeFilterDayText(filter.value) || dateFilterDayText(filter.value);
  if (!day) return "";
  return operator === "equals" ? day : labels.pill(operator as "before" | "after" | "on_or_before" | "on_or_after", day);
}

/**
 * A date filter's value under its editor's head, as Notion's (seen
 * 2026-10-01): a day field (or two for "범위 내") or the relative date
 * (지난·다음·이번, a count, a unit), then a month calendar, Sunday first,
 * marking the chosen days, and under a relative date a note that the filter
 * follows today's date. Picking a day in the calendar sets it; for a range, its
 * ends in turn (useRangePick).
 */
export function DateFilterValue({ filter, property, onChange }: {
  filter: ViewFilter;
  property: DbProperty;
  onChange(patch: Partial<ViewFilter>): void;
}) {
  const labels = dateFilterLabels();
  const viewLabels = databaseViewLabels();
  const operator = effectiveFilterOperator(property, filter.operator);
  if (NO_VALUE_FILTERS.has(operator) && operator !== "relative_to_today") return null;
  if (operator === "relative_to_today") {
    const value = dateFilterRelativeValue(filter);
    const days = relativeDateFilterDays("relative_to_today", today(), value);
    return (
      <div className={styles.value} data-date-filter-value="relative">
        <RelativeDateFilterValueInput label={property.name} value={value}
          onChange={(next: RelativeDateFilterValue) => onChange({ operator, value: next })} />
        <DateFilterCalendar start={days?.start} end={days?.end} />
        <p className={styles.note}>{labels.relativeNote}</p>
      </div>
    );
  }
  if (operator === "is_between") return <DateRangeFilterValue filter={filter} property={property} onChange={onChange} />;
  // A day named from today shows its words in the same field, which then
  // takes no typing and no "ⓧ", its day in the calendar and under it the note
  // that the filter follows today's date; the field's ⌄ chooses one, and while
  // the field holds a typed or set day its "ⓧ" is before the ⌄ (Notion's, seen
  // 2026-11-11, 2026-11-12, 2026-11-16 and 2026-11-22 at 1440 dark, dpr 2).
  const named = isRelativeFilterDay(filter.value);
  const day = named ? relativeFilterDay(filter.value, today()) ?? "" : typeof filter.value === "string" ? dateKey(filter.value) : "";
  return (
    <div className={styles.value} data-date-filter-value="day" data-named={named ? "true" : undefined}>
      <div className={styles.dayField} data-day-field="">
        {named ? (
          <input className={styles.field} value={relativeFilterDayText(filter.value)} disabled
            aria-label={viewLabels.filterValueFor(property.name)} />
        ) : (
          <DateTextInput className={styles.field} value={day} placeholder={labels.dayPlaceholder}
            ariaLabel={viewLabels.filterValueFor(property.name)} clear={{ className: styles.dayClear }}
            onChange={(next) => onChange({ operator, value: next ?? undefined })} />
        )}
        <DateFilterDayChoice inField label={property.name} value={filter.value} className={styles.dayChoice}
          buttonClassName={styles.dayChoiceButton} onChange={(next) => onChange({ operator, value: next })} />
      </div>
      <DateFilterCalendar start={day || undefined} end={day || undefined} onPick={(next) => onChange({ operator, value: next })} />
      {named ? <p className={styles.note}>{labels.relativeNote}</p> : null}
    </div>
  );
}

/** Which end of a "범위 내" range its calendar sets next. */
export type RangeEnd = "start" | "end";

/**
 * The end a range's calendar sets first, as Notion's (seen 2026-11-12,
 * desktop and phone): the end once the start alone is set, else the start.
 */
export function firstRangeEnd(range: { start?: string; end?: string }): RangeEnd {
  return range.start && !range.end ? "end" : "start";
}

/**
 * A day picked in a range's calendar, as Notion's (seen 2026-11-12, desktop
 * and phone): it sets the end due, keeping the other, and the other end
 * comes due; a day past the other end (before the start, after the end) is
 * not taken.
 */
export function pickRangeDay(range: { start?: string; end?: string }, due: RangeEnd, day: string) {
  if (rangeDayClosed(range, due, day)) return null;
  return due === "start" ? { range: { ...range, start: day }, due: "end" as const } : { range: { ...range, end: day }, due: "start" as const };
}

function rangeDayClosed(range: { start?: string; end?: string }, due: RangeEnd | undefined, day: string) {
  return due === "end" ? Boolean(range.start && day < range.start) : due === "start" ? Boolean(range.end && day > range.end) : false;
}

/** The end a range's calendar sets next and the pick that sets it, kept while the editor is open; a field taking the focus makes its end due. */
export function useRangePick(range: { start?: string; end?: string }, write: (next: { start?: string; end?: string }) => void) {
  const [due, setDue] = useState<RangeEnd>(() => firstRangeEnd(range));
  const pick = (day: string) => {
    const next = pickRangeDay(range, due, day);
    if (!next) return;
    setDue(next.due);
    write(next.range);
  };
  return { due, setDue, pick };
}

/** "범위 내": its two fields side by side, then the month (DateFilterValue). */
function DateRangeFilterValue({ filter, property, onChange }: { filter: ViewFilter; property: DbProperty; onChange(patch: Partial<ViewFilter>): void }) {
  const labels = dateFilterLabels();
  const operator = effectiveFilterOperator(property, filter.operator);
  const range = dateRangeFilterValue(filter.value);
  const write = (next: { start?: string; end?: string }) => onChange({
    operator,
    value: { ...(next.start ? { start: next.start } : {}), ...(next.end ? { end: next.end } : {}) },
  });
  const { due, setDue, pick } = useRangePick(range, write);
  return (
    <div className={styles.value} data-date-filter-value="range">
      <div className={styles.range}>
        <DateTextInput className={styles.field} value={range.start ?? ""} placeholder={labels.parts.start} onFocus={() => setDue("start")}
          ariaLabel={labels.rangeStartFor(property.name)} onChange={(start) => write({ ...range, start: start ?? undefined })} />
        <DateTextInput className={styles.field} value={range.end ?? ""} placeholder={labels.parts.end} onFocus={() => setDue("end")}
          ariaLabel={labels.rangeEndFor(property.name)} onChange={(end) => write({ ...range, end: end ?? undefined })} />
      </div>
      <DateFilterCalendar start={range.start} end={range.end} due={due} onPick={pick} />
    </div>
  );
}

/**
 * A month of days, from the week's first day, as Notion's filter calendar (seen
 * 2026-10-01): its month and ‹ › at the top, six weeks of 28px days, the
 * first and last marked days filled and those between tinted; one only
 * shown opens on its first marked day's month.
 */
export function DateFilterCalendar({ start, end, due, onPick, phone }: {
  start?: string; end?: string; onPick?(day: string): void;
  /** A range's end set next: the days past its other end are dim and take no press, and the other end, set last, is drawn lighter (Notion's, seen 2026-11-12). */
  due?: RangeEnd;
  /** In a phone's sheet: the card's 36px days on a 44px grid, today a red circle, its weekdays a 44px row (Notion's, seen 2026-10-23 and 2026-11-11). */
  phone?: boolean;
}) {
  const { t } = useTranslation("propertyCell");
  const labels = dateFilterLabels();
  const [focused, setFocused] = useState(() => readCivilDate(start ?? "") ?? civilDate(new Date()));
  // Its first day moved — "이번" turned "지난" — the month shown moves to it (Notion's, seen 2026-11-06).
  useEffect(() => { const first = readCivilDate(start ?? ""); if (first) setFocused(first); }, [start]);
  const weekStart = useWeekStartDay();
  return <div data-date-filter-calendar>
    <NativeDateCalendar value={readCivilDate(start ?? "")} focusedValue={focused} onFocusChange={setFocused}
      onPick={onPick ? date => onPick(date.toString()) : undefined} weekStart={weekStart}
      label={t("editDateProperty")} previousLabel={labels.previousMonth} nextLabel={labels.nextMonth}
      classes={phone ? { root: styles.phoneCalendar, head: `${styles.phoneCalendarHead} ${styles.filterHead}`, title: styles.phoneMonth,
        shift: styles.shift, weekdays: `${styles.phoneWeekdays} ${styles.filterWeekdays}`, grid: `${styles.phoneDays} ${styles.filterDays}`,
        day: `${styles.day} ${styles.phoneDay}` } : {
        root: styles.calendar, head: styles.calendarHead, title: styles.month, shift: styles.shift,
        weekdays: styles.weekdays, grid: styles.days, day: styles.day }}
      isSelected={date => date.toString() === start || date.toString() === end}
      isUnavailable={due ? date => rangeDayClosed({ start, end }, due, date.toString()) : undefined}
      marker={date => { const key = date.toString(); return {
        "data-mark": start && end && key >= start && key <= end
          ? key === start ? "start" : key === end ? "end" : "between" : key === start || key === end ? "start" : undefined,
        "data-single": start === end && key === start ? "true" : undefined,
        // A range with one end picked so far marks that end alone (Notion's phone, seen 2026-11-11).
        "data-lone": (start ? !end && key === start : Boolean(end) && key === end) ? "true" : undefined,
        // The end set last is the lighter box: the end until the start is
        // picked again, and a relative date's end (Notion's, seen 2026-11-12).
        "data-picked": start !== end && key === (due === "end" ? start : end) ? "true" : undefined,
        // A relative date's calendar, only shown, grays the days after its range (Notion's, seen 2026-11-06).
        "data-after-range": !onPick && end && key > end ? "true" : undefined,
      }; }} />
  </div>;
}
