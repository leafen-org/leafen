"use client";

import { useCallback, useEffect, useMemo, useRef } from "react";
import { listenForPageUndo } from "@/components/editor/pageUndo";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { imeComposing } from "./fieldKeyBoundary";

/**
 * A change made on a screen with no editor of its own: what takes it back
 * and what makes it again, and the key a value typed on in it is known by
 * (valueStepRecorder).
 */
export type SurfaceStep = { back(): void; again(): void; key?: string };

const MOST_STEPS = 100;

/**
 * The ⌘Z and ⌘⇧Z of a screen that has no page editor to keep its history —
 * "내 작업", People — as Notion's take back what was changed there (seen
 * 2026-11-09 and 2026-11-15 in "내 작업": a task checked off came back with
 * ⌘Z; 2026-11-15 in People, its owner's "About" written into another's row
 * went with ⌘Z and came back with ⌘⇧Z, the cell staying chosen). The screen
 * keeps its steps here and hears the keys pressed on it, or asked of it from
 * a cell (requestPageUndo with the screen's key); a field, menu, window or an
 * open row's peek keeps its own.
 */
export function useSurfaceUndo(pageKey: string) {
  const done = useRef<SurfaceStep[]>([]);
  const undone = useRef<SurfaceStep[]>([]);
  const record = useCallback((step: SurfaceStep) => {
    done.current.push(step);
    if (done.current.length > MOST_STEPS) done.current.shift();
    undone.current = [];
  }, []);
  /** Whether the history ends with this step, nothing done or undone since. */
  const isLatest = useCallback((key: string) => !undone.current.length && done.current.at(-1)?.key === key, []);
  /** A step whose change the server refused leaves the history. */
  const drop = useCallback((key: string) => {
    done.current = done.current.filter((step) => step.key !== key);
    undone.current = undone.current.filter((step) => step.key !== key);
  }, []);
  useEffect(() => {
    const step = (forward: boolean) => {
      const taken = (forward ? undone : done).current.pop();
      if (!taken) return false;
      (forward ? done : undone).current.push(taken);
      if (forward) taken.again(); else taken.back();
      return true;
    };
    const keys = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !primaryModifier(event) || event.altKey || imeComposing(event)) return;
      const key = chordLetter(event);
      if (key !== "z" && key !== "y") return;
      const target = event.target instanceof HTMLElement ? event.target : null;
      if (target && (target.isContentEditable || target.matches("input:not([type=\"checkbox\"]), textarea, select")
        || target.closest("[role=\"dialog\"], [role=\"menu\"], [role=\"listbox\"]"))) return;
      if (document.querySelector("[data-row-peek-panel], [data-page-peek]")) return;
      if (step(event.shiftKey || key === "y")) event.preventDefault();
    };
    document.addEventListener("keydown", keys);
    const stopAsked = listenForPageUndo(pageKey, step);
    return () => { document.removeEventListener("keydown", keys); stopAsked(); };
  }, [pageKey]);
  return useMemo(() => Object.assign((step: SurfaceStep) => record(step), { isLatest, drop }), [drop, isLatest, record]);
}
