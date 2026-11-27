"use client";

import { memo, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type RefCallback } from "react";
import { useTranslation } from "react-i18next";
import { activeDateLocale, activeOwnDigitsLocale } from "@/lib/i18n";
import type { DbProperty, Page } from "@/lib/types";
import { Plus } from "../icons";
import { CardChoiceHalo } from "./cardSelection";
import { CardSubitemMeta, CardSubitemParent, rowCardProperties } from "./CardSubitemMeta";
import { CardTitleIcon } from "./CardTitleIcon";
import { CardValueTip } from "./CardValueTip";
import type { CalendarWeekCard } from "./calendarViewModel";
import { conditionalColorBackground, type ConditionalColorMatch } from "./conditionalColorModel";
import styles from "./databaseSheet";
import { dateKey } from "./dateUtils";
import { dateInShape } from "./dateShapes";
import { PersonValueLookArea } from "./PersonBadge";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import type { CardViewSubitemPresentation } from "./queryProjection";
import { RowCommentCount } from "./RowCommentCount";
import { RowTitleText } from "./RichTextCellText";
import { rowOpenModeOfClick } from "./rowOpenClick";
import { useRowDisplayTitle } from "./untitledRowName";

// Each formatter made once a language (dateShapes.ts).
export const shortDateLabel = (date: Date) => dateInShape(date, "shortDay", activeDateLocale());
export const fullDateLabel = (date: Date) => dateInShape(date, "longDate", activeDateLocale());
// A month's first day in its cell, in the language's own digits as Notion's
// calendar writes it (Arabic "١ نوفمبر" among Western 2…30, seen 2026-11-19).
const monthStartLabel = (date: Date) => dateInShape(date, "shortDay", activeOwnDigitsLocale());

/** What a week's cards are drawn with: made again as the view's rows, values or settings change. */
export type CalendarCardLook = {
  metaProps: DbProperty[];
  presentation: CardViewSubitemPresentation;
  pagesById: Record<string, Page>;
  propertyValueContext: PropertyValueReadContext;
  cardColor(row: Page): ConditionalColorMatch | null;
  pageIcons: boolean;
  cardTime(row: Page, continuesBefore: boolean, continuesAfter: boolean): string;
};

/** What a week's days and cards do, the same functions for the calendar's life (they read its latest state). */
export type CalendarWeekActions = {
  dayRef(key: string): RefCallback<HTMLElement>;
  cardRef(rowId: string): RefCallback<HTMLElement>;
  addOn(day: Date): void;
  openRow(rowId: string, mode: ReturnType<typeof rowOpenModeOfClick>): void;
  cardKey(row: Page, event: ReactKeyboardEvent<HTMLElement>): void;
  cardMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
};

type CalendarWeekProps = {
  weekIndex: number;
  days: readonly Date[];
  cards: readonly CalendarWeekCard<Page>[];
  /** Its lines' measured heights, and a line's height before it is measured. */
  heights: readonly number[] | undefined;
  cardLine: number;
  /** The month a month's view shows ("year-month"), its other days faint; none for a week's. */
  shownMonth: string | null;
  todayKey: string;
  /** Whether its days make a page ("+", a double click). */
  adds: boolean;
  /** The day of this week a card is dragged over, and the card of this week being dragged. */
  dragOverKey: string | null;
  draggingRowId: string | null;
  look: CalendarCardLook;
  act: CalendarWeekActions;
};

/**
 * A week of the calendar: its days, and the cards that start in each. It is
 * drawn again only for what changed in it — a card moved, dropped or dragged
 * over it, its lines measured anew — and with every change to the view's rows
 * where it holds cards, whose values may show other rows' (a relation's title,
 * a rollup, a sub-item's parent). A month on its database's own page runs to
 * 27 weeks, all drawn again five times as a card was dropped on a day
 * (2026-11-18: 155–163 ms to the card in its day at CPU ×2 under the NAS
 * condition); a week with no cards reads no rows.
 */
export const CalendarWeek = memo(function CalendarWeek({ weekIndex, days, cards, heights, cardLine, shownMonth, todayKey, adds, dragOverKey, draggingRowId, look, act }: CalendarWeekProps) {
  const { t } = useTranslation(["calendarView", "common"]);
  const displayTitle = useRowDisplayTitle();
  const { metaProps, presentation, pagesById, propertyValueContext, cardColor, pageIcons, cardTime } = look;
  const laneTop = (lane: number) => Array.from({ length: lane }, (_, index) => heights?.[index])
    .reduce<number>((sum, height) => sum + (height || cardLine) + 6, 0);
  // A week grows with its cards, however many a day has, as Notion's (seen 2026-11-04).
  const lanes = cards.reduce((count, card) => Math.max(count, card.lane + 1), 1);
  return (
    <div className={styles.calendarGridWeek} role="row">
      {days.map((day, dayIndex) => {
        const key = dateKey(day);
        const covering = cards.filter((card) => card.start <= dayIndex && dayIndex < card.start + card.span);
        return (
          <div key={key} ref={act.dayRef(key)} className={styles.calendarCell} role="gridcell" tabIndex={-1}
            aria-label={t("calendarView:cell.ariaLabel", { date: fullDateLabel(day), count: covering.length })}
            aria-current={key === todayKey ? "date" : undefined}
            data-outside={shownMonth !== null && `${day.getFullYear()}-${day.getMonth()}` !== shownMonth ? "true" : undefined}
            data-today={key === todayKey ? "true" : undefined}
            data-weekend={day.getDay() === 0 || day.getDay() === 6 ? "true" : undefined}
            data-drag-over={dragOverKey === key ? "true" : undefined}
            onDoubleClick={(e) => { if (adds && !(e.target as HTMLElement).closest("button")) act.addOn(day); }}>
            {/* A day's space adds a page on a double click alone, not one click nor one closing a panel (Notion's, 2026-11-15, two tabs); "+" left, the day right, a month's first with its month (2026-11-03). */}
            <div className={styles.calendarDayHead}>
              {adds && <button type="button" aria-label={t("calendarView:cell.newRowAriaLabel", { date: key })} onClick={() => act.addOn(day)}><Plus size="small" /></button>}
              {day.getDate() === 1 ? <span data-month-start>{`${monthStartLabel(day)} `}</span> : <span>{day.getDate()}</span>}
            </div>
            <div className={styles.calendarCards} style={{ height: laneTop(lanes) }}>
              {cards.filter((card) => card.start === dayIndex).map(({ row, span, lane, continuesBefore, continuesAfter }) => {
                const rowTitle = displayTitle(row);
                const tint = cardColor(row);
                const shownProps = rowCardProperties(row, metaProps, presentation);
                const time = cardTime(row, Boolean(continuesBefore), Boolean(continuesAfter));
                return (
                  <button type="button" key={row.id} ref={act.cardRef(row.id)} className={styles.calendarCard} data-row-id={row.id}
                    data-card-choice={row.id} data-card-title={rowTitle}
                    data-calendar-week={weekIndex} data-calendar-lane={lane} data-calendar-span={span}
                    data-continues-before={continuesBefore ? "true" : undefined} data-continues-after={continuesAfter ? "true" : undefined}
                    data-conditional-color={tint?.color}
                    style={{ top: laneTop(lane), minHeight: heights?.[lane], ["--calendar-span" as string]: span, ["--calendar-meta-lines" as string]: metaProps.length, ...(tint ? { background: conditionalColorBackground(tint.color) } : {}) }}
                    aria-label={t("calendarView:card.openAriaLabelWithDate", { title: rowTitle, date: fullDateLabel(day) })}
                    data-dragging={draggingRowId === row.id ? "true" : undefined}
                    onKeyDown={(e) => act.cardKey(row, e)}
                    onClick={(e) => act.openRow(row.id, rowOpenModeOfClick(e))}
                    onAuxClick={(e) => { if (e.button === 1) { e.preventDefault(); act.openRow(row.id, "new_tab"); } }}
                    onContextMenu={(e) => act.cardMenu(row.id, e)}>
                    <CardChoiceHalo itemId={row.id} inset={1} />
                    <CardSubitemParent row={row} pagesById={pagesById} presentation={presentation} />
                    <span className={styles.calendarCardHead}>
                      {time ? <span className={styles.calendarCardTime}>{time}</span> : null}
                      {/* With no values shown, its comment count rides on the name's line at its end (Notion's calendar, seen 2026-11-06 and 2026-11-15). */}
                      {shownProps.length === 0 ? <RowCommentCount rowId={row.id} insideButton /> : null}
                      <span className={styles.calendarCardTitle}><CardTitleIcon row={row} show={pageIcons} size={14} className={styles.cardTitleIcon} /><RowTitleText row={row} title={rowTitle} /></span>
                    </span>
                    <span className={styles.calendarCardMeta}>
                      <PersonValueLookArea look="calendar">{shownProps.map((property) => <CardValueTip key={property.id} name={property.name}><span className={styles.calendarCardProp} data-prop-type={property.type}><PropValue context={propertyValueContext} row={row} prop={property} interactive={false} checkboxName /></span></CardValueTip>)}</PersonValueLookArea>
                      <CardSubitemMeta row={row} presentation={presentation} />
                    </span>
                    {shownProps.length ? <RowCommentCount rowId={row.id} line inset={0} insideButton /> : null}
                  </button>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}, sameWeek);

function sameWeek(before: CalendarWeekProps, after: CalendarWeekProps) {
  if (before.weekIndex !== after.weekIndex || before.cardLine !== after.cardLine || before.shownMonth !== after.shownMonth
    || before.todayKey !== after.todayKey || before.adds !== after.adds || before.dragOverKey !== after.dragOverKey
    || before.draggingRowId !== after.draggingRowId || before.act !== after.act) return false;
  if (before.days.length !== after.days.length || before.days.some((day, index) => day.getTime() !== after.days[index].getTime())) return false;
  if ((before.heights?.length ?? 0) !== (after.heights?.length ?? 0) || before.heights?.some((height, index) => height !== after.heights?.[index])) return false;
  if (before.cards.length !== after.cards.length || before.cards.some((card, index) => {
    const other = after.cards[index];
    return card.row !== other.row || card.start !== other.start || card.span !== other.span || card.lane !== other.lane
      || card.continuesBefore !== other.continuesBefore || card.continuesAfter !== other.continuesAfter;
  })) return false;
  // A week with no cards draws nothing of the rows.
  return !after.cards.length || before.look === after.look;
}
