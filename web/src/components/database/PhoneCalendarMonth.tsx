import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { Page } from "@/lib/types";
import { ChevronLeft, ChevronRight, Plus } from "../icons";
import { addDays, dateKey } from "./dateUtils";
import styles from "./PhoneCalendarMonth.module.css";

/**
 * A phone's calendar, as Notion's (seen 2026-11-03 and 11-04 at 375px): the
 * month's name with "‹ 오늘 ›" on one row, the weekdays, then a week to a
 * row of numbers — a dot under a day something falls on, today in a red
 * circle, other months' days gray — and no cards. Pressing a day opens the
 * whole screen on it: "‹ day ›" and "완료", its items in the order they
 * start — a timed one under its time — then "+ 새 항목".
 */
export function PhoneCalendarMonth({
  title, weekdays, weeks, month, previousLabel, nextLabel, rowsOn, rowTitle, rowDate, dayLabel, fullDayLabel,
  onMove, onToday, onOpenRow, onAdd,
}: {
  title: string;
  weekdays: readonly string[];
  weeks: readonly { days: readonly Date[] }[];
  /** The month shown, its other days gray; none for a week. */
  month: number | null;
  previousLabel: string;
  nextLabel: string;
  /** The rows a day holds, a range's on each of its days. */
  rowsOn(day: Date): Page[];
  rowTitle(row: Page): string;
  /** A row's date as its property shows it, where it has a time or an end. */
  rowDate(row: Page): string | null;
  dayLabel(day: Date): string;
  fullDayLabel(day: Date): string;
  onMove(delta: number): void;
  onToday(): void;
  onOpenRow(rowId: string): void;
  onAdd?: (day: Date) => void;
}) {
  const { t } = useTranslation(["calendarView", "common"]);
  const [openDay, setOpenDay] = useState<Date | null>(null);
  const today = dateKey(new Date());
  return (
    <div className={styles.calendar} data-phone-calendar>
      <div className={styles.head}>
        <div className={styles.title}>{title}</div>
        <div className={styles.nav}>
          <button type="button" className={styles.arrow} aria-label={previousLabel} onClick={() => onMove(-1)}><ChevronLeft size="regular" /></button>
          <button type="button" className={styles.today} onClick={onToday}>{t("calendarView:toolbar.today")}</button>
          <button type="button" className={styles.arrow} aria-label={nextLabel} onClick={() => onMove(1)}><ChevronRight size="regular" /></button>
        </div>
      </div>
      <div className={styles.grid} role="grid" aria-label={t("calendarView:gridAriaLabel", { title })}
        style={{ ["--phone-calendar-columns" as string]: weekdays.length }}>
        <div className={styles.weekdays} role="row">
          {weekdays.map((day) => <div key={day} className={styles.weekday} role="columnheader">{day}</div>)}
        </div>
        {weeks.map(({ days }, index) => (
          <div key={index} className={styles.week} role="row">
            {days.map((day) => {
              const key = dateKey(day), count = rowsOn(day).length;
              return (
                <button key={key} type="button" className={styles.day} role="gridcell"
                  aria-label={t("calendarView:cell.ariaLabel", { date: fullDayLabel(day), count })}
                  aria-current={key === today ? "date" : undefined} aria-haspopup="dialog"
                  data-outside={month !== null && day.getMonth() !== month ? "true" : undefined}
                  data-today={key === today ? "true" : undefined} onClick={() => setOpenDay(day)}>
                  <span className={styles.number}>{day.getDate()}</span>
                  {count > 0 ? <span className={styles.dot} aria-hidden="true" /> : null}
                </button>
              );
            })}
          </div>
        ))}
      </div>
      {openDay ? createPortal(<PhoneCalendarDay day={openDay} rows={rowsOn(openDay)} rowTitle={rowTitle} rowDate={rowDate} label={dayLabel(openDay)}
        onDay={setOpenDay} onClose={() => setOpenDay(null)}
        onOpenRow={(rowId) => { setOpenDay(null); onOpenRow(rowId); }}
        onAdd={onAdd ? () => { setOpenDay(null); onAdd(openDay); } : undefined} />, document.body) : null}
    </div>
  );
}

/** A day's items over the whole screen; "‹ ›" go a day back and on. */
function PhoneCalendarDay({ day, rows, rowTitle, rowDate, label, onDay, onClose, onOpenRow, onAdd }: {
  day: Date; rows: Page[]; rowTitle(row: Page): string; rowDate(row: Page): string | null; label: string;
  onDay(day: Date): void; onClose(): void; onOpenRow(rowId: string): void; onAdd?: () => void;
}) {
  const { t } = useTranslation(["calendarView", "common"]);
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === "Escape" && !event.defaultPrevented) onClose(); };
    document.addEventListener("keydown", close);
    return () => document.removeEventListener("keydown", close);
  }, [onClose]);
  return (
    <div className={styles.daySheet} role="dialog" aria-modal="true" aria-label={label} data-phone-calendar-day={dateKey(day)}>
      <div className={styles.dayHead}>
        <div className={styles.dayTitle}>
          <button type="button" className={styles.arrow} aria-label={t("calendarView:phone.previousDay")} onClick={() => onDay(addDays(day, -1))}><ChevronLeft size="regular" /></button>
          <span>{label}</span>
          <button type="button" className={styles.arrow} aria-label={t("calendarView:phone.nextDay")} onClick={() => onDay(addDays(day, 1))}><ChevronRight size="regular" /></button>
        </div>
        <button type="button" className={styles.done} onClick={onClose}>{t("common:actions.done")}</button>
      </div>
      {rows.length ? (
        <div className={styles.dayGroup}>
          {rows.map((row) => {
            const date = rowDate(row);
            return (
              <button key={row.id} type="button" className={styles.dayItem} data-dated={date ? "true" : undefined} onClick={() => onOpenRow(row.id)}>
                {date ? <span className={styles.dayItemDate}>{date}</span> : null}
                <span className={styles.dayItemTitle}>{rowTitle(row)}</span>
              </button>
            );
          })}
        </div>
      ) : null}
      {onAdd ? (
        <div className={styles.dayGroup}>
          <button type="button" className={styles.dayAdd} onClick={onAdd}><Plus size="regular" aria-hidden="true" />{t("calendarView:phone.newItem")}</button>
        </div>
      ) : null}
    </div>
  );
}
