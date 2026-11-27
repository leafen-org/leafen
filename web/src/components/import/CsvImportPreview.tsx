import { CheckIcon, EyeSlashIcon, FileText, TableIcon } from "@/icons/leafen";
import { csvURLValue, readCSVCheckbox, readCSVImportDate, readCSVImportNumber, type CSVPreviewColumn } from "@/lib/csvImportModel";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { COLOR_NAMES, chipStyle } from "../database/colors";
import { formatDate } from "../database/dateUtils";
import { PropertyTypeIcon } from "../database/PropertyTypeIcon";
import { shortUrlParts } from "../database/urlDisplay";
import type { PropertyType } from "@/lib/types";
import styles from "./CsvImportPreview.module.css";

/** The colors a preview's chips take, as Notion's new options do: any but the default. */
const CHIP_COLORS = COLOR_NAMES.filter((color) => color !== "default");

function hashed(text: string) {
  let hash = 0;
  for (const character of text) hash = (hash * 31 + character.codePointAt(0)!) >>> 0;
  return hash;
}

/**
 * The right of the CSV window, as Notion's (seen 2026-10-01): before the
 * columns are set, the table's name over a blank table; after, the table
 * with its "표" view, the columns it takes and the CSV's rows in them, read
 * as the import reads them — a number right-aligned, a checkbox as its box,
 * a day in full, a URL short, a select's names as chips. Notion colors those chips anew
 * each time it draws the table, whatever the options' colors; here they keep
 * one color while the window is open. A new table no column is the title of
 * shows no table but "미리보기 사용 불가" and why, as Notion's (seen 2026-11-12).
 */
export function CsvImportPreview({ title, columns, rows }: {
  title: string;
  /** The table's columns once set; none shows the blank table. */
  columns?: readonly CSVPreviewColumn[];
  rows: readonly (readonly string[])[];
}) {
  const { t } = useTranslation(["importDialog"]);
  const [seed] = useState(() => Math.floor(Math.random() * CHIP_COLORS.length));
  const chip = (column: CSVPreviewColumn, name: string) => (
    <span key={name} className={styles.chip} data-csv-chip="true"
      style={chipStyle(CHIP_COLORS[(hashed(`${column.key}\u0000${name}`) + seed) % CHIP_COLORS.length])}>
      {name}
    </span>
  );
  const cell = (column: CSVPreviewColumn, text: string) => {
    switch (column.type) {
      case "number": {
        const value = readCSVImportNumber(text);
        return value === null ? "" : String(value);
      }
      case "checkbox": {
        const checked = readCSVCheckbox(text) === true;
        return (
          <span className={styles.checkbox} data-csv-checkbox="true" data-checked={checked ? "true" : undefined}>
            {checked ? <CheckIcon size="tiny" aria-hidden="true" /> : null}
          </span>
        );
      }
      case "date": {
        const day = readCSVImportDate(text);
        return day ? formatDate(day, { dateFormat: "full" }) : "";
      }
      case "select":
      case "status":
        return text.trim() ? <span className={styles.chips}>{chip(column, text.trim())}</span> : "";
      case "multi_select": {
        const names = [...new Set(text.split(",").map((part) => part.trim()).filter(Boolean))];
        return names.length ? <span className={styles.chips}>{names.map((name) => chip(column, name))}</span> : "";
      }
      case "url": {
        // Short, as the table shows it: its host, then what follows dimmed.
        const { host, path } = shortUrlParts(csvURLValue(text));
        return <>{host}{path ? <span className={styles.urlPath}>{path}</span> : null}</>;
      }
      case "title":
      case "email":
      case "phone":
        return text.trim();
      default:
        return text;
    }
  };
  if (columns && !columns.some((column) => column.title)) {
    return (
      <section className={styles.preview} aria-label={t("importDialog:csvWindow.preview")} data-csv-preview="true"
        data-unavailable="true">
        <div className={styles.unavailable} data-csv-preview-unavailable="true">
          <EyeSlashIcon size="card" weight="regular" aria-hidden="true" />
          <strong>{t("importDialog:csvWindow.previewUnavailable")}</strong>
          <span>{t("importDialog:csvWindow.previewNeedsTitle")}</span>
        </div>
      </section>
    );
  }
  return (
    <section className={styles.preview} aria-label={t("importDialog:csvWindow.preview")} data-csv-preview="true"
      data-set={columns ? "true" : undefined}>
      <h3 className={styles.previewTitle} data-empty={title ? undefined : "true"} data-set={columns ? "true" : undefined}>
        <TableIcon size="large" aria-hidden="true" />
        <span>{title || t("importDialog:csvWindow.newTable")}</span>
      </h3>
      {columns ? (
        <>
          <div className={styles.previewViews}>
            <span className={styles.previewTab}><TableIcon size="small" aria-hidden="true" />{t("importDialog:csvWindow.tableView")}</span>
          </div>
          <div className={styles.previewTable}>
            <table>
              <thead>
                <tr>{columns.map((column) => (
                  <th key={column.key} scope="col"><span className={styles.previewHead}><PropertyTypeIcon type={column.type as PropertyType} />{column.name}</span></th>
                ))}</tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={index}>
                    {columns.map((column) => (
                      <td key={column.key} data-title={column.title ? "true" : undefined} data-type={column.type}>
                        {column.title ? <span className={styles.previewRowTitle}><FileText size="small" aria-hidden="true" />{column.source >= 0 ? cell(column, row[column.source] ?? "") : ""}</span>
                          : column.source >= 0 ? cell(column, row[column.source] ?? "") : ""}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : (
        <div className={styles.skeleton} aria-hidden="true">
          {Array.from({ length: 18 }, (_, index) => <span key={index} data-head={index < 3 ? "true" : undefined} />)}
        </div>
      )}
    </section>
  );
}
