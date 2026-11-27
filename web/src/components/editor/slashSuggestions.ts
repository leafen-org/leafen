import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { blockDefKey, type BlockDef } from "./blockDefinitions";

// "추천" at the head of "/" and "+", as Notion's (read 2026-11-18 from the
// store its page keeps and the menu it draws, and foretold right in Chrome:
// picking 제목1 while suggesting put it first, ahead of 할 일 목록): each row
// picked counts once with the time it was picked, and its score is that count
// times the weight of how long ago — 100 within 4 hours, then 80, 70, 60, 50,
// 40 and 30 within 12 hours, a day, 3 days, a week, 2 weeks and 4 weeks, 20
// after that — rounded up; rows of one score keep the order they were first
// picked in. Four rows are taken from the menu's own rows, best first, and
// "추천" has none until four of them were picked. On an empty line a "전환"
// or "작업" row is not taken; after words it is, but four of what makes or
// colors must be there besides. Notion's "AI 노트" and "HTML" lead whatever
// was picked, taking places of the four: Leafen leaves AI 노트 out (no AI in
// the editor) and keeps its place, and its HTML embed stands for
// "HTML" where the build has the app platform, for a member of the page's
// workspace, not a guest (slashHtmlLead). Rows the menu doesn't show
// there, as while suggesting, still take their places, so suggesting lists
// two (seen 2026-11-18: 할 일 목록·제목3, then 제목1·할 일 목록). Notion keeps
// the picks per browser for every account on it; Leafen keeps each person's.

const KEY = "leafen:v2:slash-menu-picks";
const ROWS = 4;
const WEIGHTS: ReadonlyArray<readonly [hours: number, weight: number]> = [[4, 100], [12, 80], [24, 70], [72, 60], [168, 50], [336, 40], [672, 30]];
const OLDEST_WEIGHT = 20;
const HOUR = 3_600_000;
const HTML_LEAD = "embed_html";

type Pick = { frequency: number; lastAccessed: number };
type Picks = Record<string, Pick>;

function readPicks(): Picks {
  const stored = parseStored(personValue(KEY));
  if (!stored || typeof stored !== "object" || Array.isArray(stored)) return {};
  const picks: Picks = {};
  for (const [key, value] of Object.entries(stored as Record<string, unknown>)) {
    const pick = value as Partial<Pick> | null;
    if (pick && typeof pick.frequency === "number" && typeof pick.lastAccessed === "number"
      && Number.isFinite(pick.frequency) && Number.isFinite(pick.lastAccessed)) picks[key] = { frequency: pick.frequency, lastAccessed: pick.lastAccessed };
  }
  return picks;
}

/** Counts a row picked from "/" or "+" toward "추천". */
export function rememberSlashPick(def: BlockDef, now = Date.now()) {
  const picks = readPicks(), key = blockDefKey(def), pick = picks[key];
  picks[key] = { frequency: (pick?.frequency ?? 0) + 1, lastAccessed: now };
  storePersonValue(KEY, JSON.stringify(picks));
}

function score(pick: Pick, now: number) {
  const hours = (now - pick.lastAccessed) / HOUR;
  return Math.ceil(pick.frequency * (WEIGHTS.find(([upTo]) => hours <= upTo)?.[1] ?? OLDEST_WEIGHT));
}

// What acts on the line ("전환", "작업"), a color, or what makes something.
function kindOf(def: BlockDef): "line" | "color" | "make" {
  if (def.group === "TurnInto" || def.group === "Actions") return "line";
  return def.group === "TextColor" || def.group === "BackgroundColor" ? "color" : "make";
}

/**
 * The rows "추천" holds, from those the menu shows (`offered`, unsearched):
 * whether its line had nothing but the "/" (`lineEmpty`) and whether the
 * HTML embed leads it (`html`, slashHtmlLead).
 */
export function slashSuggestions(offered: readonly BlockDef[], { lineEmpty, html }: { lineEmpty: boolean; html: boolean },
  picks: Picks = readPicks(), now = Date.now()): BlockDef[] {
  const order = Object.keys(picks).sort((a, b) => score(picks[b], now) - score(picks[a], now));
  const rank = new Map(order.map((key, index) => [key, index]));
  const rows = new Map<string, BlockDef>();
  for (const def of offered) if (!rows.has(blockDefKey(def))) rows.set(blockDefKey(def), def);
  let counted = 0;
  const taken = [...rows].filter(([key, def]) => {
    if (!rank.has(key)) return false;
    const kind = kindOf(def);
    if (lineEmpty) return kind !== "line";
    if (kind !== "line") counted += 1;
    return true;
  }).sort(([a], [b]) => rank.get(a)! - rank.get(b)!).slice(0, ROWS).map(([key]) => key);
  const best = taken.length < ROWS || !lineEmpty && counted < ROWS ? [] : taken;
  // The leads: AI 노트's place (never shown), then the HTML embed.
  const leads: (string | null)[] = [null, ...(html ? [HTML_LEAD] : [])];
  const rest = best.filter((key) => !leads.includes(key));
  const over = Math.max(0, leads.length - (best.length - rest.length));
  return [...leads, ...rest.slice(0, Math.max(0, rest.length - over))]
    .flatMap((key) => key !== null && rows.has(key) ? [rows.get(key)!] : []);
}
