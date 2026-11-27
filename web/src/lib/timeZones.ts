import { i18next } from "@/i18n";
import { TIME_ZONES } from "@/lib/timeZoneList";
import { notionSearchScore } from "@/lib/notionSearch";

/** The zones' places in a language, as "Asia/Seoul": "서울" (dateTimeZones.ts loadZoneCities). */
export type ZoneCities = Readonly<Record<string, string>>;

// CLDR keeps a zone IANA has renamed under its older name, as browsers built on
// ICU list it; a zone of the newer name finds its place under the older.
const CLDR_ZONE_KEYS: Readonly<Record<string, string>> = {
  "Africa/Asmara": "Africa/Asmera",
  "America/Argentina/Buenos_Aires": "America/Buenos_Aires",
  "America/Argentina/Catamarca": "America/Catamarca",
  "America/Argentina/Cordoba": "America/Cordoba",
  "America/Argentina/Jujuy": "America/Jujuy",
  "America/Argentina/Mendoza": "America/Mendoza",
  "America/Atikokan": "America/Coral_Harbour",
  "America/Indiana/Indianapolis": "America/Indianapolis",
  "America/Kentucky/Louisville": "America/Louisville",
  "America/Nuuk": "America/Godthab",
  "Asia/Ho_Chi_Minh": "Asia/Saigon",
  "Asia/Kathmandu": "Asia/Katmandu",
  "Asia/Kolkata": "Asia/Calcutta",
  "Asia/Yangon": "Asia/Rangoon",
  "Atlantic/Faroe": "Atlantic/Faeroe",
  "Europe/Kyiv": "Europe/Kiev",
  "Pacific/Chuuk": "Pacific/Truk",
  "Pacific/Kanton": "Pacific/Enderbury",
  "Pacific/Pohnpei": "Pacific/Ponape",
};

/**
 * A zone's place in the reader's language, as every zone list names it:
 * Unicode CLDR's exemplar city (user decisions 2026-09-30 and 2026-11-21;
 * Notion names some places its own way, Leafen keeps CLDR's) — the
 * language's places (locales/<language>/timeZones.json: English, Korean,
 * Arabic, Hebrew, scripts/generate-zone-city-names.mjs) or its zone city
 * table (`cities`, every language), else English's, each read under the
 * zone's ID and then the older one CLDR keeps it under ("Asia/Kolkata" as
 * "Asia/Calcutta"); else the name its ID gives, as CLDR's root names a zone
 * it has no city for ("Pacific" for US/Pacific, "ROK").
 */
export function timeZonePlace(zone: string, cities: ZoneCities = {}) {
  const language = i18next.resolvedLanguage ?? i18next.language;
  const keys = CLDR_ZONE_KEYS[zone] ? [zone, CLDR_ZONE_KEYS[zone]] : [zone];
  const sources = [
    (key: string) => i18next.getResource(language, "timeZones", key),
    (key: string) => cities[key],
    (key: string) => i18next.getResource("en", "timeZones", key),
  ];
  for (const read of sources) {
    for (const key of keys) {
      const place = read(key);
      if (typeof place === "string") return place;
    }
  }
  return (zone.split("/").pop() ?? zone).replace(/_/gu, " ");
}

/**
 * Zones with one zone for each place, the first of those named alike kept, as
 * Notion's lists keep them (seen 2026-11-14: Asia/Yangon, named as
 * Asia/Rangoon is, is not listed).
 */
export function uniqueTimeZonePlaces<T>(items: readonly T[], place: (item: T) => string): T[] {
  const named = new Set<string>();
  return items.filter((item) => {
    const name = place(item);
    if (named.has(name)) return false;
    named.add(name);
    return true;
  });
}

/**
 * The time zones "기본 설정 → 시간대" offers, as Notion's list: the person's
 * own first, then Notion's zones (timeZoneList.ts), one for each place,
 * each named by its place over its offset from GMT, closed as
 * "(GMT+9:00) 서울".
 */
export function timeZoneChoices(current: string, cities: ZoneCities = {}): string[] {
  const zones = uniqueTimeZonePlaces(TIME_ZONES, (zone) => timeZonePlace(zone, cities));
  return [current, ...zones.filter((zone) => zone !== current)].filter(Boolean);
}

export type TimeZoneNameStyle = "short" | "longOffset";

function readNamePart(zone: string, style: TimeZoneNameStyle, at: Date) {
  try {
    return new Intl.DateTimeFormat("en-US", { timeZone: zone, timeZoneName: style })
      .formatToParts(at).find((part) => part.type === "timeZoneName")?.value ?? "";
  } catch {
    return "";
  }
}

// Each zone's name needs a formatter of its own, some four hundred for a zone
// list. A zone's clock changes on a quarter hour (UTC), so what it reads in
// this quarter hour holds until the next.
const QUARTER_HOUR_MS = 15 * 60 * 1000;
let namesNow: { quarter: number; byKey: Map<string, string> } = { quarter: -1, byKey: new Map() };

/**
 * A zone's name at `at` (now unless said) as the browser writes it in
 * English — "GMT+09:00" (longOffset), "EDT" (short) — or "" for a zone it
 * doesn't know. Every zone list reads its zones here: what this quarter hour
 * reads is kept.
 */
export function timeZoneNamePart(zone: string, style: TimeZoneNameStyle, at?: Date): string {
  const moment = at ?? new Date();
  const quarter = Math.floor(moment.getTime() / QUARTER_HOUR_MS);
  if (quarter !== Math.floor(Date.now() / QUARTER_HOUR_MS)) return readNamePart(zone, style, moment);
  if (quarter !== namesNow.quarter) namesNow = { quarter, byKey: new Map() };
  const key = `${style} ${zone}`;
  let name = namesNow.byKey.get(key);
  if (name === undefined) {
    name = readNamePart(zone, style, moment);
    namesNow.byKey.set(key, name);
  }
  return name;
}

/**
 * A zone's offset from GMT (now, unless `at` says when), as Notion's zone
 * lists write it (seen 2026-11-14): "GMT" at none, a whole hour without its
 * leading zero ("GMT+9:00", "GMT-4:00", "GMT+10:00") and any other with
 * it ("GMT+09:30" for Darwin, "GMT-03:30", "GMT+05:45"), so a search for
 * "gmt+9" finds Darwin only after every +9:00 zone. Every zone list reads
 * its offsets here.
 */
export function timeZoneOffset(zone: string, at?: Date) {
  const match = /^GMT([+-])(\d{2}):(\d{2})$/u.exec(timeZoneNamePart(zone, "longOffset", at));
  return match ? gmtOffsetText(match[1], match[2], match[3]) : "GMT";
}

/** An offset by its sign, hours and minutes ("+", "09", "30") as timeZoneOffset writes it. */
export function gmtOffsetText(sign: string, hours: string, minutes: string) {
  if (hours === "00" && minutes === "00") return "GMT";
  return `GMT${sign}${minutes === "00" ? Number(hours) : hours}:${minutes}`;
}

export function timeZoneLabel(zone: string, at?: Date) {
  return `(${timeZoneOffset(zone, at)}) ${timeZonePlace(zone)}`;
}

/** A zone of the list as it shows: its place over its offset now, and the two as a closed list names it. */
export type TimeZoneChoice = { zone: string; place: string; offset: string; label: string };

export function timeZoneChoice(zone: string, cities: ZoneCities = {}): TimeZoneChoice {
  const place = timeZonePlace(zone, cities), offset = timeZoneOffset(zone);
  return { zone, place, offset, label: `(${offset}) ${place}` };
}

/** A zone's texts a search reads, as its row shows them: its offset ("GMT+9:00"), its place ("서울") and its IANA name. */
export type TimeZoneSearchTexts = { offset: string; place: string; zone: string };

/**
 * How well a zone answers what is typed in a zone list's search, higher
 * first and 0 for no answer: Notion's list search (notionSearch.ts) over its
 * IANA name, place and offset read as one, "Asia/Seoul 서울 GMT+9:00", as
 * Notion's zone list searches (read 2026-11-14; "lon": Miquelon,
 * Longyearbyen, London… Fernando de Noronha, Isle of Man). What is typed is
 * read as typed, spaces too ("lon " finds London's "lon" followed by a word).
 * A zone whose texts hold what is typed only with accents aside answers
 * too, in every zone list, below one that holds it as typed: "Sao Paulo"
 * finds São Paulo, "Zurich" Zürich, "Kobenhavn" København (user decision
 * 2026-11-21; notionSearch.ts).
 */
export function timeZoneSearchRank(texts: TimeZoneSearchTexts, typed: string): number {
  const score = notionSearchScore(typed, `${texts.zone} ${texts.place} ${texts.offset}`);
  return score >= 1 ? score : 0;
}
