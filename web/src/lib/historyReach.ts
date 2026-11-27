import { useSyncExternalStore } from "react";
import { ROUTE_CHANGE_EVENT, type RouteMove } from "./router";

/*
 * How far back and forward the tab's history reaches within the app, for a
 * touch screen's ← and → (TopBarHistoryArrows): each step this document
 * makes is numbered in its history state, one past the step it left, so the
 * way back reaches as far as the first step and the way forward as far as the
 * furthest step made from here. Nothing is said until the tab has moved
 * within the app — the ← and → of Notion's iPad come only then (seen
 * 2026-11-16 in iPad Safari 834: a page opened by its address has none; a
 * link followed shows ← and a dim →, and ← back shows a dim ← and →).
 */
const INDEX = "leafenStepIndex";

export type HistoryReach = { moved: boolean; back: boolean; forward: boolean };

let at = 0;
let furthest = 0;
let shown: HistoryReach = { moved: false, back: false, forward: false };
const listeners = new Set<() => void>();

function stateIndex(): number | undefined {
  const state: unknown = window.history.state;
  const index = state && typeof state === "object" ? (state as Record<string, unknown>)[INDEX] : undefined;
  return typeof index === "number" ? index : undefined;
}

function markIndex(index: number) {
  const state: unknown = window.history.state;
  window.history.replaceState({ ...(state && typeof state === "object" ? state : {}), [INDEX]: index }, "", window.location.href);
}

function tell(moved: boolean) {
  const next = { moved, back: at > 0, forward: at < furthest };
  if (next.moved === shown.moved && next.back === shown.back && next.forward === shown.forward) return;
  shown = next;
  for (const listener of listeners) listener();
}

if (typeof window !== "undefined") {
  const kept = stateIndex();
  at = kept ?? 0;
  furthest = at;
  if (kept === undefined) markIndex(at);
  let address = window.location.href;
  window.addEventListener(ROUTE_CHANGE_EVENT, (event) => {
    const moved = window.location.href !== address;
    address = window.location.href;
    if (!moved) return;
    // A step that took the place of the one shown keeps its number, and an
    // address the tab was sent on to (a sign-in's return, the start page) is
    // not a move within the app (router.ts openedInApp).
    if ((event as CustomEvent<RouteMove | undefined>).detail?.replace) {
      markIndex(at);
      return;
    }
    at += 1;
    furthest = at;
    markIndex(at);
    tell(true);
  });
  window.addEventListener("popstate", () => {
    address = window.location.href;
    at = stateIndex() ?? at;
    furthest = Math.max(furthest, at);
    tell(true);
  });
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

const before: HistoryReach = { moved: false, back: false, forward: false };

/** Whether the tab has moved within the app, and whether its way back and forward lead anywhere in it. */
export function useHistoryReach(): HistoryReach {
  return useSyncExternalStore(subscribe, () => shown, () => before);
}
