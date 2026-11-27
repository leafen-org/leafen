import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";

const KEY = "leafen:v2:timeline-centers";
const KEPT_VIEWS = 200;
const WRITE_AFTER_MS = 250;

type Centers = Record<string, { center: number; at: number }>;

function stored(): Centers {
  const value = parseStored(personValue(KEY));
  return value && typeof value === "object" && !Array.isArray(value) ? value as Centers : {};
}

/**
 * Where this viewer last left a timeline, kept on this browser for them
 * alone, as Notion keeps each person's timeline center (its local
 * preference store, seen 2026-11-12): a timeline opens there again. Unlike
 * Notion's, the view itself is not written as one opens or scrolls it, so
 * opening a timeline edits nothing for anyone else.
 */
export function rememberedTimelineCenter(viewId: string): number | null {
  const center = stored()[viewId]?.center;
  return typeof center === "number" && Number.isFinite(center) ? center : null;
}

const pending = new Map<string, { center: number; timer: ReturnType<typeof setTimeout> }>();

/** Keeps the time at the timeline's middle as the viewer scrolls, once the scrolling rests. */
export function rememberTimelineCenter(viewId: string, center: number): void {
  if (!Number.isFinite(center)) return;
  const waiting = pending.get(viewId);
  if (waiting) clearTimeout(waiting.timer);
  pending.set(viewId, {
    center,
    timer: setTimeout(() => {
      pending.delete(viewId);
      const all = stored();
      all[viewId] = { center, at: Date.now() };
      const views = Object.entries(all).sort(([, a], [, b]) => b.at - a.at);
      storePersonValue(KEY, JSON.stringify(Object.fromEntries(views.slice(0, KEPT_VIEWS))));
    }, WRITE_AFTER_MS),
  });
}
