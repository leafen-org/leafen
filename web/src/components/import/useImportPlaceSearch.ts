import { useEffect, useState } from "react";
import { usePageNames } from "@/lib/pageNames";
import { searchWorkspacePages } from "@/lib/workspacePageSearch";
import { workspaceSearchBounds } from "@/workspaceSearchWire.generated";
import type { Page } from "@/types";
import type { ImportPlaceDatabaseSource } from "./useImportPlaceDatabases";

const SEARCH_PAUSE_MS = 150;

/**
 * The pages a search in an import window's "위치 가져오기" finds, as Notion's
 * (seen 2026-11-19 in two Chrome tabs, "Qmv" with 46 sub-pages so named):
 * first the pages the reader visited last whose names hold it, newest first,
 * the open one too (Qmv 04, 03, 02 visited in turn led), then `limit` the
 * workspace's search finds by their names or by what is in them — a page
 * holding the sub-pages so named among them —, each once; with none of them
 * visited, those five alone. `visited` is the reader's visits, newest first,
 * and `listed` which pages the list may offer. While a search is read the
 * pages visited show, the found joining once it answers.
 */
export function useImportPlaceSearch(reader: ImportPlaceDatabaseSource, query: string, visited: readonly Page[],
  listed: (page: Page | undefined) => page is Page, limit: number): Page[] {
  const pageNames = usePageNames();
  const needle = query.trim().toLocaleLowerCase();
  const recent = needle ? visited.filter((page) => pageNames.title(page).toLocaleLowerCase().includes(needle)).slice(0, limit) : [];
  const [answer, setAnswer] = useState<{ query: string; pages: readonly Page[] }>({ query: "", pages: [] });
  const { session, workspaceId } = reader;
  const asked = Math.min(workspaceSearchBounds.results, limit * 2);
  useEffect(() => {
    const trimmed = query.trim();
    if (!trimmed || !workspaceId) return;
    const read = new AbortController();
    const timer = window.setTimeout(() => {
      searchWorkspacePages(session, trimmed, asked, workspaceId, { signal: read.signal, places: true })
        .then((result) => { if (!read.signal.aborted) setAnswer({ query: trimmed, pages: result.pages }); })
        .catch(() => undefined);
    }, SEARCH_PAUSE_MS);
    return () => {
      window.clearTimeout(timer);
      read.abort();
    };
  }, [asked, query, session, workspaceId]);
  if (!needle) return [];
  const shown = new Set(recent.map(({ id }) => id));
  const found = answer.query === query.trim() ? answer.pages.filter((page) => listed(page) && !shown.has(page.id)).slice(0, limit) : [];
  return [...recent, ...found];
}
