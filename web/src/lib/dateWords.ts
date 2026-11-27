// How Notion's dates read in the reader's language, in one place: a date
// picker's weekday heads, a day in a date editor's field, a date's time and
// the words a typed time may carry. Seen 2026-11-19 in Notion's ar, de, ja,
// he, en and ko on one date property (time format unset), side by side.
import { i18next } from "@/i18n";
import { activeDateLocale } from "@/lib/i18n";

// Notion's pickers head their weeks with date-fns's "short" day names in
// Notion's own locales ('cccccc': "Su Mo …", "So Mo …", "أحد اثنين …", "א׳ ב׳ …"),
// a width Intl does not give; Sunday first. A language Notion has no locale
// for takes its catalog's (propertyCell:calendarWeekdays).
const NOTION_WEEKDAY_HEADS: Readonly<Record<string, readonly string[]>> = {
  en: ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"],
  ko: ["일", "월", "화", "수", "목", "금", "토"],
  ja: ["日", "月", "火", "水", "木", "金", "土"],
  "zh-Hans": ["日", "一", "二", "三", "四", "五", "六"],
  "zh-Hant": ["日", "一", "二", "三", "四", "五", "六"],
  fr: ["di", "lu", "ma", "me", "je", "ve", "sa"],
  de: ["So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"],
  es: ["do", "lu", "ma", "mi", "ju", "vi", "sá"],
  "pt-BR": ["dom", "seg", "ter", "qua", "qui", "sex", "sab"],
  it: ["dom", "lun", "mar", "mer", "gio", "ven", "sab"],
  nl: ["zo", "ma", "di", "wo", "do", "vr", "za"],
  sv: ["sö", "må", "ti", "on", "to", "fr", "lö"],
  nb: ["sø", "ma", "ti", "on", "to", "fr", "lø"],
  da: ["sø", "ma", "ti", "on", "to", "fr", "lø"],
  fi: ["su", "ma", "ti", "ke", "to", "pe", "la"],
  th: ["อา.", "จ.", "อ.", "พ.", "พฤ.", "ศ.", "ส."],
  vi: ["CN", "Th 2", "Th 3", "Th 4", "Th 5", "Th 6", "Th 7"],
  id: ["Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"],
  he: ["א׳", "ב׳", "ג׳", "ד׳", "ה׳", "ו׳", "ש׳"],
  ar: ["أحد", "اثنين", "ثلاثاء", "أربعاء", "خميس", "جمعة", "سبت"],
};

const WEEKDAY_KEYS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"] as const;

/** A date picker's weekday heads, Sunday first. */
export function weekdayHeads(): string[] {
  const own = NOTION_WEEKDAY_HEADS[i18next.resolvedLanguage ?? i18next.language];
  return own ? [...own] : WEEKDAY_KEYS.map((day) => i18next.t(`propertyCell:calendarWeekdays.${day}`));
}

const formats = new Map<string, Intl.DateTimeFormat>();
function format(name: string, options: Intl.DateTimeFormatOptions) {
  const locale = activeDateLocale(), key = `${name} ${locale}`;
  let found = formats.get(key);
  if (!found) formats.set(key, found = new Intl.DateTimeFormat(locale, options));
  return found;
}
const clock = (hour: number, minute: number) => new Date(2000, 0, 1, hour, minute);

/**
 * A date's time on Notion's default clock: the language's own short time
 * (its 'LT'), "10:17 AM", "오전 10:17", "10:17 ص", and "10:17" or "18:00"
 * where the language counts 24 hours, as German, Hebrew and Japanese do.
 */
export function shortTime(hour: number, minute: number): string {
  return format("time", { hour: "numeric", minute: "2-digit" }).format(clock(hour, minute));
}

/** A time on the 12-hour clock in the language's order and day period words: "2:05 PM", "오후 2:05", "2:05 م". */
export function twelveHourTime(hour: number, minute: number): string {
  return format("time12", { hour: "numeric", minute: "2-digit", hourCycle: "h12" }).format(clock(hour, minute));
}

/**
 * A day as Notion's date editor writes it in its field: "Nov 1, 2026" in
 * English and the language's numeric date elsewhere, "2026. 11. 1.",
 * "1.11.2026", "2026/11/1", "1‏/11‏/2026".
 */
export function dayFieldText(date: Date): string {
  return isEnglish()
    ? format("dayMedium", { year: "numeric", month: "short", day: "numeric" }).format(date)
    : format("dayNumeric", { year: "numeric", month: "numeric", day: "numeric" }).format(date);
}

/** Whether a typed numeric day reads day first in the language, as "1.11.2026" does. */
export function readsDayFirst(): boolean {
  const parts = format("dayNumeric", { year: "numeric", month: "numeric", day: "numeric" }).formatToParts(new Date(2000, 10, 22));
  return parts.find((part) => part.type !== "literal")?.type === "day";
}

const DIRECTION_MARKS = /[‎‏؜]/gu;

/** Typed date text without the direction marks a right-to-left date carries. */
export function withoutDirectionMarks(value: string): string {
  return value.replace(DIRECTION_MARKS, "");
}

/** A typed time with the language's day period words ("ص", "午前", "ap.") read as "am" or "pm" after it. */
export function plainTimeDraft(value: string): string {
  let text = withoutDirectionMarks(value).trim().toLowerCase();
  for (const [hour, mark] of [[9, "am"], [21, "pm"]] as const) {
    const word = format("time12", { hour: "numeric", minute: "2-digit", hourCycle: "h12" }).formatToParts(clock(hour, 0))
      .find((part) => part.type === "dayPeriod")?.value.toLowerCase();
    if (!word || word === mark || !text.includes(word)) continue;
    text = `${text.replace(word, "").trim()} ${mark}`;
  }
  return text;
}

function isEnglish() {
  return activeDateLocale().toLowerCase().startsWith("en");
}
