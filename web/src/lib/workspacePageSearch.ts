import type { InlineDatabaseName } from "@/lib/pagePath";
import { isPeopleDatabasePage } from "@/lib/peopleDatabasePage";
import type { Page, Session } from "@/types";
import {
  workspaceSearchBounds,
  workspaceSearchErrorCodes,
  type WorkspaceSearchResponse,
} from "@/workspaceSearchWire.generated";
import {
  continueWorkspaceSearch,
  searchWorkspaceAfterProjection,
} from "@/lib/workspaceSearchService";

export interface PageSearchWindowResult {
  pages: Page[];
  /** The database pages found, by page: where each sits, for its path. */
  databasePages?: Record<string, { containerPageId: string } & InlineDatabaseName>;
  hasMore: boolean;
  nextCursor?: string;
}

export interface SearchPagesRemoteOptions {
  sourceCursor?: string;
  signal?: AbortSignal;
  /**
   * Pages for a place to put something in — pages, blocks or rows moved,
   * files imported: People's database page and its people's pages are none,
   * as Notion's "옮기기" and "위치 가져오기" find neither "People" nor a
   * person by name (seen 2026-11-19 in Chrome at 1440, two tabs); Leafen
   * adds no rows or pages there (database_added_rows.go).
   */
  places?: boolean;
  /**
   * Pages by their titles alone (the server's titleOnly), as a picker of a
   * page to link finds them: not by their bodies — a child page's title in
   * its parent's body included — nor by the pages above them (seen
   * 2026-11-25 in Notion's link window, two tabs: "qvchildw" listed the page
   * so named, not its parent; "qvparentw" that page, not the pages under it;
   * a word of a page's body nothing).
   */
  titles?: boolean;
}

type InitialPageSearch = (
  session: Session,
  workspaceId: string,
  query: string,
  options: { limit?: number; filterParams?: Record<string, string>; signal?: AbortSignal },
) => Promise<WorkspaceSearchResponse>;

type ContinuedPageSearch = (
  session: Session,
  workspaceId: string,
  query: string,
  options: { cursor: string; limit?: number; filterParams?: Record<string, string>; signal?: AbortSignal },
) => Promise<WorkspaceSearchResponse>;

class WorkspacePageSearchError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "WorkspacePageSearchError";
  }
}

function protocolError(code: string, status: number, message: string) {
  return new WorkspacePageSearchError(code, status, message);
}

function cursorBytes(value: string) {
  return new TextEncoder().encode(value).byteLength;
}

function validatedWindow(
  result: WorkspaceSearchResponse,
  workspaceId: string,
  limit: number,
  sourceCursor?: string,
  places = false,
): PageSearchWindowResult {
  if (result.indexing !== false || !Array.isArray(result.results) || result.results.length > limit) {
    throw protocolError(
      workspaceSearchErrorCodes.failed,
      502,
      "The page search source did not return a completed result window.",
    );
  }

  const pages: Page[] = [];
  const databasePages: NonNullable<PageSearchWindowResult["databasePages"]> = {};
  const seen = new Set<string>();
  for (const entry of result.results) {
    const page = entry?.page;
    if (
      !page
      || typeof page.id !== "string"
      || page.id.length === 0
      || page.workspaceId !== workspaceId
      || seen.has(page.id)
    ) {
      throw protocolError(
        workspaceSearchErrorCodes.failed,
        502,
        "The page search source returned an invalid page.",
      );
    }
    seen.add(page.id);
    if (places && (isPeopleDatabasePage(page) || entry.row?.person)) continue;
    pages.push(page);
    if (entry.row) {
      databasePages[page.id] = {
        containerPageId: entry.row.containerPageId, inlineDatabase: entry.row.inlineDatabase, databaseTitle: entry.row.databaseTitle,
      };
    }
  }

  if (typeof result.hasMore !== "boolean") {
    throw protocolError(workspaceSearchErrorCodes.failed, 502, "The page search completion state is missing.");
  }
  if (
    result.hasMore
    && (
      typeof result.nextCursor !== "string"
      || result.nextCursor.length === 0
      || result.nextCursor === sourceCursor
      || cursorBytes(result.nextCursor) > workspaceSearchBounds.cursorBytes
    )
  ) {
    throw protocolError(
      workspaceSearchErrorCodes.failed,
      502,
      "The page search source did not return a position to continue from.",
    );
  }
  if (!result.hasMore && result.nextCursor !== null) {
    throw protocolError(
      workspaceSearchErrorCodes.failed,
      502,
      "The page search source returned a position after completing.",
    );
  }

  return {
    pages,
    databasePages,
    hasMore: result.hasMore,
    ...(result.hasMore ? { nextCursor: result.nextCursor as string } : {}),
  };
}

export function createWorkspacePageSearchService(dependencies: {
  initial: InitialPageSearch;
  continuation: ContinuedPageSearch;
}) {
  async function searchPages(
    session: Session,
    query: string,
    limit: number,
    workspaceId: string,
    options: SearchPagesRemoteOptions = {},
  ): Promise<PageSearchWindowResult> {
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > workspaceSearchBounds.results) {
      throw protocolError(
        workspaceSearchErrorCodes.invalid,
        400,
        `The page search limit must be an integer from 1 to ${workspaceSearchBounds.results}.`,
      );
    }
    if (!workspaceId.trim()) {
      throw protocolError(workspaceSearchErrorCodes.invalid, 400, "A workspace ID is required.");
    }
    if (
      options.sourceCursor !== undefined
      && (
        options.sourceCursor.length === 0
        || cursorBytes(options.sourceCursor) > workspaceSearchBounds.cursorBytes
      )
    ) {
      throw protocolError(
        workspaceSearchErrorCodes.cursorInvalid,
        400,
        "The page search position is invalid.",
      );
    }

    const scope = {
      ...(options.titles ? { filterParams: { titleOnly: "true" } } : {}),
      ...(options.signal ? { signal: options.signal } : {}),
    };
    const result = options.sourceCursor === undefined
      ? await dependencies.initial(session, workspaceId, query, { limit, ...scope })
      : await dependencies.continuation(session, workspaceId, query, { cursor: options.sourceCursor, limit, ...scope });
    return validatedWindow(result, workspaceId, limit, options.sourceCursor, options.places);
  }

  return { searchPages };
}

const workspacePageSearchService = createWorkspacePageSearchService({
  initial: searchWorkspaceAfterProjection,
  continuation: continueWorkspaceSearch,
});

export const searchWorkspacePages = workspacePageSearchService.searchPages;
