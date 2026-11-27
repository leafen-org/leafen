import type { GraphProperty } from "../../contentGraph";
import { personValue } from "@/lib/browserStorage";
import { activeNumberLocale, activeOwnDigitsLocale } from "@/lib/i18n";
import { numberSeparators } from "@/lib/numberSeparators";
import { cutNumberText, joinNumberText, SHOWN_DECIMALS, smallNumberText } from "@/lib/numberText";

// A percent being written in shows the decimal mark of the account's "숫자
// 형식" (numberSeparators.ts), the one parseNumberInput reads back.
function plainNumberText(text: string) {
  const separators = numberSeparators();
  return separators ? text.replace(".", separators.decimal) : text;
}

function intlNumberText(formatter: Intl.NumberFormat, n: number) {
  const separators = numberSeparators();
  if (!separators) return formatter.format(n);
  return formatter.formatToParts(n)
    .map((part) => part.type === "group" ? separators.group : part.type === "decimal" ? separators.decimal : part.value)
    .join("");
}

/**
 * The currencies of Notion's "숫자 형식" menu in its order (seen 2026-10-01),
 * each by its Notion name with the code the menu shows after it.
 */
const CURRENCIES = {
  dollar: "USD", australian_dollar: "AUD", canadian_dollar: "CAD", singapore_dollar: "SGD", euro: "EUR",
  pound: "GBP", yen: "JPY", ruble: "RUB", rupee: "INR", won: "KRW", yuan: "CNY", real: "BRL", lira: "TRY",
  rupiah: "IDR", franc: "CHF", hong_kong_dollar: "HKD", new_zealand_dollar: "NZD", krona: "SEK",
  norwegian_krone: "NOK", mexican_peso: "MXN", rand: "ZAR", new_taiwan_dollar: "TWD", danish_krone: "DKK",
  zloty: "PLN", baht: "THB", forint: "HUF", koruna: "CZK", shekel: "ILS", chilean_peso: "CLP",
  philippine_peso: "PHP", dirham: "AED", colombian_peso: "COP", riyal: "SAR", ringgit: "MYR", leu: "RON",
  argentine_peso: "ARS", uruguayan_peso: "UYU", peruvian_sol: "PEN", vietnamese_dong: "VND",
  pakistani_rupee: "PKR", nigerian_naira: "NGN", bitcoin: "BTC",
} as const;
type Currency = keyof typeof CURRENCIES;

/** A number format by its Notion name, but "comma" for Notion's "number_with_commas". */
export type NumberFormat = "number" | "comma" | "percent" | Currency;

/** Every number format, in the order of Notion's menu. */
export const NUMBER_FORMATS: NumberFormat[] = ["number", "comma", "percent", ...Object.keys(CURRENCIES) as Currency[]];
const KNOWN_NUMBER_FORMATS = new Set<string>(NUMBER_FORMATS);

/** Where the currency picked last is kept for the person, which Notion's format menus list first. */
export const LAST_CURRENCY_KEY = "leafen:number-last-currency";

/**
 * Every number format in the order Notion's format menus list them: the
 * currency this person last chose for a number property on this browser
 * leads the currencies, after the number, comma and percent formats. Notion
 * keeps it on the device per person ("LastUsedCurrency", read from its web
 * client 2026-11-16 and seen there: 유로 fourth in Korean with the property
 * back on 숫자; a browser with none — a phone's — keeps the menu's own order,
 * 미국 달러 fourth). A formula's or rollup's list follows it without setting
 * it, as Notion's; a number chart's "형식 › 숫자 형식" lists it so too (seen
 * 2026-11-18: 원 (KRW) fourth).
 */
export function numberFormatsLastCurrencyFirst(): NumberFormat[] {
  const lead = normalizeNumberFormat(personValue(LAST_CURRENCY_KEY));
  if (!lead || !numberFormatCode(lead)) return NUMBER_FORMATS;
  const rest = NUMBER_FORMATS.filter((format) => format !== lead);
  const at = rest.findIndex((format) => numberFormatCode(format));
  return [...rest.slice(0, at), lead, ...rest.slice(at)];
}

/** The code Notion's menu shows after a currency's name, as "원 (KRW)". */
export function numberFormatCode(format: NumberFormat): string | undefined {
  return format in CURRENCIES ? CURRENCIES[format as Currency] : undefined;
}

/**
 * Whether a search matches a format as Notion's menu search does (seen
 * 2026-10-01): its letters in order, in any case, within the format's Notion
 * name — "usd" finds australian_dollar, not the names the menu shows.
 */
export function numberFormatMatches(format: NumberFormat, query: string) {
  const name = format === "comma" ? "number_with_commas" : format;
  let at = 0;
  for (const letter of query.toLowerCase()) {
    at = name.indexOf(letter, at) + 1;
    if (!at) return false;
  }
  return true;
}

/**
 * Where a format a search matches stands, as a number chart's "숫자 형식
 * 필터링..." lists them (seen 2026-11-18 in Chrome at 1440): 0 where the
 * words stand together in its Notion name, 1 where only its letters do in
 * order, each kept in the menu's order ("in": 싱가포르 달러 · 포린트 · …
 * · 비트코인, then 호주 달러 · 캐나다 달러 · …; "rup": 루피 · 루피아 ·
 * 파키스탄 루피, then 우루과이 페소); none where it doesn't match.
 */
export function numberFormatMatchRank(format: NumberFormat, query: string): 0 | 1 | undefined {
  if (!numberFormatMatches(format, query)) return undefined;
  return (format === "comma" ? "number_with_commas" : format).includes(query.toLowerCase()) ? 0 : 1;
}

export function normalizeNumberFormat(value: unknown): NumberFormat | undefined {
  if (typeof value !== "string") return undefined;
  const format = value.trim();
  if (!format) return undefined;
  if (format === "number_with_commas") return "comma";
  if (KNOWN_NUMBER_FORMATS.has(format)) return format as NumberFormat;
  return undefined;
}

type NativeNumberConfig = {
  format?: unknown;
  numberFormat?: unknown;
  notion?: { number?: { format?: unknown } };
  decimalPlaces?: unknown;
  numberDisplay?: unknown;
  numberDisplayColor?: unknown;
  numberDivideBy?: unknown;
  numberShowValue?: unknown;
};

/** The decimal places a number property offers, as Notion's (seen 2026-09-30). */
export const NUMBER_DECIMAL_PLACES = [0, 1, 2, 3, 4, 5] as const;
export const NUMBER_DISPLAY_COLORS = [
  "default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red",
] as const;
export type NumberDisplayKind = "number" | "bar" | "ring";

/**
 * How a number property shows its values, as Notion's "속성 편집" sets it
 * (seen 2026-09-30): its format and decimal places, and a number, a bar or a
 * ring filled up to "divide by" (100) in a color (green), with the number
 * beside it unless turned off.
 */
export function numberDisplayForProperty(prop: Pick<GraphProperty, "config">) {
  const config = prop.config as NativeNumberConfig;
  const decimals = typeof config.decimalPlaces === "number"
    && (NUMBER_DECIMAL_PLACES as readonly number[]).includes(config.decimalPlaces) ? config.decimalPlaces : undefined;
  const kind: NumberDisplayKind = config.numberDisplay === "bar" || config.numberDisplay === "ring" ? config.numberDisplay : "number";
  const color = typeof config.numberDisplayColor === "string"
    && (NUMBER_DISPLAY_COLORS as readonly string[]).includes(config.numberDisplayColor) ? config.numberDisplayColor : "green";
  const format = numberFormatForProperty(prop);
  // A percent bar/ring measures the fraction out of 1; Notion omits "divide by".
  const divideBy = format === "percent" ? 1 : typeof config.numberDivideBy === "number" && Number.isFinite(config.numberDivideBy)
    && config.numberDivideBy > 0 ? config.numberDivideBy : 100;
  return {
    format, decimals, kind, color, divideBy,
    showValue: config.numberShowValue !== false,
  };
}

/** How full a value's bar or ring is: its share of "divide by", 0 to 1. */
export function numberDisplayFraction(value: unknown, divideBy: number) {
  const n = typeof value === "number" ? value : Number(value);
  if (value == null || value === "" || !Number.isFinite(n) || !(divideBy > 0)) return 0;
  return Math.min(1, Math.max(0, n / divideBy));
}

/** A number property's value as its format and decimal places show it. */
export function formatNumberForProperty(value: unknown, prop: Pick<GraphProperty, "config">) {
  const { format, decimals } = numberDisplayForProperty(prop);
  return formatNumberValue(value, format, decimals);
}

// A number property's format is its numberFormat, the key the property menus
// and the server write; an older "format" key counts only without one, so a
// format chosen later wins.
export function numberFormatForProperty(prop: Pick<GraphProperty, "config">): NumberFormat {
  const config = prop.config as NativeNumberConfig;
  return (
    normalizeNumberFormat(config.numberFormat) ??
    normalizeNumberFormat(config.format) ??
    normalizeNumberFormat(config.notion?.number?.format) ??
    "number"
  );
}

// The separators a language writes a number with, unless the account
// chooses others (numberSeparators.ts): its own locale's, as Notion's cells
// write them (Arabic "1٬234٫5" in Western digits, seen 2026-11-19).
const languageSeparators = new Map<string, { group: string; decimal: string }>();
function writtenSeparators() {
  const chosen = numberSeparators();
  if (chosen) return chosen;
  const locale = activeOwnDigitsLocale();
  let separators = languageSeparators.get(locale);
  if (!separators) {
    const parts = new Intl.NumberFormat(locale).formatToParts(1000.5);
    separators = {
      group: parts.find((part) => part.type === "group")?.value ?? ",",
      decimal: parts.find((part) => part.type === "decimal")?.value ?? ".",
    };
    languageSeparators.set(locale, separators);
  }
  return separators;
}

// A percent and a currency in the language's own digits, as Notion's cells
// write them (Arabic "٢٥٪؜", "‏١٬٢٣٤٫٥٠ US$", seen 2026-11-19).
const percentFormatters = new Map<string, Intl.NumberFormat>();
function percentFormatter(decimals?: number) {
  const locale = activeOwnDigitsLocale(), key = `${locale} ${decimals ?? ""}`;
  let formatter = percentFormatters.get(key);
  if (!formatter) {
    formatter = new Intl.NumberFormat(locale, {
      style: "percent", useGrouping: false,
      maximumFractionDigits: decimals ?? SHOWN_DECIMALS, minimumFractionDigits: decimals ?? 0,
    });
    percentFormatters.set(key, formatter);
  }
  return formatter;
}

// A number shows as Notion's cells show one (seen 2026-11-14): with the
// decimal places chosen, or else up to 12, cut from its own text as Notion
// cuts it (numberText.ts) — a number and a "number_with_commas" alike, the
// commas between its whole digits and its decimal mark in the reader's
// language (German "1234,5", Arabic "1234٫5", seen 2026-11-19); a small number
// written with an exponent, a plain number or bitcoin, by its exponent
// whatever is chosen (1e-7 shows 0.0000001). A percent is the reader's
// language's, its fraction times 100 exactly, without thousands separators
// ("1234.5%", 1/3 "33.333333333333%"); a currency its usual decimals in the
// reader's language ("US$1,234.50", "₩1,235" in Korean), and bitcoin "₿"
// before the number with commas, its minus after it ("₿-1,234.567").
// Chosen places pad with zeros ("3" with 2 places shows "3.00", seen
// 2026-09-30).
export function formatNumberValue(value: unknown, format: NumberFormat = "number", decimals?: number) {
  if (value == null) return "";
  if (typeof value === "string" && value.trim() === "") return "";
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(n)) return "";
  const cut = () => cutNumberText(n, decimals ?? SHOWN_DECIMALS, decimals ?? 0);
  if (format === "number") return smallNumberText(n) ?? joinNumberText(cut(), writtenSeparators().decimal);
  if (format === "percent") return intlNumberText(percentFormatter(decimals), n);
  if (format === "comma" || format === "bitcoin") {
    const { group, decimal } = writtenSeparators();
    if (format === "comma") return joinNumberText(cut(), decimal, group);
    return `₿${smallNumberText(n) ?? joinNumberText(cut(), decimal, group)}`;
  }
  const fixed = decimals === undefined ? {} : { minimumFractionDigits: decimals, maximumFractionDigits: decimals };
  return intlNumberText(new Intl.NumberFormat(activeOwnDigitsLocale(), { style: "currency", currency: CURRENCIES[format], ...fixed }), n);
}

/**
 * A number chart's value as Notion's draws it: thousands and more in the
 * reader's short words, its currency kept ("US$1천" for 1,000 dollars in
 * Korean, seen 2026-11-06); a percent, or chosen decimals, as the property shows it.
 */
export function compactNumberValue(value: number, format: NumberFormat = "number", decimals?: number) {
  if (decimals !== undefined || format === "percent" || format === "bitcoin" || !Number.isFinite(value) || Math.abs(value) < 1000) {
    return formatNumberValue(value, format, decimals);
  }
  const plain = format === "number" || format === "comma";
  const currency = plain ? {} : { style: "currency" as const, currency: CURRENCIES[format as Currency] };
  // A currency in the language's own digits, as its cell (formatNumberValue).
  return intlNumberText(new Intl.NumberFormat(plain ? activeNumberLocale() : activeOwnDigitsLocale(), { notation: "compact", ...currency }), value);
}

// Move the decimal point in the numeric text rather than multiplying a
// float: editing 0.29 must show 29%, and keep its original precision.
function shiftedNumberText(value: number, places: number) {
  const [coefficient, exponent = "0"] = String(value).split("e");
  const negative = coefficient.startsWith("-");
  const [whole, fraction = ""] = coefficient.replace(/^-/, "").split(".");
  const digits = whole + fraction;
  const point = whole.length + Number(exponent) + places;
  const text = point <= 0 ? `0.${"0".repeat(-point)}${digits}`
    : point >= digits.length ? digits + "0".repeat(point - digits.length)
      : `${digits.slice(0, point)}.${digits.slice(point)}`;
  return `${negative ? "-" : ""}${text.replace(/^0+(?=\d)/, "")}`;
}

/** A property value's editable text; percentages are entered in percent units. */
export function numberInputText(value: unknown, prop: Pick<GraphProperty, "config">) {
  if (value == null || value === "") return "";
  if (numberFormatForProperty(prop) !== "percent") return String(value);
  const number = Number(value);
  return Number.isFinite(number) ? `${plainNumberText(shiftedNumberText(number, 2))}%` : "";
}

/** A property value typed by a person; undefined rejects invalid input. API values stay raw. */
export function parseNumberInput(text: string, prop: Pick<GraphProperty, "config">): number | null | undefined {
  const percent = numberFormatForProperty(prop) === "percent";
  let normalized = text.trim().replace(/\s/g, "");
  if (percent) normalized = normalized.replace(/%$/, "");
  if (percent) {
    const separators = numberSeparators();
    normalized = separators?.decimal === ","
      ? normalized.replace(/\./g, "").replace(",", ".") : normalized.replace(/,/g, "");
  }
  if (!normalized) return null;
  const number = Number(normalized);
  if (!Number.isFinite(number)) return undefined;
  return percent ? Number(shiftedNumberText(number, -2)) : number;
}
