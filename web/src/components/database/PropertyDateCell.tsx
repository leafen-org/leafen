import { Children, useRef, useState, type ReactNode } from "react";
import { useCellText } from "./cellText";
import { CalendarIcon, ClockIcon } from "@/icons/leafen";
import { chosenTimeZone, clockForDay, localTimeZone, reminderChoiceKey, reminderChoices, reminderDateDue, reminderValueKey, safeDateReminderValue } from "@/lib/dateReminders";
import { storedDeadline } from "@/lib/deadlines";
import { differenceInCalendarDays } from "@/lib/calendarDay";
import type { DbProperty } from "@/lib/types";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { addDays, dateKey, extractEnd, extractTime, formatDateDraft, formatDateForProperty, formatTimeDraft, parseDate, parseDateDraftIn, parseTimeDraft, shownDate, startOfMonth, storedTimeZone, writtenDate, writtenOffset } from "./dateUtils";
import { rememberTimeZone, wallNow, wallNowAtOffset } from "./dateTimeZones";
import { DATE_FORMATS, DateChoiceRow, DateDraftInput, DateSwitchRow, DateZoneRow, TIME_FORMATS } from "./PropertyDateEditorOptions";
import { pickInRange, rangeCalendarMarks, type DateRangeEnd } from "./dateRangeCalendar";
import { useSingleWrite } from "./useSingleWrite";
import { DateEditorFrame } from "./DateEditorFrame";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { DeadlineFlag, PropertyDateDeadline } from "./PropertyDateDeadline";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { civilDate, localDate } from "@/lib/nativeDate";
import styles from "./databaseSheet";
import editorStyles from "./PropertyDateEditor.module.css";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";

type DateConfig = Pick<NonNullable<DbProperty["config"]>, "dateFormat" | "timeFormat">;

export function PropertyDateCell({
  prop,
  value,
  rowId,
  autoFocus,
  templateValues = false,
  emptyLabel,
  inline = false,
  onCommit,
  onPropertyConfig,
}: {
  prop: DbProperty;
  value: unknown;
  /** The item the date is on, for its deadline's notices. */
  rowId?: string;
  autoFocus?: boolean;
  /** A database template's date, which may be today or now "when duplicated". */
  templateValues?: boolean;
  /** What an empty date's button says, as a new list row's "마감일 추가" (Notion's, seen 2026-11-05). */
  emptyLabel?: string;
  /** Standing in a line of words (a new list row's date): its button as wide as what it says, bare. */
  inline?: boolean;
  /** A date as "start" or "start/end", or with a reminder, a deadline or its own zone as {start, end, reminder, deadline, timeZone}. */
  onCommit: (value: string | Record<string, string> | null) => void;
  /** Sets the property's date and time formats; absent where its properties may not change. */
  onPropertyConfig?: (patch: DateConfig) => void;
}) {
  const t = useCellText();
  const readOnly = useCellReadOnly();
  const dateFormat = prop.config?.dateFormat;
  const timeFormat = prop.config?.timeFormat;
  // The editor's inputs keep the date's own clock, its day as written, while
  // its calendar marks the day the date shows on for the reader, as Notion's.
  const selected = writtenDate(value);
  const shownDay = shownDate(value)?.date ?? null;
  const [open, setOpen] = useState(false);
  const [month, setMonth] = useState(() => startOfMonth(shownDay ?? new Date()));
  const [focusKey, setFocusKey] = useState(() => dateKey(shownDay ?? new Date()));
  const [includeTime, setIncludeTime] = useState(() => !!extractTime(value));
  // A date given a time takes its day's clock (clockForDay).
  const [time, setTime] = useState(() => extractTime(value) || timeFor(value));
  const [dateDraft, setDateDraft] = useState("");
  // An empty date's input shows today as Notion's does; left as it is, the
  // date stays empty.
  const [todayDraft, setTodayDraft] = useState("");
  const [timeDraft, setTimeDraft] = useState("");
  const [includeEnd, setIncludeEnd] = useState(() => !!extractEnd(value));
  const [endKey, setEndKey] = useState(() => writtenEndKey(value) || dateKey(selected ?? new Date()));
  const [endTime, setEndTime] = useState(() => extractTime(extractEnd(value)) || extractTime(value) || timeFor(value));
  const [endDraft, setEndDraft] = useState("");
  const [endTimeDraft, setEndTimeDraft] = useState("");
  // The end being edited, which the calendar marks in full.
  const [activeEnd, setActiveEnd] = useState<DateRangeEnd>("start");
  const inputsRef = useRef<HTMLDivElement>(null);
  // A new date takes its property's reminder, as Notion's "알림" in the
  // property's settings sets it (seen 2026-10-05); a date already set keeps its own.
  const initialReminder = () => isEmptyDate(value)
    ? String((prop.config as { defaultReminder?: unknown } | undefined)?.defaultReminder ?? "") : storedReminder(value);
  const [reminder, setReminder] = useState(initialReminder);
  // A deadline not to miss is Leafen's own (PropertyDateDeadline).
  const [deadline, setDeadline] = useState(() => storedDeadline(value));
  const triggerRef = useRef<HTMLButtonElement>(null);
  const displayDate = formatDateForProperty(value, prop);
  const ownZone = storedTimeZone(value);
  const ownOffset = writtenOffset(value);
  const dayDraft = (date: Date | null) => formatDateDraft(date, dateFormat);
  const clockDraft = (next: string) => formatTimeDraft(next, timeFormat);
  // One pick is one write (useSingleWrite).
  const write = useSingleWrite(value, onCommit);

  // A reminder, as in Notion, reminds the person who sets it; it keeps the
  // time zone it was set in and follows the date while it stays a valid
  // choice for it. A date that keeps its own time zone keeps it through an
  // edit, and its reminder follows that zone; a zone chosen for the date keeps
  // its written times, as Notion's does.
  function commitValue(next: string | null, nextReminder = reminder, zone = ownZone, nextDeadline = deadline) {
    // A time written with an offset of its own keeps it, until a zone is chosen.
    const offset = zone ? "" : ownOffset;
    const clocked = (part: string) => offset && /T\d{2}:\d{2}$/u.test(part) ? part + offset : part;
    const [start, end] = next ? next.split("/").map(clocked) : [];
    const kept = start ? safeDateReminderValue(nextReminder, start) : undefined;
    if (!next || (!kept && !zone && !nextDeadline)) {
      write(next ? [start, end].filter(Boolean).join("/") : null);
      return;
    }
    write({
      start, ...(end ? { end } : {}), ...(kept ? { reminder: kept } : {}), ...(nextDeadline ? { deadline: nextDeadline } : {}),
      timeZone: zone || (/T\d{2}:\d{2}/u.test(start) ? localTimeZone() : chosenTimeZone()),
    });
  }

  function chronologicalEnd(startDate: Date, candidateEndKey = endKey, startTime = time, candidateEndTime = endTime) {
    const startKey = dateKey(startDate);
    if (candidateEndKey < startKey) return { key: startKey, time: startTime };
    if (candidateEndKey === startKey && candidateEndTime < startTime) return { key: startKey, time: startTime };
    return { key: candidateEndKey, time: candidateEndTime };
  }

  // Build the stored value from a start date plus the time/range options. Explicit
  // overrides avoid reading not-yet-committed state when a toggle changes.
  function buildValue(
    startDate: Date,
    o: { includeTime?: boolean; time?: string; includeEnd?: boolean; endKey?: string; endTime?: string } = {}
  ) {
    const withTime = o.includeTime ?? includeTime;
    const startTime = o.time ?? time;
    const end = chronologicalEnd(startDate, o.endKey ?? endKey, startTime, o.endTime ?? endTime);
    const start = dateKey(startDate) + (withTime ? `T${startTime}` : "");
    if (!(o.includeEnd ?? includeEnd)) return start;
    return `${start}/${end.key}${withTime ? `T${end.time}` : ""}`;
  }
  const weekStart = useWeekStartDay();

  // A day picked on the calendar is the reader's day; a date on a clock of
  // its own moves by as many days.
  function writtenDayFor(day: Date) {
    return selected && shownDay ? addDays(selected, differenceInCalendarDays(day, shownDay)) : day;
  }

  function showEnd(key: string, nextEndTime: string, startDate: Date | null) {
    setEndKey(key);
    setEndTime(nextEndTime);
    setEndDraft(dayDraft(parseDate(key) ?? startDate));
    setEndTimeDraft(clockDraft(nextEndTime));
  }

  // The editor opens on the date as it is now, which may have changed since
  // the cell first showed it.
  function openMenu() {
    if (readOnly) return;
    const next = selected ?? new Date();
    const nextTime = extractTime(value) || timeFor(value);
    const end = chronologicalEnd(next, writtenEndKey(value) || dateKey(next), nextTime,
      extractTime(extractEnd(value)) || nextTime);
    setMonth(startOfMonth(shownDay ?? next));
    setFocusKey(dateKey(shownDay ?? next));
    const today = selected ? "" : dayDraft(new Date());
    setTodayDraft(today);
    setDateDraft(selected ? dayDraft(selected) : today);
    setIncludeTime(!!extractTime(value));
    setTime(nextTime);
    setTimeDraft(clockDraft(nextTime));
    setIncludeEnd(!!extractEnd(value));
    showEnd(end.key, end.time, next);
    setReminder(initialReminder());
    setDeadline(storedDeadline(value));
    setOpen(true);
  }

  function closeMenu() { setOpen(false); }

  // Picking a day keeps the editor open, as in Notion; clearing closes it.
  function commitDate(date: Date | null, o: { time?: string; shownDay?: Date } = {}) {
    if (!date) {
      commitValue(null);
      setDateDraft("");
      closeMenu();
      return;
    }
    const startTime = o.time ?? time;
    const end = chronologicalEnd(date, endKey, startTime);
    commitValue(buildValue(date, { time: startTime, endKey: end.key, endTime: end.time }));
    setMonth(startOfMonth(o.shownDay ?? date));
    setFocusKey(dateKey(o.shownDay ?? date));
    setDateDraft(dayDraft(date));
    if (includeEnd) showEnd(end.key, end.time, date);
  }

  // "Today", or "Now" with a time, as the calendar head offers it: the day
  // and time it is on the date's own clock.
  function commitNow() {
    const now = ownOffset ? wallNowAtOffset(ownOffset) : wallNow(ownZone);
    if (includeTime) {
      setTime(now.time);
      setTimeDraft(clockDraft(now.time));
    }
    commitDate(now.date, { ...(includeTime ? { time: now.time } : {}), shownDay: new Date() });
  }

  function commitDateDraft(raw = dateDraft, asked = false) {
    if (!raw.trim()) {
      if (selected) commitValue(null);
      return;
    }
    if (!selected && !asked && raw === todayDraft) return;
    const date = parseDateDraftIn(raw, dateFormat, selected?.getFullYear() ?? month.getFullYear());
    if (!date) {
      setDateDraft(dayDraft(selected));
      return;
    }
    if (selected && dateKey(date) === dateKey(selected)) {
      setDateDraft(dayDraft(selected));
      return;
    }
    commitDate(date);
  }

  function setTimeEnabled(enabled: boolean) {
    setIncludeTime(enabled);
    // An empty date given a time becomes today at it, as Notion's (seen 2026-11-05).
    const day = selected ?? (enabled ? new Date() : null);
    if (!day) return;
    if (!enabled) {
      commitValue(buildValue(day, { includeTime: false }));
      return;
    }
    // Its start and end take its day's clock, whatever time it had before
    // (Notion's, seen 2026-11-14: a day picked, then a time, is 9:00).
    const clock = clockForDay(dateKey(day));
    setTime(clock);
    setEndTime(clock);
    setTimeDraft(clockDraft(clock));
    setEndTimeDraft(clockDraft(clock));
    commitValue(buildValue(day, { includeTime: true, time: clock, endTime: clock }));
  }

  function commitTimeDraft(raw = timeDraft) {
    const next = parseTimeDraft(raw);
    if (!next) {
      setTimeDraft(clockDraft(time));
      return;
    }
    setTime(next);
    setTimeDraft(clockDraft(next));
    if (!selected) return;
    if (next === extractTime(value)) return;
    const end = chronologicalEnd(selected, endKey, next);
    if (includeEnd) showEnd(end.key, end.time, selected);
    commitValue(buildValue(selected, { includeTime: true, time: next, endKey: end.key, endTime: end.time }));
  }

  // An empty date given an end becomes today to today at once, so the next
  // day picked is its end, as Notion's (seen 2026-10-06).
  function setEndEnabled(enabled: boolean) {
    const start = selected ?? (enabled ? new Date() : null);
    const end = start ? chronologicalEnd(start, selected ? endKey : dateKey(start)) : { key: endKey, time: endTime };
    setIncludeEnd(enabled);
    if (enabled) showEnd(end.key, end.time, start);
    // Its start stays the end being set, its field ringed; a later day picked
    // then ends the range and takes the ring (Notion's, seen 2026-11-08).
    if (enabled) setActiveEnd("start");
    if (start) commitValue(buildValue(start, { includeEnd: enabled, endKey: end.key, endTime: end.time }));
    if (!selected && start) setDateDraft(dayDraft(start));
  }

  function commitEndDraft(raw = endDraft) {
    const date = raw.trim() ? parseDateDraftIn(raw, dateFormat, selected?.getFullYear() ?? month.getFullYear()) : null;
    if (!date) {
      setEndDraft(dayDraft(parseDate(endKey)));
      return;
    }
    const end = selected ? chronologicalEnd(selected, dateKey(date)) : { key: dateKey(date), time: endTime };
    showEnd(end.key, end.time, date);
    if (end.key === writtenEndKey(value)) return;
    if (selected) commitValue(buildValue(selected, { endKey: end.key, endTime: end.time, includeEnd: true }));
  }

  function commitEndTimeDraft(raw = endTimeDraft) {
    const next = parseTimeDraft(raw);
    if (!next || !selected) {
      setEndTimeDraft(clockDraft(endTime));
      return;
    }
    const end = chronologicalEnd(selected, endKey, time, next);
    showEnd(end.key, end.time, selected);
    if (end.key === writtenEndKey(value) && end.time === extractTime(extractEnd(value))) return;
    commitValue(buildValue(selected, { endKey: end.key, endTime: end.time }));
  }

  // With an end, the day picked replaces the end being set, the ends kept in
  // order, and the ring passes to the other end (pickInRange), as Notion's
  // (seen 2026-11-14).
  function pickDay(picked: Date) {
    if (!includeEnd || !selected || !shownDay) {
      setActiveEnd("start");
      commitDate(writtenDayFor(picked), { shownDay: picked });
      return;
    }
    const range = pickInRange(dateKey(selected), endKey, dateKey(writtenDayFor(picked)), activeEnd);
    const start = parseDate(range.start) ?? selected;
    const end = chronologicalEnd(start, range.end);
    setActiveEnd(range.active);
    setDateDraft(dayDraft(start));
    showEnd(end.key, end.time, start);
    setFocusKey(dateKey(picked));
    commitValue(buildValue(start, { endKey: end.key, endTime: end.time, includeEnd: true }));
  }
  // An empty date shows today as the chosen day, as Notion's (seen
  // 2026-11-02) — it is written only once picked.
  const rangeMarks = includeEnd && shownDay
    ? rangeCalendarMarks(dateKey(shownDay), endKey, activeEnd)
    : shownDay ? {} : { isSelected: (date: { toString(): string }) => date.toString() === dateKey(new Date()) };

  // A format chosen in the editor rewrites its inputs in that format too.
  function chooseFormats(patch: DateConfig) {
    onPropertyConfig?.(patch);
    const nextDateFormat = patch.dateFormat ?? dateFormat;
    const nextTimeFormat = patch.timeFormat ?? timeFormat;
    setDateDraft(formatDateDraft(selected, nextDateFormat));
    setEndDraft(formatDateDraft(parseDate(endKey) ?? selected, nextDateFormat));
    setTimeDraft(formatTimeDraft(time, nextTimeFormat));
    setEndTimeDraft(formatTimeDraft(endTime, nextTimeFormat));
  }

  // An empty date takes today on its own clock with the reminder, zone or
  // deadline chosen for it, as Notion's reminder does (seen 2026-11-02); a
  // template's, which may say "today" instead, is picked first as before.
  const emptyTemplate = Boolean(templateValues) && !selected;
  function chosenStart() {
    if (selected) return buildValue(selected);
    const now = ownOffset ? wallNowAtOffset(ownOffset) : wallNow(ownZone);
    const startTime = includeTime ? now.time : time;
    if (includeTime) {
      setTime(now.time);
      setTimeDraft(clockDraft(now.time));
    }
    setDateDraft(dayDraft(now.date));
    setMonth(startOfMonth(now.date));
    setFocusKey(dateKey(now.date));
    return buildValue(now.date, { time: startTime });
  }

  function commitZone(zone: string) {
    rememberTimeZone(zone);
    commitValue(chosenStart(), reminder, zone);
  }

  const reminderChoiceList = [
    { value: "", label: t("propertyCell:reminderNone") },
    ...reminderChoices(includeTime ? "0000-00-00T00:00" : "0000-00-00").map((minutes) => ({
      value: String(minutes),
      label: t(reminderChoiceKey(includeTime, minutes)),
      current: t(reminderValueKey(includeTime, minutes)),
    })),
  ];

  return (
    <div className={styles.dateCell}>
      <button
        ref={triggerRef}
        type="button"
        className={styles.dateButton}
        data-inline={inline ? "true" : undefined}
        autoFocus={autoFocus}
        aria-label={
          selected
            ? t("propertyCell:editDateValueFor", { name: prop.name, value: displayDate })
            : t("propertyCell:editDateFor", { name: prop.name })
        }
        aria-haspopup="dialog"
        aria-expanded={open}
        {...readOnlyStop(readOnly)}
        onClick={openMenu}
      >
        {emptyLabel && !selected ? (
          <span className={styles.dateValue} data-date-empty-label><CalendarIcon size="small" aria-hidden="true" /> {emptyLabel}</span>
        ) : templateValues && (value === "@today" || value === "@now") ? (
          <span className={styles.dateValue} data-template-value={value}>
            {t(`propertyCell:templateValue.${value.slice(1)}`)}
          </span>
        ) : selected ? (
          <>
            {/* A date whose reminder's moment has come reads red with its clock, as a date mention's
                and Notion's (seen 2026-11-05). */}
            <span className={styles.dateValue} data-reminder={reminder ? "true" : undefined}
              data-reminder-due={reminder && reminderDateDue(`${dateKey(selected)}${extractTime(value) ? `T${extractTime(value)}` : ""}`, reminder) ? "true" : undefined}>
              {displayDate}
            </span>
            {reminder ? (
              <ClockIcon size="tiny" aria-label={t("blockItem:dateMention.remind")} data-date-reminder />
            ) : null}
            {storedDeadline(value) ? <DeadlineFlag /> : null}
          </>
        ) : (
          <span className={styles.cellEmpty}>&nbsp;</span>
        )}
      </button>
      {open && !readOnly && (
        <DateEditorFrame label={t("propertyCell:editDateProperty")} triggerRef={triggerRef} onClose={() => closeMenu()}>
            {/* With a time, each end is one field "2026. 11. 8. | 오전 9:00", start over end, as Notion's
                (seen 2026-11-02); without, the days side by side. */}
            <div ref={inputsRef} className={editorStyles.inputs} data-date-inputs="" data-single={!includeTime && !includeEnd ? "true" : undefined}
              data-timed={includeTime ? "true" : undefined}
              onFocus={(event) => setActiveEnd((event.target as HTMLElement).dataset.dateInput?.startsWith("end") ? "end" : "start")}>
              <DatePair timed={includeTime} active={includeEnd && activeEnd === "start"}>
                <DateDraftInput label={t("propertyCell:editDateFor", { name: prop.name })} value={dateDraft} autoFocus={fieldFocusOnOpen()} data="start"
                  active={includeEnd && !includeTime && activeEnd === "start"} iso={dateKey(selected ?? new Date())}
                  onChange={setDateDraft} onCommit={(asked, picked = dateDraft) => commitDateDraft(picked, asked)}
                  onReset={() => setDateDraft(selected ? dayDraft(selected) : todayDraft)} />
                {includeTime ? (
                  <DateDraftInput label={t("propertyCell:time")} value={timeDraft} iso={time} data="start-time" onChange={setTimeDraft}
                    onCommit={(_asked, picked) => commitTimeDraft(picked)} onReset={() => setTimeDraft(clockDraft(time))} />
                ) : null}
              </DatePair>
              {includeEnd ? (
                <DatePair timed={includeTime} active={activeEnd === "end"}>
                  <DateDraftInput label={t("propertyCell:endDate")} value={endDraft} iso={endKey} data="end" onChange={setEndDraft}
                    active={!includeTime && activeEnd === "end"}
                    onCommit={(_asked, picked) => commitEndDraft(picked)} onReset={() => setEndDraft(dayDraft(parseDate(endKey)))} />
                  {includeTime ? (
                    <DateDraftInput label={t("propertyCell:endTime")} value={endTimeDraft} iso={endTime} data="end-time" onChange={setEndTimeDraft}
                      onCommit={(_asked, picked) => commitEndTimeDraft(picked)} onReset={() => setEndTimeDraft(clockDraft(endTime))} />
                  ) : null}
                </DatePair>
              ) : null}
            </div>
            {templateValues ? (
              <div className={styles.dateTemplateValues} role="group"
                aria-label={t("propertyCell:templateValue.whenDuplicated")}>
                {(["@today", "@now"] as const).map((choice) => (
                  <button key={choice} type="button" data-template-value-choice={choice}
                    aria-pressed={value === choice}
                    onClick={() => { write(choice); closeMenu(); }}>
                    <span>{t(`propertyCell:templateValue.${choice.slice(1)}`)}</span>
                    <span>{t("propertyCell:templateValue.whenDuplicated")}</span>
                  </button>
                ))}
              </div>
            ) : null}
            <NativeDateCalendar value={shownDay ? civilDate(shownDay) : null}
              focusedValue={civilDate(parseDate(focusKey) ?? shownDay ?? new Date())}
              onFocusChange={date => { setFocusKey(date.toString()); setMonth(startOfMonth(localDate(date))); }}
              onPick={date => pickDay(localDate(date))} {...rangeMarks}
              weekStart={weekStart} label={t("propertyCell:editDateProperty")}
              previousLabel={t("propertyCell:prevMonth")} nextLabel={t("propertyCell:nextMonth")}
              classes={{ head: editorStyles.calendarHead, weekdays: styles.dateWeekdays, grid: styles.dateGrid }}
              headerExtra={<button type="button" data-date-now onClick={commitNow}>
                {includeTime ? t("propertyCell:now") : t("propertyCell:today")}
              </button>} />
            <div className={editorStyles.options} data-date-options="">
              <DateSwitchRow label={t("propertyCell:endDate")} checked={includeEnd} onChange={setEndEnabled} />
              <DateChoiceRow label={t("propertyCell:dateFormat.label")} selected={dateFormat || "full"}
                choices={DATE_FORMATS.map((format) => ({ value: format, label: t(`propertyCell:dateFormat.${format}`) }))}
                disabled={!onPropertyConfig} data={{ "data-date-format": dateFormat || "full" }}
                onChoose={(next) => chooseFormats({ dateFormat: next })} />
              <DateSwitchRow label={t("propertyCell:includeTime")} checked={includeTime} onChange={setTimeEnabled} />
              {includeTime ? (
                <>
                  <DateChoiceRow label={t("propertyCell:timeFormat.label")} selected={timeFormat || "12"}
                    choices={TIME_FORMATS.map((format) => ({ value: format, label: t(`propertyCell:timeFormat.${format}`) }))}
                    disabled={!onPropertyConfig} data={{ "data-time-format": timeFormat || "12" }}
                    onChoose={(next) => chooseFormats({ timeFormat: next })} />
                  <DateZoneRow zone={ownZone || localTimeZone()} offset={ownOffset} current={localTimeZone()}
                    at={selected ?? new Date()} disabled={emptyTemplate} onChoose={commitZone} />
                </>
              ) : null}
              <DateChoiceRow label={t("blockItem:dateMention.remind")} selected={reminder} choices={reminderChoiceList} width={250}
                disabled={emptyTemplate} data={{ "data-date-property-reminder": reminder }}
                onChoose={(next) => {
                  setReminder(next);
                  if (selected || next) commitValue(chosenStart(), next);
                }} />
              <button type="button" className={editorStyles.clearRow} data-date-clear onClick={() => commitDate(null)}>
                {t("propertyCell:clearDate")}
              </button>
              {/* Leafen's deadline rows stand where Notion's foot line ("리마인더에 대해 알아보기") does. */}
              {templateValues ? null : (
                <div className={editorStyles.foot}>
                  <PropertyDateDeadline deadline={deadline} rowId={rowId} propertyId={prop.id}
                    valueKey={JSON.stringify(value ?? null)}
                    onChange={(next) => {
                      setDeadline(next);
                      if (selected || next) commitValue(chosenStart(), reminder, ownZone, next);
                    }} />
                </div>
              )}
            </div>
        </DateEditorFrame>
      )}
    </div>
  );
}

function writtenEndKey(value: unknown) {
  const end = writtenDate(extractEnd(value));
  return end ? dateKey(end) : "";
}

function isEmptyDate(value: unknown) {
  if (value == null || value === "") return true;
  return typeof value === "object" && !(value as { start?: unknown }).start;
}

function storedReminder(value: unknown) {
  if (!value || typeof value !== "object") return "";
  const { start, reminder } = value as { start?: unknown; reminder?: unknown };
  return typeof start === "string" && typeof reminder === "string"
    ? safeDateReminderValue(reminder, start) ?? ""
    : "";
}

/** The time a date is first given: its day's clock, today's for an empty one. */
function timeFor(value: unknown) {
  return clockForDay(dateKey(writtenDate(value) ?? new Date()));
}

/**
 * A day and its time as one field split by a faint line, or the day's field
 * alone — in the same box either way, so switching the time on keeps the
 * day's field and its focus (re-made, it took the focus and the ring to the
 * start, unlike Notion's, seen 2026-11-12).
 */
function DatePair({ timed, active, children }: { timed: boolean; active: boolean; children: ReactNode }) {
  const [day, clock] = Children.toArray(children);
  return <div className={timed ? editorStyles.pair : editorStyles.pairOff} data-date-pair={timed ? "" : undefined}
    data-active={timed && active ? "true" : undefined}>
    {day}{timed ? <span className={editorStyles.pairRule} aria-hidden="true" /> : null}{timed ? clock : null}</div>;
}
