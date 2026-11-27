import { api, workspaceSearchBounds, workspaceSearchErrorCodes } from "@/api";
import type { WorkspaceSearchContentState } from "@/lib/pageArchive";
import type { Session } from "@/types";
import { request } from "@/apiTransport";
import type {
  WorkspaceSearchBlockResponse, WorkspaceSearchEventInput, WorkspaceSearchResponse,
} from "@/workspaceSearchWire.generated";

type WorkspaceSearchTransportOptions = {
  cursor?: string | null;
  indexCursor?: string | null;
  limit?: number;
  contentState?: WorkspaceSearchContentState;
  filterParams?: Record<string, string>;
  signal?: AbortSignal;
};

export type WorkspaceSearchTransport = (
  session: Session,
  workspaceId: string,
  query: string,
  options?: WorkspaceSearchTransportOptions,
) => Promise<WorkspaceSearchResponse>;

export const apiWorkspaceSearchTransport: WorkspaceSearchTransport = (
  session,
  workspaceId,
  query,
  options,
) => api.searchWorkspace(session, workspaceId, query, options);

const cursorRefused = (cause: unknown) =>
  typeof cause === "object" && cause !== null && "code" in cause && cause.code === workspaceSearchErrorCodes.cursorInvalid;

export function createWorkspaceSearchService(transport: WorkspaceSearchTransport) {
  async function drainSearchIndex(
    session: Session,
    workspaceId: string,
    query: string,
    initialOptions: WorkspaceSearchTransportOptions,
  ) {
    let requestOptions = initialOptions;
    const seenIndexCursors = new Set<string>();
    for (;;) {
      const result = await transport(session, workspaceId, query, requestOptions);
      if (!result.indexing) return result;
      if (!result.indexCursor || seenIndexCursors.has(result.indexCursor)) {
        throw new Error("The search index position repeated.");
      }
      seenIndexCursors.add(result.indexCursor);
      requestOptions = {
        indexCursor: result.indexCursor,
        limit: initialOptions.limit ?? workspaceSearchBounds.defaultResults,
        ...(initialOptions.contentState ? { contentState: initialOptions.contentState } : {}),
        ...(initialOptions.filterParams ? { filterParams: initialOptions.filterParams } : {}),
        ...(initialOptions.signal ? { signal: initialOptions.signal } : {}),
      };
    }
  }

  async function searchAfterProjection(
    session: Session,
    workspaceId: string,
    query: string,
    options: {
      limit?: number; contentState?: WorkspaceSearchContentState; filterParams?: Record<string, string>; signal?: AbortSignal;
    } = {},
  ) {
    const first = {
      indexCursor: null,
      limit: options.limit ?? workspaceSearchBounds.defaultResults,
      ...(options.contentState ? { contentState: options.contentState } : {}),
      ...(options.filterParams ? { filterParams: options.filterParams } : {}),
      ...(options.signal ? { signal: options.signal } : {}),
    };
    try {
      return await drainSearchIndex(session, workspaceId, query, first);
    } catch (cause) {
      // An index position the server no longer takes starts the search over, once.
      if (!cursorRefused(cause) || options.signal?.aborted) throw cause;
      return drainSearchIndex(session, workspaceId, query, first);
    }
  }

  async function continueSearch(
    session: Session,
    workspaceId: string,
    query: string,
    options: {
      cursor: string; limit?: number; contentState?: WorkspaceSearchContentState;
      filterParams?: Record<string, string>; signal?: AbortSignal;
    },
  ) {
    try {
      return await drainSearchIndex(session, workspaceId, query, options);
    } catch (cause) {
      if (!cursorRefused(cause)) throw cause;
      const restarted = await searchAfterProjection(session, workspaceId, query, {
        limit: options.limit,
        contentState: options.contentState,
        filterParams: options.filterParams,
        signal: options.signal,
      });
      return { ...restarted, restarted: true as const };
    }
  }

  return { continueSearch, searchAfterProjection };
}

const workspaceSearchService = createWorkspaceSearchService(apiWorkspaceSearchTransport);

export const continueWorkspaceSearch = workspaceSearchService.continueSearch;
export const searchWorkspaceAfterProjection = workspaceSearchService.searchAfterProjection;

/**
 * Tells the workspace that a search finished, for its owner's search
 * analytics: the query, and whether it opened a result.
 */
export function recordWorkspaceSearch(session: Session, workspaceId: string, input: WorkspaceSearchEventInput) {
  return request<null>(`/workspaces/${encodeURIComponent(workspaceId)}/search/events`, {
    method: "POST",
    body: JSON.stringify(input),
  }, session);
}

/**
 * The block of a page found by its body whose passage for the search's
 * keywords is the line its result showed, for the result to open at it;
 * "" when none is (it opens at its top). Asked when the result is opened, so
 * a search's answer doesn't read every result's page for it.
 */
export async function findWorkspaceSearchBlock(session: Session, pageId: string, query: string, line: string) {
  const params = new URLSearchParams({ q: query, line });
  const answer = await request<WorkspaceSearchBlockResponse>(
    `/pages/${encodeURIComponent(pageId)}/search-block?${params.toString()}`, {}, session,
  );
  return answer.blockId ?? "";
}
