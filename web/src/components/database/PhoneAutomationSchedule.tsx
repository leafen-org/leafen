import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { CalendarDate } from "@internationalized/date";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { civilDate, readCivilDate } from "@/lib/nativeDate";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { pickerWindowBox, pickerWindowLine, usePickerWindow } from "@/lib/usePickerWindow";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { databaseAutomationBounds, databaseAutomationScheduleFrequencies } from "@/databaseAutomationWire.generated";
import {
  allTimeZones, loadZoneCities, readTimeZoneNamesAhead, recentTimeZones, rememberTimeZone, timeZoneGroups, timeZoneRow, timeZoneShortName,
  type TimeZoneRow, type ZoneCities,
} from "./dateTimeZones";
import { formatTime12 } from "./dateUtils";
import { SCHEDULE_MIDNIGHT, scheduleIntervalLimit, type ScheduleFrequency, type ScheduleTriggerDraft } from "./databaseAutomationPanelModel";
import { nextScheduleRunText, SCHEDULE_PRESETS, SCHEDULE_TIMES as TIMES, SCHEDULE_WEEK as WEEK, scheduleDayLabel as dayLabel, scheduleTimeLabel,
  scheduleUnitName, scheduleWeekdayName as weekdayName, withScheduleUnit, type SchedulePreset } from "./automationScheduleModel";
import calendarStyles from "./DateFilterEditor.module.css";
import styles from "./PhoneAutomationSchedule.module.css";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

function today() {
  return civilDate(new Date());
}

/** "매..."'s choices at the screen's bottom, as Notion's: picking one opens its sheet. */
export function PhoneSchedulePresets({ onPick, onClose }: { onPick(preset: SchedulePreset): void; onClose(): void }) {
  const { t } = useTranslation("databaseView");
  return (
    <PhoneSheetMenu label={t("schedulePending")} onClose={onClose}>
      {SCHEDULE_PRESETS.map((preset) => (
        <PhoneSheetRow key={preset} label={t(`schedulePresets.${preset}`)} more={false} onPress={() => onPick(preset)}
          data={{ "data-phone-schedule-preset": preset }} />
      ))}
    </PhoneSheetMenu>
  );
}

/** A short list of choices at the bottom with the chosen one ticked, as Notion's "일 / 주 / 개월 / 년". */
function ChoiceMenu<Key extends string>({ label, choices, chosen, onPick, onClose, data, children }: {
  label: string; choices: ReadonlyArray<{ key: Key; label: string; description?: string }>; chosen: Key | null; onPick(key: Key): void; onClose(): void;
  data: string; children?: ReactNode;
}) {
  const list = useRef<HTMLDivElement>(null);
  useEffect(() => {
    list.current?.querySelector("[data-chosen]")?.scrollIntoView({ block: "center" });
  }, []);
  return (
    <PhoneSheetMenu label={label} onClose={onClose} className={styles.choiceMenu}>
      <div ref={list} className={styles.choiceList}>
        {choices.map((choice) => (
          <PhoneSheetRow key={choice.key} label={choice.label} description={choice.description} more={false} onPress={() => onPick(choice.key)}
            value={choice.key === chosen ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
            data={{ [`data-${data}`]: choice.key, "data-chosen": choice.key === chosen ? "true" : undefined }} />
        ))}
      </div>
      {children}
    </PhoneSheetMenu>
  );
}

/** A month to pick a day from, today in red, days before the earliest faint, as Notion's sheet (seen 2026-10-24). */
function DayCalendar({ value, min, label, onPick }: { value: string; min: string; label: string; onPick(day: string): void }) {
  const { t } = useTranslation("propertyCell");
  const weekStart = useWeekStartDay();
  const chosen = readCivilDate(value);
  const earliest = readCivilDate(min);
  const [focused, setFocused] = useState<CalendarDate>(() => chosen ?? earliest ?? today());
  return (
    <div className={styles.calendar} data-phone-schedule-calendar>
      <NativeDateCalendar value={chosen} focusedValue={focused} onFocusChange={setFocused} weekStart={weekStart} label={label}
        previousLabel={t("prevMonth")} nextLabel={t("nextMonth")}
        classes={{ root: calendarStyles.phoneCalendar, head: `${calendarStyles.phoneCalendarHead} ${styles.calendarHead}`, title: calendarStyles.phoneMonth,
          shift: calendarStyles.shift, weekdays: `${calendarStyles.phoneWeekdays} ${styles.calendarWeekdays}`, grid: calendarStyles.phoneDays,
          day: `${calendarStyles.day} ${calendarStyles.phoneDay} ${styles.day}` }}
        isSelected={(date) => Boolean(chosen && date.compare(chosen) === 0)}
        marker={(date) => ({
          "data-mark": chosen && date.compare(chosen) === 0 ? "start" : undefined,
          "data-past": earliest && date.compare(earliest) < 0 ? "true" : undefined,
        })}
        onPick={(date) => { if (!earliest || date.compare(earliest) >= 0) onPick(date.toString()); }} />
    </div>
  );
}

/** The time zone, found by city or offset, as Notion's half sheet "시간대". */
function TimeZoneSheet({ zone, onPick, onClose }: { zone: string; onPick(zone: string): void; onClose(): void }) {
  const { t, i18n } = useTranslation("propertyCell");
  const [query, setQuery] = useState("");
  const [cities, setCities] = useState<ZoneCities>({});
  const language = i18n.resolvedLanguage ?? i18n.language;
  useEffect(() => {
    let current = true;
    void loadZoneCities(language).then((loaded) => { if (current) setCities(loaded); });
    return () => { current = false; };
  }, [language]);
  const rows = useMemo(() => { const now = new Date(); return allTimeZones().map((candidate) => timeZoneRow(candidate, now, cities)); }, [cities]);
  const groups = useMemo(() => timeZoneGroups(rows, query, zone, recentTimeZones(), cities), [cities, query, rows, zone]);
  // Every zone's group (some 420) draws only the rows around what the sheet
  // shows (usePickerWindow), as the date window's list: drawn whole, the sheet
  // made and laid out every row as it opened. The sheet's body, which holds
  // these groups, scrolls them (PhoneSheet); a row with its offset under its
  // city is 66 high, the last 65 (PhoneSheet's last row).
  const stackRef = useRef<HTMLDivElement>(null), scrollRef = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => { scrollRef.current = stackRef.current?.parentElement ?? null; });
  const all = groups.find((group) => group.key === "all")?.rows ?? [];
  const lines = usePickerWindow({ scrollRef, open: true, count: all.length, keyAt: (index) => all[index].zone, sizeAt: (index) => (index === all.length - 1 ? 65 : 66), focused: -1 });
  const zoneRow = (groupKey: string, row: TimeZoneRow) => (
    <PhoneSheetRow key={`${groupKey}:${row.zone}`} label={row.city} description={row.offset} more={false}
      value={row.zone === zone ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
      data={{ "data-time-zone-choice": row.zone }} onPress={() => { rememberTimeZone(row.zone); onPick(row.zone); onClose(); }} />
  );
  return (
    <PhoneSheet size="half" title={t("timeZone.label")} onClose={onClose} data={{ "data-phone-schedule-zones": "" }}>
      <div ref={stackRef} className={sheetStyles.stack}>
        <PhoneSheetGroup>
          <PhoneSheetSearch value={query} placeholder={t("timeZone.search")} aria-label={t("timeZone.search")} onValueChange={setQuery} />
        </PhoneSheetGroup>
        {groups.map((group) => (
          <PhoneSheetGroup key={group.key} label={t(`timeZone.${group.key}`)}>
            {group.key === "all" && lines.windowed ? (
              <div ref={lines.linesRef} role="presentation" style={pickerWindowBox(lines.height)}>
                {lines.lines.map((line) => (
                  <div key={line.key} {...pickerWindowLine(line, lines.measure)} data-window-line-last={line.index === all.length - 1 ? "" : undefined}>
                    {zoneRow(group.key, all[line.index])}
                  </div>
                ))}
              </div>
            ) : group.rows.map((row) => zoneRow(group.key, row))}
          </PhoneSheetGroup>
        ))}
        {/* A search nothing answers keeps the zones' head, as the date window's (DateZoneChoices). */}
        {groups.length === 0 ? <PhoneSheetGroup label={t("timeZone.all")}><p className={styles.none}>{t("timeZone.none")}</p></PhoneSheetGroup> : null}
      </div>
    </PhoneSheet>
  );
}

type Picking = "unit" | "ordinal" | "weekday" | "time" | "start" | "end" | "zone" | null;

/**
 * A recurring trigger's sheet at the screen's bottom, as Notion's (seen
 * 2026-10-24 at 375px): "매 [1] [주 ⌄]", a week's days as circles, a
 * month's "지정 날짜 [1일 ⌄] [일 ⌄]" or "[1번째 ⌄] [월요일 ⌄]", 시간, 시작
 * 시간, 종료일 and 시간대 with their values in grey, and "다음 주기: …".
 */
export function PhoneScheduleSheet({ draft, readOnly, label, head, children, onChange, onClose }: {
  draft: ScheduleTriggerDraft; readOnly?: boolean; onChange(draft: ScheduleTriggerDraft): void; onClose(): void;
  /** Its name, and a head over its rows, as a template's "주기 편집 / 저장하기". */
  label?: string; head?: ReactNode;
  /** Rows after 시간대, as a template's "반복 취소". */
  children?: ReactNode;
}) {
  const { t } = useTranslation(["databaseView", "propertyCell"]);
  const [picking, setPicking] = useState<Picking>(null);
  // The zones' names are read past the sheet's first frame, so its zone list opens drawing from them.
  useEffect(() => readTimeZoneNamesAhead(), []);
  const update = (change: Partial<ScheduleTriggerDraft>) => onChange({ ...draft, ...change });
  const close = () => setPicking(null);
  const byWeekday = draft.frequency === "monthly" && draft.monthlyBy === "weekday";
  const toggleDay = (weekday: number) => {
    const chosen = draft.weekdays.includes(weekday);
    // A week runs on one day at least.
    if (chosen && draft.weekdays.length === 1) return;
    update({ weekdays: chosen ? draft.weekdays.filter((day) => day !== weekday) : [...draft.weekdays, weekday].sort((left, right) => left - right) });
  };
  const row = (key: Exclude<Picking, null>, label: string, value: string) => (
    <button type="button" className={styles.row} disabled={readOnly} onClick={() => setPicking(key)} data-phone-schedule-row={key}>
      <span>{label}</span><span className={styles.value}>{value}</span>
    </button>
  );
  const chip = (key: Exclude<Picking, null>, label: string) => (
    <button type="button" className={styles.chip} disabled={readOnly} onClick={() => setPicking(key)} data-phone-schedule-chip={key}>
      {label}<ChevronDown size="tiny" aria-hidden="true" />
    </button>
  );
  const next = nextScheduleRunText(t, draft);
  const endLabel = draft.endsAfter ? t("databaseView:scheduleEndAfterRunsValue", { count: Number(draft.endsAfter) })
    : draft.endsOn ? dayLabel(draft.endsOn) : t("databaseView:scheduleEndNone");
  const todayValue = today().toString();
  return (
    <PhoneSheetMenu label={label ?? t("databaseView:scheduleFrequency")} onClose={onClose} className={styles.sheet}>
      {head}
      <div className={styles.row} data-phone-schedule-row="every">
        <span>{t("databaseView:scheduleEveryLabel")}</span>
        <span className={styles.controls}>
          <input className={styles.interval} inputMode="numeric" value={draft.interval} readOnly={readOnly} data-phone-schedule-interval
            aria-label={t(`databaseView:scheduleEvery.${draft.frequency}`)}
            onChange={(event) => update({ interval: event.currentTarget.value.replace(/\D/gu, "").slice(0, 3) })}
            onBlur={() => update({ interval: String(Math.min(Math.max(1, Number(draft.interval) || 1), scheduleIntervalLimit(draft.frequency))) })} />
          {chip("unit", scheduleUnitName(t, draft.frequency, draft.interval))}
        </span>
      </div>
      {draft.frequency === "weekly" ? (
        <div className={styles.days} role="group" aria-label={t("databaseView:scheduleOnDays")}>
          {WEEK.map((weekday) => (
            <button key={weekday} type="button" className={styles.circle} aria-pressed={draft.weekdays.includes(weekday)} disabled={readOnly}
              aria-label={weekdayName(weekday, "long")} data-selected={draft.weekdays.includes(weekday) || undefined} data-phone-schedule-day={weekday}
              onClick={() => toggleDay(weekday)}>
              {t(`databaseView:scheduleWeekdayShort.${weekday}`)}
            </button>
          ))}
        </div>
      ) : null}
      {draft.frequency === "monthly" ? (
        <div className={styles.row} data-phone-schedule-row="on">
          <span>{t("databaseView:scheduleMonthlyOnLabel")}</span>
          <span className={styles.controls}>
            {chip("ordinal", byWeekday ? t(`databaseView:scheduleWeekOrdinal.${draft.monthWeek || "1"}`)
              : t("databaseView:scheduleMonthDayOrdinal", { day: draft.monthDay || "1" }))}
            {chip("weekday", byWeekday ? weekdayName(Number(draft.monthWeekday), "long") : t("databaseView:scheduleMonthDayWord"))}
          </span>
        </div>
      ) : null}
      {row("time", t("databaseView:scheduleTime"), scheduleTimeLabel(t, draft.time))}
      {row("start", t("databaseView:scheduleStartLabel"), dayLabel(draft.startsOn))}
      {row("end", t("databaseView:scheduleEndsOn"), endLabel)}
      {row("zone", t("databaseView:scheduleTimeZone"), timeZoneShortName(draft.timeZone))}
      {children}
      {next ? <p className={styles.next} data-phone-schedule-next>{next}</p> : null}
      {picking === "unit" ? (
        <ChoiceMenu label={t("databaseView:scheduleFrequency")} data="phone-schedule-unit" chosen={draft.frequency} onClose={close}
          choices={databaseAutomationScheduleFrequencies.map((frequency) => ({ key: frequency, label: scheduleUnitName(t, frequency, draft.interval) }))}
          onPick={(frequency: ScheduleFrequency) => { onChange(withScheduleUnit(draft, frequency)); close(); }} />
      ) : null}
      {picking === "ordinal" ? (
        <ChoiceMenu label={t("databaseView:scheduleMonthlyOnLabel")} data="phone-schedule-ordinal" onClose={close}
          chosen={byWeekday ? draft.monthWeek || "1" : draft.monthDay || "1"}
          choices={byWeekday ? ["1", "2", "3", "4", "5"].map((week) => ({ key: week, label: t(`databaseView:scheduleWeekOrdinal.${week}`) }))
            : Array.from({ length: 31 }, (_, index) => String(index + 1)).map((day) => ({ key: day, label: t("databaseView:scheduleMonthDayOrdinal", { day }) }))}
          onPick={(key) => { update(byWeekday ? { monthWeek: key } : { monthDay: key }); close(); }} />
      ) : null}
      {picking === "weekday" ? (
        <ChoiceMenu label={t("databaseView:scheduleWeekday")} data="phone-schedule-weekday" onClose={close}
          chosen={byWeekday ? draft.monthWeekday : "day"}
          choices={[{ key: "day", label: t("databaseView:scheduleMonthDayWord") },
            ...[0, 1, 2, 3, 4, 5, 6].map((weekday) => ({ key: String(weekday), label: weekdayName(weekday, "long") }))]}
          onPick={(key) => {
            if (key === "day") update({ monthlyBy: "day", monthDay: draft.monthDay || "1" });
            // A weekday keeps the first to fourth or last week the day stood in.
            else update({ monthlyBy: "weekday", monthWeekday: key,
              monthWeek: draft.monthWeek || String(Math.min(5, Math.ceil((Number(draft.monthDay) || 1) / 7))) });
            close();
          }} />
      ) : null}
      {picking === "time" ? (
        <ChoiceMenu label={t("databaseView:scheduleTime")} data="phone-schedule-time" chosen={draft.time} onClose={close}
          choices={[{ key: SCHEDULE_MIDNIGHT, label: t("databaseView:scheduleMidnight"), description: t("databaseView:scheduleMidnightWindow") },
            ...TIMES.map((time) => ({ key: time, label: formatTime12(time) }))]}
          onPick={(time) => { update({ time }); close(); }} />
      ) : null}
      {picking === "start" ? (
        <PhoneSheetMenu label={t("databaseView:scheduleStartLabel")} onClose={close} className={styles.calendarMenu}>
          <DayCalendar value={draft.startsOn} min={todayValue} label={t("databaseView:scheduleStartLabel")}
            onPick={(startsOn) => { update({ startsOn, endsOn: draft.endsOn && draft.endsOn < startsOn ? "" : draft.endsOn }); close(); }} />
        </PhoneSheetMenu>
      ) : null}
      {picking === "end" ? (
        <ChoiceMenu label={t("databaseView:scheduleEndsOn")} data="phone-schedule-end" onClose={close}
          chosen={draft.endsAfter ? "count" : draft.endsOn ? "date" : "none"}
          choices={[{ key: "none", label: t("databaseView:scheduleEndNone") }, { key: "count", label: t("databaseView:scheduleEndAfterRuns") },
            { key: "date", label: t("databaseView:scheduleEndOnDate") }]}
          onPick={(key) => {
            if (key === "none") update({ endsOn: "", endsAfter: "" });
            // "실행 횟수" counts from 1, as Notion's.
            else if (key === "count") update({ endsOn: "", endsAfter: draft.endsAfter || "1" });
            // A day is a week from today at first, as Notion's.
            else if (!draft.endsOn) {
              const week = today().add({ days: 7 }).toString();
              update({ endsAfter: "", endsOn: week < draft.startsOn ? draft.startsOn : week });
            }
          }}>
          {draft.endsAfter ? (
            <label className={styles.runs}>
              <span>{t("databaseView:scheduleEndAfterRuns")}</span>
              <input className={styles.interval} inputMode="numeric" value={draft.endsAfter} data-phone-schedule-runs
                aria-label={t("databaseView:scheduleEndAfterRuns")}
                onChange={(event) => update({ endsAfter: event.currentTarget.value.replace(/\D/gu, "").slice(0, 3) })}
                onBlur={() => update({ endsAfter: String(Math.min(Math.max(1, Number(draft.endsAfter) || 1), databaseAutomationBounds.scheduleMaxRuns)) })} />
            </label>
          ) : null}
          {draft.endsOn ? <DayCalendar value={draft.endsOn} min={draft.startsOn} label={t("databaseView:scheduleEndsOn")}
            onPick={(endsOn) => update({ endsOn })} /> : null}
        </ChoiceMenu>
      ) : null}
      {picking === "zone" ? <TimeZoneSheet zone={draft.timeZone} onPick={(timeZone) => update({ timeZone })} onClose={close} /> : null}
    </PhoneSheetMenu>
  );
}

