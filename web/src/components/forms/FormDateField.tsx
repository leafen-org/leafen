import { useRef, useState } from "react";
import type { CalendarDate } from "@internationalized/date";
import { useTranslation } from "react-i18next";
import { ChevronUpDown } from "@/icons/leafen";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { readCivilDate } from "@/lib/nativeDate";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { DbProperty, FormPublicProperty, FormViewQuestion } from "@/lib/types";
import { pickInRange, rangeCalendarMarks, type DateRangeEnd } from "../database/dateRangeCalendar";
import { dateKey, formatDateDraft, formatDateForProperty, formatTimeDraft, parseDate, parseDateDraftIn, parseTimeDraft } from "../database/dateUtils";
import { CellMenuPortal } from "../database/PropertyCellMenu";
import { DateEditorSheet } from "../database/DateEditorFrame";
import { DateDraftInput } from "../database/PropertyDateEditorOptions";
import cellStyles from "../database/databaseSheet";
import editorStyles from "../database/PropertyDateEditor.module.css";
import styles from "./FormSurface.module.css";

/** A date's answer: a day, or {start, end} when the question also asks for an end or a time. */
export type FormDateAnswer = string | { start: string; end?: string };

const MIDNIGHT = "00:00";

function dayOf(part: string | undefined) { return part ? part.slice(0, 10) : ""; }
function timeOf(part: string | undefined) { return part && part.length > 10 ? part.slice(11, 16) : MIDNIGHT; }

/**
 * A date question's answer as Notion's form asks it (seen 2026-11-05): the
 * field opens the days to type — with their times and the end when the
 * question's "시간 포함" and "종료일" ask for them — over a calendar, and "삭제".
 * A range begins today until a day before it is picked; nothing is answered
 * until a day is picked. On a touch screen they are the sheet "날짜", its days
 * not focused and "삭제" a group of its own, as Notion's phone form (seen
 * 2026-11-16, DateEditorSheet).
 */
export function FormDateField({ property, question, value, disabled, label, placeholder, clearLabel, onChange }: {
  property: DbProperty | FormPublicProperty;
  question: FormViewQuestion;
  value: unknown;
  disabled: boolean;
  label: string;
  placeholder: string;
  clearLabel: string;
  onChange(value: FormDateAnswer | ""): void;
}) {
  const { t } = useTranslation("propertyCell");
  const weekStart = useWeekStartDay();
  const withEnd = question.includeEndDate === true, withTime = question.includeTime === true;
  const answer = typeof value === "string" ? { start: value } : value && typeof value === "object" ? value as { start?: string; end?: string } : {};
  const start = answer.start ?? "", end = answer.end ?? "";
  const trigger = useRef<HTMLButtonElement>(null);
  const { touch } = useSidebarLayout(true);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<DateRangeEnd>("start");
  const [focused, setFocused] = useState<CalendarDate | null>(null);
  const timeFormat = (property.config as { timeFormat?: string } | undefined)?.timeFormat;
  const dateFormat = (property.config as { dateFormat?: string } | undefined)?.dateFormat;
  const today = dateKey(new Date());
  const dayDraft = (day: string) => formatDateDraft(parseDate(day), dateFormat);
  const drafted = () => ({
    start: dayDraft(dayOf(start) || today), startTime: formatTimeDraft(timeOf(start), timeFormat),
    end: dayDraft(dayOf(end) || dayOf(start) || today), endTime: formatTimeDraft(timeOf(end || start), timeFormat),
  });
  const [drafts, setDrafts] = useState(drafted);
  const part = (day: string, time: string) => withTime ? `${day}T${time}` : day;
  const send = (nextStart: string, nextEnd: string) => {
    const next = withEnd && nextEnd ? { start: nextStart, end: nextEnd } : withTime ? { start: nextStart } : nextStart;
    setDrafts({
      start: dayDraft(dayOf(nextStart)), startTime: formatTimeDraft(timeOf(nextStart), timeFormat),
      end: dayDraft(dayOf(nextEnd || nextStart)), endTime: formatTimeDraft(timeOf(nextEnd || nextStart), timeFormat),
    });
    onChange(next);
  };
  // A typed day or time replaces its end; one past the other end moves both. A touch screen's own
  // picker gives its day or time at once (picked), as Notion's phone form (seen 2026-11-15 on a Galaxy A90).
  const commit = (side: DateRangeEnd, picked: { day?: string; time?: string } = {}) => {
    const day = parseDateDraftIn(picked.day ?? (side === "start" ? drafts.start : drafts.end), dateFormat);
    const time = parseTimeDraft(picked.time ?? (side === "start" ? drafts.startTime : drafts.endTime)) ?? timeOf(side === "start" ? start : end);
    if (!day) return setDrafts(drafted());
    const typed = part(dateKey(day), time);
    if (side === "start") return send(typed, withEnd ? (end && end >= typed ? end : typed) : "");
    const from = start || part(today, MIDNIGHT);
    send(from <= typed ? from : typed, typed);
  };
  const pick = (picked: string) => {
    if (!withEnd) {
      send(part(picked, timeOf(start)), "");
      if (!withTime) setOpen(false);
      return;
    }
    const next = pickInRange(dayOf(start) || today, dayOf(end) || dayOf(start) || today, picked, active);
    setActive(next.active);
    send(part(next.start, timeOf(start)), part(next.end, timeOf(end || start)));
  };
  const shownDay = dayOf(active === "end" && end ? end : start);
  const anchor = readCivilDate(shownDay);
  const pickedOrToday = anchor ?? readCivilDate(today);
  // An empty answer marks today, where its typed fields begin, as Notion's.
  const marks = rangeCalendarMarks(dayOf(start) || today, dayOf(end) || null, active);
  const nowClock = () => {
    const now = new Date();
    return `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
  };
  const shown = start ? formatDateForProperty(end ? { start, end } : start, property as Pick<DbProperty, "config">) : "";
  const pair = (side: DateRangeEnd, dayLabel: string, timeLabel: string, autoFocus: boolean) => {
    const dayKey = side, timeKey = side === "start" ? "startTime" as const : "endTime" as const;
    // A range's end being set is ringed, as the date editor's.
    const ringed = withEnd && active === side;
    const row = (
      <>
        <DateDraftInput label={dayLabel} value={drafts[dayKey]} data={side} autoFocus={autoFocus} active={ringed && !withTime}
          iso={dayOf(side === "start" ? start : end || start) || today}
          onChange={(next) => setDrafts((current) => ({ ...current, [dayKey]: next }))}
          onCommit={(_asked, day) => commit(side, { day })} onReset={() => setDrafts(drafted())} />
        {withTime ? <span className={editorStyles.pairRule} aria-hidden="true" /> : null}
        {withTime ? (
          <DateDraftInput label={timeLabel} value={drafts[timeKey]} data={`${side}-time`} iso={timeOf(side === "start" ? start : end || start)}
            onChange={(next) => setDrafts((current) => ({ ...current, [timeKey]: next }))}
            onCommit={(_asked, time) => commit(side, { time })} onReset={() => setDrafts(drafted())} />
        ) : null}
      </>
    );
    // One box with a time or without, so switching it keeps the day's field and its focus.
    return <div className={withTime ? editorStyles.pair : editorStyles.pairOff} data-date-pair={withTime ? "" : undefined}
      data-active={withTime && ringed ? "true" : undefined} key={side}>{row}</div>;
  };
  return (
    <span className={styles.dateAnswer} data-form-answer={property.id}>
      <button ref={trigger} type="button" className={styles.dateAnswerButton} aria-label={`${label}: ${shown || placeholder}`}
        aria-haspopup="dialog" aria-expanded={open} data-form-date data-empty={start ? undefined : "true"} disabled={disabled}
        onClick={() => { setDrafts(drafted()); setActive("start"); setFocused(anchor); setOpen((current) => !current); }}>
        {shown || placeholder}
      </button>
      <ChevronUpDown size="small" className={styles.pickedMark} aria-hidden="true" />
      {question.required && <input className={styles.dropdownRequired} tabIndex={-1} aria-hidden="true" required disabled={disabled}
        value={start} onChange={() => undefined} />}
      {open ? (() => {
        // The sheet keeps "삭제" with nothing answered yet too, as Notion's phone form (seen 2026-11-16).
        const clear = start || touch ? (
          <button type="button" className={touch ? editorStyles.clearRow : styles.dateAnswerClear} data-form-date-clear data-date-clear
            onClick={() => { onChange(""); setOpen(false); }}>{clearLabel}</button>
        ) : null;
        const body = <>
          <div className={editorStyles.inputs} data-single={!withTime && !withEnd ? "true" : undefined} data-timed={withTime ? "true" : undefined}
            data-date-inputs="" onFocus={(event) => setActive((event.target as HTMLElement).dataset.dateInput?.startsWith("end") ? "end" : "start")}>
            {pair("start", t("propertyCell:editDateFor", { name: label }), t("propertyCell:time"), fieldFocusOnOpen())}
            {withEnd ? pair("end", t("propertyCell:endDate"), t("propertyCell:endTime"), false) : null}
          </div>
          <NativeDateCalendar value={pickedOrToday} focusedValue={focused ?? pickedOrToday ?? undefined} onFocusChange={setFocused}
            weekStart={weekStart} label={label} previousLabel={t("prevMonth")} nextLabel={t("nextMonth")}
            classes={{ head: editorStyles.calendarHead, weekdays: cellStyles.dateWeekdays, grid: cellStyles.dateGrid }}
            {...marks} onPick={(picked) => pick(picked.toString())}
            headerExtra={<button type="button" data-date-now onClick={() => {
              const at = part(today, withTime ? nowClock() : MIDNIGHT);
              send(at, withEnd ? (end && end >= at ? end : at) : "");
            }}>{withTime ? t("propertyCell:now") : t("propertyCell:today")}</button>} />
        </>;
        return touch ? (
          <DateEditorSheet label={label} onClose={() => setOpen(false)}>
            {body}{clear ? <div className={editorStyles.options} data-date-options="">{clear}</div> : null}
          </DateEditorSheet>
        ) : (
          <CellMenuPortal triggerRef={trigger} onClose={() => setOpen(false)} ariaLabel={label} className={editorStyles.choiceMenu}
            backdropClassName={cellStyles.menuBackdrop} width={248} widestLine>
            {body}{clear}
          </CellMenuPortal>
        );
      })() : null}
    </span>
  );
}
