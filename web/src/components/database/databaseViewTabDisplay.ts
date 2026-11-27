import { useCallback, useEffect, useState } from "react";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import type { DatabaseViewTabShows } from "./databaseViewModel";

/**
 * How a database's view tabs show, as Notion's view tab "다음과 같이 표시"
 * (seen 2026-10-11): text and icon, text alone or the icon alone, for the
 * person alone and wherever the database shows. Kept in this browser for the
 * person and data source, as a viewer's own filters are.
 */
export type ViewTabDisplay = DatabaseViewTabShows;
export const VIEW_TAB_DISPLAYS: readonly ViewTabDisplay[] = ["both", "text", "icon"];

const KEY = "leafen:v2:database-view-tab-display";
const listeners = new Set<() => void>();
// This page's choices, for a reader the browser keeps nothing for.
const chosen = new Map<string, ViewTabDisplay>();

function readDisplay(dataSourceId: string): ViewTabDisplay {
  const value = personValue(KEY, { part: dataSourceId }) ?? chosen.get(dataSourceId);
  return value === "text" || value === "icon" ? value : "both";
}

/** The person's tab display for a data source, and how to change it everywhere it shows. */
export function useViewTabDisplay(dataSourceId: string) {
  const [display, setDisplay] = useState(() => readDisplay(dataSourceId));
  useEffect(() => {
    const update = () => setDisplay(readDisplay(dataSourceId));
    update();
    listeners.add(update);
    return () => { listeners.delete(update); };
  }, [dataSourceId]);
  const choose = useCallback((next: ViewTabDisplay) => {
    chosen.set(dataSourceId, next);
    storePersonValue(KEY, next === "both" ? null : next, { part: dataSourceId });
    for (const listener of listeners) listener();
  }, [dataSourceId]);
  return [display, choose] as const;
}
