import { LeafenApiError, request } from "@/apiTransport";
import {
  pageBacklinksBounds,
  pageBacklinksErrorCodes,
  pageBacklinksOperations,
  type PageBacklinksBlock,
  type PageBacklinksResponse,
} from "@/pageBacklinksWire.generated";
import {
  lastPageBacklinks,
  listPageBacklinks,
  type BacklinkBounds,
} from "@/lib/backlinks";
import type { Block, Page as RetainedPage } from "@/lib/types";
import type { Page, Session } from "@/types";

export type NativeBacklinkPage = RetainedPage & Page;

const nativePageBacklinkBounds: BacklinkBounds = {
  maxInflight: 8,
  maxBlocks: pageBacklinksBounds.pageSize,
};

function retainedPage(page: Page): NativeBacklinkPage {
  return {
    ...page,
    parentType: page.parentId ? "page" : "workspace",
    kind: "page",
    iconType: page.iconType === "emoji" || page.iconType === "image" || page.iconType === "custom_emoji" || page.iconType === "icon"
      ? page.iconType
      : "none",
  };
}

// A database page's body keeps the row it is, so its backlink opens it as
// its item on the page that holds it.
function retainedBlock(block: PageBacklinksBlock, position: number): Block & Pick<PageBacklinksBlock, "sourceRow"> {
  return {
    id: block.id,
    pageId: block.pageId,
    parentId: block.parentId || null,
    position,
    type: block.type as Block["type"],
    content: block.content as Block["content"],
    createdAt: new Date(block.createdAt).toISOString(),
    updatedAt: new Date(block.updatedAt).toISOString(),
    ...(block.sourceRow ? { sourceRow: block.sourceRow } : {}),
  };
}

function mapResponse(response: PageBacklinksResponse) {
  return {
    blocks: response.blocks.map(retainedBlock),
    pages: response.pages.map(retainedPage),
    privateSources: response.privateSources ?? 0,
    hasPrevious: response.hasPrevious,
    previousCursor: response.previousCursor,
    hasMore: response.hasMore,
    nextCursor: response.nextCursor,
    snapshotRevision: response.snapshotRevision,
  };
}

async function requestBacklinks(
  session: Session,
  pageId: string,
  limit: number,
  cursor: string | undefined,
  signal: AbortSignal,
) {
  const parameters = new URLSearchParams({ limit: String(limit) });
  if (cursor) parameters.set("cursor", cursor);
  const path = `${pageBacklinksOperations.read.path.replace("{pageId}", encodeURIComponent(pageId))}?${parameters}`;
  return request<PageBacklinksResponse>(path, { signal }, session);
}

export function createPageBacklinksService(session: Session) {
  const requestScope = session.sessionId ?? session.user.id;
  const list = async function listNativePageBacklinks(
    pageId: string,
    limit: number,
    workspaceId: string,
    options: { sourceCursor?: string; signal: AbortSignal },
  ) {
    let restarted = false;
    const remote = async (_target: string, boundedLimit: number, expectedWorkspace: string) => {
      let response: PageBacklinksResponse;
      try {
        response = await requestBacklinks(
          session, pageId, boundedLimit, options.sourceCursor, options.signal,
        );
      } catch (error) {
        if (
          !options.sourceCursor
          || !(error instanceof LeafenApiError)
          || error.code !== pageBacklinksErrorCodes.revisionChanged
        ) throw error;
        restarted = true;
        response = await requestBacklinks(session, pageId, boundedLimit, undefined, options.signal);
      }
      if (response.workspaceId !== expectedWorkspace || response.targetPageId !== pageId) {
        throw new Error("Backlinks response authority did not match the current page.");
      }
      return mapResponse(response);
    };
    const result = await listPageBacklinks(
      pageId,
      limit,
      workspaceId,
      remote,
      nativePageBacklinkBounds,
      { requestScope, ...(options.sourceCursor ? { sourceCursor: options.sourceCursor } : {}) },
    );
    const window = result as ReturnType<typeof mapResponse>;
    return { ...window, restarted };
  };
  /** The first window last read of a page this session asked for, if any. */
  const last = (pageId: string, limit: number, workspaceId: string) => {
    const window = lastPageBacklinks(requestScope, workspaceId, pageId, limit) as ReturnType<typeof mapResponse> | undefined;
    return window ? { ...window, restarted: false } : undefined;
  };
  return Object.assign(list, { last });
}

export { retainedPage as retainedPageForBacklinks };
