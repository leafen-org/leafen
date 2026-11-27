import { useEffect, useRef } from "react";
import { lineRowId } from "@/components/database/viewGroupLines";
import { historyArrival, historyStep, keptHistoryStep, onStepLeft } from "./historyStep";
import { onReloadKeepingPlace, takeKeptPlace } from "./reloadKeepingPlace";

/**
 * What a page's views and blocks have chosen comes back with the browser's
 * way back or forward to the step it was chosen in, as Notion's does (seen
 * 2026-11-27 in Chrome at 1440, dark, after each step by the screen, in two
 * tabs at different times: a table's ticked row with its "1개 선택됨", its
 * view opening pages whole, the row opened from its "열기", then the
 * browser's back, ticked again, and again after forward and back; a board's
 * card chosen with Shift and opened with Enter, back, chosen again; the
 * same page opened afresh from a link, nothing ticked, and two steps back,
 * ticked; seen 2026-11-16: a page's block chosen, then a page link or a
 * sidebar row, back, chosen again). Each chooser keeps its choice under its
 * own scope for each step of the tab's history (historyStep.ts) with the
 * address it was made at, and takes it back once each time the tab comes
 * back to that step, of the items still there.
 *
 * A press that takes the tab to another page — a row's "열기" opening it
 * whole, the breadcrumb, a link — keeps for the step it leaves what that
 * press let go on its way, as Notion's (seen 2026-11-27: a row ticked, the
 * breadcrumb pressed, back, ticked).
 *
 * A page's chosen blocks are kept for the tab (sessionStorage) and come back
 * after a reload too, of that step or of another one gone back from (seen
 * 2026-11-12, and 2026-11-27: a block chosen, forward to a row's page,
 * reloaded there, back, chosen). A view's ticked rows and chosen cards are
 * kept while the app stays loaded: a reload lets them go, of their step or
 * of one the tab then goes back from, as Notion's (seen 2026-11-27 in two
 * tabs at different times: a row ticked, reloaded, none; ticked, opened
 * whole, reloaded there, back, none) — but a reload the app asks for itself
 * keeps those of the step it shows (reloadKeepingPlace.ts). A new tab on
 * the same page, or the page opened afresh, has nothing chosen.
 *
 * A view's ticked rows or chosen cards go on with it into the layout it is
 * switched to, as Notion's (seen 2026-11-27 in Chrome at 1440, dark, after
 * each switch by the screen, in two tabs at different times: a board's card
 * chosen with Shift, the view made a table from its "레이아웃", showed its
 * row ticked and "1개 선택됨"; a table's ticked row made a board, a list, a
 * gallery and a timeline showed it chosen in each; two gallery cards chosen
 * made a table showed both ticked, "2개 선택됨"; a ticked row made a gallery,
 * then Shift on another card, chose those between too, once beside it and
 * once two cards on, in another group). Its rows go, each chosen whole there
 * (viewGroupLines.ts), of those the new layout shows — unless another press
 * came before the new layout drew them (carryViewChoice).
 */

const KEY_PREFIX = "leafen:chosen:";
// The steps a chooser keeps its choice for, the newest last.
const STEPS_KEPT = 20;

type KeptStep = { step: string; at: string; ids: string[] };

// The choices kept while the app stays loaded, by scope (a chooser not keeping them across reloads).
const keptLoaded = new Map<string, KeptStep[]>();

// The scopes that have taken their choice back, or passed it over, by the arrival at a step.
const takenBack = new Set<string>();

// The presses (a pointer's or a key's) made, counted, and what the latest let go of each step's kept choice.
let presses = 0;
let letGo: { scope: string; acrossReloads: boolean; kept: KeptStep }[] = [];

// What each view shown now has chosen, read by its id, and the rows a layout switch carries on, with the press that made it.
const viewChoices = new Map<string, Set<() => ReadonlySet<string>>>();
let carried: { view: string; rows: string[]; press: number } | null = null;

/** The view switches its layout: the rows it has chosen go on to the chooser of its new layout (useKeptChoice's `view`). */
export function carryViewChoice(view: string) {
  const rows = new Set<string>();
  for (const read of viewChoices.get(view) ?? []) for (const id of read()) rows.add(lineRowId(id));
  carried = rows.size ? { view, rows: [...rows], press: presses } : null;
}

/** The rows a layout switch of the view carried, taken once — none after another press came since. */
function carriedTo(view: string): string[] | undefined {
  if (carried?.view !== view) return undefined;
  const { rows, press } = carried;
  carried = null;
  return press === presses ? rows : undefined;
}

function address() {
  return `${window.location.pathname}${window.location.search}`;
}

function keptSteps(value: unknown): KeptStep[] {
  if (!Array.isArray(value)) return [];
  return value.filter((entry): entry is KeptStep => Boolean(entry) && typeof entry === "object"
    && typeof (entry as KeptStep).step === "string" && typeof (entry as KeptStep).at === "string" && Array.isArray((entry as KeptStep).ids))
    .map((entry) => ({ ...entry, ids: entry.ids.filter((id): id is string => typeof id === "string") }));
}

function readKept(scope: string, acrossReloads: boolean): KeptStep[] {
  if (!acrossReloads) return keptLoaded.get(scope) ?? [];
  try {
    return keptSteps(JSON.parse(window.sessionStorage.getItem(KEY_PREFIX + scope) ?? "null"));
  } catch {
    return [];
  }
}

function writeKept(scope: string, acrossReloads: boolean, kept: KeptStep[]) {
  if (!acrossReloads) {
    if (kept.length) keptLoaded.set(scope, kept);
    else keptLoaded.delete(scope);
    return;
  }
  try {
    if (kept.length) window.sessionStorage.setItem(KEY_PREFIX + scope, JSON.stringify(kept));
    else window.sessionStorage.removeItem(KEY_PREFIX + scope);
  } catch {
    // Storage closed or full: the choice is not kept, as without a reload.
  }
}

if (typeof window !== "undefined") {
  // Heard before any chooser's own press lets its choice go.
  const pressed = () => { presses += 1; letGo = []; };
  window.addEventListener("pointerdown", pressed, true);
  window.addEventListener("keydown", pressed, true);
  onStepLeft((left) => {
    const given = letGo.filter(({ kept }) => kept.step === left);
    letGo = [];
    for (const { scope, acrossReloads, kept } of given) {
      writeKept(scope, acrossReloads, [...readKept(scope, acrossReloads).filter((entry) => entry.step !== kept.step), kept].slice(-STEPS_KEPT));
    }
  });
  for (const [scope, kept] of Object.entries(takeKeptPlace<Record<string, unknown>>("chosen") ?? {})) {
    if (keptSteps(kept).length) keptLoaded.set(scope, keptSteps(kept));
  }
  onReloadKeepingPlace("chosen", () => {
    const step = keptHistoryStep();
    const shown = [...keptLoaded].map(([scope, kept]) => [scope, kept.filter((entry) => entry.step === step)] as const)
      .filter(([, kept]) => kept.length);
    return shown.length ? Object.fromEntries(shown) : null;
  });
}

/** Keeps a chooser's choice for the step the tab shows; none chosen keeps nothing for it. */
export function keepChoice(scope: string, ids: Iterable<string>, acrossReloads = false) {
  const chosen = [...ids];
  try {
    const step = chosen.length ? historyStep() : keptHistoryStep();
    const kept = readKept(scope, acrossReloads);
    const was = chosen.length ? undefined : kept.find((entry) => entry.step === step);
    if (was?.ids.length && presses) letGo.push({ scope, acrossReloads, kept: was });
    const others = kept.filter((entry) => entry.step !== step);
    writeKept(scope, acrossReloads, chosen.length && step ? [...others, { step, at: address(), ids: chosen }].slice(-STEPS_KEPT) : others);
  } catch {
    // No history state to mark the step in: the choice is not kept.
  }
}

/** The choice a chooser kept for the step the tab came back to, once each arrival; none otherwise. */
export function keptChoice(scope: string, acrossReloads = false): string[] {
  const arrival = historyArrival();
  const once = `${scope}\u0000${arrival.count}`;
  if (takenBack.has(once)) return [];
  takenBack.add(once);
  if (arrival.by === null) return [];
  const step = keptHistoryStep();
  const kept = step ? readKept(scope, acrossReloads).find((entry) => entry.step === step) : undefined;
  return kept?.at === address() ? kept.ids : [];
}

/**
 * Keeps a chooser's choice (chosen, set by choose) under its scope, and once
 * it shows its items (ready) after the way back or forward — or a reload, of
 * a chooser keeping them across reloads (`acrossReloads`) — chooses again
 * the kept ones it still has. A view's chooser (`view`, its id) also takes
 * the rows its layout switch carried (carryViewChoice).
 */
export function useKeptChoice(scope: string | null, chosen: ReadonlySet<string>, choose: (ids: ReadonlySet<string>) => void,
  has: (id: string) => boolean, ready: () => boolean, acrossReloads = false, view?: string) {
  const given = useRef<string | null>(null);
  const latest = useRef(chosen);
  latest.current = chosen;
  useEffect(() => {
    if (!view) return;
    const read = () => latest.current, reads = viewChoices.get(view) ?? new Set<() => ReadonlySet<string>>();
    viewChoices.set(view, reads.add(read));
    return () => { reads.delete(read); if (!reads.size && viewChoices.get(view) === reads) viewChoices.delete(view); };
  }, [view]);
  useEffect(() => {
    if (!scope || given.current === scope || !ready()) return;
    given.current = scope;
    const kept = keptChoice(scope, acrossReloads);
    const ids = ((view ? carriedTo(view) : undefined) ?? kept).filter(has);
    if (ids.length) choose(new Set(ids));
  });
  useEffect(() => {
    if (scope && given.current === scope) keepChoice(scope, chosen, acrossReloads);
  }, [scope, chosen, acrossReloads]);
}
