import { useRef, useState } from "react";
import type { CalendarDate } from "@internationalized/date";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { activeDateLocale } from "@/lib/i18n";
import { readCivilDate } from "@/lib/nativeDate";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { pickInRange, rangeCalendarMarks, type DateRangeEnd } from "./dateRangeCalendar";
import { dateKey, formatDateDraft, parseDate, parseDateDraftIn } from "./dateUtils";
import { CellMenuPortal } from "./PropertyCellMenu";
import { DateDraftInput } from "./PropertyDateEditorOptions";
import cellStyles from "./databaseSheet";
import editorStyles from "./PropertyDateEditor.module.css";
import styles from "./DatabaseSprintComplete.module.css";

/** A day as the sprint dialog names it: "12월 14일". */
export function sprintDay(day: string) {
  const date = readCivilDate(day);
  return date ? new Intl.DateTimeFormat(activeDateLocale(), { month: "long", day: "numeric", timeZone: "UTC" })
    .format(new Date(Date.UTC(date.year, date.month - 1, date.day))) : "";
}

/**
 * The next sprint's dates in the sprint dialog, as Notion's (seen
 * 2026-11-03): "12월 14일 → 12월 27일 ⌄" opens under it, 256 wide, the start
 * and end to type, the range on a calendar and "지우기", which leaves the
 * sprint undated.
 */
export function DatabaseSprintDates({ label, start, end, disabled, onChange }: {
  label: string; start: string; end: string; disabled?: boolean;
  onChange(start: string, end: string): void;
}) {
  const { t } = useTranslation("propertyCell");
  const weekStart = useWeekStartDay();
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<DateRangeEnd>("start");
  const [focused, setFocused] = useState<CalendarDate | null>(null);
  const draft = (day: string) => formatDateDraft(parseDate(day));
  const [drafts, setDrafts] = useState({ start: draft(start), end: draft(end) });
  const set = (nextStart: string, nextEnd: string) => {
    setDrafts({ start: draft(nextStart), end: draft(nextEnd) });
    onChange(nextStart, nextEnd);
  };
  // A typed day replaces its end; one past the other end moves both.
  const commit = (side: DateRangeEnd) => {
    const typed = parseDateDraftIn(drafts[side]);
    if (!typed) return setDrafts({ start: draft(start), end: draft(end) });
    const day = dateKey(typed);
    if (side === "start") set(day, end && end >= day ? end : day);
    else set(start && start <= day ? start : day, day);
  };
  const shown = start ? `${sprintDay(start)} → ${sprintDay(end || start)}` : t("databaseTasks:sprintUndated");
  const anchor = readCivilDate(active === "end" && end ? end : start);
  return (
    <>
      <button ref={trigger} type="button" className={styles.rowValue} aria-haspopup="dialog" aria-expanded={open}
        aria-label={`${label}: ${shown}`} disabled={disabled} data-sprint-dates
        onClick={() => { setDrafts({ start: draft(start), end: draft(end) }); setFocused(anchor); setOpen((current) => !current); }}>
        <span>{shown}</span><ChevronDown size="tiny" />
      </button>
      {open ? (
        <CellMenuPortal triggerRef={trigger} onClose={() => setOpen(false)} ariaLabel={label} className={editorStyles.choiceMenu}
          backdropClassName={cellStyles.menuBackdrop} width={256}>
          <div className={editorStyles.inputs} onFocus={(event) => setActive((event.target as HTMLElement).dataset.dateInput === "end" ? "end" : "start")}>
            <DateDraftInput label={t("databaseTasks:startDate")} value={drafts.start} data="start" autoFocus active={active === "start"}
              onChange={(value) => setDrafts((current) => ({ ...current, start: value }))}
              onCommit={() => commit("start")} onReset={() => setDrafts((current) => ({ ...current, start: draft(start) }))} />
            <DateDraftInput label={t("databaseTasks:endDate")} value={drafts.end} data="end" active={active === "end"}
              onChange={(value) => setDrafts((current) => ({ ...current, end: value }))}
              onCommit={() => commit("end")} onReset={() => setDrafts((current) => ({ ...current, end: draft(end) }))} />
          </div>
          <NativeDateCalendar value={anchor} focusedValue={focused ?? anchor ?? undefined} onFocusChange={setFocused}
            weekStart={weekStart} label={label} previousLabel={t("prevMonth")} nextLabel={t("nextMonth")}
            classes={{ head: editorStyles.calendarHead, weekdays: cellStyles.dateWeekdays, grid: cellStyles.dateGrid }}
            {...rangeCalendarMarks(start || null, end || null, active)}
            onPick={(picked) => {
              const day = picked.toString();
              if (!start) return set(day, day);
              const next = pickInRange(start, end || start, day, active);
              setActive(next.active);
              set(next.start, next.end);
            }} />
          <button type="button" className={styles.datesClear} data-sprint-dates-clear
            onClick={() => { set("", ""); setOpen(false); }}>
            {t("clear")}
          </button>
        </CellMenuPortal>
      ) : null}
    </>
  );
}
