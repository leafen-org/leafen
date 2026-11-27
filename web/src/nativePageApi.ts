import type { request as apiRequest } from "./apiTransport";
import type { Page, Session } from "./types";
import { workspaceMembershipOperations, type PageCreateInput, type PageCreateResponse } from "./workspaceMembershipWire.generated";
import type { PageMetadataInput, PageMetadataResponse } from "./workspaceSearchWire.generated";

/** Thin page read/create/metadata/body adapters; their native owners decide access. */
export function createNativePageApi(request: typeof apiRequest) {
  const pageCreateOperation = workspaceMembershipOperations.createPage;
  return {
    /** Current page metadata from the native page authority. */
    readPage: (session: Session, pageId: string, signal?: AbortSignal) => request<Page>(
      `/pages/${encodeURIComponent(pageId)}`, { signal }, session,
    ),
    createPage: (
      session: Session,
      input: PageCreateInput,
      signal?: AbortSignal,
    ) => request<PageCreateResponse>(
      pageCreateOperation.path,
      {
        method: pageCreateOperation.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
    updatePageMetadata: (
      session: Session,
      pageId: string,
      input: PageMetadataInput,
      signal?: AbortSignal,
    ) => request<PageMetadataResponse>(
      `/pages/${encodeURIComponent(pageId)}/metadata`,
      { method: "PATCH", body: JSON.stringify(input), signal },
      session,
    ),
    updatePage: (session: Session, pageId: string, content: string, mutationId: string, baseCursor: number, signal?: AbortSignal) => request<{ page: Page; receipt: { mutationId: string; cursor: number; duplicate: boolean } }>(
      `/pages/${encodeURIComponent(pageId)}`,
      { method: "PATCH", body: JSON.stringify({ content, mutationId, baseCursor }), signal },
      session,
    ),
  };
}
