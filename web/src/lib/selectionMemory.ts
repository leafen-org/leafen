import { useEffect, useRef } from "react";
import { historyArrival, historyStep, keptHistoryStep } from "./historyStep";

/**
 * What a page's views and blocks have chosen comes back when the tab
 * reloads, as Notion's does (seen 2026-11-12: a table's ticked row, a block
 * chosen with Escape and a board's card chosen with Shift+press are still
 * chosen after a reload; a new tab on the same page, or the page opened
 * afresh, has nothing chosen). Each chooser keeps its choice for this tab
 * (sessionStorage) under its own scope, for each step of the tab's history
 * (historyStep.ts) with the address it was made at, and takes it back once
 * each time the tab comes back to that step: on its first showing after a
 * reload — or, for a chooser that says so (`throughHistory`), after the way
 * back or forward — of the items still there.
 *
 * A page's chosen blocks come back with the way back or forward, as Notion's
 * (seen 2026-11-16 at 1440, the letter typed after going nowhere: a page
 * link just made, chosen, pressed, then the browser's back; a chosen block
 * left by a sidebar row, back; two steps back past the same page opened
 * afresh from the sidebar, which had none, the first step's chosen again,
 * and forward again to that one, none).
 */

const KEY_PREFIX = "leafen:chosen:";
// The steps a chooser keeps its choice for, the newest last.
const STEPS_KEPT = 20;

type KeptStep = { step: string; at: string; ids: string[] };

// The scopes that have taken their choice back, or passed it over, by the arrival at a step.
const takenBack = new Set<string>();

function address() {
  return `${window.location.pathname}${window.location.search}`;
}

function readKept(scope: string): KeptStep[] {
  try {
    const kept = JSON.parse(window.sessionStorage.getItem(KEY_PREFIX + scope) ?? "null") as unknown;
    if (!Array.isArray(kept)) return [];
    return kept.filter((entry): entry is KeptStep => Boolean(entry) && typeof entry === "object"
      && typeof (entry as KeptStep).step === "string" && typeof (entry as KeptStep).at === "string" && Array.isArray((entry as KeptStep).ids))
      .map((entry) => ({ ...entry, ids: entry.ids.filter((id): id is string => typeof id === "string") }));
  } catch {
    return [];
  }
}

/** Keeps a chooser's choice for the step the tab shows; none chosen keeps nothing for it. */
export function keepChoice(scope: string, ids: Iterable<string>) {
  const chosen = [...ids];
  try {
    const step = chosen.length ? historyStep() : keptHistoryStep();
    const others = readKept(scope).filter((entry) => entry.step !== step);
    const kept = chosen.length && step ? [...others, { step, at: address(), ids: chosen }].slice(-STEPS_KEPT) : others;
    if (kept.length) window.sessionStorage.setItem(KEY_PREFIX + scope, JSON.stringify(kept));
    else window.sessionStorage.removeItem(KEY_PREFIX + scope);
  } catch {
    // Storage closed or full: the choice is not kept, as without a reload.
  }
}

/** The choice a chooser kept for the step the tab came back to, once each arrival; none otherwise. */
export function keptChoice(scope: string, throughHistory = false): string[] {
  const arrival = historyArrival();
  const once = `${scope}\u0000${arrival.count}`;
  if (takenBack.has(once)) return [];
  takenBack.add(once);
  if (arrival.by === null || arrival.by === "history" && !throughHistory) return [];
  const step = keptHistoryStep();
  const kept = step ? readKept(scope).find((entry) => entry.step === step) : undefined;
  return kept?.at === address() ? kept.ids : [];
}

/**
 * Keeps a chooser's choice (chosen, set by choose) under its scope, and once
 * it shows its items (ready) after a reload — or after the way back or
 * forward, `throughHistory` — chooses again the kept ones it still has.
 */
export function useKeptChoice(scope: string | null, chosen: ReadonlySet<string>, choose: (ids: ReadonlySet<string>) => void,
  has: (id: string) => boolean, ready: () => boolean, throughHistory = false) {
  const given = useRef<string | null>(null);
  useEffect(() => {
    if (!scope || given.current === scope || !ready()) return;
    given.current = scope;
    const ids = keptChoice(scope, throughHistory).filter(has);
    if (ids.length) choose(new Set(ids));
  });
  useEffect(() => {
    if (scope && given.current === scope) keepChoice(scope, chosen);
  }, [scope, chosen]);
}
