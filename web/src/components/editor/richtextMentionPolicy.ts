import { safeUrl } from "./richtextUrlPolicy";

export function safeMentionIdValue(value: string | undefined) {
  const trimmed = value?.trim();
  if (!trimmed || trimmed.length > 200 || !/^[A-Za-z0-9._:@-]+$/.test(trimmed)) return undefined;
  return trimmed;
}

export function safeDateMentionValueValue(value: string | undefined) {
  const trimmed = value?.trim();
  if (!trimmed || trimmed.length > 80) return undefined;
  const match =
    /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?(?:Z|([+-])(\d{2}):(\d{2}))?)?$/.exec(
      trimmed,
    );
  if (!match) return undefined;
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, millisText, , zoneHourText, zoneMinuteText] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const date = new Date(Date.UTC(year, month - 1, day));
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) {
    return undefined;
  }
  if (hourText === undefined) return trimmed;
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = secondText === undefined ? 0 : Number(secondText);
  const millis = millisText === undefined ? 0 : Number(millisText);
  if (hour > 23 || minute > 59 || second > 59 || millis > 999) return undefined;
  if (zoneHourText !== undefined) {
    const zoneHour = Number(zoneHourText);
    const zoneMinute = Number(zoneMinuteText);
    if (zoneHour > 23 || zoneMinute > 59) return undefined;
  }
  return trimmed;
}

export function safeDateMentionTimeZoneValue(value: string | undefined) {
  const trimmed = value?.trim();
  if (!trimmed || trimmed.length > 100 || !/^[A-Za-z0-9._+-]+(?:\/[A-Za-z0-9._+-]+)*$/.test(trimmed)) {
    return undefined;
  }
  return trimmed;
}

export function safeExternalMentionUrlValue(url: string | undefined) {
  const href = safeUrl(url);
  return href && /^https?:/i.test(href) ? href : "";
}

/**
 * Where a link mention leads: a web page, or a mail address or phone number,
 * which Notion's paste menu mentions too (seen 2026-11-11: "mailto:" by its
 * domain's site, "tel:" by its number). Its icon and picture stay web ones.
 */
export function safeLinkMentionHref(url: string | undefined) {
  const href = safeUrl(url);
  return href && /^(?:https?|mailto|tel):/iu.test(href) ? href : "";
}
