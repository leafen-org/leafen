import { waitForDurableJob } from "@/lib/durableJobPolling";
import { newId } from "@/lib/ids";
import { pageWritesSent } from "@/lib/pageKindChange";
import {
  wikiMutationOperations,
  wikiMutationStatuses,
  wikiOperations,
  type PageVerification,
  type PageVerificationInput,
  type WikiDatabaseNames,
  type WikiMutationInput,
  type WikiMutationOperation,
  type WikiMutationResponse,
  type WikiPageSummary,
} from "./wikiWire.generated";
import type { Session } from "./types";

type Request = <T>(path: string, init: RequestInit, session: Session) => Promise<T>;

export type WikiMutationOutcome = {
  isWiki: boolean;
  page: WikiPageSummary;
  wikiRootId?: string;
};

function operationPath(template: string, key: string, value: string) {
  return template.replace(`{${key}}`, encodeURIComponent(value));
}

function invalidResponse(kind: string): never {
  throw Object.assign(new Error(`Invalid ${kind} response.`), {
    code: "LEAFEN_WIKI_RESPONSE_INVALID",
  });
}

export function createWikiApi(request: Request) {
  return {
    startWikiMutation(session: Session, pageId: string, input: WikiMutationInput, signal: AbortSignal) {
      return request<WikiMutationResponse>(
        operationPath(wikiOperations.startWikiMutation.path, "pageId", pageId),
        { method: wikiOperations.startWikiMutation.method, body: JSON.stringify(input), signal },
        session,
      );
    },
    readWikiMutation(session: Session, jobId: string, signal: AbortSignal) {
      return request<WikiMutationResponse>(
        operationPath(wikiOperations.readWikiMutation.path, "jobId", jobId),
        { method: wikiOperations.readWikiMutation.method, signal },
        session,
      );
    },
    /** A page's verification and its owners. */
    readPageVerification(session: Session, pageId: string, signal?: AbortSignal) {
      return request<PageVerification>(
        operationPath(wikiOperations.readPageVerification.path, "pageId", pageId),
        { method: wikiOperations.readPageVerification.method, signal },
        session,
      );
    },
    /** Verifies a page outside a wiki, takes its verification away or names its owners. */
    writePageVerification(session: Session, pageId: string, input: PageVerificationInput) {
      return request<PageVerification>(
        operationPath(wikiOperations.writePageVerification.path, "pageId", pageId),
        { method: wikiOperations.writePageVerification.method, body: JSON.stringify(input) },
        session,
      );
    },
    /** A database's verification and its owners, by its block on the page. */
    readDatabaseVerification(session: Session, pageId: string, blockId: string, signal?: AbortSignal) {
      return request<PageVerification>(
        operationPath(operationPath(wikiOperations.readDatabaseVerification.path, "pageId", pageId), "blockId", blockId),
        { method: wikiOperations.readDatabaseVerification.method, signal },
        session,
      );
    },
    /** Verifies a database opened as its own page, as a page is. */
    writeDatabaseVerification(session: Session, pageId: string, blockId: string, input: PageVerificationInput) {
      return request<PageVerification>(
        operationPath(operationPath(wikiOperations.writeDatabaseVerification.path, "pageId", pageId), "blockId", blockId),
        { method: wikiOperations.writeDatabaseVerification.method, body: JSON.stringify(input) },
        session,
      );
    },
  };
}

export type WikiApi = ReturnType<typeof createWikiApi>;

function validateMutationResult(
  response: WikiMutationResponse,
  operation: WikiMutationOperation,
  rootId: string,
  source: WikiPageSummary,
): WikiMutationOutcome {
  if (response.status === wikiMutationStatuses.failed) {
    throw Object.assign(new Error(response.errorCode || "LEAFEN_WIKI_UNAVAILABLE"), {
      code: response.errorCode || "LEAFEN_WIKI_UNAVAILABLE",
    });
  }
  const result = response.result;
  if (response.status !== wikiMutationStatuses.completed
    || response.operation !== operation
    || response.rootPageId !== rootId
    || !result
    || result.schemaVersion !== 1
    || result.workspaceId !== source.workspaceId
    || result.pageId !== source.id
    || result.wikiRootId !== rootId
    || !Number.isSafeInteger(result.revision)
    || result.revision !== source.wikiRevision + 1) {
    invalidResponse("wiki mutation");
  }
  return {
    isWiki: result.isWiki,
    page: {
      ...source,
      wikiRevision: result.revision,
      verifiedAt: result.verifiedAt,
      verifiedBy: result.verifiedBy,
      verificationExpiresAt: result.verificationExpiresAt,
    },
    wikiRootId: operation === wikiMutationOperations.undo ? undefined : result.wikiRootId,
  };
}

/**
 * Turns a page into a wiki, or a wiki back into a page, through the durable
 * job, once the page writes made before it have gone (lib/pageKindChange.ts).
 */
export function createWikiService(
  api: WikiApi,
  session: Session,
  rootId: string,
  createRequestId: () => string = newId,
) {
  async function mutate(
    source: WikiPageSummary,
    operation: WikiMutationOperation,
    signal: AbortSignal,
    names?: WikiDatabaseNames,
  ) {
    const input: WikiMutationInput = {
      requestId: createRequestId(),
      operation,
      expectedRevision: source.wikiRevision,
      ...(names ? { names } : {}),
    };
    // The page's title typed just before goes first: the job holds the page, and the wiki is named as it is then.
    await pageWritesSent();
    signal.throwIfAborted();
    const initial = await api.startWikiMutation(session, source.id, input, signal);
    if (initial.rootPageId !== rootId || initial.operation !== operation) invalidResponse("wiki mutation");
    const settled = await waitForDurableJob({
      initial,
      signal,
      isSameJob: (next) => next.jobId === initial.jobId
        && next.rootPageId === rootId
        && next.operation === operation,
      isTerminal: (next) => next.status !== wikiMutationStatuses.pending,
      onProgress: () => {},
      read: () => api.readWikiMutation(session, initial.jobId, signal),
    });
    return validateMutationResult(settled, operation, rootId, source);
  }

  return {
    /** Makes the page a wiki whose database takes the names given. */
    convert(source: WikiPageSummary, signal: AbortSignal, names?: WikiDatabaseNames) {
      return mutate(source, wikiMutationOperations.convert, signal, names);
    },
    undo(source: WikiPageSummary, signal: AbortSignal) {
      return mutate(source, wikiMutationOperations.undo, signal);
    },
  };
}
export type { WikiPageSummary };
