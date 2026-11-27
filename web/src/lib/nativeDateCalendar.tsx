import { useRef, type ReactNode } from "react";
import { CalendarDate, today, getLocalTimeZone, startOfWeek, endOfWeek, type DateValue } from "@internationalized/date";
import { I18nProvider, useButton, useCalendar, useCalendarCell, useCalendarGrid } from "react-aria";
import { useCalendarState, type CalendarState } from "react-stately";
import { ChevronLeft, ChevronRight } from "@/icons/leafen";
import { activeDateLocale } from "./i18n";
import { weekdayHeads } from "./dateWords";
import { nativeCalendar } from "./nativeDate";

type CalendarClasses = { root?: string; head?: string; title?: string; shift?: string; weekdays?: string; grid?: string; day?: string };
type NativeCalendarProps = {
  value: CalendarDate | null; focusedValue?: CalendarDate | null; onFocusChange?(date: CalendarDate): void;
  onPick?(date: CalendarDate): void; label: string; previousLabel: string; nextLabel: string; weekStart: 0 | 1;
  classes: CalendarClasses; headerExtra?: ReactNode; previousFirst?: boolean;
  marker?(date: CalendarDate): Record<string, string | undefined>;
  isSelected?(date: CalendarDate): boolean;
  /** The earliest day one can pick; earlier ones stay in the grid, dim. */
  min?: CalendarDate;
  /** Days that stay in the grid and in reach of the keyboard but take no press, marked data-unavailable. */
  isUnavailable?(date: DateValue): boolean;
  /** The focused day takes the focus as the calendar opens, for a picker that opens on its own. */
  autoFocus?: boolean;
};

/** Calendar state owns day/month movement, roving focus and activation.
 * Native date values, formatting, range marks and writes stay with the caller. */
export function NativeDateCalendar(props: NativeCalendarProps) {
  return <I18nProvider locale={activeDateLocale()}><DateCalendar {...props} /></I18nProvider>;
}

function DateCalendar({ value, focusedValue, onFocusChange, onPick, label, previousLabel, nextLabel, weekStart,
  classes, headerExtra, previousFirst, marker, isSelected, autoFocus, min, isUnavailable }: NativeCalendarProps) {
  const firstDayOfWeek = weekStart ? "mon" : "sun";
  const props = { value, focusedValue, onFocusChange, onChange: (date: CalendarDate) => onPick?.(date),
    isReadOnly: !onPick, isDateUnavailable: isUnavailable, firstDayOfWeek, weeksInMonth: 6, "aria-label": label, autoFocus,
    minValue: min ?? new CalendarDate(100, 1, 1), maxValue: new CalendarDate(9999, 12, 31) } as const;
  const engine = useCalendarState({ ...props, locale: activeDateLocale(), createCalendar: nativeCalendar });
  // Native six-row pickers admit adjacent-month days and keep Home/End at
  // the chosen week's boundaries. Public state policy methods provide these
  // differences; Aria still dispatches every key, focus and press operation.
  const state: CalendarState = { ...engine,
    // Native filters select both stored endpoints; their two-step write
    // policy remains with the filter rather than a second range state.
    isSelected: isSelected ?? engine.isSelected,
    isCellDisabled: date => engine.isDisabled || engine.isInvalid(date),
    focusSectionStart: () => engine.setFocusedDate(startOfWeek(engine.focusedDate, "en-US", firstDayOfWeek)),
    focusSectionEnd: () => engine.setFocusedDate(endOfWeek(engine.focusedDate, "en-US", firstDayOfWeek)),
  };
  const { calendarProps, prevButtonProps, nextButtonProps } = useCalendar(props, state);
  const { gridProps, headerProps, weeksInMonth } = useCalendarGrid({ firstDayOfWeek }, state);
  // Its month by the short name with the year, over the days' short heads,
  // as Notion's date pickers (seen 2026-11-19 in a date cell's and an
  // automation's "Starting": "Nov 2026" over "Su Mo Tu We Th Fr Sa", "2026년
  // 11월" over "일 월 화 …", "Nov. 2026" over "So Mo Di …"; dateWords.ts).
  const month = new Intl.DateTimeFormat(activeDateLocale(), { year: "numeric", month: "short", timeZone: state.timeZone })
    .format(state.visibleRange.start.toDate(state.timeZone));
  const heads = weekdayHeads(), weekDays = weekStart ? [...heads.slice(1), heads[0]] : heads;
  const previous = <CalendarControl props={{ ...prevButtonProps, "aria-label": previousLabel }} className={classes.shift}><ChevronLeft size="small" /></CalendarControl>;
  const next = <CalendarControl props={{ ...nextButtonProps, "aria-label": nextLabel }} className={classes.shift}><ChevronRight size="small" /></CalendarControl>;
  return <div {...calendarProps} className={classes.root}>
    <div className={classes.head}>
      {previousFirst && previous}<span className={classes.title}>{month}</span>{headerExtra}
      {!previousFirst && previous}{next}
    </div>
    <div {...gridProps}>
      <div {...headerProps}><div role="row" className={classes.weekdays}>
        {weekDays.map((day, index) => <span role="columnheader" key={index}>{day}</span>)}
      </div></div>
      <div role="rowgroup" className={classes.grid}>
        {Array.from({ length: weeksInMonth }, (_, week) => <div role="row" key={week} style={{ display: "contents" }}>
          {state.getDatesInWeek(week).map((date, day) => date ? <DateCell key={date.toString()} date={date} state={state}
            className={classes.day} marker={marker?.(date)} selectable={!!onPick} /> : <div role="gridcell" key={day} />)}
        </div>)}
      </div>
    </div>
  </div>;
}
function CalendarControl({ props, className, children }: { props: Parameters<typeof useButton>[0]; className?: string; children: ReactNode }) {
  const ref = useRef<HTMLButtonElement>(null), { buttonProps } = useButton(props, ref);
  return <button {...buttonProps} ref={ref} type="button" className={className}>{children}</button>;
}
function DateCell({ date, state, className, marker, selectable }: {
  date: CalendarDate; state: CalendarState; className?: string; marker?: Record<string, string | undefined>; selectable: boolean;
}) {
  const ref = useRef<HTMLButtonElement>(null);
  const outside = date.month !== state.visibleRange.start.month;
  // Adjacent-month days remain selectable in the native six-row picker.
  const { cellProps, buttonProps, isSelected, isFocused, isUnavailable, formattedDate } = useCalendarCell({ date }, state, ref);
  return <div {...cellProps} style={{ display: "contents" }}>
    <button {...buttonProps} ref={ref} type="button" disabled={!selectable} className={className}
      data-date={date.toString()} data-date-day={date.toString()} data-outside={outside ? "true" : undefined}
      data-selected={isSelected ? "true" : undefined} data-focused={isFocused ? "true" : undefined} data-unavailable={isUnavailable ? "true" : undefined}
      data-today={date.compare(today(getLocalTimeZone())) === 0 ? "true" : undefined} {...marker}>
      {formattedDate}
    </button>
  </div>;
}
