import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import { createPortal } from "react-dom";
import { CountChevronDown } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { dateRangeFilterValue } from "@/shared/database/queryFilterPolicy";
import { dateFilterLabels } from "./databaseFilterLabels";
import { databaseViewLabels } from "./databaseViewLabels";
import { DateFilterCalendar, useRangePick } from "./DateFilterEditor";
import { DateTextInput } from "./DateTextInput";
import { dateKey, formatDateInput, parseDate } from "./dateUtils";
import styles from "./DateRangeFilterField.module.css";

type DateRange = { start?: string; end?: string };

/** A range's days as its field writes them: "2026년 11월 2일 → 2026년 11월 9일", or the one day picked so far. */
export function dateRangeFilterText(value: unknown) {
  const range = dateRangeFilterValue(value);
  const start = range.start ? formatDateInput(parseDate(range.start)) : "";
  const end = range.end ? formatDateInput(parseDate(range.end)) : "";
  if (start && end) return dateFilterLabels().pillRange(start, end);
  return start || (end ? dateFilterLabels().pillRange("", end).trim() : "");
}

/**
 * A rule's "범위 내" value, as Notion's (seen 2026-11-11, 1440 dark): one
 * field, "범위 선택 ⌄" in grey till a day is picked and then the days
 * ("2026년 11월 2일 → 2026년 11월 9일", cut at its end), opening right under
 * it a 260px window of the start and end fields, each with "ⓧ" while it holds text, and
 * the month's calendar, which stays open as days are picked.
 */
export function DateRangeFilterField({ label, value, onChange }: {
  label: string;
  value: unknown;
  onChange(value: DateRange): void;
}) {
  const rangeLabel = dateFilterLabels().rangeFor(label);
  return (
    <DateValueButton label={rangeLabel} text={dateRangeFilterText(value)} placeholder={dateFilterLabels().pickRange} kind="range">
      {(anchor, close) => <DateRangeWindow anchor={anchor} label={label} value={value} onChange={onChange} onClose={close} />}
    </DateValueButton>
  );
}

/**
 * A rule's day of its own ("사용자 지정 날짜"), as Notion's (seen
 * 2026-11-12, 1440 dark): the same field, "날짜 선택 ⌄" in grey till a day
 * is set and then the day ("2026년 11월 1일"), opening right under it a
 * 260px window of the day's field ("날짜를 선택하거나 입력하세요." while
 * empty, its "ⓧ" while it holds text) and the month's calendar, today circled, which
 * stays open as a day is picked.
 */
export function DateDayFilterField({ label, value, onChange }: {
  label: string;
  value: unknown;
  onChange(value: string | undefined): void;
}) {
  const day = typeof value === "string" && parseDate(value) ? dateKey(value) : "";
  return (
    <DateValueButton label={databaseViewLabels().filterValueFor(label)} text={day ? formatDateInput(parseDate(day)) : ""}
      placeholder={dateFilterLabels().pickDay} kind="day">
      {(anchor, close) => (
        <DateValueWindow anchor={anchor} label={databaseViewLabels().filterValueFor(label)} kind="day" onClose={close}>
          <span className={styles.end}>
            <DateTextInput className={styles.input} value={day} placeholder={dateFilterLabels().dayPlaceholder}
              ariaLabel={databaseViewLabels().filterValueFor(label)} clear={{}}
              onChange={(next) => {
                // An empty field left empty keeps the day: its blur, as a day is picked, would undo the pick.
                if (next || day) onChange(next ?? undefined);
              }} />
          </span>
          <DateFilterCalendar start={day || undefined} end={day || undefined} onPick={onChange} />
        </DateValueWindow>
      )}
    </DateValueButton>
  );
}

/** A rule's date value: a 165px field showing its days or its grey words, its ⌄ at the end, opening its window. */
function DateValueButton({ label, text, placeholder, kind, children }: {
  label: string;
  text: string;
  placeholder: string;
  kind: "range" | "day";
  children(anchor: RefObject<HTMLButtonElement | null>, close: () => void): ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const fieldRef = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button ref={fieldRef} type="button" className={styles.field} aria-haspopup="dialog" aria-expanded={open}
        aria-label={label} {...{ [`data-date-${kind}-field`]: "" }} onClick={() => setOpen(!open)}>
        <span className={text ? styles.text : styles.placeholder}>{text || placeholder}</span>
        <CountChevronDown size="tiny" trim aria-hidden="true" />
      </button>
      {open ? children(fieldRef, () => setOpen(false)) : null}
    </>
  );
}

/** Its window right under its field at its start, its first field taking the keys with the caret at its end, as Notion's. */
// Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: a "범위 내" rule's field pressed, "z" went into
// its 시작일 field (Leafen the same).
function DateValueWindow({ anchor, label, kind, onClose, children }: {
  anchor: RefObject<HTMLButtonElement | null>;
  label: string;
  kind: "range" | "day";
  onClose(): void;
  children: ReactNode;
}) {
  const windowRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(windowRef, (size, viewport) => placeAround(
    popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 0, align: "start" }),
  [anchor], undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current] });
  useEffect(() => {
    const input = windowRef.current?.querySelector<HTMLInputElement>("input");
    input?.focus();
    input?.setSelectionRange(input.value.length, input.value.length);
  }, []);
  return createPortal(
    <div ref={windowRef} role="dialog" aria-label={label} className={`${styles.window} ${kind === "day" ? styles.dayWindow : ""}`}
      style={popoverPlacementStyle(place)} {...{ [`data-date-${kind}-window`]: "" }}
      // Its fields keep the keys as its calendar's days are pressed, a range's
      // going to the end set next (data-due), as Notion's (seen 2026-11-12).
      onPointerUpCapture={(event) => {
        // After the calendar has moved its own focus to the day.
        if ((event.target as Element).closest("[data-date-filter-calendar] button")) focusOpenedField(() =>
          windowRef.current?.querySelector<HTMLInputElement>("[data-due] input") ?? windowRef.current?.querySelector("input"));
      }}>
      {children}
    </div>,
    document.body,
  );
}

function DateRangeWindow({ anchor, label, value, onChange, onClose }: {
  anchor: RefObject<HTMLButtonElement | null>;
  label: string;
  value: unknown;
  onChange(value: DateRange): void;
  onClose(): void;
}) {
  const range = dateRangeFilterValue(value);
  const write = (next: DateRange) => onChange({ ...(next.start ? { start: next.start } : {}), ...(next.end ? { end: next.end } : {}) });
  // Its ends are picked in turn, a field taking the focus making its end next (useRangePick).
  const { due, setDue, pick } = useRangePick(range, write);
  const ends = [
    { part: "start" as const, label: dateFilterLabels().rangeStartFor(label), placeholder: dateFilterLabels().parts.start },
    { part: "end" as const, label: dateFilterLabels().rangeEndFor(label), placeholder: dateFilterLabels().parts.end },
  ];
  return (
    <DateValueWindow anchor={anchor} label={dateFilterLabels().rangeFor(label)} kind="range" onClose={onClose}>
      {ends.map(({ part, label: endLabel, placeholder }) => (
        <span key={part} className={styles.end} data-range-part={part} data-due={due === part ? "" : undefined}>
          <DateTextInput className={styles.input} value={range[part] ?? ""} placeholder={placeholder} ariaLabel={endLabel}
            clear={{ onClear: () => setDue(part) }}
            onFocus={() => setDue(part)} onChange={(day) => {
              // An empty field left empty keeps the range: its blur, as a day is picked, would undo the pick.
              if (day || range[part]) write({ ...range, [part]: day ?? undefined });
            }} />
        </span>
      ))}
      <DateFilterCalendar start={range.start} end={range.end} due={due} onPick={pick} />
    </DateValueWindow>
  );
}
