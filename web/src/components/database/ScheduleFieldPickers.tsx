import { useEffect, useRef, useState } from "react";
import type { CalendarDate } from "@internationalized/date";
import { useTranslation } from "react-i18next";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { activeDateLocale } from "@/lib/i18n";
import { readCivilDate } from "@/lib/nativeDate";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { formatTime12, parseTimeDraft } from "./dateUtils";
import { CellMenuPortal } from "./PropertyCellMenu";
import cellStyles from "./databaseSheet";
import editorStyles from "./PropertyDateEditor.module.css";
import styles from "./DatabaseAutomation.module.css";
import ownStyles from "./ScheduleFieldPickers.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// A time and days, picked as the date editor picks them rather than with
// the browser's own pickers (2026-10-16: a native time field and date
// fields showing "연도. 월. 일.").

/** A time typed as the date editor takes it ("9:30", "오후 3:15") and shown on the 12-hour clock. */
function ScheduleTimeField({ label, value, onChange }: { label: string; value: string; onChange(time: string): void }) {
  const [draft, setDraft] = useState<string | null>(null);
  const commit = () => {
    if (draft === null) return;
    const parsed = parseTimeDraft(draft);
    if (parsed) onChange(parsed);
    setDraft(null);
  };
  return (
    <input type="text" aria-label={label} data-schedule-time placeholder={formatTime12("09:00")}
      value={draft ?? (value ? formatTime12(value) : "")}
      onChange={(event) => setDraft(event.target.value)} onBlur={commit}
      onKeyDown={(event) => {
        if (imeComposing(event)) return;
        if (fieldEnter(event)) { event.preventDefault(); commit(); }
        else if (event.key === "Escape" && draft !== null) { event.stopPropagation(); setDraft(null); }
      }} />
  );
}

/** A day picked on the app's calendar, for any form; one with a clear choice can be left empty. */
export function AppDateField({ label, value, min, emptyLabel, clearLabel, disabled, onChange }: {
  label: string;
  value: string;
  min?: string;
  /** What the field says while no day is chosen. */
  emptyLabel?: string;
  /** Given for a day that may be emptied again: the choice under the calendar that does it. */
  clearLabel?: string;
  disabled?: boolean;
  onChange(day: string): void;
}) {
  const { t } = useTranslation("propertyCell");
  const weekStart = useWeekStartDay();
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const day = readCivilDate(value);
  const earliest = min ? readCivilDate(min) : null;
  // The day the keys move, from the chosen one or the earliest allowed.
  const [focused, setFocused] = useState<CalendarDate | null>(null);
  const shown = day
    ? new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long", timeZone: "UTC" })
      .format(new Date(Date.UTC(day.year, day.month - 1, day.day)))
    : emptyLabel ?? "";
  return (
    <>
      <button ref={trigger} type="button" className={styles.scheduleDateButton} aria-label={`${label}: ${shown}`}
        aria-haspopup="dialog" aria-expanded={open} data-schedule-date data-empty={day ? undefined : "true"} disabled={disabled}
        onClick={() => { setFocused(day ?? earliest); setOpen((current) => !current); }}>
        {shown}
      </button>
      {open ? (
        <CellMenuPortal triggerRef={trigger} onClose={() => setOpen(false)} ariaLabel={label} className={editorStyles.choiceMenu}
          backdropClassName={cellStyles.menuBackdrop} width={260}>
          <NativeDateCalendar value={day} focusedValue={focused ?? day ?? earliest ?? undefined} onFocusChange={setFocused}
            weekStart={weekStart} label={label} autoFocus
            previousLabel={t("prevMonth")} nextLabel={t("nextMonth")}
            classes={{ head: editorStyles.calendarHead, weekdays: cellStyles.dateWeekdays, grid: cellStyles.dateGrid }}
            isSelected={(candidate) => Boolean(day && candidate.compare(day) === 0)}
            onPick={(picked) => {
              if (earliest && picked.compare(earliest) < 0) return;
              onChange(picked.toString());
              setOpen(false);
            }} />
          {clearLabel !== undefined && day ? (
            <button type="button" className={styles.scheduleDateClear} data-schedule-date-clear
              onClick={() => { onChange(""); setOpen(false); }}>
              {clearLabel}
            </button>
          ) : null}
        </CellMenuPortal>
      ) : null}
    </>
  );
}

/**
 * A day and a time picked as the app picks them, side by side, holding what
 * a browser "datetime-local" field held ("2026-10-21T09:30"): nothing until
 * both are picked.
 */
export function AppDateTimeField({ label, timeLabel, value, min, emptyLabel, clearLabel, disabled, onChange }: {
  label: string;
  timeLabel: string;
  value: string;
  min?: string;
  emptyLabel?: string;
  clearLabel?: string;
  disabled?: boolean;
  onChange(value: string): void;
}) {
  const split = (whole: string) => ({ day: whole.slice(0, 10), time: whole.slice(11, 16) });
  const [parts, setParts] = useState(() => split(value));
  // A value set from outside wins; one cleared from outside clears a whole pick.
  useEffect(() => {
    setParts((current) => value ? split(value) : current.day && current.time ? { day: "", time: "" } : current);
  }, [value]);
  const change = (next: { day: string; time: string }) => {
    setParts(next);
    onChange(next.day && next.time ? `${next.day}T${next.time}` : "");
  };
  return (
    <span className={ownStyles.dateTime} data-app-date-time>
      <AppDateField label={label} value={parts.day} min={min} emptyLabel={emptyLabel} clearLabel={clearLabel} disabled={disabled}
        onChange={(day) => change({ ...parts, day })} />
      <ScheduleTimeField label={timeLabel} value={parts.time} onChange={(time) => change({ ...parts, time })} />
    </span>
  );
}
