import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import { DateFilterCalendar } from "../database/DateFilterEditor";
import { SEARCH_DATE_PRESETS, type SearchDateField, type SearchFilters } from "./searchFilterModel";
import styles from "./SearchFilterMenu.module.css";

const DATE_FIELDS: SearchDateField[] = ["created", "edited"];

/**
 * The search window's date filter, as Notion's (seen 2026-10-12): the time it
 * bounds ("최종 편집 ⌄" — 생성일 or 최종 편집) and "초기화" at its head, then
 * "오늘", "지난 7일" and "지난 30일", and a month of days where two picks make
 * a range.
 */
export function SearchDateFilter({ date, onChange }: {
  date: SearchFilters["date"];
  onChange(next: SearchFilters["date"]): void;
}) {
  const { t } = useTranslation("searchDialog");
  const [fields, setFields] = useState(false);
  const field = date.field ?? "edited";
  // A first day starts a range and a later one ends it; any other pick starts again.
  const pick = (day: string) => onChange(date.preset === "custom" && date.from && date.from === date.to && day > date.from
    ? { ...date, to: day } : { preset: "custom", field, from: day, to: day });
  return (
    <div className={styles.dateFilter} data-search-date-filter>
      <div className={styles.dateFilterHead}>
        <button type="button" className={styles.dateFilterField} aria-haspopup="menu" aria-expanded={fields}
          data-search-date-field={field} onClick={() => setFields((open) => !open)}>
          {t(`date.fields.${field}`)}<ChevronDown size="micro" aria-hidden="true" />
        </button>
        <button type="button" className={styles.dateFilterReset} data-search-date-reset
          onClick={() => onChange({ preset: "any", field })}>{t("date.reset")}</button>
        {fields ? (
          <div className={styles.dateFilterFields} role="menu" aria-label={t("filters.date")}>
            {DATE_FIELDS.map((choice) => (
              <button key={choice} type="button" role="menuitemradio" aria-checked={choice === field} className={styles.filterMenuItem}
                onClick={() => { onChange({ ...date, field: choice }); setFields(false); }}>
                <span className={styles.filterMenuLabel}>{t(`date.fields.${choice}`)}</span>
                {choice === field ? <CheckIcon size="small" aria-hidden="true" /> : null}
              </button>
            ))}
          </div>
        ) : null}
      </div>
      {SEARCH_DATE_PRESETS.map((preset) => (
        <button key={preset} type="button" role="menuitemradio" aria-checked={date.preset === preset} className={styles.filterMenuItem}
          data-active={date.preset === preset ? "true" : undefined} data-search-date-preset={preset}
          onClick={() => onChange({ preset, field })}>
          <span className={styles.filterMenuCheck} aria-hidden="true">{date.preset === preset ? <CheckIcon size="small" /> : null}</span>
          <span className={styles.filterMenuLabel}>{t(`date.${preset}`)}</span>
        </button>
      ))}
      <DateFilterCalendar start={date.preset === "custom" ? date.from : undefined} end={date.preset === "custom" ? date.to : undefined}
        onPick={pick} />
    </div>
  );
}
