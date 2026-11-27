import { SwitchInput } from "@/components/Switch";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight, SheetChevronRight } from "@/icons/leafen";
import {
  allTimeZones, loadZoneCities, offsetShortName, readTimeZoneNamesAhead, recentTimeZones, timeZoneGroups, timeZoneRow, timeZoneShortName,
  type ZoneCities,
} from "./dateTimeZones";
import { DateEditorChoices, DateZoneChoices } from "./DateEditorChoices";
import { CellMenuPortal } from "./PropertyCellMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { PhoneSheetTick } from "./TablePropertyPhoneSheet";
import cellStyles from "./databaseSheet";
import styles from "./PropertyDateEditor.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

export type DateEditorChoice = {
  value: string;
  label: string;
  /** Its words on the row once chosen, where they are shorter than the list's ("당일" for "당일 (오전 9:00)"). */
  current?: string;
  /** A second, grey line ("자정~오전 3시"). */
  description?: string;
  /** A line after it, as Notion parts "일" from the weekdays. */
  ruleAfter?: boolean;
};

/** The date property's formats, in the order Notion's date editor lists them (seen 2026-09-30). */
export const DATE_FORMATS = ["full", "short", "month_day_year", "day_month_year", "year_month_day", "relative"] as const;
export const TIME_FORMATS = ["hidden", "12", "24"] as const;

/**
 * A row of a desktop window that opens a list beside it: its label, its value
 * in grey and "›"; on a touch screen's date sheet the sheets' 18px "›"
 * (DateEditorSheet.module.css sets it 16 after the value).
 */
export function ChoiceRowButton({ rowRef, label, current, open, disabled, onOpen, data }: {
  rowRef: React.RefObject<HTMLButtonElement | null>;
  label: string;
  current: ReactNode;
  open: boolean;
  disabled?: boolean;
  onOpen: () => void;
  data?: Record<`data-${string}`, string>;
}) {
  const { touch } = useSidebarLayout(true);
  return (
    <button ref={rowRef} type="button" className={styles.choiceRow} disabled={disabled}
      aria-haspopup="listbox" aria-expanded={open} onClick={onOpen} {...data}>
      <span className={styles.choiceLabel}>{label}</span>
      <span className={styles.choiceValue}>{current}</span>
      {touch ? <SheetChevronRight size="page" className={styles.choiceChevron} aria-hidden="true" />
        : <ChevronRight size="tiny" className={styles.choiceChevron} />}
    </button>
  );
}

/**
 * A row of the date editor that opens a list of choices, as in Notion: its
 * label, its choice and a chevron.
 */
export function DateChoiceRow({ label, choices, selected, disabled, onChoose, data, width = 180 }: {
  label: string;
  /** Its list's width: Notion's formats 180, its reminders 250 (seen 2026-11-05). */
  width?: number;
  choices: readonly DateEditorChoice[];
  selected: string;
  disabled?: boolean;
  onChoose: (value: string) => void;
  data?: Record<`data-${string}`, string>;
}) {
  const [open, setOpen] = useState(false);
  const { touch } = useSidebarLayout(true);
  const rowRef = useRef<HTMLButtonElement>(null);
  const close = () => setOpen(false);
  const chosen = choices.find((choice) => choice.value === selected);
  const current = chosen?.current ?? chosen?.label ?? "";
  return (
    <>
      <ChoiceRowButton rowRef={rowRef} label={label} current={current} open={open} disabled={disabled}
        onOpen={() => setOpen(true)} data={data} />
      {/* A touch screen's list is a half sheet named for the row, the chosen one ticked, as Notion's phone
          sheets of choices (seen 2026-11-15 at 402pt). */}
      {open && touch ? (
        <PhoneSheet size="half" title={label} onClose={close} data={{ "data-date-choices": "" }}>
          <PhoneSheetGroup>
            {choices.map((choice) => (
              <PhoneSheetRow key={choice.value} label={choice.label} description={choice.description} more={false}
                value={choice.value === selected ? <PhoneSheetTick /> : undefined}
                onPress={() => { onChoose(choice.value); close(); }} data={{ "data-phone-choice": choice.value }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : open && (
        <CellMenuPortal beside triggerRef={rowRef} onClose={close} ariaLabel={label} className={styles.choiceMenu}
          backdropClassName={cellStyles.menuBackdrop} width={width}>
          {placed => <DateEditorChoices choices={choices} selected={selected} label={label} open={placed}
            onChoose={value => { onChoose(value); close(); }} />}
        </CellMenuPortal>
      )}
    </>
  );
}

/**
 * The date editor's time zone row: the zone's short name, and a list to search
 * by city, name or offset, as in Notion.
 */
export function DateZoneRow({ zone, offset = "", current, at, disabled, label: ownLabel, onChoose }: {
  /** The zone the date keeps its time in. */
  zone: string;
  /** The offset a time is written with instead of a zone, as ":00.000-04:00". */
  offset?: string;
  /** The reader's own zone. */
  current: string;
  /** The moment whose name the zone shows, as summer time changes it. */
  at: Date;
  disabled?: boolean;
  /** Its name where a screen names it otherwise, as an automation's schedule ("Timezone", Notion's). */
  label?: string;
  onChoose: (zone: string) => void;
}) {
  const { t, i18n } = useTranslation("propertyCell");
  const label = ownLabel ?? t("timeZone.label");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const rowRef = useRef<HTMLButtonElement>(null);
  // The cities in the reader's language load with the editor, before the
  // list opens.
  const language = i18n.resolvedLanguage ?? i18n.language;
  const [cities, setCities] = useState<ZoneCities>({});
  useEffect(() => {
    let current = true;
    void loadZoneCities(language).then((loaded) => { if (current) setCities(loaded); });
    return () => { current = false; };
  }, [language]);
  // The zones' names are read past the row's first frame, so the list opens drawing from them.
  useEffect(() => readTimeZoneNamesAhead(), []);
  const rows = useMemo(() => {
    if (!open) return [];
    const now = new Date();
    return allTimeZones().map((candidate) => timeZoneRow(candidate, now, cities));
  }, [cities, open]);
  const groups = useMemo(() => open ? timeZoneGroups(rows, query, current, recentTimeZones(), cities) : [],
    [cities, current, open, query, rows]);
  const close = () => setOpen(false);
  return (
    <>
      <ChoiceRowButton rowRef={rowRef} label={label} current={offset ? offsetShortName(offset) : timeZoneShortName(zone, at)} open={open}
        disabled={disabled} onOpen={() => { setQuery(""); setOpen(true); }} data={{ "data-date-time-zone": zone }} />
      {open && (
        <CellMenuPortal beside triggerRef={rowRef} onClose={close} ariaLabel={label} className={`${styles.choiceMenu} ${styles.zoneMenu}`}
          backdropClassName={cellStyles.menuBackdrop} width={342}>
          {placed => <DateZoneChoices groups={groups} zone={zone} offset={offset} query={query} onQuery={setQuery}
            open={placed} label={label} searchLabel={t("timeZone.search")}
            groupLabel={key => t(`timeZone.${key}`)} noneLabel={t("timeZone.none")}
            onChoose={value => { onChoose(value); close(); }} />}
        </CellMenuPortal>
      )}
    </>
  );
}

/** A row of the date editor with a switch, as Notion's end date and include time rows. */
export function DateSwitchRow({ label, checked, disabled, onChange }: {
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  // The row's words switch it too, as Notion's (seen 2026-11-02): a press on them did nothing
  // where the menu holding the row takes its clicks' default.
  return (
    <label className={styles.switchRow} data-disabled={disabled ? "true" : undefined}
      onClick={(event) => {
        if (disabled || event.target instanceof HTMLInputElement) return;
        event.preventDefault();
        onChange(!checked);
      }}>
      <span className={styles.choiceLabel}>{label}</span>
      <SwitchInput checked={checked} disabled={disabled}
        onChange={(event) => onChange(event.target.checked)} />
    </label>
  );
}

/**
 * One of the date editor's inputs for a day or a time: Enter or leaving it
 * takes what was typed, Escape puts back the date as it is.
 */
export function DateDraftInput({ label, value, iso, placeholder, autoFocus, data, active, onChange, onCommit, onReset }: {
  label: string;
  value: string;
  /** The day ("2026-11-15") or time ("14:30") a touch screen's own picker field shows. */
  iso?: string;
  placeholder?: string;
  autoFocus?: boolean;
  data: string;
  /** The end of a range being set, ringed. */
  active?: boolean;
  onChange: (next: string) => void;
  /** Takes the input's text: on Enter, as asked; on leaving it, as left; or the picked day or time given. */
  onCommit: (asked: boolean, picked?: string) => void;
  onReset: () => void;
}) {
  // Opened, its text is chosen to type over, as Notion's (seen 2026-11-02).
  const ref = useRef<HTMLInputElement>(null);
  const { touch } = useSidebarLayout(true);
  useEffect(() => { if (autoFocus && !touch) ref.current?.select(); }, [autoFocus, touch]);
  // On a touch screen the field is the system's own date or time field: a press opens the phone's picker,
  // no keyboard, and a pick is written at once, as Notion's phone sheet "날짜" (seen 2026-11-15 on a Galaxy A90).
  if (touch && iso !== undefined) {
    return <input ref={ref} className={styles.input} type={data.endsWith("time") ? "time" : "date"} aria-label={label}
      value={iso} data-date-input={data} data-native-date="" data-active={active ? "true" : undefined}
      onChange={(event) => { if (event.target.value) onCommit(true, event.target.value); }} />;
  }
  return (
    <input ref={ref} className={styles.input} type="text" aria-label={label} autoFocus={autoFocus}
      placeholder={placeholder} value={value} data-date-input={data} data-active={active ? "true" : undefined}
      onChange={(event) => onChange(event.target.value)} onBlur={() => onCommit(false)}
      onKeyDown={(event) => {
        if (imeComposing(event)) return;
        if (fieldEnter(event)) {
          event.preventDefault();
          onCommit(true);
        } else if (event.key === "Escape") {
          onReset();
        }
      }} />
  );
}
