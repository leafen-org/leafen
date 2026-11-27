import { useEffect, useState, useSyncExternalStore } from "react";
import { request } from "@/apiTransport";
import type { Session } from "@/types";
import { deviceTimeZone, setAccountTimeZone } from "./dateReminders";
import { setNumberSeparatorChoice } from "./numberSeparators";
import { parseStored, personValue, storePersonValue } from "./browserStorage";
import { onUncertainWritesSettled } from "./uncertainWrites";
import { primaryModifier } from "@/lib/keyboardShortcuts";
import { fieldEnter } from "./fieldKeyBoundary";

/**
 * The account's display preferences other than its language, kept by the
 * server so they follow the person to every device, as Notion's do (seen
 * 2026-10-02). This device keeps a copy per person so the first screen
 * already shows them.
 */
export type AccountDisplayPreferences = {
  /** Calendars and date pickers start their weeks on Monday, not Sunday. */
  weekStartsOnMonday: boolean;
  /** Enter breaks the line in comment boxes, and Cmd/Ctrl+Enter sends. */
  enterAddsNewLine: boolean;
  /** The developer bar and page menu group are shown (developerMode.ts). */
  developerMode: boolean;
  /** The format new @date mentions take (DATE_MENTION_FORMATS). */
  dateMentionFormat: string;
  /** The separators numbers show: the language's, "1,000,000.00" or "1.000.000,00". */
  numberFormat: "default" | "comma_period" | "period_comma";
  /** The time zone follows the device; the server keeps the device's. */
  timeZoneAuto: boolean;
  /** The person's time zone, an IANA name; reminders without one fire in it. */
  timeZone: string;
  /** The block menu offers text direction whatever the language (textDirection.ts). */
  alwaysShowTextDirection: boolean;
  /** The person opened Leafen's GitHub page from the workspace menu's line, which then shows no more (WorkspaceMenuGitHubLine.tsx). */
  githubSupportOpened: boolean;
};

const PATH = "/account/display-preferences";
const DATE_MENTION_FORMATS = new Set(["full", "short", "month_day_year", "day_month_year", "year_month_day", "relative"]);
const DEFAULTS: AccountDisplayPreferences = Object.freeze({
  weekStartsOnMonday: false, enterAddsNewLine: false, developerMode: false, dateMentionFormat: "relative",
  numberFormat: "default", timeZoneAuto: true, timeZone: "", alwaysShowTextDirection: false, githubSupportOpened: false,
});

let owner = "";
let current: AccountDisplayPreferences = DEFAULTS;
const listeners = new Set<() => void>();

// The person's display settings as last read from the server, on this browser.
const DISPLAY_KEY = "leafen:display-preferences";

function normalize(value: unknown): AccountDisplayPreferences {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  return {
    weekStartsOnMonday: record.weekStartsOnMonday === true,
    enterAddsNewLine: record.enterAddsNewLine === true,
    developerMode: record.developerMode === true,
    dateMentionFormat: typeof record.dateMentionFormat === "string" && DATE_MENTION_FORMATS.has(record.dateMentionFormat)
      ? record.dateMentionFormat
      : "relative",
    numberFormat: record.numberFormat === "comma_period" || record.numberFormat === "period_comma"
      ? record.numberFormat
      : "default",
    timeZoneAuto: record.timeZoneAuto !== false,
    timeZone: typeof record.timeZone === "string" ? record.timeZone : "",
    alwaysShowTextDirection: record.alwaysShowTextDirection === true,
    githubSupportOpened: record.githubSupportOpened === true,
  };
}

function cached(userId: string): AccountDisplayPreferences {
  return normalize(parseStored(personValue(DISPLAY_KEY, {
    userId, previous: [`${DISPLAY_KEY}:${encodeURIComponent(userId.trim())}`],
  })));
}

function replace(userId: string, next: AccountDisplayPreferences, notify: boolean) {
  const changed = next.weekStartsOnMonday !== current.weekStartsOnMonday
    || next.enterAddsNewLine !== current.enterAddsNewLine
    || next.developerMode !== current.developerMode
    || next.dateMentionFormat !== current.dateMentionFormat
    || next.numberFormat !== current.numberFormat
    || next.timeZoneAuto !== current.timeZoneAuto
    || next.timeZone !== current.timeZone
    || next.alwaysShowTextDirection !== current.alwaysShowTextDirection
    || next.githubSupportOpened !== current.githubSupportOpened;
  owner = userId;
  current = next;
  setNumberSeparatorChoice(next.numberFormat);
  setAccountTimeZone(next.timeZoneAuto ? "" : next.timeZone);
  if (changed && notify) for (const listener of listeners) listener();
}

function publish(userId: string, next: AccountDisplayPreferences) {
  // Without storage the server keeps them; this browser reads them again next time.
  storePersonValue(DISPLAY_KEY, JSON.stringify(next), { userId });
  replace(userId, next, true);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function displayPreferences(): AccountDisplayPreferences {
  return current;
}

export function useDisplayPreferences(): AccountDisplayPreferences {
  return useSyncExternalStore(subscribe, displayPreferences, displayPreferences);
}

/** The day a week starts on, as `Date.getDay()` counts: 0 Sunday, 1 Monday. */
export function useWeekStartDay(): 0 | 1 {
  return useDisplayPreferences().weekStartsOnMonday ? 1 : 0;
}

/** A Sunday-first list of the week's days, in the order the week starts. */
export function weekOrder<T>(sundayFirst: readonly T[], weekStart: 0 | 1): T[] {
  return weekStart === 1 ? [...sundayFirst.slice(1), ...sundayFirst.slice(0, 1)] : [...sundayFirst];
}

/** How many days `date` is past the start of its week. */
export function daysIntoWeek(date: Date, weekStart: 0 | 1): number {
  return (date.getDay() - weekStart + 7) % 7;
}

/**
 * Shows this person's copy of their preferences before the first screen
 * draws, then the server's.
 */
export function useAccountDisplayPreferenceSync(session: Session) {
  useState(() => {
    if (owner !== session.user.id) replace(session.user.id, cached(session.user.id), false);
    return null;
  });
  // A change sent without the server's answer may have been taken after all:
  // once the server answers again, the preferences are read again.
  const [readAgain, setReadAgain] = useState(0);
  useEffect(() => onUncertainWritesSettled(() => setReadAgain((count) => count + 1)), []);
  useEffect(() => {
    if (owner !== session.user.id) replace(session.user.id, cached(session.user.id), true);
    const controller = new AbortController();
    request<AccountDisplayPreferences>(PATH, { signal: controller.signal }, session)
      .then((saved) => {
        if (controller.signal.aborted) return;
        const preferences = normalize(saved);
        publish(session.user.id, preferences);
        // While automatic, the server keeps this device's time zone for the
        // reminders it sends.
        const device = deviceTimeZone();
        if (preferences.timeZoneAuto && device && preferences.timeZone !== device) {
          void saveDisplayPreferences(session, { timeZone: device }).catch(() => undefined);
        }
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, [readAgain, session]);
}

export async function saveDisplayPreferences(
  session: Session,
  change: Partial<AccountDisplayPreferences>,
): Promise<AccountDisplayPreferences> {
  const saved = normalize(await request<AccountDisplayPreferences>(
    PATH,
    { method: "PATCH", body: JSON.stringify(change) },
    session,
  ));
  publish(session.user.id, saved);
  return saved;
}

/**
 * The workspace menu's GitHub line pressed: it goes from this window at once,
 * and the server keeps it, so the person's other windows and devices show it
 * no more once they read their preferences (as they start). A save the
 * server refused brings it back here.
 */
export function markGitHubSupportOpened(session: Session) {
  const before = owner === session.user.id ? current : cached(session.user.id);
  if (before.githubSupportOpened) return;
  publish(session.user.id, { ...before, githubSupportOpened: true });
  void saveDisplayPreferences(session, { githubSupportOpened: true }).catch(() => {
    if (owner === session.user.id && current.githubSupportOpened) publish(session.user.id, { ...current, githubSupportOpened: false });
  });
}

/**
 * Whether a key sends a comment box: Enter unless the account makes Enter
 * break the line, then Cmd/Ctrl+Enter; Shift+Enter always breaks the line.
 */
export function commentBoxSends(
  event: { key: string; shiftKey: boolean; metaKey: boolean; ctrlKey: boolean },
  enterAddsNewLine: boolean,
): boolean {
  if (!fieldEnter(event) || event.shiftKey) return false;
  return enterAddsNewLine ? primaryModifier(event) : true;
}
