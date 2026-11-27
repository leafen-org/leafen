import type { AppPermission } from "@/appPlatformWire.generated";
import { propertyTypeLabel } from "@/components/database/propertyTypes";
import type { PropertyType } from "@/lib/types";
import { formatFileSize } from "../database/files";
import { appText } from "./administratorAppsApi";

/**
 * An app's declarations in the reader's words (the serverConsole namespace):
 * a schedule's cadence, a data change's name, the places of a menu line, and
 * each permission line — names in the reader's language, keys and raw values
 * only beside them (2026-10-16: the app's review and jobs windows showed
 * keys, cron and event types).
 */
type Translate = (key: string, values?: Record<string, unknown>) => string;

function list(items: string[], language: string) {
  try {
    return new Intl.ListFormat(language, { type: "conjunction" }).format(items);
  } catch {
    return items.join(", ");
  }
}

function clock(language: string, hour: number, minute: number) {
  return new Intl.DateTimeFormat(language, { hour: "numeric", minute: "2-digit", timeZone: "UTC" })
    .format(new Date(Date.UTC(2024, 0, 1, hour, minute)));
}

function weekdayName(language: string, day: number) {
  // 2024-01-07 was a Sunday, cron's day 0.
  return new Intl.DateTimeFormat(language, { weekday: "long", timeZone: "UTC" }).format(new Date(Date.UTC(2024, 0, 7 + day)));
}

/** The weekdays a cron field names (0 Sunday), or null for one this doesn't read. */
function cronWeekdays(field: string): number[] | null {
  const days = new Set<number>();
  for (const part of field.split(",")) {
    const range = /^(\d)(?:-(\d))?$/u.exec(part);
    if (!range) return null;
    const from = Number(range[1]);
    const to = range[2] === undefined ? from : Number(range[2]);
    if (from > 7 || to > 7 || to < from) return null;
    for (let day = from; day <= to; day += 1) days.add(day % 7);
  }
  return [...days].sort((left, right) => left - right);
}

/** How often a five-field cron runs, said as a person would; one this doesn't read stays as it is. */
export function describeAppCron(cron: string, t: Translate, language: string): string {
  const fields = cron.trim().split(/\s+/u);
  if (fields.length !== 5 || fields[3] !== "*") return cron;
  const [minute, hour, day, , weekday] = fields;
  const number = (value: string) => (/^\d+$/u.test(value) ? Number(value) : Number.NaN);
  const every = (value: string) => Number(/^\*\/(\d+)$/u.exec(value)?.[1] ?? Number.NaN);
  const pad = (value: number) => String(value).padStart(2, "0");
  if (hour === "*" && day === "*" && weekday === "*") {
    if (minute === "*" || every(minute) === 1) return t("apps.cron.everyMinute");
    if (every(minute) > 1) return t("apps.cron.everyMinutes", { count: every(minute) });
    return Number.isNaN(number(minute)) ? cron : t("apps.cron.hourly", { minute: pad(number(minute)) });
  }
  if (!Number.isNaN(number(minute)) && every(hour) >= 1 && day === "*" && weekday === "*") {
    return every(hour) === 1
      ? t("apps.cron.hourly", { minute: pad(number(minute)) })
      : t("apps.cron.everyHours", { count: every(hour), minute: pad(number(minute)) });
  }
  if (Number.isNaN(number(minute)) || Number.isNaN(number(hour)) || number(hour) > 23 || number(minute) > 59) return cron;
  const time = clock(language, number(hour), number(minute));
  if (day === "*" && weekday === "*") return t("apps.cron.daily", { time });
  if (day === "*") {
    const days = cronWeekdays(weekday);
    if (!days?.length) return cron;
    if (days.join() === "1,2,3,4,5") return t("apps.cron.weekdays", { time });
    if (days.join() === "0,6") return t("apps.cron.weekends", { time });
    return t("apps.cron.onDays", { days: list(days.map((value) => weekdayName(language, value)), language), time });
  }
  return weekday === "*" && number(day) >= 1 && number(day) <= 31 ? t("apps.cron.monthly", { day: number(day), time }) : cron;
}

/** A data change an app runs on, by its name ("page.properties_updated" → "a page's properties change"). */
export function appEventLabel(type: string, t: Translate) {
  const key = type.replace(/[._](\w)/gu, (_, letter: string) => letter.toUpperCase());
  return t(`apps.event.${key}`, { defaultValue: type });
}

/** The places a menu line is in ("row", "page"). */
function placesText(places: string[], t: Translate, language: string) {
  return list(places.map((place) => t(`apps.place.${place}`, { defaultValue: place })), language);
}

/** File types by their extensions; types named otherwise only when there are no extensions. */
function fileTypesText(types: string[], language: string) {
  const extensions = types.filter((type) => type.startsWith("."));
  return list(extensions.length ? extensions : types, language);
}

/** An address's host as written ("*.atlassian.net" stays so), or the address as it is. */
function hostOf(address: string) {
  return /^[a-z][a-z0-9+.-]*:\/\/([^/?#]+)/iu.exec(address)?.[1] ?? address;
}

/** What one permission lets the app do, in plain words. */
export function permissionText(permission: AppPermission, t: Translate, language: string) {
  const name = permission.name ? appText(permission.name, language) : "";
  const target = permission.target ? appText(permission.target, language) : "";
  const types = permission.types ?? [];
  const named = (kind: string) => t(`apps.permission.${kind}`, { detail: name || permission.detail });
  switch (permission.kind) {
    case "personAccess":
      return t(`apps.permission.personAccess_${permission.detail}`);
    case "device":
      return t("apps.permission.device", { detail: t(`apps.device.${permission.detail}`) });
    case "credentialUse": {
      const [key, where = ""] = permission.detail.split(" → ");
      const credential = name || key;
      if (where.startsWith("setting:")) return t("apps.permission.credentialUseSetting", { credential, setting: target || where.slice(8) });
      return t("apps.permission.credentialUseTo", { credential, where: hostOf(where) });
    }
    case "directAddress": {
      if (!types.length) return named("directAddress");
      const address = permission.detail.replace(/ \([^)]*\)$/u, "");
      const kinds = list(types.map((kind) => t(`apps.directKind.${kind}`, { defaultValue: kind })), language);
      return t("apps.permission.directAddressFor", { address, kinds });
    }
    case "outsideSignInUse": {
      const [provider, where = ""] = permission.detail.split(" → ");
      return t("apps.permission.outsideSignInUseTo", { provider, where: hostOf(where) });
    }
    case "outsideSignIn": {
      const [provider, address = ""] = permission.detail.split(" ");
      return t("apps.permission.outsideSignInAt", { provider, where: hostOf(address) });
    }
    case "schedule": {
      if (permission.cron === undefined) return named("schedule");
      const cadence = describeAppCron(permission.cron, t, language);
      const when = permission.perPerson ? `${cadence}, ${t("apps.jobs.perPerson")}` : cadence;
      return name ? t("apps.permission.scheduleNamed", { name, when }) : t("apps.permission.schedule", { detail: when });
    }
    case "dataEvent":
      return types.length ? t("apps.permission.dataEvent", { detail: list(types.map((type) => appEventLabel(type, t)), language) }) : named("dataEvent");
    case "slashItem":
      if (!target) return named("slashItem");
      return t(permission.targetKind === "block" ? "apps.permission.slashItemBlock" : "apps.permission.slashItemAction", { name, target });
    case "menuAction":
      return target ? t("apps.permission.menuActionTarget", { places: placesText(types, t, language), name, target }) : named("menuAction");
    case "propertyEditor":
      return types.length
        ? t("apps.permission.propertyEditor", { detail: `${name} (${list(types.map((type) => propertyTypeLabel(type as PropertyType)), language)})` })
        : named("propertyEditor");
    case "fileOpener":
    case "fileEditor":
      return t(`apps.permission.${permission.kind}`, { detail: types.length ? fileTypesText(types, language) : permission.detail });
    case "storage":
      return t("apps.permission.storage", { detail: formatFileSize(Number(permission.detail)) });
    case "guests":
      // Rooms the app gave no names are counted, not listed by their keys.
      return name ? named("guests") : t("apps.permission.guestsCount", { count: permission.detail.split(", ").length });
    case "longRunningWorker":
    case "residentWorker":
    case "roomLoop":
      return t(`apps.permission.${permission.kind}`);
    default:
      return named(permission.kind);
  }
}

export type AppPermissionGroup = "data" | "accounts" | "outside" | "agents" | "background" | "places";

const PERMISSION_GROUPS: Record<AppPermissionGroup, readonly string[]> = {
  data: ["personAccess", "lockedData", "groups"],
  accounts: ["personCredential", "credentialUse", "outsideSignIn", "outsideSignInUse", "secret"],
  outside: ["directAddress", "relayAddress", "privateNetwork", "socket", "settingSocket", "webhook", "endpoint", "publicEndpoint", "endpointPages"],
  agents: ["toolWithoutApproval", "agentInstructions"],
  background: ["schedule", "dataEvent", "longRunningWorker", "residentWorker", "roomLoop"],
  places: [],
};

/** An app's permissions by what they are about, in the order people read them; the rest is what it adds to Leafen. */
export function groupAppPermissions(permissions: readonly AppPermission[]) {
  const groupOf = (kind: string) => (Object.keys(PERMISSION_GROUPS) as AppPermissionGroup[])
    .find((group) => PERMISSION_GROUPS[group].includes(kind)) ?? "places";
  return (Object.keys(PERMISSION_GROUPS) as AppPermissionGroup[])
    .map((group) => ({ group, permissions: permissions.filter((permission) => groupOf(permission.kind) === group) }))
    .filter((entry) => entry.permissions.length > 0);
}

/** What a catalog row says of an app after its maker, each a few words. */
export function appCatalogFacts(app: { catalogId?: string; reviewed?: boolean; network?: string; risk?: string; paid?: boolean; example?: boolean },
  t: Translate) {
  return [
    ...(app.reviewed && app.catalogId !== "builtin" ? [t("apps.browse.reviewed")] : []),
    ...(app.network === "none" ? [t("apps.browse.noNetwork")] : []),
    ...(app.risk === "high" ? [t("apps.browse.risk.high")] : []),
    ...(app.paid ? [t("apps.browse.paid")] : []),
    ...(app.example ? [t("apps.browse.example")] : []),
  ];
}

/** A line's identity across versions: what it is and what it names. */
export function permissionKey(permission: AppPermission) {
  return `${permission.kind}\u0000${permission.detail}`;
}
