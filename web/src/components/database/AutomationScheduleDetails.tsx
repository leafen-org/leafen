import { useRef, useState, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import type { CalendarDate } from "@internationalized/date";
import { CheckIcon, ChevronDown, ChevronRight } from "@/icons/leafen";
import { chosenTimeZone } from "@/lib/dateReminders";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { civilDate, readCivilDate } from "@/lib/nativeDate";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { databaseAutomationBounds, databaseAutomationScheduleFrequencies } from "@/databaseAutomationWire.generated";
import { DateEditorChoices } from "./DateEditorChoices";
import { formatTime12 } from "./dateUtils";
import { CellMenuPortal } from "./PropertyCellMenu";
import { ChoiceRowButton, DateZoneRow, type DateEditorChoice } from "./PropertyDateEditorOptions";
import { SCHEDULE_MIDNIGHT, scheduleIntervalLimit, type ScheduleFrequency, type ScheduleTriggerDraft } from "./databaseAutomationPanelModel";
import { nextScheduleRunWhen, SCHEDULE_TIMES, SCHEDULE_WEEK, scheduleDayLabel, scheduleTimeLabel, scheduleWeekdayName,
  scheduleUnitName, withScheduleUnit } from "./automationScheduleModel";
import cellStyles from "./databaseSheet";
import editorStyles from "./PropertyDateEditor.module.css";
import styles from "./AutomationScheduleDetails.module.css";

type Picking = "unit" | "ordinal" | "weekday" | "time" | "start" | "end" | null;

function today() {
  return civilDate(new Date());
}

/** A month to pick a day from, days before the earliest faint. */
function DayCalendar({ value, min, label, autoFocus = false, onPick }: {
  value: string; min: string; label: string; autoFocus?: boolean; onPick(day: string): void;
}) {
  const { t } = useTranslation("propertyCell");
  const weekStart = useWeekStartDay();
  const chosen = readCivilDate(value);
  const earliest = readCivilDate(min);
  const [focused, setFocused] = useState<CalendarDate>(() => chosen ?? earliest ?? today());
  return (
    <NativeDateCalendar value={chosen} focusedValue={focused} onFocusChange={setFocused} weekStart={weekStart} label={label} autoFocus={autoFocus}
      previousLabel={t("prevMonth")} nextLabel={t("nextMonth")}
      classes={{ head: editorStyles.calendarHead, weekdays: cellStyles.dateWeekdays, grid: cellStyles.dateGrid, day: styles.day }}
      isSelected={(date) => Boolean(chosen && date.compare(chosen) === 0)}
      marker={(date) => ({ "data-past": earliest && date.compare(earliest) < 0 ? "true" : undefined })}
      onPick={(date) => { if (!earliest || date.compare(earliest) >= 0) onPick(date.toString()); }} />
  );
}

/** A month beside its row to pick a day from. */
function DayMenu({ rowRef, value, min, label, onPick, onClose }: {
  rowRef: RefObject<HTMLElement | null>; value: string; min: string; label: string; onPick(day: string): void; onClose(): void;
}) {
  return (
    <CellMenuPortal beside triggerRef={rowRef} onClose={onClose} ariaLabel={label} className={`${editorStyles.choiceMenu} ${styles.calendar}`}
      backdropClassName={cellStyles.menuBackdrop} width={252}>
      <DayCalendar value={value} min={min} label={label} autoFocus onPick={onPick} />
    </CellMenuPortal>
  );
}

/** A short list under its chip or beside its row, the chosen one ticked. */
function ChoiceMenu({ anchorRef, beside = false, label, choices, selected, width, tall = false, lightFirst = false, onPick, onClose }: {
  anchorRef: RefObject<HTMLElement | null>; beside?: boolean; label: string; choices: DateEditorChoice[]; selected: string; width: number;
  /** A long list, cut at Notion's 250px. */
  tall?: boolean;
  /** Opens with its first row lit, not its chosen one (DateEditorChoices). */
  lightFirst?: boolean;
  onPick(value: string): void; onClose(): void;
}) {
  return (
    <CellMenuPortal beside={beside} triggerRef={anchorRef} onClose={onClose} ariaLabel={label} scroll={!tall}
      className={tall ? `${editorStyles.choiceMenu} ${styles.tall}` : editorStyles.choiceMenu} backdropClassName={cellStyles.menuBackdrop} width={width}>
      {(placed) => <DateEditorChoices choices={choices} selected={selected} label={label} open={placed} lightFirst={lightFirst}
        onChoose={(value) => { onPick(value); onClose(); }} />}
    </CellMenuPortal>
  );
}

/**
 * A recurring trigger's details on a desktop, as Notion's (seen 2026-10-25
 * at 1280px): a 260px window under its "주기" chip with the schedule's
 * fields, wider for a longer line (widestLine; seen 2026-11-19: 313 and 329
 * for a longer "시작 시간" and time zone). The same draft as a phone's sheet (PhoneScheduleSheet).
 */
export function AutomationScheduleDetails({ draft, readOnly, chipRef, onChange, onClose }: {
  draft: ScheduleTriggerDraft;
  readOnly: boolean;
  chipRef: RefObject<HTMLElement | null>;
  onChange(draft: ScheduleTriggerDraft): void;
  onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  return (
    <CellMenuPortal triggerRef={chipRef} onClose={onClose} ariaLabel={t("databaseView:scheduleFrequency")}
      className={`${editorStyles.choiceMenu} ${styles.details}`} backdropClassName={cellStyles.menuBackdrop} width={260} widestLine scroll={false}
      focusFirst={false}>
      <ScheduleDetailsFields draft={draft} readOnly={readOnly} onChange={onChange} />
    </CellMenuPortal>
  );
}

/**
 * A desktop schedule's fields, as Notion's: "매 [1] [일 ⌄]", a week's days
 * as circles, a month's "지정 날짜 [1일 ⌄] [일 ⌄]", "시간" with its list
 * beside, a line, "시작 시간" with a month beside, "종료일" (없음, 실행
 * 횟수 [N], 특정 날짜 ›), "시간대", what its owner adds under a line (a
 * template's "반복 취소", seen 2026-10-30), and under a line "다음 주기:" and
 * when — or, when it has no run left, a note in red that it will never
 * run. In a window with the class `details`.
 */
export function ScheduleDetailsFields({ draft, readOnly, onChange, children }: {
  draft: ScheduleTriggerDraft;
  readOnly: boolean;
  onChange(draft: ScheduleTriggerDraft): void;
  children?: ReactNode;
}) {
  const { t } = useTranslation(["databaseView", "propertyCell"]);
  const [picking, setPicking] = useState<Picking>(null);
  const unitRef = useRef<HTMLButtonElement>(null), ordinalRef = useRef<HTMLButtonElement>(null), weekdayRef = useRef<HTMLButtonElement>(null);
  const timeRef = useRef<HTMLButtonElement>(null), startRef = useRef<HTMLButtonElement>(null), endRef = useRef<HTMLButtonElement>(null);
  const update = (change: Partial<ScheduleTriggerDraft>) => onChange({ ...draft, ...change });
  const close = () => setPicking(null);
  const byWeekday = draft.frequency === "monthly" && draft.monthlyBy === "weekday";
  const toggleDay = (weekday: number) => {
    const chosen = draft.weekdays.includes(weekday);
    // A week runs on one day at least.
    if (chosen && draft.weekdays.length === 1) return;
    update({ weekdays: chosen ? draft.weekdays.filter((day) => day !== weekday) : [...draft.weekdays, weekday].sort((left, right) => left - right) });
  };
  const chip = (ref: RefObject<HTMLButtonElement | null>, key: Exclude<Picking, null>, label: string) => (
    <button ref={ref} type="button" className={styles.chip} disabled={readOnly} aria-haspopup="listbox" aria-expanded={picking === key}
      onClick={() => setPicking(key)} data-schedule-chip={key}>
      {label}<ChevronDown size="tiny" aria-hidden="true" />
    </button>
  );
  const row = (ref: RefObject<HTMLButtonElement | null>, key: Exclude<Picking, null>, label: string, value: string) => (
    <ChoiceRowButton rowRef={ref} label={label} current={value} open={picking === key} disabled={readOnly}
      onOpen={() => setPicking(key)} data={{ "data-schedule-row": key }} />
  );
  const when = nextScheduleRunWhen(t, draft);
  const endLabel = draft.endsAfter ? t("databaseView:scheduleEndAfterRunsValue", { count: Number(draft.endsAfter) })
    : draft.endsOn ? scheduleDayLabel(draft.endsOn) : t("databaseView:scheduleEndNone");
  const ending = draft.endsAfter ? "count" : draft.endsOn ? "date" : "none";
  const endRow = (key: "none" | "count" | "date", label: string, onPress: () => void) => (
    <button type="button" role="menuitemradio" aria-checked={ending === key} className={styles.endRow} data-schedule-end={key}
      onClick={onPress}>
      <span className={styles.endLabel}>{label}</span>
      {key === "count" && draft.endsAfter ? (
        <input className={styles.interval} inputMode="numeric" value={draft.endsAfter} data-schedule-runs aria-label={t("databaseView:scheduleEndAfterRuns")}
          onClick={(event) => event.stopPropagation()}
          onChange={(event) => update({ endsAfter: event.currentTarget.value.replace(/\D/gu, "").slice(0, 3) })}
          onBlur={() => update({ endsAfter: String(Math.min(Math.max(1, Number(draft.endsAfter) || 1), databaseAutomationBounds.scheduleMaxRuns)) })} />
      ) : null}
      {key === "date" && ending === "date" ? <span className={styles.endValue}>{scheduleDayLabel(draft.endsOn)}</span> : null}
      {key !== "date" ? ending === key ? <CheckIcon size="small" aria-hidden="true" /> : null
        : ending === "date" ? <ChevronDown size="tiny" className={editorStyles.choiceChevron} aria-hidden="true" />
          : <ChevronRight size="tiny" className={editorStyles.choiceChevron} aria-hidden="true" />}
    </button>
  );
  return (
    <>
      <div className={styles.every} data-schedule-row="every">
        <span>{t("databaseView:scheduleEveryLabel")}</span>
        <span className={styles.controls}>
          <input className={styles.interval} inputMode="numeric" value={draft.interval} readOnly={readOnly} data-schedule-interval
            aria-label={t(`databaseView:scheduleEvery.${draft.frequency}`)}
            onChange={(event) => update({ interval: event.currentTarget.value.replace(/\D/gu, "").slice(0, 3) })}
            onBlur={() => update({ interval: String(Math.min(Math.max(1, Number(draft.interval) || 1), scheduleIntervalLimit(draft.frequency))) })} />
          {chip(unitRef, "unit", scheduleUnitName(t, draft.frequency, draft.interval))}
        </span>
      </div>
      {draft.frequency === "weekly" ? (
        <div className={styles.days} role="group" aria-label={t("databaseView:scheduleOnDays")}>
          {SCHEDULE_WEEK.map((weekday) => (
            <button key={weekday} type="button" className={styles.circle} aria-pressed={draft.weekdays.includes(weekday)} disabled={readOnly}
              aria-label={scheduleWeekdayName(weekday, "long")} data-selected={draft.weekdays.includes(weekday) || undefined} data-schedule-day={weekday}
              onClick={() => toggleDay(weekday)}>
              {t(`databaseView:scheduleWeekdayShort.${weekday}`)}
            </button>
          ))}
        </div>
      ) : null}
      {draft.frequency === "monthly" ? (
        <div className={styles.on} data-schedule-row="on">
          <span>{t("databaseView:scheduleMonthlyOnLabel")}</span>
          <span className={styles.controls}>
            {chip(ordinalRef, "ordinal", byWeekday ? t(`databaseView:scheduleWeekOrdinal.${draft.monthWeek || "1"}`)
              : t("databaseView:scheduleMonthDayOrdinal", { day: draft.monthDay || "1" }))}
            {chip(weekdayRef, "weekday", byWeekday ? scheduleWeekdayName(Number(draft.monthWeekday), "long") : t("databaseView:scheduleMonthDayWord"))}
          </span>
        </div>
      ) : null}
      {row(timeRef, "time", t("databaseView:scheduleTime"), scheduleTimeLabel(t, draft.time))}
      <div className={styles.rule} role="separator" />
      {row(startRef, "start", t("databaseView:scheduleStartLabel"), scheduleDayLabel(draft.startsOn))}
      {row(endRef, "end", t("databaseView:scheduleEndsOn"), endLabel)}
      <DateZoneRow zone={draft.timeZone} current={chosenTimeZone()} at={new Date()} disabled={readOnly} label={t("databaseView:scheduleTimeZone")} onChoose={(timeZone) => update({ timeZone })} />
      {children ? <><div className={styles.rule} role="separator" />{children}</> : null}
      <div className={styles.rule} role="separator" />
      {when ? (
        <p className={styles.next} data-schedule-next>
          <span>{t("databaseView:scheduleNextLabel")}</span>
          <span>{when}</span>
        </p>
      ) : <p className={styles.never} role="status" data-schedule-never>{t("databaseView:scheduleNeverRuns")}</p>}
      {picking === "unit" ? (
        <ChoiceMenu anchorRef={unitRef} label={t("databaseView:scheduleFrequency")} width={180} selected={draft.frequency} onClose={close}
          choices={databaseAutomationScheduleFrequencies.map((frequency) => ({ value: frequency, label: scheduleUnitName(t, frequency, draft.interval) }))}
          onPick={(frequency) => onChange(withScheduleUnit(draft, frequency as ScheduleFrequency))} />
      ) : null}
      {picking === "ordinal" ? (
        <ChoiceMenu anchorRef={ordinalRef} label={t("databaseView:scheduleMonthlyOnLabel")} width={165} tall onClose={close}
          selected={byWeekday ? draft.monthWeek || "1" : draft.monthDay || "1"}
          choices={byWeekday ? ["1", "2", "3", "4", "5"].map((week) => ({ value: week, label: t(`databaseView:scheduleWeekOrdinal.${week}`) }))
            : Array.from({ length: 31 }, (_, index) => String(index + 1)).map((day) => ({ value: day, label: t("databaseView:scheduleMonthDayOrdinal", { day }) }))}
          onPick={(value) => update(byWeekday ? { monthWeek: value } : { monthDay: value })} />
      ) : null}
      {picking === "weekday" ? (
        <ChoiceMenu anchorRef={weekdayRef} label={t("databaseView:scheduleWeekday")} width={180} tall onClose={close}
          selected={byWeekday ? draft.monthWeekday : "day"}
          choices={[{ value: "day", label: t("databaseView:scheduleMonthDayWord"), ruleAfter: true },
            ...[0, 1, 2, 3, 4, 5, 6].map((weekday) => ({ value: String(weekday), label: scheduleWeekdayName(weekday, "long") }))]}
          onPick={(value) => {
            if (value === "day") update({ monthlyBy: "day", monthDay: draft.monthDay || "1" });
            // A weekday keeps the first to fourth or last week the day stood in.
            else update({ monthlyBy: "weekday", monthWeekday: value,
              monthWeek: draft.monthWeek || String(Math.min(5, Math.ceil((Number(draft.monthDay) || 1) / 7))) });
          }} />
      ) : null}
      {picking === "time" ? (
        <ChoiceMenu anchorRef={timeRef} beside label={t("databaseView:scheduleTime")} width={180} tall lightFirst selected={draft.time} onClose={close}
          choices={[{ value: SCHEDULE_MIDNIGHT, label: t("databaseView:scheduleMidnight"), description: t("databaseView:scheduleMidnightWindow") },
            ...SCHEDULE_TIMES.map((time) => ({ value: time, label: formatTime12(time) }))]}
          onPick={(time) => update({ time })} />
      ) : null}
      {picking === "start" ? (
        <DayMenu rowRef={startRef} value={draft.startsOn} min={today().toString()} label={t("databaseView:scheduleStartLabel")} onClose={close}
          onPick={(startsOn) => { update({ startsOn, endsOn: draft.endsOn && draft.endsOn < startsOn ? "" : draft.endsOn }); close(); }} />
      ) : null}
      {picking === "end" ? (
        <CellMenuPortal beside triggerRef={endRef} onClose={close} ariaLabel={t("databaseView:scheduleEndsOn")} className={editorStyles.choiceMenu}
          backdropClassName={cellStyles.menuBackdrop} width={252}>
          <div role="menu" aria-label={t("databaseView:scheduleEndsOn")} className={styles.endMenu}>
            {endRow("none", t("databaseView:scheduleEndNone"), () => { update({ endsOn: "", endsAfter: "" }); close(); })}
            {/* "실행 횟수" counts from 1, as Notion's. */}
            {endRow("count", t("databaseView:scheduleEndAfterRuns"), () => update({ endsOn: "", endsAfter: draft.endsAfter || "1" }))}
            <div className={styles.rule} role="separator" />
            {/* The day is a week from today at first, set at once, with its month under it, as Notion's (seen 2026-10-30). */}
            {endRow("date", t("databaseView:scheduleEndOnDate"), () => update({ endsAfter: "",
              endsOn: draft.endsOn || today().add({ days: 7 }).toString() }))}
          </div>
          {ending === "date" ? (
            <div className={styles.calendar}>
              <DayCalendar value={draft.endsOn} min={today().toString()} label={t("databaseView:scheduleEndOnDate")}
                onPick={(endsOn) => { update({ endsAfter: "", endsOn }); close(); }} />
            </div>
          ) : null}
        </CellMenuPortal>
      ) : null}
    </>
  );
}
