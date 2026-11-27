import { api } from "@/api";
import { createDurableJobMemory } from "@/lib/durableJobMemory";
import { newId } from "@/lib/ids";
import { tellParentEditedHere } from "@/lib/pageMadeHere";
import type { NativePageCreation } from "@/lib/nativePageLaneEntries";
import type { Page, Session } from "@/types";
import type {
  PageCreateInput,
  PageCreateResponse,
} from "@/workspaceMembershipWire.generated";

type RememberedPageCreationIntent = {
  requestId: string;
};

type PageCreationIntentMemory = {
  clear: (identity: readonly string[], requestId: string) => boolean;
  read: (identity: readonly string[]) => RememberedPageCreationIntent | null;
  remember: (
    identity: readonly string[],
    intent: RememberedPageCreationIntent,
  ) => boolean;
};

type PageCreationTransport = (
  session: Session,
  input: PageCreateInput,
  signal?: AbortSignal,
) => Promise<PageCreateResponse>;

export type PageCreationRequest = {
  parentId?: string;
  signal?: AbortSignal;
  teamspaceId?: string;
  title: string;
  workspaceId: string;
};

type PageCreationServiceDependencies = {
  createRequestId: () => string;
  memory: PageCreationIntentMemory;
  transport: PageCreationTransport;
};

const volatileIntents = new Map<string, RememberedPageCreationIntent>();
const VOLATILE_INTENT_LIMIT = 32;
const durableIntents = createDurableJobMemory<RememberedPageCreationIntent>({
  bounds: {
    maxIdentityCharacters: 12_000,
    maxSerializedCharacters: 1_024,
    maxStorageKeyCharacters: 48_000,
  },
  decode(value) {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    const requestId = Reflect.get(parsed, "requestId");
    return typeof requestId === "string" && requestId.length > 0 && requestId.length <= 255
      ? { requestId }
      : null;
  },
  encode(value) {
    return value.requestId.length > 0 && value.requestId.length <= 255
      ? JSON.stringify(value)
      : null;
  },
  identityPartCount: 6,
  storagePrefix: "leafen:page-create",
});

function intentKey(identity: readonly string[]) {
  return durableIntents.storageKey(identity);
}

const pageCreationIntentMemory: PageCreationIntentMemory = {
  clear(identity, requestId) {
    const key = intentKey(identity);
    const current = this.read(identity);
    if (!key || current?.requestId !== requestId) return false;
    volatileIntents.delete(key);
    durableIntents.clear(identity);
    return true;
  },
  read(identity) {
    const key = intentKey(identity);
    if (!key) return null;
    const durable = durableIntents.read(identity);
    if (durable) {
      volatileIntents.delete(key);
      return durable;
    }
    return volatileIntents.get(key) ?? null;
  },
  remember(identity, intent) {
    const key = intentKey(identity);
    if (!key) return false;
    if (durableIntents.remember(identity, intent)) {
      volatileIntents.delete(key);
      return true;
    }
    volatileIntents.delete(key);
    volatileIntents.set(key, { ...intent });
    while (volatileIntents.size > VOLATILE_INTENT_LIMIT) {
      const oldest = volatileIntents.keys().next().value as string | undefined;
      if (!oldest) break;
      volatileIntents.delete(oldest);
    }
    return true;
  },
};

const defaultTransport: PageCreationTransport = (session, input, signal) => (
  api.createPage(session, input, signal)
);

function pageCreationError(message: string) {
  return Object.assign(new Error(message), {
    code: "LEAFEN_PAGE_CREATE_RESPONSE_INVALID",
  });
}

export function createPageCreationService(
  overrides: Partial<PageCreationServiceDependencies> = {},
) {
  const dependencies: PageCreationServiceDependencies = {
    createRequestId: newId,
    memory: pageCreationIntentMemory,
    transport: defaultTransport,
    ...overrides,
  };
  const active = new Map<string, Promise<Page>>();

  function create(session: Session, input: PageCreationRequest): Promise<Page> {
    const request = {
      parentId: input.parentId?.trim() ?? "",
      signal: input.signal,
      teamspaceId: input.teamspaceId?.trim() ?? "",
      title: input.title,
      workspaceId: input.workspaceId.trim(),
    };
    if (!request.workspaceId) {
      return Promise.reject(pageCreationError("The page workspace is empty."));
    }
    const identity = [
      session.user.id,
      session.sessionId ?? "sessionless",
      request.workspaceId,
      JSON.stringify(request.teamspaceId),
      JSON.stringify(request.parentId),
      JSON.stringify(request.title),
    ];
    const key = JSON.stringify(identity);
    const current = active.get(key);
    if (current) return current;

    const run = async () => {
      const retryIntent = dependencies.memory.read(identity);
      const requestId = retryIntent?.requestId ?? dependencies.createRequestId();
      if (!dependencies.memory.remember(identity, { requestId })) {
        throw pageCreationError("Couldn't keep the page creation retry details.");
      }
      const response = await dependencies.transport(session, {
        parentId: request.parentId,
        requestId,
        teamspaceId: request.teamspaceId || undefined,
        title: request.title,
        workspaceId: request.workspaceId,
      }, request.signal);
      if (
        response.requestId === requestId
        && response.page.workspaceId === request.workspaceId
        && response.page.parentId === request.parentId
        && (!request.teamspaceId || response.page.teamspaceId === request.teamspaceId)
      ) {
        dependencies.memory.clear(identity, requestId);
        return response.page;
      }
      throw pageCreationError("The page creation response has a different scope.");
    };

    let promise: Promise<Page>;
    promise = run().finally(() => {
      if (active.get(key) === promise) active.delete(key);
    });
    active.set(key, promise);
    return promise;
  }

  return { create };
}

const pageCreationService = createPageCreationService();

export function createNativePage(
  session: Session,
  request: PageCreationRequest,
): Promise<Page> {
  return pageCreationService.create(session, request).then((page) => {
    tellParentEditedHere(session, page);
    return page;
  });
}

/**
 * A page made on this device first, as Notion opens a new page at once
 * without the server: at the top of the private pages or a teamspace's, or
 * under a parent page, in its teamspace — the page as it shows until the
 * server has it, last among its siblings as the server puts it, and what the
 * server is asked to make, under the same ID.
 */
export function pageCreationHere(
  session: Session,
  request: { siblings: readonly Page[]; parentId?: string; teamspaceId?: string; title: string; workspaceId: string },
): NativePageCreation {
  const pageId = newId();
  const mutationId = newId();
  const workspaceId = request.workspaceId.trim();
  const parentId = request.parentId?.trim() ?? "";
  const teamspaceId = request.teamspaceId?.trim() ?? "";
  const title = request.title;
  const position = request.siblings.reduce((last, page) => (
    page.workspaceId === workspaceId && (page.parentId || "") === parentId ? Math.max(last, page.position) : last
  ), 0) + 1;
  const now = new Date().toISOString();
  return {
    // A sub-page takes its parent's teamspace on the server.
    input: { pageId, parentId, requestId: mutationId, title, workspaceId, ...(teamspaceId && !parentId ? { teamspaceId } : {}) },
    mutationId,
    page: {
      id: pageId, workspaceId, teamspaceId, title, content: "", revision: 0, cursor: 0, parentId, position,
      isLocked: false, inTrash: false, icon: "", iconType: "", cover: "", coverPosition: 0, font: "default",
      smallText: false, fullWidth: false, backlinksDisplay: "default", pageCommentsDisplay: "default",
      tableOfContentsDisplay: "default", inlineCommentsDisplay: "default", kind: "page",
      createdAt: now, updatedAt: now, createdBy: session.user.id, lastEditedBy: session.user.id,
    },
    pageId,
  };
}

/**
 * Asks the server to make a page made on this device first. Sent again, the
 * request ID gives the page the first try made; one the server made under
 * another ID or elsewhere is refused here, since the changes kept behind it
 * name this one.
 */
export async function sendPageCreationHere(session: Session, creation: NativePageCreation, signal?: AbortSignal) {
  const response = await api.createPage(session, creation.input, signal);
  if (response.requestId !== creation.mutationId || response.page.id !== creation.pageId
    || response.page.workspaceId !== creation.input.workspaceId) {
    // A refusal, not a wait: the lane goes on without it.
    throw Object.assign(pageCreationError("The page creation response has a different scope."), { status: 409 });
  }
  return response;
}
