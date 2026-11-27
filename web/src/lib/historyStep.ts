import { ROUTE_CHANGE_EVENT } from "./router";

/*
 * Each step of the tab's history is marked in its history state, so what a
 * page keeps for a step — where it was read (pageScrollPlace.ts), what was
 * chosen in it (selectionMemory.ts) — comes back with the way back or forward
 * to that step, or a reload of it. A link's push copies the mark of the step
 * it leaves, so the new step takes a mark of its own as it is made, before
 * the page it opens is drawn.
 */
const MARK = "leafenPlace";

/** The mark of the step the tab shows, if it has one yet. */
export function keptHistoryStep(): string | undefined {
  const state: unknown = window.history.state;
  const kept = state && typeof state === "object" ? (state as Record<string, unknown>)[MARK] : undefined;
  return typeof kept === "string" ? kept : undefined;
}

/** The mark of the step the tab shows, given it now if it has none (or anew, `fresh`). */
export function historyStep(fresh = false): string {
  const kept = fresh ? undefined : keptHistoryStep();
  if (kept) return kept;
  const state: unknown = window.history.state;
  const mark = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
  window.history.replaceState({ ...(state && typeof state === "object" ? state : {}), [MARK]: mark }, "", window.location.href);
  return mark;
}

/** How the tab came to the step it shows: a reload of it, the way back or forward to it, or none (a new step). */
export type HistoryArrival = { count: number; by: "reload" | "history" | null };

// The tab's arrivals at a step, counted: a step come back to holds what was
// kept for it; a new one (a link, the first load) holds nothing.
let arrival: HistoryArrival = { count: 0, by: null };

function documentArrival(): HistoryArrival["by"] {
  try {
    const entry = performance.getEntriesByType("navigation")[0] as PerformanceNavigationTiming | undefined;
    return entry?.type === "reload" ? "reload" : entry?.type === "back_forward" ? "history" : null;
  } catch {
    return null;
  }
}

/** The tab's last arrival at a step. */
export function historyArrival(): Readonly<HistoryArrival> {
  return arrival;
}

const leaving = new Set<(left: string | undefined) => void>();

/** Hears the tab leave a step for a new one (a link's push), with the mark of the step it left. */
export function onStepLeft(left: (step: string | undefined) => void): () => void {
  leaving.add(left);
  return () => { leaving.delete(left); };
}

if (typeof window !== "undefined") {
  arrival = { count: 0, by: documentArrival() };
  let lastAddress = window.location.href;
  window.addEventListener(ROUTE_CHANGE_EVENT, (event) => {
    const replaced = (event as CustomEvent<{ replace?: boolean } | undefined>).detail?.replace === true;
    const moved = window.location.href !== lastAddress;
    lastAddress = window.location.href;
    if (!moved || replaced) return;
    // The push copied the mark of the step it left.
    const left = keptHistoryStep();
    historyStep(true);
    for (const hear of leaving) hear(left);
    arrival = { count: arrival.count + 1, by: null };
  });
  window.addEventListener("popstate", () => {
    lastAddress = window.location.href;
    arrival = { count: arrival.count + 1, by: "history" };
  });
}
