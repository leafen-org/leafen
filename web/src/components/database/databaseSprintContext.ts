import { createContext, useSyncExternalStore } from "react";
import type { DatabaseSprintActionInput, DatabaseSprintChanges, DatabaseSprintState } from "@/databaseToolbarConfigWire.generated";

export type DatabaseSprintRequest = Omit<DatabaseSprintActionInput, "requestId" | "expectedRevision">;

/**
 * How a sprint action ended: done, refused by a legal hold, failed, or
 * unanswered — said already, and the sprints read again once the server
 * answers (useDatabaseSprintSurface).
 */
export type DatabaseSprintOutcome = "done" | "legal-hold" | "failed" | "unanswered";

// A task database's sprints reach its settings menu and the Sprint board's
// toolbar through this context.
export type DatabaseSprintSurface = {
  state: DatabaseSprintState | null;
  timeZone: string;
  run(request: DatabaseSprintRequest): Promise<DatabaseSprintOutcome>;
  /** Runs a request and tells what a completion made and changed. */
  runWithChanges(request: DatabaseSprintRequest): Promise<{ outcome: DatabaseSprintOutcome; changes?: DatabaseSprintChanges }>;
  /** The view the settings menu was opened from. */
  activeViewId?: string;
  /** The task database's own page, where turning sprints off goes back to. */
  pageId: string;
};

export const DatabaseSprintContext = createContext<DatabaseSprintSurface | null>(null);

/**
 * Whether the sprints manage a property while they are on — a task's
 * "스프린트", a sprint's "작업" or its status — as the server marks it in
 * every read: every property menu keeps its type and its two-way relation,
 * as Notion's (seen 2026-11-07 and 2026-11-08), and the server refuses
 * either change too.
 */
export function sprintManagedProperty(property: { sprintManaged?: string }) {
  return Boolean(property.sprintManaged);
}

/**
 * The end of a database's filter bar row, where the Sprint board's current
 * sprint view puts its tasks done and "스프린트 완료", as Notion's (seen
 * 2026-11-03).
 */
export const DatabaseSprintRowContext = createContext<HTMLElement | null>(null);

// The current sprint's view keeps its name and its tab names the sprint,
// "현재: 스프린트 3", as Notion's (seen 2026-11-03). The toolbar reads the
// sprints; the tabs beside it learn the name here.
const currentSprintNames = new Map<string, string>();
const listeners = new Set<() => void>();
let namesVersion = 0;

function namesChanged() {
  namesVersion += 1;
  listeners.forEach((listener) => listener());
}

/** Names a view's tab for its current sprint until the returned call. */
export function nameCurrentSprintView(viewId: string, sprintTitle: string) {
  currentSprintNames.set(viewId, sprintTitle);
  namesChanged();
  return () => {
    if (currentSprintNames.get(viewId) !== sprintTitle) return;
    currentSprintNames.delete(viewId);
    namesChanged();
  };
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The current sprint's name by its view. */
export function useCurrentSprintViewNames(): ReadonlyMap<string, string> {
  useSyncExternalStore(subscribe, () => namesVersion, () => namesVersion);
  return currentSprintNames;
}

// The Sprint board's page keeps its views off the sidebar, its tabs'
// "사이드바에 보기 추가" dimmed, as Notion's (seen 2026-11-04). Each task
// database reading its sprints marks its board, by how many read it.
const sprintBoardPages = new Map<string, number>();

/** Marks a page as a Sprint board until the returned call. */
export function markSprintBoardPage(pageId: string) {
  sprintBoardPages.set(pageId, (sprintBoardPages.get(pageId) ?? 0) + 1);
  namesChanged();
  return () => {
    const left = (sprintBoardPages.get(pageId) ?? 1) - 1;
    if (left > 0) sprintBoardPages.set(pageId, left);
    else sprintBoardPages.delete(pageId);
    namesChanged();
  };
}

/** Whether the page is a Sprint board. */
export function useSprintBoardPage(pageId: string | undefined) {
  useSyncExternalStore(subscribe, () => namesVersion, () => namesVersion);
  return pageId !== undefined && sprintBoardPages.has(pageId);
}

// Each sprint database's sprints by their place, as its task database last
// read them, for the filters' "현재", "다음" and "마지막" read here as the
// server reads them (database_query_relative_sprints.go).
const relativeSprints = new Map<string, ReadonlyMap<string, readonly string[]>>();

/** Keeps a sprint database's sprints by place until the returned call. */
export function keepRelativeSprints(dataSourceId: string, sprints: readonly { id: string; status: string }[]) {
  const byPlace = new Map<string, string[]>();
  for (const { id, status } of sprints) byPlace.set(status, [...byPlace.get(status) ?? [], id]);
  relativeSprints.set(dataSourceId, byPlace);
  namesChanged();
  return () => {
    if (relativeSprints.get(dataSourceId) !== byPlace) return;
    relativeSprints.delete(dataSourceId);
    namesChanged();
  };
}

/** What a task database's "마지막" group goes by before any sprint has ended, as Notion's sprint planning lists it (seen 2026-11-05). */
export const NO_LAST_SPRINT = "sprint:no-last";

/** A sprint database's sprints in that place; undefined while none of its task databases read them here. */
export function relativeSprintIds(dataSourceId: unknown, which: string): string[] | undefined {
  const byPlace = typeof dataSourceId === "string" ? relativeSprints.get(dataSourceId) : undefined;
  return byPlace ? [...byPlace.get(which) ?? []] : undefined;
}

/** Which of the current, next and last sprints a sprint is, if any. */
export function relativeSprintPlace(dataSourceId: unknown, sprintId: string): string | undefined {
  const byPlace = typeof dataSourceId === "string" ? relativeSprints.get(dataSourceId) : undefined;
  for (const [place, ids] of byPlace ?? []) if (ids.includes(sprintId)) return place;
  return undefined;
}

/** Changes as sprints are named and placed here, for views that read them. */
export function useRelativeSprintsVersion() {
  return useSyncExternalStore(subscribe, () => namesVersion, () => namesVersion);
}

