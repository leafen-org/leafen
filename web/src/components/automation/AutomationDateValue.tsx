import { useRef, useState, type ReactNode } from "react";
import type { CalendarDate } from "@internationalized/date";
import { useTranslation } from "react-i18next";
import { ArrowLeft, CalendarIcon, CheckIcon, ChevronDown, ChevronRight } from "@/icons/leafen";
import { clockForDay } from "@/lib/dateReminders";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { activeDateLocale } from "@/lib/i18n";
import { readCivilDate } from "@/lib/nativeDate";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import type { AutomationAction, AutomationValueExpression, EditPropertyAutomationAction } from "@/lib/types";
import { pickInRange, rangeCalendarMarks, type DateRangeEnd } from "@/components/database/dateRangeCalendar";
import { dateKey, formatDateDraft, formatTime12, formatTimeDraft, parseDate, parseDateDraftIn, parseTimeDraft, writtenDateRange } from "@/components/database/dateUtils";
import { DateDraftInput, DateSwitchRow } from "@/components/database/PropertyDateEditorOptions";
import { DateEditorSheet } from "@/components/database/DateEditorFrame";
import { useSidebarLayout } from "@/components/shell/AppShell";
import cellStyles from "@/components/database/databaseSheet";
import dateStyles from "@/components/database/PropertyDateEditor.module.css";
import editStyles from "./AutomationEditProperty.module.css";
import { AutomationValuePopover } from "./AutomationValuePopover";
import type { AutomationFormulaOpen } from "./AutomationWordsField";
import styles from "./AutomationDateValue.module.css";

// The two values a run gives, Notion's first two lines (seen 2026-11-10).
const RUN_VALUES = [
  { kind: "execution_time", label: "databaseView:messageNames.executionTime" },
  { kind: "execution_date", label: "databaseView:messageNames.executionDate" },
] as const;

/** Whether a date's value sets anything: a run's time or day, a formula, or a day written. */
export function automationDateValueSet(value: AutomationValueExpression) {
  if (value.kind !== "literal") return true;
  return value.value !== null && value.value !== undefined && value.value !== "";
}

/**
 * A written date as an automation's card says it, as Notion's (seen
 * 2026-11-10): "11월 12일", its year when not this year's ("2027년 1월 15일"),
 * its time after it ("11월 10일 오후 1:00"), an end after "→" ("2027년 1월
 * 15일 → 1월 20일", its year when not the start's).
 */
export function automationDateText(value: unknown) {
  const { start, end } = writtenDateRange(value);
  const part = (written: string, year: number) => {
    const day = parseDate(written.slice(0, 10));
    if (!day) return "";
    const days = day.toLocaleDateString(activeDateLocale(), { month: "long", day: "numeric",
      ...(day.getFullYear() === year ? {} : { year: "numeric" }) });
    return written.length > 10 ? `${days} ${formatTime12(written.slice(11, 16))}` : days;
  };
  const shown = part(start, new Date().getFullYear());
  if (!shown || !end || end === start) return shown;
  return `${shown} → ${part(end, parseDate(start.slice(0, 10))?.getFullYear() ?? new Date().getFullYear())}`;
}

/**
 * A date's value as its card and its line show it (Notion's, seen
 * 2026-11-10): a run's time or day blue with its calendar, a day written on
 * grey ("11월 12일"); null for anything else.
 */
export function AutomationDateChip({ value }: { value: AutomationValueExpression }) {
  const { t } = useTranslation("databaseView");
  if (value.kind === "execution_time" || value.kind === "execution_date") return <span className={editStyles.runChip}>
    <CalendarIcon size="tiny" aria-hidden="true" />{t(value.kind === "execution_time" ? "messageNames.executionTime" : "messageNames.executionDate")}</span>;
  if (value.kind !== "literal" || !automationDateValueSet(value)) return null;
  return <span className={editStyles.valueChip} data-date>{automationDateText(value.value)}</span>;
}

/**
 * What a date property is set to, as Notion's list (seen 2026-11-10): under
 * its head "자동 실행된 시간" and "자동 실행된 날짜", the one chosen checked,
 * each closing it as it is chosen; a line, "날짜 선택 ›", which opens the
 * days, and "Σ 사용자 지정 수식 작성 ›"; once a value is set a line and
 * "지우기", which clears it and closes. On a touch screen "날짜 선택" opens
 * the date sheet every date editor there opens (DateEditorSheet), over the
 * list, which it goes back to when put away.
 */
export function AutomationDateValue({ action, head, formulaRow, onChange, onDone }: {
  action: EditPropertyAutomationAction;
  /** The value's head ("← 날짜 속성 설정: 완료"), over the list but not over the days. */
  head?: ReactNode;
  /** "Σ 사용자 지정 수식 작성 ›", where Notion's list has it. */
  formulaRow?: ReactNode;
  onChange(action: AutomationAction): void;
  onDone(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const [picking, setPicking] = useState(false);
  const { touch } = useSidebarLayout(true);
  const set = (value: AutomationValueExpression) => onChange({ ...action, value });
  if (picking && !touch) {
    return <>
      <div className={`${editStyles.editorHead} ${editStyles.pickHead}`}>
        <button type="button" className={editStyles.back} aria-label={t("common:actions.back")} onClick={() => setPicking(false)}>
          <ArrowLeft size="small" aria-hidden="true" /></button>
        <span className={editStyles.editorTitle}>{t("databaseView:automationDatePick")}</span>
        <button type="button" className={editStyles.done} onClick={onDone}>{t("common:actions.done")}</button>
      </div>
      <AutomationDatePicker value={action.value.kind === "literal" ? action.value.value : null}
        onChange={(value) => set({ kind: "literal", value })} onClear={() => { set({ kind: "literal", value: null }); onDone(); }} />
    </>;
  }
  return <>
    {head}
    {head ? <span className={styles.rule} role="separator" /> : null}
    <div role="menu" className={styles.list}>
      {RUN_VALUES.map(({ kind, label }) => (
        <button key={kind} type="button" role="menuitemradio" aria-checked={action.value.kind === kind} className={styles.row}
          data-automation-date-value={kind} onClick={() => { set({ kind }); onDone(); }}>
          <CalendarIcon size="tiny" aria-hidden="true" /><span>{t(label)}</span>
          {action.value.kind === kind ? <CheckIcon size="small" aria-hidden="true" /> : null}
        </button>
      ))}
    </div>
    <span className={styles.rule} role="separator" />
    <div className={styles.list}>
      <button type="button" className={styles.row} data-automation-date-pick onClick={() => setPicking(true)}>
        <CalendarIcon size="tiny" aria-hidden="true" /><span>{t("databaseView:automationDatePick")}</span>
        <ChevronRight size="small" aria-hidden="true" />
      </button>
      {formulaRow}
    </div>
    {automationDateValueSet(action.value) ? <>
      <span className={styles.rule} role="separator" />
      <div className={styles.list}>
        <button type="button" className={styles.row} data-automation-date-clear onClick={() => { set({ kind: "literal", value: null }); onDone(); }}>
          <span>{t("databaseView:clearValue")}</span>
        </button>
      </div>
    </> : null}
    {picking ? <DateEditorSheet label={t("databaseView:automationDatePick")} onClose={() => setPicking(false)}>
      <AutomationDatePicker sheet value={action.value.kind === "literal" ? action.value.value : null}
        onChange={(value) => set({ kind: "literal", value })} onClear={() => { set({ kind: "literal", value: null }); setPicking(false); onDone(); }} />
    </DateEditorSheet> : null}
  </>;
}

/**
 * A date's value where a line holds it, as a page added or edited: what it
 * is set to in a field as wide as its column that opens the list
 * (AutomationDateValue) under it, headed "속성 설정", "Σ" among it where the
 * line takes a formula (Notion's, seen 2026-11-10 on a page added).
 */
export function AutomationDateField({ action, label, onFormula, onChange }: {
  action: EditPropertyAutomationAction; label: string; onFormula?: AutomationFormulaOpen; onChange(action: AutomationAction): void;
}) {
  const { t } = useTranslation("databaseView");
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const field = useRef<HTMLButtonElement>(null);
  const chip = <AutomationDateChip value={action.value} />;
  const set = action.value.kind !== "formula" && automationDateValueSet(action.value);
  return <>
    <button ref={field} type="button" className={styles.field} aria-label={label} aria-haspopup="dialog" aria-expanded={Boolean(anchor)} data-automation-date-field
      onClick={(event) => setAnchor(event.currentTarget.getBoundingClientRect())}>
      <span data-empty={set ? undefined : ""}>{set ? chip : t("automationChooseValue")}</span><ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {anchor ? <AutomationValuePopover anchor={anchor} line onClose={() => setAnchor(null)}>
      <div className={editStyles.editor} data-automation-value-window>
        <AutomationDateValue action={action} onChange={onChange} onDone={() => setAnchor(null)}
          head={<div className={`${editStyles.editorHead} ${editStyles.lineHead}`}><span className={editStyles.editorTitle}>{t("automationValueHead")}</span></div>} formulaRow={onFormula
          ? <button type="button" className={editStyles.formulaRow} onClick={() => { setAnchor(null); onFormula(field.current ?? undefined); }}>
            <span aria-hidden="true">Σ</span><span>{t("editPropertyFormula")}</span><ChevronRight size="small" aria-hidden="true" />
          </button> : undefined} />
      </div>
    </AutomationValuePopover> : null}
  </>;
}

const dayOf = (part: string) => part.slice(0, 10);
const timeOf = (part: string) => part.length > 10 ? part.slice(11, 16) : "";

/**
 * "날짜 선택"'s days, as Notion's (seen 2026-11-10), its date editor's: the
 * day typed over a calendar ("오늘", or "지금" with a time), "종료일" and "시간
 * 포함", and "삭제". A day picked sets it and stays open; with nothing set
 * it shows today, and a switch sets today (at the next whole hour, with a time).
 * In the touch screen's sheet (`sheet`) its field, calendar and settings are
 * the sheet's own groups.
 */
function AutomationDatePicker({ value, sheet, onChange, onClear }: {
  value: unknown; sheet?: boolean; onChange(value: string): void; onClear(): void;
}) {
  const { t } = useTranslation("propertyCell");
  const weekStart = useWeekStartDay();
  const written = value === null || value === undefined || value === "" ? null : writtenDateRange(value);
  const today = dateKey(new Date());
  const start = written?.start ?? "", end = written && written.end !== written.start ? written.end : "";
  const [withEnd, setWithEnd] = useState(Boolean(end));
  const [withTime, setWithTime] = useState(Boolean(timeOf(start)));
  const [active, setActive] = useState<DateRangeEnd>("start");
  const [focused, setFocused] = useState<CalendarDate | null>(null);
  const drafted = (from = start, to = end) => ({
    start: formatDateDraft(parseDate(dayOf(from) || today)), startTime: formatTimeDraft(timeOf(from) || clockForDay(dayOf(from) || today)),
    end: formatDateDraft(parseDate(dayOf(to || from) || today)), endTime: formatTimeDraft(timeOf(to || from) || clockForDay(dayOf(from) || today)),
  });
  const [drafts, setDrafts] = useState(() => drafted());
  // A day given a time takes its clock, the start's for the end (clockForDay).
  const part = (day: string, time: string, timed = withTime, clockDay = day) => timed ? `${day}T${time || clockForDay(clockDay)}` : day;
  const send = (nextStart: string, nextEnd: string, ranged = withEnd) => {
    setDrafts(drafted(nextStart, nextEnd));
    onChange(ranged && nextEnd && nextEnd !== nextStart ? `${nextStart}/${nextEnd}` : nextStart);
  };
  // A typed day or time replaces its end; one past the other end moves both.
  // An empty value's field shows today; left as it is (not Enter), the value
  // stays empty, as Notion's (seen 2026-11-10: "날짜 선택" then "완료" clears). A touch screen's own
  // picker gives its day or time at once (picked), as Notion's phone (seen 2026-11-15 on a Galaxy A90).
  const commit = (side: DateRangeEnd, asked: boolean, picked: { day?: string; time?: string } = {}) => {
    if (!asked && !start && side === "start" && drafts.start === drafted().start) return;
    const day = parseDateDraftIn(picked.day ?? (side === "start" ? drafts.start : drafts.end));
    const time = parseTimeDraft(picked.time ?? (side === "start" ? drafts.startTime : drafts.endTime)) ?? timeOf(side === "start" ? start : end);
    if (!day) return setDrafts(drafted());
    const typed = part(dateKey(day), time);
    if (side === "start") return send(typed, withEnd ? (end && end >= typed ? end : typed) : "");
    const from = start || part(today, "");
    send(from <= typed ? from : typed, typed);
  };
  const pick = (picked: string) => {
    if (!withEnd) return send(part(picked, timeOf(start)), "");
    const next = pickInRange(dayOf(start) || today, dayOf(end) || dayOf(start) || today, picked, active);
    setActive(next.active);
    send(part(next.start, timeOf(start)), part(next.end, timeOf(end || start)));
  };
  const switchEnd = (on: boolean) => {
    setWithEnd(on);
    const from = start || part(today, "");
    send(from, "", on);
  };
  const switchTime = (on: boolean) => {
    setWithTime(on);
    const at = (written: string) => on ? part(dayOf(written), timeOf(written), true, dayOf(start) || today) : dayOf(written);
    const from = at(start || today);
    send(from, end ? at(end) : withEnd ? from : "");
  };
  const anchor = readCivilDate(dayOf(active === "end" && end ? end : start) || today);
  const marks = rangeCalendarMarks(dayOf(start) || today, dayOf(end) || null, active);
  const pair = (side: DateRangeEnd, autoFocus: boolean) => {
    const timeKey = side === "start" ? "startTime" as const : "endTime" as const;
    const ringed = withEnd && active === side;
    const day = <DateDraftInput label={t(side === "start" ? "editDateProperty" : "endDate")} value={drafts[side]} data={side}
      iso={dayOf(side === "start" ? start : end || start) || today}
      autoFocus={autoFocus} active={ringed && !withTime} onChange={(next) => setDrafts((current) => ({ ...current, [side]: next }))}
      onCommit={(asked, picked) => commit(side, asked, { day: picked })} onReset={() => setDrafts(drafted())} />;
    // One box with a time or without, so switching it keeps the day's field and its focus.
    return <div key={side} className={withTime ? dateStyles.pair : dateStyles.pairOff} data-date-pair={withTime ? "" : undefined}
      data-active={withTime && ringed ? "true" : undefined}>
      {day}{withTime ? <span className={dateStyles.pairRule} aria-hidden="true" /> : null}
      {withTime ? <DateDraftInput label={t(side === "start" ? "time" : "endTime")} value={drafts[timeKey]} data={`${side}-time`}
        iso={timeOf(side === "start" ? start : end || start) || clockForDay(dayOf(start) || today)}
        onChange={(next) => setDrafts((current) => ({ ...current, [timeKey]: next }))}
        onCommit={(asked, picked) => commit(side, asked, { time: picked })} onReset={() => setDrafts(drafted())} /> : null}
    </div>;
  };
  const parts = <>
    <div className={dateStyles.inputs} data-single={!withTime && !withEnd ? "true" : undefined} data-timed={withTime ? "true" : undefined}
      data-date-inputs="" onFocus={(event) => setActive((event.target as HTMLElement).dataset.dateInput?.startsWith("end") ? "end" : "start")}>
      {pair("start", !sheet)}
      {withEnd ? pair("end", false) : null}
    </div>
    <NativeDateCalendar value={anchor} focusedValue={focused ?? anchor ?? undefined} onFocusChange={setFocused}
      weekStart={weekStart} label={t("editDateProperty")} previousLabel={t("prevMonth")} nextLabel={t("nextMonth")}
      classes={{ head: dateStyles.calendarHead, weekdays: cellStyles.dateWeekdays, grid: cellStyles.dateGrid }}
      {...marks} onPick={(picked) => pick(picked.toString())}
      headerExtra={<button type="button" data-date-now onClick={() => {
        const now = new Date();
        const at = part(today, `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`);
        send(at, withEnd ? (end && end >= at ? end : at) : "");
      }}>{withTime ? t("now") : t("today")}</button>} />
    <div className={dateStyles.options} data-date-options="">
      <DateSwitchRow label={t("endDate")} checked={withEnd} onChange={switchEnd} />
      <DateSwitchRow label={t("includeTime")} checked={withTime} onChange={switchTime} />
      <button type="button" className={dateStyles.clearRow} data-date-clear onClick={onClear}>{t("clearDate")}</button>
    </div>
  </>;
  return sheet ? parts : <div className={styles.picker} data-automation-date-picker>{parts}</div>;
}
