import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createFocusManager } from "@react-aria/focus";
import { useDialog } from "react-aria";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { DateEditorSheet } from "@/components/database/DateEditorFrame";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { readCivilDate, readClock, civilDate } from "@/lib/nativeDate";
import type { DateMentionChange } from "@/collaboration";
import { chosenTimeZone, clockForDay, localTimeZone, reminderChoiceKey, reminderChoices, reminderValueKey, safeDateReminderValue } from "@/lib/dateReminders";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import { formatDateDraft, formatTimeDraft, parseDate, parseDateDraftIn, parseTimeDraft } from "@/components/database/dateUtils";
import { pickInRange, rangeCalendarMarks, type DateRangeEnd } from "@/components/database/dateRangeCalendar";
import { DateChoiceRow, DateDraftInput, DateSwitchRow, DateZoneRow } from "@/components/database/PropertyDateEditorOptions";
import dateEditorStyles from "@/components/database/PropertyDateEditor.module.css";
import sheet from "@/components/database/databaseSheet";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { DATE_MENTION_FORMATS, DATE_MENTION_TIME_FORMATS, safeDateMentionFormat, safeDateMentionTimeFormat } from "./dateMentions";
import { localIsoDate } from "./mentionCalendarModel";
import styles from "./DateMentionEditor.module.css";

export type DateMentionTarget = {
  ordinal: number;
  date: string;
  dateEnd?: string;
  dateTimeZone?: string;
  dateReminder?: string;
  dateFormat?: string;
  dateTimeFormat?: string;
  anchor: { left: number; top: number; bottom: number; contextElement?: Element };
};

const clockOf = (value: string) => /[T ](\d{2}:\d{2})/.exec(value)?.[1];

// A day or time edit replaces only those fields of a stored timestamp,
// keeping its seconds and offset.
function clocked(stored: string, day: string, time: string) {
  const rest = stored.slice(10);
  return /^[T ]\d{2}:\d{2}/.test(rest) ? `${day}${rest.replace(/^([T ])\d{2}:\d{2}/, `$1${time}`)}` : `${day}T${time}`;
}

type Draft = { startDay: string; startTime: string; withTime: boolean; withEnd: boolean; endDay: string; endTime: string };

/**
 * Edits a date mention in place, as Notion's date editor (seen 2026-10-05):
 * its day, and its end when "종료일" is on, typed or picked on a month's
 * calendar, whose head offers today, or now with a time; then its end date
 * and time switches, the format it shows its date in, the time zone of a
 * time and the reminder that fires before it; "삭제" takes the mention out.
 * Each change shows at once. A time or a reminder keeps the time zone it was
 * set in, so the server fires it at the same moment the editor meant.
 */
export function DateMentionEditor({
  target,
  onChange,
  onClose,
  onEscape = onClose,
  onOutsidePress = onClose,
  onRemove,
  reminders = true,
  formats = true,
}: {
  target: DateMentionTarget;
  onChange: (change: DateMentionChange) => void;
  onClose: () => void;
  /** Escape's own close, where it differs from a press outside's. */
  onEscape?: () => void;
  /** A press beside it's own close, where it differs from the sheet's (its date taking the focus back). */
  onOutsidePress?: () => void;
  onRemove: () => void;
  /** Whether it offers a reminder: a comment's date has none, as Notion's (seen 2026-11-09). */
  reminders?: boolean;
  /** Whether it offers how the date and time show: a to-do's due date has no "날짜 형식", as Notion's (seen 2026-11-09). */
  formats?: boolean;
}) {
  const { t } = useTranslation(["propertyCell", "blockItem"]);
  const panelRef = useRef<HTMLDivElement>(null);
  // A touch screen's is the sheet "날짜" (DateEditorSheet), its field not focused.
  const { touch } = useSidebarLayout(true);
  // Under the date's text with no gap; turned over above it when only there
  // it fits whole, the date left in sight (seen 2026-11-07); moved up to
  // show whole where neither side has room, as Notion's (2026-11-03, 11-06).
  const place = usePopoverPlacement(panelRef, touch ? null : (size, viewport) => placeAround(
    popoverRectAt(target.anchor), size, viewport, { gap: 0, whole: true },
  ), [target.anchor], undefined, {
    kind: "picker", restoreFocus: false, onEscape, onOutsidePress,
  });
  const stored = useRef({ start: target.date, end: target.dateEnd ?? "" });
  const initialTime = clockOf(target.date) ?? clockForDay(target.date.slice(0, 10));
  const [draft, setDraft] = useState<Draft>(() => ({
    startDay: target.date.slice(0, 10), startTime: initialTime, withTime: Boolean(clockOf(target.date)),
    withEnd: Boolean(target.dateEnd), endDay: (target.dateEnd || target.date).slice(0, 10),
    endTime: clockOf(target.dateEnd ?? "") ?? initialTime,
  }));
  const [reminder, setReminder] = useState(safeDateReminderValue(target.dateReminder, target.date) ?? "");
  const [timeZone, setTimeZone] = useState(target.dateTimeZone ?? "");
  const [format, setFormat] = useState<string>(safeDateMentionFormat(target.dateFormat) ?? "relative");
  const [timeFormat, setTimeFormat] = useState<string>(safeDateMentionTimeFormat(target.dateTimeFormat) ?? "12");
  const [active, setActive] = useState<DateRangeEnd>("start");
  const [focused, setFocused] = useState(() => readCivilDate(target.date.slice(0, 10))
    ?? readCivilDate((target.dateEnd ?? "").slice(0, 10)) ?? civilDate(new Date()));
  const dayText = (day: string) => formatDateDraft(parseDate(day), format === "relative" ? undefined : format);
  const [texts, setTexts] = useState(() => ({
    start: dayText(draft.startDay), startTime: formatTimeDraft(draft.startTime, timeFormat),
    end: dayText(draft.endDay), endTime: formatTimeDraft(draft.endTime, timeFormat),
  }));
  const entered = useRef(false);
  const { dialogProps } = useDialog({ "aria-label": t("blockItem:dateMention.editor") }, panelRef);

  // The page's text lets go of the keyboard the tap on the date raised, the sheet's field taking it only when
  // pressed, and the page stays at rest once the sheet closes, as Notion's phone (seen twice 2026-11-16 in iOS
  // Safari at 402: the date pressed, the sheet up, no keyboard; closed, no keyboard and no tools). Before the
  // sheet notes what had the focus, to give it back.
  useLayoutEffect(() => { if (touch && document.activeElement instanceof HTMLElement) document.activeElement.blur(); }, [touch]);
  useEffect(() => {
    if (!place || entered.current) return;
    entered.current = true;
    const first = createFocusManager(panelRef).focusFirst({ tabbable: true });
    if (first instanceof HTMLInputElement) first.select();
  }, [Boolean(place)]);

  function show(next: Draft, nextFormat = format, nextTimeFormat = timeFormat) {
    const text = (day: string) => formatDateDraft(parseDate(day), nextFormat === "relative" ? undefined : nextFormat);
    setTexts({ start: text(next.startDay), startTime: formatTimeDraft(next.startTime, nextTimeFormat),
      end: text(next.endDay), endTime: formatTimeDraft(next.endTime, nextTimeFormat) });
  }

  // An end before the start moves to the start, as Notion's does.
  function inOrder(next: Draft): Draft {
    const startAt = `${next.startDay}T${next.withTime ? next.startTime : "00:00"}`;
    const endAt = `${next.endDay}T${next.withTime ? next.endTime : "00:00"}`;
    return endAt < startAt ? { ...next, endDay: next.startDay, endTime: next.startTime } : next;
  }

  function commit(patch: Partial<Draft> = {}, options: { reminder?: string; format?: string; zone?: string; timeFormat?: string } = {}) {
    const next = inOrder({ ...draft, ...patch });
    // An end is read only while the date has one: a date whose start was
    // never given has no day to start its end from.
    if (!readCivilDate(next.startDay) || (next.withEnd && !readCivilDate(next.endDay))
      || (next.withTime && (!readClock(next.startTime) || !readClock(next.endTime)))) return;
    // Editing presentation or reminders keeps the exact stored timestamps.
    const dateChanged = ["startDay", "startTime", "withTime"].some((key) => draft[key as keyof Draft] !== next[key as keyof Draft]);
    const endChanged = dateChanged || ["withEnd", "endDay", "endTime"].some((key) => draft[key as keyof Draft] !== next[key as keyof Draft]);
    const start = !dateChanged ? stored.current.start
      : next.withTime ? clocked(stored.current.start, next.startDay, next.startTime) : next.startDay;
    const end = !next.withEnd ? "" : !endChanged && stored.current.end ? stored.current.end
      : next.withTime ? clocked(stored.current.end || stored.current.start, next.endDay, next.endTime) : next.endDay;
    const nextReminder = safeDateReminderValue(options.reminder ?? reminder, start) ?? "";
    const ownOffset = /(?:Z|[+-]\d{2}:?\d{2})$/i.test(start);
    // A time starts in the device's zone. A date without one has a zone only
    // for its reminder, the one chosen in settings when the reminder is set.
    const zone = options.zone ?? (next.withTime
      ? (draft.withTime && timeZone) || (ownOffset ? "" : localTimeZone())
      : nextReminder ? (reminder && !draft.withTime && timeZone) || chosenTimeZone() : "");
    const nextFormat = safeDateMentionFormat(options.format ?? format) ?? "relative";
    const nextTimeFormat = safeDateMentionTimeFormat(options.timeFormat ?? timeFormat) ?? "12";
    stored.current = { start, end };
    setDraft(next);
    show(next, nextFormat, nextTimeFormat);
    setReminder(nextReminder);
    setTimeZone(zone);
    setFormat(nextFormat);
    setTimeFormat(nextTimeFormat);
    onChange({ date: start, dateEnd: end || null, dateTimeZone: zone || null, dateReminder: nextReminder || null, dateFormat: nextFormat,
      dateTimeFormat: nextTimeFormat === "12" ? null : nextTimeFormat });
  }

  function pick(day: string) {
    setFocused(readCivilDate(day) ?? focused);
    // A date whose start was never given takes the day picked as its start.
    if (!draft.withEnd || !readCivilDate(draft.startDay)) {
      commit({ startDay: day });
      return;
    }
    const range = pickInRange(draft.startDay, draft.endDay, day, active);
    setActive(range.active);
    commit({ startDay: range.start, endDay: range.end });
  }

  // A typed day or time takes effect when it reads as one and differs.
  function typedDay(which: DateRangeEnd, raw: string) {
    // A day typed without its year takes the start's, or this year's for a date without a start.
    const day = parseDateDraftIn(raw, format, Number(draft.startDay.slice(0, 4)) || new Date().getFullYear());
    const key = day ? civilDate(day).toString() : "";
    if (!key || key === (which === "start" ? draft.startDay : draft.endDay)) {
      show(draft);
      return;
    }
    setFocused(readCivilDate(key) ?? focused);
    commit(which === "start" ? { startDay: key } : { endDay: key });
  }

  function typedTime(which: DateRangeEnd, raw: string) {
    const time = parseTimeDraft(raw);
    if (!time || time === (which === "start" ? draft.startTime : draft.endTime)) {
      show(draft);
      return;
    }
    commit(which === "start" ? { startTime: time } : { endTime: time });
  }

  // "오늘", or "지금" with a time, as the calendar head offers it.
  function now() {
    const at = new Date();
    const time = `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")}`;
    const day = localIsoDate(0);
    setFocused(readCivilDate(day) ?? focused);
    commit(draft.withTime ? { startDay: day, startTime: time } : { startDay: day });
  }

  const weekStart = useWeekStartDay();
  const marks = rangeCalendarMarks(draft.startDay, draft.withEnd ? draft.endDay : null, active);
  const reminderOptions = [
    { value: "", label: t("propertyCell:reminderNone") },
    ...reminderChoices(draft.withTime ? `${draft.startDay}T${draft.startTime}` : draft.startDay).map((minutes) => ({
      value: String(minutes),
      label: t(reminderChoiceKey(draft.withTime, minutes)),
      current: t(reminderValueKey(draft.withTime, minutes)),
    })),
  ];
  const input = (which: DateRangeEnd, part: "day" | "time") => {
    const key = part === "day" ? which : (`${which}Time` as const);
    const label = which === "start"
      ? t(part === "day" ? "blockItem:dateMention.date" : "propertyCell:time")
      : t(part === "day" ? "propertyCell:endDate" : "propertyCell:endTime");
    return (
      <span className={styles.field} onFocus={() => setActive(which)}>
        {/* A touch screen's field is the system's own (iso), its pick taken at once, as Notion's phone (seen 2026-11-15 on a Galaxy A90). */}
        <DateDraftInput label={label} value={texts[key]} data={part === "day" ? which : `${which}-time`} iso={draft[`${which}${part === "day" ? "Day" : "Time"}` as const]}
          onChange={(value) => setTexts((current) => ({ ...current, [key]: value }))}
          onCommit={(_asked, picked = texts[key]) => (part === "day" ? typedDay(which, picked) : typedTime(which, picked))}
          onReset={() => show(draft)} />
      </span>
    );
  };

  const parts = (
    <>
      <div className={styles.inputs} data-range={draft.withEnd ? "true" : undefined} data-timed={draft.withTime ? "true" : undefined} data-date-inputs="">
        <div className={styles.inputRow} data-active={draft.withEnd && active === "start" ? "true" : undefined}>
          {input("start", "day")}{draft.withTime && <span className={dateEditorStyles.pairRule} aria-hidden="true" />}{draft.withTime && input("start", "time")}</div>
        {draft.withEnd && <div className={styles.inputRow} data-active={active === "end" ? "true" : undefined}>
          {input("end", "day")}{draft.withTime && <span className={dateEditorStyles.pairRule} aria-hidden="true" />}{draft.withTime && input("end", "time")}</div>}
      </div>
      <NativeDateCalendar value={readCivilDate(draft.startDay)} focusedValue={focused} onFocusChange={setFocused}
        onPick={(date) => pick(date.toString())} weekStart={weekStart} label={t("blockItem:dateMention.editor")}
        previousLabel={t("propertyCell:prevMonth")} nextLabel={t("propertyCell:nextMonth")}
        isSelected={marks.isSelected} marker={marks.marker}
        classes={{ head: dateEditorStyles.calendarHead, weekdays: sheet.dateWeekdays, grid: sheet.dateGrid }}
        headerExtra={<button type="button" data-date-now onClick={now}>
          {draft.withTime ? t("propertyCell:now") : t("propertyCell:today")}
        </button>} />
      <div className={dateEditorStyles.options} data-date-options="">
        <DateSwitchRow label={t("propertyCell:endDate")} checked={draft.withEnd}
          onChange={(on) => { setActive("start"); commit({ withEnd: on, endDay: draft.startDay, endTime: draft.startTime }); }} />
        {formats && <DateChoiceRow label={t("blockItem:dateMention.format")} selected={format}
          choices={DATE_MENTION_FORMATS.map((choice) => ({ value: choice, label: t(`blockItem:dateMention.formats.${choice}`) }))}
          data={{ "data-date-mention-format": format }} onChoose={(next) => commit({}, { format: next })} />}
        {/* A time turned on takes its day's clock at both ends, as the date window's (clockForDay). */}
        <DateSwitchRow label={t("propertyCell:includeTime")} checked={draft.withTime} onChange={(on) => {
          const clock = clockForDay(draft.startDay);
          commit(on ? { withTime: true, startTime: clock, endTime: clock } : { withTime: false });
        }} />
        {draft.withTime && (
          <>
            {formats && <DateChoiceRow label={t("propertyCell:timeFormat.label")} selected={timeFormat}
              choices={DATE_MENTION_TIME_FORMATS.map((choice) => ({ value: choice, label: t(`propertyCell:timeFormat.${choice}`) }))}
              data={{ "data-date-mention-time-format": timeFormat }} onChoose={(next) => commit({}, { timeFormat: next })} />}
            <DateZoneRow zone={timeZone || localTimeZone()} current={localTimeZone()} at={new Date(`${draft.startDay}T${draft.startTime}`)}
              onChoose={(zone) => commit({}, { zone })} />
          </>
        )}
        {reminders && <DateChoiceRow label={t("blockItem:dateMention.remind")} width={250} selected={reminder} choices={reminderOptions}
          data={{ "data-date-mention-reminder": reminder }} onChoose={(next) => commit({}, { reminder: next })} />}
        <button type="button" className={dateEditorStyles.clearRow} data-date-mention-remove data-date-clear onClick={onRemove}>
          {t("propertyCell:clearDate")}
        </button>
      </div>
    </>
  );
  if (touch) return <DateEditorSheet label={t("blockItem:dateMention.editor")} onClose={onClose}>{parts}</DateEditorSheet>;
  return (
    <>
      <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
        aria-label={t("blockItem:dateMention.close")} />
      <div ref={panelRef} className={styles.panel} style={popoverPlacementStyle(place)} {...dialogProps} data-date-mention-editor>
        {parts}
      </div>
    </>
  );
}
