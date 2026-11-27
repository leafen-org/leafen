import { LeafenApiError, request } from "../apiTransport";
import type { Session } from "../types";
import {
  publicMutationErrorCodes,
  publicMutationPaths,
  type PublicMutationAgentGrantScopeInput,
  type PublicMutationAppConnectionInput,
  type PublicMutationAppConnections,
  type PublicMutationInheritedPeople,
  type PublicMutationInheritedPerson,
  type PublicMutationInput,
  type PublicMutationPageRuleInput,
  type PublicMutationPageRules,
  type PublicMutationOperationResponse,
  type PublicMutationResponse,
  type PublicMutationShareInheritanceInput,
  type PublicMutationState,
  type PublicMutationStateResponse,
} from "../publicMutationWire.generated";

type StateWindowListener = (state: PublicMutationState) => void;

// What this tab last heard of each page's sharing, for each person: every
// read and change of it here leaves its answer, and a share window opens with
// it — who has the page and how it is open — in its first frame while the
// server's answer follows, as Notion's does (2026-11-16: on a NAS-class
// network each opening drew neither until the server answered, and grew some
// 90px as they came in). The invite directory is not kept: a summary leaves it
// out, and the window's own read brings it. An answer to a request asked
// before the one kept doesn't replace it (a summary out since the page opened,
// answered after a change made in the window).
const KNOWN_PAGES = 100;
const lastKnown = new Map<string, { state: PublicMutationState; asked: number }>();
let asks = 0;

function knownKey(session: Session, pageId: string) {
  return `${session.user.id}\u0000${pageId}`;
}

function heard(session: Session, pageId: string, asked: number, state: PublicMutationState) {
  const key = knownKey(session, pageId);
  if ((lastKnown.get(key)?.asked ?? 0) > asked) return state;
  lastKnown.delete(key);
  lastKnown.set(key, { state: { ...state, principalOptions: [], principalOptionsNextCursor: null }, asked });
  if (lastKnown.size > KNOWN_PAGES) lastKnown.delete(lastKnown.keys().next().value!);
  return state;
}

function mutationPath(pageId: string): string {
  return publicMutationPaths.state.path.replace("{pageId}", encodeURIComponent(pageId));
}

function operationPath(pageId: string, operationId: string, continuation = false): string {
  const route = continuation
    ? publicMutationPaths.continueOperation.path
    : publicMutationPaths.operation.path;
  return route
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{operationId}", encodeURIComponent(operationId));
}

function throwIfAborted(signal: AbortSignal): void {
  if (signal.aborted) throw new DOMException("Public mutation request aborted.", "AbortError");
}

async function stateWindow(
  session: Session,
  pageId: string,
  permissionCursor: string | null,
  principalCursor: string | null,
  signal: AbortSignal,
): Promise<PublicMutationStateResponse> {
  const params = new URLSearchParams();
  if (permissionCursor) params.set("permissionCursor", permissionCursor);
  if (principalCursor) params.set("principalCursor", principalCursor);
  const query = params.size > 0 ? `?${params.toString()}` : "";
  return request<PublicMutationStateResponse>(
    `${mutationPath(pageId)}${query}`,
    { method: publicMutationPaths.state.method, signal },
    session,
  );
}

function assertContinuation(pageId: string, expectedRevision: number, next: PublicMutationState) {
  if (next.revision !== expectedRevision || next.pageId !== pageId) {
    throw new LeafenApiError(
      publicMutationErrorCodes.revisionConflict,
      409,
      "public-mutation-state-changed",
      "Public mutation state changed during continuation.",
    );
  }
}

async function drainPermissionWindows(
  session: Session,
  pageId: string,
  initial: PublicMutationState,
  signal: AbortSignal,
  onWindow?: StateWindowListener,
): Promise<PublicMutationState> {
  let merged = { ...initial, permissions: [...initial.permissions] };
  onWindow?.(merged);
  let permissionCursor = initial.permissionsNextCursor;
  while (permissionCursor) {
    throwIfAborted(signal);
    const next = await stateWindow(session, pageId, permissionCursor, null, signal);
    assertContinuation(pageId, merged.revision, next.state);
    merged = {
      ...merged,
      permissions: [...merged.permissions, ...next.state.permissions],
      permissionsNextCursor: next.state.permissionsNextCursor,
    };
    permissionCursor = next.state.permissionsNextCursor;
    onWindow?.(merged);
  }
  return { ...merged, permissionsNextCursor: null };
}

async function readState(
  session: Session,
  pageId: string,
  signal: AbortSignal,
  onWindow?: StateWindowListener,
): Promise<PublicMutationStateResponse> {
  for (let attempt = 0; attempt < 2; attempt += 1) {
    throwIfAborted(signal);
    try {
      const asked = ++asks;
      const initial = await stateWindow(session, pageId, null, null, signal);
      return {
        state: heard(session, pageId, asked, await drainPermissionWindows(session, pageId, initial.state, signal, onWindow)),
      };
    } catch (error) {
      if (!(error instanceof LeafenApiError) ||
          error.code !== publicMutationErrorCodes.revisionConflict || attempt === 1) throw error;
    }
  }
  throw new Error("Public mutation state continuation exhausted.");
}

async function mutate(
  session: Session,
  pageId: string,
  input: PublicMutationInput,
  signal: AbortSignal,
  onWindow?: StateWindowListener,
): Promise<PublicMutationResponse> {
  throwIfAborted(signal);
  const expectedRevision = input.expectedRevision, asked = ++asks;
  const response = await request<PublicMutationResponse>(
    mutationPath(pageId),
    {
      method: publicMutationPaths.mutate.method,
      body: JSON.stringify({ ...input, expectedRevision }),
      signal,
    },
    session,
  );
  try {
    return {
      ...response,
      state: heard(session, pageId, asked, await drainPermissionWindows(session, pageId, response.state, signal, onWindow)),
    };
  } catch (error) {
    if (!(error instanceof LeafenApiError) || error.code !== publicMutationErrorCodes.revisionConflict) {
      throw error;
    }
    return { ...response, state: (await readState(session, pageId, signal, onWindow)).state };
  }
}

/** Unlinks a page's sharing from its parent's with one change, or links it again. */
async function shareInheritance(
  session: Session,
  pageId: string,
  input: PublicMutationShareInheritanceInput,
  signal: AbortSignal,
  onWindow?: StateWindowListener,
): Promise<PublicMutationStateResponse> {
  throwIfAborted(signal);
  const asked = ++asks;
  const response = await request<PublicMutationStateResponse>(
    publicMutationPaths.shareInheritance.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: publicMutationPaths.shareInheritance.method, body: JSON.stringify(input), signal },
    session,
  );
  return { state: heard(session, pageId, asked, await drainPermissionWindows(session, pageId, response.state, signal, onWindow)) };
}

/** Who may connect agents to a page: everyone with access, or full access only. */
async function agentGrantScope(
  session: Session,
  pageId: string,
  input: PublicMutationAgentGrantScopeInput,
  signal: AbortSignal,
  onWindow?: StateWindowListener,
): Promise<PublicMutationStateResponse> {
  throwIfAborted(signal);
  const asked = ++asks;
  const response = await request<PublicMutationStateResponse>(
    publicMutationPaths.agentGrantScope.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: publicMutationPaths.agentGrantScope.method, body: JSON.stringify(input), signal },
    session,
  );
  return { state: heard(session, pageId, asked, await drainPermissionWindows(session, pageId, response.state, signal, onWindow)) };
}

/** The apps connected to a page, for those who may edit it, and whether they may change them. */
function appConnections(session: Session, pageId: string, signal: AbortSignal) {
  return request<PublicMutationAppConnections>(
    publicMutationPaths.appConnections.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: publicMutationPaths.appConnections.method, signal },
    session,
  );
}

function changeAppConnection(session: Session, pageId: string, input: PublicMutationAppConnectionInput, signal?: AbortSignal) {
  return request<PublicMutationAppConnections>(
    publicMutationPaths.changeAppConnection.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: publicMutationPaths.changeAppConnection.method, body: JSON.stringify(input), signal },
    session,
  );
}

/** Everyone one page above gives a page, every window of them. */
async function inheritedPeople(session: Session, pageId: string, fromPageId: string, signal?: AbortSignal) {
  const people: PublicMutationInheritedPerson[] = [];
  let cursor = "";
  do {
    const search = new URLSearchParams({ from: fromPageId, ...(cursor ? { cursor } : {}) });
    const read = await request<PublicMutationInheritedPeople>(
      `${publicMutationPaths.inheritedPeople.path.replace("{pageId}", encodeURIComponent(pageId))}?${search}`,
      { method: publicMutationPaths.inheritedPeople.method, signal },
      session,
    );
    people.push(...read.people);
    cursor = read.nextCursor ?? "";
  } while (cursor);
  return people;
}

/** A database page's page-level rules and the properties one may name. */
function pageRules(session: Session, pageId: string, signal: AbortSignal) {
  return request<PublicMutationPageRules>(
    publicMutationPaths.pageRules.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: publicMutationPaths.pageRules.method, signal },
    session,
  );
}

function changePageRule(session: Session, pageId: string, input: PublicMutationPageRuleInput) {
  return request<PublicMutationPageRules>(
    publicMutationPaths.changePageRule.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: publicMutationPaths.changePageRule.method, body: JSON.stringify(input) },
    session,
  );
}

export const publicMutationService = {
  shareInheritance,
  inheritedPeople,
  pageRules,
  changePageRule,
  agentGrantScope,
  appConnections,
  changeAppConnection,
  state: readState,
  /** What was last heard of a page's sharing here, as the share window opens with it (`heard`). */
  lastKnown: (session: Session, pageId: string) => lastKnown.get(knownKey(session, pageId))?.state ?? null,
  // A page's top bar and its database each read the summary as they open;
  // they share the one read out (lib/sameTaskReads.ts). The share button's
  // asks for the people too (`people`), the ones its window opens with.
  summary: async (session: Session, pageId: string, signal: AbortSignal, people = false) => {
    const asked = ++asks;
    const response = await request<PublicMutationStateResponse>(
      `${mutationPath(pageId)}?summary=1${people ? "&people=1" : ""}`,
      { method: publicMutationPaths.state.method, signal },
      session,
      { shareWhileOut: true },
    );
    if (people) heard(session, pageId, asked, response.state);
    return response;
  },
  mutate,
  principalWindow: async (
    session: Session,
    pageId: string,
    principalCursor: string,
    expectedRevision: number,
    signal: AbortSignal,
  ) => {
    const next = await stateWindow(session, pageId, null, principalCursor, signal);
    assertContinuation(pageId, expectedRevision, next.state);
    return next;
  },
  operation: (
    session: Session,
    pageId: string,
    operationId: string,
    signal: AbortSignal,
  ) => request<PublicMutationOperationResponse>(
    operationPath(pageId, operationId),
    { method: publicMutationPaths.operation.method, signal },
    session,
  ),
  continueOperation: (
    session: Session,
    pageId: string,
    operationId: string,
    signal: AbortSignal,
  ) => request<PublicMutationOperationResponse>(
    operationPath(pageId, operationId, true),
    { method: publicMutationPaths.continueOperation.method, signal },
    session,
  ),
};
