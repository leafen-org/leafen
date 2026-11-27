"use client";

import { useSurfaceUndo, type SurfaceStep } from "@/lib/surfaceUndo";

/** A change made in "내 작업": what takes it back and what makes it again. */
export type MyTasksStep = SurfaceStep;

/**
 * "내 작업"'s own ⌘Z and ⌘⇧Z: a task checked off, renamed or given a date
 * there comes back with ⌘Z, as Notion's "내 작업" (seen 2026-11-09 by the
 * OP-03-c audit and again 2026-11-15 in Chrome at 1440: a task checked off
 * left the list and ⌘Z put it back in its place; a name changed with ✎ went
 * back). It is a screen with no editor of its own (useSurfaceUndo).
 */
export const useMyTasksUndo: (pageKey: string) => (step: MyTasksStep) => void = useSurfaceUndo;
