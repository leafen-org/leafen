/**
 * A CSV as Notion's "CSV를 Notion으로 가져오기" reads it (seen 2026-10-01):
 * its records, the names its first row gives the columns, the type a new
 * database's column takes at first in the window (inferCSVColumnType — a CSV
 * in a ZIP the server types by a stricter rule of Notion's ZIP import,
 * workspace_import_csv_values.go); and a cell's number, checkbox or day, as
 * the server reads it.
 */

/** The types a new database's column takes, in the order "CSV 열 설정" lists them; "" leaves it out. */
export const CSV_NEW_DATABASE_TYPES = [
  "title", "rich_text", "number", "select", "multi_select", "date", "checkbox", "url", "email", "phone",
] as const;

export type CSVNewDatabaseType = (typeof CSV_NEW_DATABASE_TYPES)[number];

/** The formats "날짜" offers in its submenu (seen 2026-10-01), the new date property's. */
export const CSV_DATE_FORMATS = ["full", "short", "month_day_year", "day_month_year", "year_month_day"] as const;

export type CSVDateFormat = (typeof CSV_DATE_FORMATS)[number];

/** A column's choice: a type, a date with the format it names, or "" to leave the column out. */
export type CSVNewDatabaseChoice = "" | CSVNewDatabaseType | `date:${CSVDateFormat}`;

/** A choice's type and, for a date, its format: "full" unless it names one. */
export function csvChoiceType(choice: CSVNewDatabaseChoice): { type: "" | CSVNewDatabaseType; dateFormat?: CSVDateFormat } {
  if (choice.startsWith("date:")) return { type: "date", dateFormat: choice.slice("date:".length) as CSVDateFormat };
  return choice === "date" ? { type: "date", dateFormat: "full" } : { type: choice as "" | CSVNewDatabaseType };
}

const NUMBER = /^[-+]?(?:\d+|\d*\.\d+)$/u;
const PERCENT = /^(?:\d+|\d*\.\d+)%$/u;
const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/u;
const SCHEME_URL = /^[a-z][a-z\d+.-]*:\/\/\S+$/iu;
const BARE_URL = /^(?:[a-z\d](?:[a-z\d-]*[a-z\d])?\.)+[a-z]{2,}(?::\d+)?(?:[/?#]\S*)?$/iu;
const URL_SCHEME = /^[a-z][a-z\d+.-]*:(?!\d)/iu;
const DASH_DATE_TIME = /^(\d{4})-(\d{1,2})-(\d{1,2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$/iu;
const SLASH_YEAR_DATE_TIME = /^(\d{4})\/(\d{1,2})\/(\d{1,2})(?: (\d{1,2}):(\d{2})(?::(\d{2}))?)?$/u;
const SLASH_DATE_TIME = /^(\d{1,2})\/(\d{1,2})\/(\d{4})(?: (\d{1,2}):(\d{2})(?::(\d{2}))?)?$/u;
const FULL_MONTHS = ["january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"];
const FULL_MONTH_DATE = /^([a-z]+) (\d{1,2}), (\d{4})$/iu;
const DIGITS = /^\d+$/u;
const KOREAN_DATE = /^(\d{4})\s*년\s*(\d{1,2})\s*월\s*(\d{1,2})\s*일?$/u;
const YMD_DATE = /^(\d{4})\s*[-/.]\s*(\d{1,2})\s*[-/.]\s*(\d{1,2})$/u;
const US_DATE = /^(\d{1,2})\s*[-/.]\s*(\d{1,2})\s*[-/.]\s*(\d{2,4})$/u;
const MONTH_DATE = /^([A-Za-z]{3,9})\.? (\d{1,2}), (\d{4})$/u;
const MONTHS = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];
const TRUE_WORDS = new Set(["true", "yes", "y", "1", "checked", "check", "x", "on"]);
const FALSE_WORDS = new Set(["false", "no", "n", "0", "unchecked", "off"]);

const CSV_DELIMITERS = [",", "\t", "|", ";"];
const CSV_DELIMITER_RECORDS = 10;

/**
 * The delimiter a CSV is written with, found as the server finds it and as
 * Notion's window finds it (commas, tabs, pipes and semicolons, seen
 * 2026-11-05): of those whose first records split into two fields or more,
 * the one whose records differ least and, among those, have the most fields.
 * A comma when none does.
 */
export function csvDelimiter(text: string, candidates: readonly string[] = CSV_DELIMITERS) {
  let best = ",", bestDifference = Number.POSITIVE_INFINITY, bestFields = 0;
  for (const candidate of candidates) {
    const records = readCSVRecords(text, CSV_DELIMITER_RECORDS, candidate);
    const fields = records.reduce((sum, record) => sum + record.length, 0) / Math.max(records.length, 1);
    const difference = records.reduce((sum, record, index) => index === 0 ? 0 : sum + Math.abs(record.length - records[index - 1]!.length), 0);
    if (fields < 2 || difference > bestDifference || fields <= bestFields) continue;
    best = candidate;
    bestDifference = difference;
    bestFields = fields;
  }
  return best;
}

/**
 * A CSV's records with a value, read as the server reads them: quoted fields
 * may hold delimiters, quotes and line breaks. At most `limit` records are read.
 */
export function readCSVRecords(text: string, limit = Number.POSITIVE_INFINITY, delimiter = csvDelimiter(text)): string[][] {
  const records: string[][] = [];
  let record: string[] = [];
  let field = "";
  let quoted = false;
  const finish = () => {
    record.push(field);
    field = "";
    if (record.some((value) => value.trim() !== "")) records.push(record);
    record = [];
  };
  for (let index = text.charCodeAt(0) === 0xfeff ? 1 : 0; index < text.length && records.length < limit; index += 1) {
    const character = text[index];
    if (quoted) {
      if (character === "\"" && text[index + 1] === "\"") {
        field += "\"";
        index += 1;
      } else if (character === "\"") {
        quoted = false;
      } else {
        field += character;
      }
    } else if (character === "\"" && field === "") {
      quoted = true;
    } else if (character === delimiter) {
      record.push(field);
      field = "";
    } else if (character === "\n" || character === "\r") {
      if (character === "\r" && text[index + 1] === "\n") index += 1;
      finish();
    } else {
      field += character;
    }
  }
  if (records.length < limit && (field !== "" || record.length > 0)) finish();
  return records;
}

/** The column names the first record gives, with the server's names for blank and repeated ones. */
export function csvColumnNames(first: readonly string[], width: number, names: { title: string; column: (index: number) => string }) {
  const used = new Map<string, number>();
  return Array.from({ length: width }, (_, index) => {
    const base = first[index]?.trim() || (index === 0 ? names.title : names.column(index + 1));
    const count = (used.get(base) ?? 0) + 1;
    used.set(base, count);
    return count > 1 ? `${base} ${count}` : base;
  });
}

const ISO_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/u;

function dayKey(year: number, month: number, day: number) {
  const date = new Date(Date.UTC(year, month - 1, day));
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return null;
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

/** A cell's number, as the server reads it: an optional sign, digits and a point, its commas left out. */
export function readCSVNumber(value: string): number | null {
  const normalized = value.trim().replaceAll(",", "");
  if (!NUMBER.test(normalized)) return null;
  const parsed = Number(normalized);
  return Number.isFinite(parsed) ? parsed : null;
}

/** A cell's checkbox, as the server reads it; null for a word it doesn't take. */
export function readCSVCheckbox(value: string): boolean | null {
  const word = value.trim().toLowerCase();
  if (TRUE_WORDS.has(word)) return true;
  return FALSE_WORDS.has(word) ? false : null;
}

/**
 * A cell's day ("YYYY-MM-DD"), as the server reads it: a Korean or
 * year-first date; a month-first one, or a day-first one when it can't be
 * month-first ("13/02/2026", as Notion reads it); a month's name; an ISO time.
 */
export function readCSVDate(value: string): string | null {
  const raw = value.trim();
  if (!raw || DIGITS.test(raw)) return null;
  for (const [pattern, order] of [[KOREAN_DATE, "ymd"], [YMD_DATE, "ymd"], [US_DATE, "mdy"]] as const) {
    const match = pattern.exec(raw);
    if (!match) continue;
    const [first, second, third] = [Number(match[1]), Number(match[2]), Number(match[3])];
    if (order === "ymd") return dayKey(first, second, third);
    const year = third < 100 ? third + 2000 : third;
    return dayKey(year, first, second) ?? dayKey(year, second, first);
  }
  const named = MONTH_DATE.exec(raw);
  if (named) {
    const month = MONTHS.indexOf(named[1].slice(0, 3).toLowerCase()) + 1;
    return month > 0 ? dayKey(Number(named[3]), month, Number(named[2])) : null;
  }
  return ISO_TIME.test(raw) && !Number.isNaN(Date.parse(raw)) ? raw.slice(0, 10) : null;
}

/** A cell's number as an import keeps it: readCSVNumber's, a percent its number ("12%" is 12, as Notion's import keeps it, seen 2026-11-12). */
export function readCSVImportNumber(value: string): number | null {
  return readCSVNumber(value.trim().replace(/%$/u, ""));
}

/** A 24-hour time to the minute ("T09:30"), "" for none, null for one that can't be. */
function csvClock(hour?: string, minute?: string) {
  if (hour === undefined || minute === undefined) return "";
  return Number(hour) < 24 && Number(minute) < 60 ? `T${hour.padStart(2, "0")}:${minute}` : null;
}

/**
 * A cell's day, or its day and time, written as Notion's import infers a
 * date column from (seen 2026-11-12): a year-first day with dashes or
 * slashes, or a month-first one with slashes (day-first when it can't be
 * month-first), each with or without a 24-hour time after a space — or after
 * a "T" — kept to the minute; a time written with its zone keeps it; a day
 * written as Notion's export writes it ("November 1, 2026"). An abbreviated
 * month, a Korean date, dots and a 12-hour time are not among them.
 */
export function readCSVDateTime(value: string): string | null {
  const raw = value.trim();
  const named = FULL_MONTH_DATE.exec(raw);
  if (named) {
    const month = FULL_MONTHS.indexOf(named[1].toLowerCase()) + 1;
    return month > 0 ? dayKey(Number(named[3]), month, Number(named[2])) : null;
  }
  let match = DASH_DATE_TIME.exec(raw) ?? SLASH_YEAR_DATE_TIME.exec(raw);
  let day: string | null = null;
  if (match) {
    day = dayKey(Number(match[1]), Number(match[2]), Number(match[3]));
  } else if ((match = SLASH_DATE_TIME.exec(raw))) {
    const [first, second, year] = [Number(match[1]), Number(match[2]), Number(match[3])];
    day = dayKey(year, first, second) ?? dayKey(year, second, first);
  }
  const time = match ? csvClock(match[4], match[5]) : null;
  if (!match || !day || time === null) return null;
  const zone = match[7];
  if (!time || !zone) return `${day}${time}`;
  return `${day}${time}:00${zone.toUpperCase() === "Z" ? "Z" : zone.replace(/^([+-]\d{2}):?(\d{2})$/u, "$1:$2")}`;
}

/** A cell's date as an import keeps it: its day and time (readCSVDateTime), else its day (readCSVDate). */
export function readCSVImportDate(value: string): string | null {
  return readCSVDateTime(value) ?? readCSVDate(value);
}

/** A URL cell as an import keeps it: one without a scheme gets "https://", as Notion's does ("example.com", seen 2026-11-12). */
export function csvURLValue(value: string) {
  const url = value.trim();
  return !url || URL_SCHEME.test(url) ? url : `https://${url}`;
}

/**
 * The type a column's values read as, as Notion's import infers it (seen
 * 2026-11-12, blank cells aside): a number when every value is one — signed,
 * or a percent without a sign, with no thousands commas — then a checkbox
 * when every value is a checkbox word ("1" and "0" being numbers first); else
 * an email, a URL (with a scheme or a bare domain) or a date (readCSVDateTime)
 * when at least half the values read as one, in that order; else text.
 */
function inferCSVColumnType(values: readonly string[]): CSVNewDatabaseType {
  const filled = values.map((value) => value.trim()).filter(Boolean);
  if (filled.length === 0) return "rich_text";
  const every = (reads: (value: string) => boolean) => filled.every(reads);
  const half = (reads: (value: string) => boolean) => filled.filter(reads).length * 2 >= filled.length;
  if (every((value) => NUMBER.test(value) || PERCENT.test(value))) return "number";
  if (every((value) => readCSVCheckbox(value) !== null)) return "checkbox";
  if (half((value) => EMAIL.test(value))) return "email";
  if (half((value) => SCHEME_URL.test(value) || BARE_URL.test(value))) return "url";
  if (half((value) => readCSVDateTime(value) !== null)) return "date";
  return "rich_text";
}

/**
 * The type each column of a new database takes at first, as Notion's window
 * chooses (seen 2026-11-12): each as its rows read, and the first read as
 * text the title — a number, date, checkbox, email or URL column before it
 * keeps its type. With no column read as text none is the title, and one is
 * chosen before the import can start.
 */
export function defaultCSVNewDatabaseChoices(records: readonly string[][], width: number): CSVNewDatabaseChoice[] {
  const rows = records.slice(1);
  const choices: CSVNewDatabaseChoice[] = Array.from({ length: width }, (_, column) => (
    inferCSVColumnType(rows.map((row) => row[column] ?? ""))
  ));
  const title = choices.indexOf("rich_text");
  return title < 0 ? choices : choices.map((choice, index) => index === title ? "title" : choice);
}

/**
 * A column of the table an import makes, as the preview shows it: its
 * property's type, and the CSV column that fills it, if any (-1).
 */
export type CSVPreviewColumn = { key: string; name: string; type: string; source: number; title?: boolean };

/** A file's name without its extension, as the preview names the table. */
export function csvFileTitle(name: string) {
  return name.replace(/\.csv$/iu, "");
}
