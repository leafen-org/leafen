import { useCallback, useState } from "react";
import type { WorkspaceSearchContentState } from "@/lib/pageArchive";
import {
  EMPTY_SEARCH_FILTERS, SEARCH_SORTS,
  type SearchFilters,
} from "./searchFilterModel";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { serverScopedName } from "@/lib/serverOrigin";

// Notion's "검색 설정" switch "세션 전반에 걸쳐 필터 유지" (seen 2026-09-30,
// off at first): off, each search starts with no filters; on, the filters of
// the last search come back, on this device, after a reload too.

const KEEP_KEY = "leafen:search:keep-filters";
const FILTERS_KEY = "leafen:search:kept-filters";
const CONTENT_STATES: readonly WorkspaceSearchContentState[] = ["active", "archived", "all", "verified"];

export type KeptSearchFilters = { filters: SearchFilters; contentState: WorkspaceSearchContentState };

const NONE: KeptSearchFilters = { filters: EMPTY_SEARCH_FILTERS, contentState: "active" };

// The switch and the filters are the person's, as the search settings are.
function readStored(key: string): unknown {
  return parseStored(personValue(key, { previous: [serverScopedName(key)] }));
}

function store(key: string, value: unknown) {
  storePersonValue(key, value === null ? null : JSON.stringify(value));
}

const strings = (value: unknown) => Array.isArray(value)
  ? value.filter((item): item is string => typeof item === "string") : [];

/** Stored filters as the search reads them; anything else is no filter. */
function keptSearchFilters(value: unknown): KeptSearchFilters {
  if (!value || typeof value !== "object") return NONE;
  const stored = value as { filters?: Partial<SearchFilters>; contentState?: unknown };
  const filters = stored.filters ?? {};
  const date = filters.date && typeof filters.date === "object" ? filters.date : EMPTY_SEARCH_FILTERS.date;
  return {
    filters: {
      titleOnly: filters.titleOnly === true,
      createdBy: Array.isArray(filters.createdBy)
        ? filters.createdBy.filter((person) => typeof person?.userId === "string" && typeof person?.label === "string")
        : [],
      teamspaces: strings(filters.teamspaces),
      inPages: strings(filters.inPages),
      date: ["any", "today", "week", "month", "custom"].includes(date.preset)
        ? { preset: date.preset, from: typeof date.from === "string" ? date.from : undefined, to: typeof date.to === "string" ? date.to : undefined,
          ...(date.field === "created" || date.field === "edited" ? { field: date.field } : {}) }
        : EMPTY_SEARCH_FILTERS.date,
      sort: SEARCH_SORTS.includes(filters.sort as SearchFilters["sort"]) ? filters.sort as SearchFilters["sort"] : "best",
    },
    contentState: CONTENT_STATES.includes(stored.contentState as WorkspaceSearchContentState)
      ? stored.contentState as WorkspaceSearchContentState : "active",
  };
}

export function useSearchKeptFilters() {
  const [keep, setKeepState] = useState(() => readStored(KEEP_KEY) === true);
  const [initial] = useState<KeptSearchFilters>(() => keep ? keptSearchFilters(readStored(FILTERS_KEY)) : NONE);
  const setKeep = useCallback((next: boolean) => {
    setKeepState(next);
    store(KEEP_KEY, next ? true : null);
    if (!next) store(FILTERS_KEY, null);
  }, []);
  /** The filters the next search starts with, once this one closes. */
  const onClose = useCallback((current: KeptSearchFilters): KeptSearchFilters => {
    if (!keep) return NONE;
    store(FILTERS_KEY, current);
    return current;
  }, [keep]);
  return { keep, setKeep, initial, onClose };
}
