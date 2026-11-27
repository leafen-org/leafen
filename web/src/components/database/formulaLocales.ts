import { TIME_ZONE_SHORT_NAMES } from "../../lib/timeZoneNames.generated";
import { GENERATED_FORMULA_LOCALES, type GeneratedFormulaLocale } from "./formulaLocales.generated";

// A formula writes dates in the viewer's language with moment.js's words and
// layouts, as Notion's formulas do: English, Korean as Notion's Korean screen
// showed them (2026-09-29), and the other languages both Leafen and Notion show
// from moment.js's locale files (formulaLocales.generated.ts). Other
// languages write English, as Notion does for a language it doesn't show, and
// as the server and the Public API always do (database_formula_formats.go).

export type FormulaLayout = "LT" | "LTS" | "L" | "LL" | "LLL" | "LLLL" | "l" | "ll" | "lll" | "llll";

export type FormulaLocale = {
  // zones names time zones, by offset in minutes, where the language's short
  // name is not the plain offset.
  zones: Readonly<Record<string, Readonly<Record<number, string>>>>;
  // intl is the language Intl names time zones in for a language without a
  // zones table.
  intl?: string;
  months: readonly string[];
  monthsShort: readonly string[];
  weekdays: readonly string[];
  weekdaysShort: readonly string[];
  weekdaysMin: readonly string[];
  meridiem: (hour: number, minute: number, lower: boolean) => string;
  ordinal: (day: number) => string;
  layouts: Readonly<Record<FormulaLayout, string>>;
};

// The viewer a formula writes for: the time zone it reads dates in and the
// language it writes them in.
export type FormulaViewer = { timeZone: string; locale: FormulaLocale };

const ENGLISH_WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const ENGLISH_MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

const ENGLISH: FormulaLocale = {
  zones: TIME_ZONE_SHORT_NAMES.en,
  months: ENGLISH_MONTHS,
  monthsShort: ENGLISH_MONTHS.map((month) => month.slice(0, 3)),
  weekdays: ENGLISH_WEEKDAYS,
  weekdaysShort: ENGLISH_WEEKDAYS.map((day) => day.slice(0, 3)),
  weekdaysMin: ENGLISH_WEEKDAYS.map((day) => day.slice(0, 2)),
  meridiem: (hour, _minute, lower) => (hour < 12 ? (lower ? "am" : "AM") : (lower ? "pm" : "PM")),
  ordinal: (day) => {
    const suffix = day % 100 >= 11 && day % 100 <= 13 ? "th"
      : day % 10 === 1 ? "st" : day % 10 === 2 ? "nd" : day % 10 === 3 ? "rd" : "th";
    return `${day}${suffix}`;
  },
  layouts: {
    LT: "h:mm A", LTS: "h:mm:ss A", L: "MM/DD/YYYY", LL: "MMMM D, YYYY", LLL: "MMMM D, YYYY h:mm A",
    LLLL: "dddd, MMMM D, YYYY h:mm A", l: "M/D/YYYY", ll: "MMM D, YYYY", lll: "MMM D, YYYY h:mm A",
    llll: "ddd, MMM D, YYYY h:mm A",
  },
};

const KOREAN_MONTHS = Array.from({ length: 12 }, (_, index) => `${index + 1}월`);
const KOREAN_WEEKDAYS_SHORT = ["일", "월", "화", "수", "목", "금", "토"];

const KOREAN: FormulaLocale = {
  zones: TIME_ZONE_SHORT_NAMES.ko,
  months: KOREAN_MONTHS,
  monthsShort: KOREAN_MONTHS,
  weekdays: KOREAN_WEEKDAYS_SHORT.map((day) => `${day}요일`),
  weekdaysShort: KOREAN_WEEKDAYS_SHORT,
  weekdaysMin: KOREAN_WEEKDAYS_SHORT,
  meridiem: (hour) => (hour < 12 ? "오전" : "오후"),
  ordinal: (day) => `${day}일`,
  layouts: {
    LT: "A h:mm", LTS: "A h:mm:ss", L: "YYYY.MM.DD.", LL: "YYYY년 MMMM D일", LLL: "YYYY년 MMMM D일 A h:mm",
    LLLL: "YYYY년 MMMM D일 dddd A h:mm", l: "YYYY.MM.DD.", ll: "YYYY년 MMMM D일", lll: "YYYY년 MMMM D일 A h:mm",
    llll: "YYYY년 MMMM D일 dddd A h:mm",
  },
};

// The Intl language that names time zones for each generated language.
const INTL_LANGUAGES: Readonly<Record<string, string>> = { "zh-Hans": "zh-CN", "zh-Hant": "zh-TW" };

function generatedLocale(language: string, table: GeneratedFormulaLocale): FormulaLocale {
  return {
    zones: {},
    intl: INTL_LANGUAGES[language] ?? language,
    months: table.months,
    monthsShort: table.monthsShort,
    weekdays: table.weekdays,
    weekdaysShort: table.weekdaysShort,
    weekdaysMin: table.weekdaysMin,
    meridiem: (hour, minute, lower) => {
      const at = hour * 60 + minute;
      let words = table.meridiem[0];
      for (const entry of table.meridiem) if (entry[0] <= at) words = entry;
      return lower ? words[2] : words[1];
    },
    ordinal: (day) => table.ordinals[day - 1] ?? String(day),
    layouts: table.layouts,
  };
}

const GENERATED = new Map(Object.entries(GENERATED_FORMULA_LOCALES)
  .map(([language, table]) => [language.toLowerCase(), generatedLocale(language, table)]));

// A language by its code, "pt-BR" or "zh-Hans", or its first part alone for a
// language written one way, "ja-JP" as "ja"; Norwegian as Bokmål.
function knownLocale(given: string) {
  const code = given.toLowerCase();
  const first = code.split("-")[0];
  return GENERATED.get(code)
    ?? (first === "zh" ? GENERATED.get(/-(tw|hk|mo|hant)/u.test(code) ? "zh-hant" : "zh-hans") : undefined)
    ?? (first === "no" ? GENERATED.get("nb") : undefined)
    ?? (first !== "pt" ? GENERATED.get(first) : undefined);
}

// formulaLocale is the language formulas write dates in: the one given, or
// the page's, else English.
/** The language the screen is in, for what the browser writes in it. */
export function formulaLanguage() {
  return (typeof document === "undefined" ? "" : document.documentElement.lang) || "en";
}

export function formulaLocale(language?: string): FormulaLocale {
  const given = language ?? (typeof document === "undefined" ? "" : document.documentElement.lang);
  if (given.toLowerCase().startsWith("ko")) return KOREAN;
  return knownLocale(given) ?? ENGLISH;
}
