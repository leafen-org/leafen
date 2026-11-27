import type { GraphView } from "@/contentGraph";
import { deviceValue, parseStored, personValue, storeDeviceValue, storePersonValue } from "@/lib/browserStorage";
import { viewQuery, type PersonalQuery } from "./databasePersonalQuery";

// A viewer's own filters and sorts on a view, as Notion keeps them (seen
// 2026-10-01): in this browser, for that person and view, so a reload keeps
// them while others see the view as saved; resetting them forgets them here.
// The most recently changed views are kept.

const KEY = "leafen:v2:database-personal-queries";
const LIMIT = 200;

type Queries = Record<string, unknown>;

function asQueries(value: unknown): Queries {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Queries : {};
}

// Each person's views are kept apart. Before, every person's were in one
// record under "person:view"; a person's first read takes their own out.
function readAll(userId: string | undefined): Queries {
  if (!userId) return {};
  const own = personValue(KEY, { userId });
  if (own !== null) return asQueries(parseStored(own));
  const shared = asQueries(parseStored(deviceValue(KEY)));
  const prefix = `${userId}:`;
  const mine: Queries = {};
  for (const [key, value] of Object.entries(shared)) {
    if (!key.startsWith(prefix)) continue;
    mine[key.slice(prefix.length)] = value;
    delete shared[key];
  }
  if (Object.keys(mine).length) {
    storePersonValue(KEY, JSON.stringify(mine), { userId });
    storeDeviceValue(KEY, Object.keys(shared).length ? JSON.stringify(shared) : null);
  }
  return mine;
}

/** The filters and sorts this person keeps for a view here, if any. */
export function readPersonalQuery(userId: string | undefined, viewId: string): PersonalQuery | undefined {
  const stored = readAll(userId)[viewId];
  if (!stored || typeof stored !== "object" || Array.isArray(stored)) return undefined;
  const query = viewQuery(stored as GraphView["config"]);
  return Object.keys(query).length ? query : undefined;
}

/** Keeps, or with null forgets, the filters and sorts this person applies to a view here. */
export function writePersonalQuery(userId: string | undefined, viewId: string, query: PersonalQuery | null) {
  if (!userId) return;
  const all = readAll(userId);
  if (!query && !(viewId in all)) return;
  delete all[viewId];
  if (query) all[viewId] = query;
  const keys = Object.keys(all);
  for (const old of keys.slice(0, Math.max(0, keys.length - LIMIT))) delete all[old];
  // Without storage the filters and sorts still apply for this visit.
  storePersonValue(KEY, JSON.stringify(all), { userId });
}
