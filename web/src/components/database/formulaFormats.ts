import type { FormulaLayout, FormulaLocale } from "./formulaLocales";
import type { FormulaValue } from "./formulaSyntax";
import { finiteNumber, toText } from "./formulaValues";
import { FormulaRunError } from "./formulaRunError";

// formatDate() reads moment.js-style tokens with the names and layouts of
// the viewer's language (formulaLocales.ts), keeping text in [brackets] or
// after a backslash; formatNumber() writes plain numbers, thousands
// separators, percents and currencies. The server writes both the same way
// in English (database_formula_formats.go).

export type FormulaWall = {
  year: number; month: number; day: number;
  hour: number; minute: number; second: number; millisecond: number;
};

// The clock a time keeps: a named time zone, or its own offset in
// milliseconds. A moment without one is on the viewer's clock.
export type FormulaClock = { zone: string } | { offset: number };

export type FormulaMoment = { instant: number; wall: FormulaWall; hasTime: boolean; own?: FormulaClock };

export function wallMilliseconds(wall: FormulaWall) {
  const date = new Date(0);
  date.setUTCFullYear(wall.year, wall.month - 1, wall.day);
  date.setUTCHours(wall.hour, wall.minute, wall.second, wall.millisecond);
  return date.getTime();
}

export function weekday(wall: FormulaWall) {
  const date = new Date(0);
  date.setUTCFullYear(wall.year, wall.month - 1, wall.day);
  return date.getUTCDay();
}

export function isoWeek(wall: FormulaWall) {
  const target = new Date(0);
  target.setUTCFullYear(wall.year, wall.month - 1, wall.day);
  const day = target.getUTCDay() || 7;
  target.setUTCDate(target.getUTCDate() + 4 - day);
  const yearStart = new Date(0);
  yearStart.setUTCFullYear(target.getUTCFullYear(), 0, 1);
  return Math.ceil(((target.getTime() - yearStart.getTime()) / 86_400_000 + 1) / 7);
}

// A layout token (LT, LL, ...) stands for the language's layout of it.
const LAYOUT_TOKENS: readonly FormulaLayout[] = ["LTS", "LT", "LLLL", "LLL", "LL", "L", "llll", "lll", "ll", "l"];
const DATE_TOKENS = [
  ...LAYOUT_TOKENS,
  "YYYY", "YY", "Y", "Q", "MMMM", "MMM", "MM", "M", "Do", "DDDD", "DDD", "DD", "D",
  "dddd", "ddd", "dd", "d", "WW", "W", "HH", "H", "hh", "h", "kk", "k",
  "mm", "m", "ss", "s", "SSS", "A", "a", "ZZ", "Z", "X", "x",
];

function pad(value: number, width: number) {
  return `${value < 0 ? "-" : ""}${String(Math.abs(value)).padStart(width, "0")}`;
}

function dateToken(token: string, moment: FormulaMoment, locale: FormulaLocale) {
  const { wall } = moment;
  const hour12 = wall.hour % 12 || 12;
  const offset = Math.round((wallMilliseconds(wall) - moment.instant) / 60_000);
  const zone = (separator: string) => `${offset < 0 ? "-" : "+"}${pad(Math.floor(Math.abs(offset) / 60), 2)}${separator}${pad(Math.abs(offset) % 60, 2)}`;
  const yearStart = new Date(0);
  yearStart.setUTCFullYear(wall.year, 0, 1);
  const dayStart = new Date(0);
  dayStart.setUTCFullYear(wall.year, wall.month - 1, wall.day);
  const yearDay = Math.round((dayStart.getTime() - yearStart.getTime()) / 86_400_000) + 1;
  switch (token) {
    case "YYYY": return pad(wall.year, 4);
    case "YY": return pad(wall.year % 100, 2);
    case "Y": return String(wall.year);
    case "Q": return String(Math.floor((wall.month - 1) / 3) + 1);
    case "MMMM": return locale.months[wall.month - 1];
    case "MMM": return locale.monthsShort[wall.month - 1];
    case "MM": return pad(wall.month, 2);
    case "M": return String(wall.month);
    case "Do": return locale.ordinal(wall.day);
    case "DDDD": return pad(yearDay, 3);
    case "DDD": return String(yearDay);
    case "DD": return pad(wall.day, 2);
    case "D": return String(wall.day);
    case "dddd": return locale.weekdays[weekday(wall)];
    case "ddd": return locale.weekdaysShort[weekday(wall)];
    case "dd": return locale.weekdaysMin[weekday(wall)];
    case "d": return String(weekday(wall));
    case "WW": return pad(isoWeek(wall), 2);
    case "W": return String(isoWeek(wall));
    case "HH": return pad(wall.hour, 2);
    case "H": return String(wall.hour);
    case "hh": return pad(hour12, 2);
    case "h": return String(hour12);
    case "kk": return pad(wall.hour + 1, 2);
    case "k": return String(wall.hour + 1);
    case "mm": return pad(wall.minute, 2);
    case "m": return String(wall.minute);
    case "ss": return pad(wall.second, 2);
    case "s": return String(wall.second);
    case "SSS": return pad(wall.millisecond, 3);
    case "A": return locale.meridiem(wall.hour, wall.minute, false);
    case "a": return locale.meridiem(wall.hour, wall.minute, true);
    case "ZZ": return zone("");
    case "Z": return zone(":");
    case "X": return String(Math.floor(moment.instant / 1000));
  }
  return String(moment.instant);
}

export function formatFormulaDate(moment: FormulaMoment, layout: string, locale: FormulaLocale): string {
  const characters = Array.from(layout);
  let result = "";
  for (let index = 0; index < characters.length;) {
    if (characters[index] === "[") {
      let end = index + 1;
      while (end < characters.length && characters[end] !== "]" && characters[end] !== "[") end += 1;
      if (characters[end] === "]") {
        result += characters.slice(index + 1, end).join("");
        index = end + 1;
        continue;
      }
    }
    if (characters[index] === "\\" && index + 1 < characters.length) {
      result += characters[index + 1];
      index += 2;
      continue;
    }
    const rest = characters.slice(index).join("");
    const token = DATE_TOKENS.find((candidate) => rest.startsWith(candidate));
    if (!token) {
      result += characters[index];
      index += 1;
      continue;
    }
    result += (LAYOUT_TOKENS as readonly string[]).includes(token)
      ? formatFormulaDate(moment, locale.layouts[token as FormulaLayout], locale)
      : dateToken(token, moment, locale);
    index += token.length;
  }
  return result;
}

// A currency formatNumber() writes in: its code, its sign as Notion writes
// it, and its usual decimals. Yen, won and dong without given decimals
// round halves up, as Notion rounds them. The server keeps the same table
// (database_formula_formats.go).
type Currency = { code: string; prefix: string; decimals: number; roundsUp?: boolean };

const CURRENCY_FORMATS: Record<string, Currency> = {
  dollar: { code: "usd", prefix: "$", decimals: 2 }, australian_dollar: { code: "aud", prefix: "A$", decimals: 2 },
  canadian_dollar: { code: "cad", prefix: "CA$", decimals: 2 }, singapore_dollar: { code: "sgd", prefix: "SGD\u00a0", decimals: 2 },
  euro: { code: "eur", prefix: "€", decimals: 2 }, pound: { code: "gbp", prefix: "£", decimals: 2 },
  yen: { code: "jpy", prefix: "¥", decimals: 0, roundsUp: true }, ruble: { code: "rub", prefix: "RUB\u00a0", decimals: 2 },
  rupee: { code: "inr", prefix: "₹", decimals: 2 }, won: { code: "krw", prefix: "₩", decimals: 0, roundsUp: true },
  yuan: { code: "cny", prefix: "CN¥", decimals: 2 }, real: { code: "brl", prefix: "R$", decimals: 2 },
  lira: { code: "try", prefix: "TRY\u00a0", decimals: 2 }, rupiah: { code: "idr", prefix: "IDR\u00a0", decimals: 0 },
  franc: { code: "chf", prefix: "CHF\u00a0", decimals: 2 }, hong_kong_dollar: { code: "hkd", prefix: "HK$", decimals: 2 },
  new_zealand_dollar: { code: "nzd", prefix: "NZ$", decimals: 2 }, krona: { code: "sek", prefix: "SEK\u00a0", decimals: 2 },
  norwegian_krone: { code: "nok", prefix: "NOK\u00a0", decimals: 2 }, mexican_peso: { code: "mxn", prefix: "MX$", decimals: 2 },
  rand: { code: "zar", prefix: "ZAR\u00a0", decimals: 2 }, new_taiwan_dollar: { code: "twd", prefix: "NT$", decimals: 2 },
  danish_krone: { code: "dkk", prefix: "DKK\u00a0", decimals: 2 }, zloty: { code: "pln", prefix: "PLN\u00a0", decimals: 2 },
  baht: { code: "thb", prefix: "THB\u00a0", decimals: 2 }, forint: { code: "huf", prefix: "HUF\u00a0", decimals: 0 },
  koruna: { code: "czk", prefix: "CZK\u00a0", decimals: 2 }, shekel: { code: "ils", prefix: "₪", decimals: 2 },
  chilean_peso: { code: "clp", prefix: "CLP\u00a0", decimals: 0 }, philippine_peso: { code: "php", prefix: "₱", decimals: 2 },
  dirham: { code: "aed", prefix: "AED\u00a0", decimals: 2 }, colombian_peso: { code: "cop", prefix: "COP\u00a0", decimals: 0 },
  riyal: { code: "sar", prefix: "SAR\u00a0", decimals: 2 }, ringgit: { code: "myr", prefix: "MYR\u00a0", decimals: 2 },
  leu: { code: "ron", prefix: "RON\u00a0", decimals: 2 }, argentine_peso: { code: "ars", prefix: "ARS\u00a0", decimals: 2 },
  uruguayan_peso: { code: "uyu", prefix: "UYU\u00a0", decimals: 2 }, peruvian_sol: { code: "pen", prefix: "PEN\u00a0", decimals: 2 },
  vietnamese_dong: { code: "vnd", prefix: "₫", decimals: 0, roundsUp: true },
  pakistani_rupee: { code: "pkr", prefix: "PKR\u00a0", decimals: 0 }, nigerian_naira: { code: "ngn", prefix: "NGN\u00a0", decimals: 2 },
};

const CURRENCIES = new Map<string, Currency>();
for (const [name, currency] of Object.entries(CURRENCY_FORMATS)) {
  CURRENCIES.set(name, currency);
  CURRENCIES.set(currency.code, currency);
}

function groupThousands(text: string) {
  const [whole, fraction] = text.split(".");
  const grouped = whole.includes("e") ? whole : whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return fraction === undefined ? grouped : `${grouped}.${fraction}`;
}

// shortestDigits is the size of a finite number as its shortest decimal form
// writes it, moved shift places left, in whole and fraction digits.
function shortestDigits(value: number, shift: number): [string, string] {
  const [mantissa, exponent] = Math.abs(value).toExponential().split("e");
  let digits = mantissa.replace(".", "");
  let point = 1 + Number(exponent) + shift;
  if (point < 1) {
    digits = "0".repeat(1 - point) + digits;
    point = 1;
  }
  if (point > digits.length) digits += "0".repeat(point - digits.length);
  return [digits.slice(0, point - 1).replace(/^0+/, "") + digits.slice(point - 1, point), digits.slice(point)];
}

// roundDigits writes whole.fraction with digits decimals, rounding halves
// away from zero.
function roundDigits(whole: string, fraction: string, digits: number) {
  const padded = fraction + "0".repeat(digits + 1);
  const kept = Array.from(whole + padded.slice(0, digits), Number);
  if (padded[digits] >= "5") {
    let index = kept.length - 1;
    for (; index >= 0; index -= 1) {
      if (kept[index] < 9) {
        kept[index] += 1;
        break;
      }
      kept[index] = 0;
    }
    if (index < 0) kept.unshift(1);
  }
  const text = kept.join("");
  return digits > 0 ? `${text.slice(0, text.length - digits)}.${text.slice(text.length - digits)}` : text;
}

// byteSize writes a number of bytes as Notion's formatNumber does (seen
// 2026-11-08): in the largest unit its size reaches, by steps of 1024
// (KiB … YiB) or 1000 (KB … YB), with at most two decimals or the given ones
// and no trailing zeros or thousands separators: 1536 is "1.5 KiB" or
// "1.54 KB", 1023 is "1023 B", -2048 is "-2 KiB".
function byteSize(value: number, binary: boolean, digits: number) {
  if (!Number.isFinite(value)) return "";
  const base = binary ? 1024 : 1000;
  const units = binary ? ["B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"] : ["B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"];
  let size = Math.abs(value), unit = 0;
  while (unit < units.length - 1 && size >= base) {
    size /= base;
    unit += 1;
  }
  let text = roundDigits(...shortestDigits(size, 0), digits < 0 ? 2 : digits);
  if (text.includes(".")) text = text.replace(/0+$/, "").replace(/\.$/, "");
  return `${value < 0 ? "-" : ""}${text} ${units[unit]}`;
}

// formatNumber(number, format, decimals) writes a number as Notion does:
// plainly ("number"), with thousands separators ("number_with_commas",
// "commas", or no format), as a percent, or in a currency named by its
// Notion format name or its code, in any case, or as a byte size; a format
// it doesn't know writes nothing. Decimals are a whole number from 0, at most
// 12 (more writes 12, as Notion's, seen 2026-11-08), else unused, and a
// currency without them has its usual decimals. A number rounds halves away
// from zero as its shortest decimal form writes it.
export function formatNumberValue(args: FormulaValue[], language: string) {
  const value = finiteNumber(args[0] ?? null);
  let format = args.length > 1 ? toText(args[1]).toLowerCase() : "number_with_commas";
  // "humanize" is short in the viewer's language: 1.2B, 12억, as Notion's (seen 2026-11-04).
  if (format === "humanize") return Number.isFinite(value) ? new Intl.NumberFormat(language, { notation: "compact" }).format(value) : "";
  const given = args.length > 2 ? finiteNumber(args[2]) : -1;
  const digits = Number.isInteger(given) && given >= 0 ? Math.min(given, 12) : -1;
  if (format === "bytes_binary" || format === "bytes_decimal") return byteSize(value, format === "bytes_binary", digits);
  const currency = CURRENCIES.get(format);
  if (format === "commas") format = "number_with_commas";
  else if (currency) format = "currency";
  // A format it doesn't know fails the formula, as Notion's (seen 2026-11-14: formatNumber(1, "foo")).
  else if (format !== "number" && format !== "number_with_commas" && format !== "percent") throw new FormulaRunError("numberFormat", { format: toText(args[1]) });
  const sign = value < 0 ? "-" : "";
  const shift = format === "percent" ? 2 : 0;
  let text: string;
  if (!Number.isFinite(value)) text = String(Math.abs(value));
  else if (digits >= 0) text = roundDigits(...shortestDigits(value, shift), digits);
  else if (currency?.roundsUp) text = String(Math.abs(Math.round(value)));
  else if (currency) text = roundDigits(...shortestDigits(value, 0), currency.decimals);
  else if (shift > 0) text = String(Number(shortestDigits(value, shift).join(".")));
  else text = String(Math.abs(value));
  if (format === "number") return `${sign}${text}`;
  if (format === "percent") return `${sign}${text}%`;
  if (currency) return `${sign}${currency.prefix}${groupThousands(text)}`;
  return `${sign}${groupThousands(text)}`;
}
