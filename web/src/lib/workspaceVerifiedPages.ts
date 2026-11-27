import { request } from "@/apiTransport";
import { readEveryWindow } from "@/lib/listWindows";
import type { TextSpan } from "@/lib/types";
import type { Session } from "@/types";

export type VerifiedPageRef = { id: string; name: string };

/** A verified page, or a database verified on its page, the reader can open. */
export type VerifiedPageItem = {
  pageId: string;
  /** A database's block on its page. */
  blockId?: string;
  title: string;
  /** A page's title's spans as its reader reads them, while it holds a mention (verified_pages_list.go). */
  titleSpans?: TextSpan[];
  owners: VerifiedPageRef[];
  /** Null outside a teamspace the reader can see. */
  teamspace: VerifiedPageRef | null;
  verifiedBy: VerifiedPageRef;
  verifiedAt: string;
  /** Empty when verified indefinitely. */
  expiresAt: string;
  /** The pages above it, nearest first. */
  ancestorIds?: string[];
};

export type WorkspaceVerifiedPages = { items: VerifiedPageItem[]; hasMore: boolean; nextCursor?: string };

/** Every verified page the reader can open, newest first, a window at a time. */
export async function listWorkspaceVerifiedPages(session: Session, workspaceId: string, signal?: AbortSignal): Promise<WorkspaceVerifiedPages> {
  const address = `/workspaces/${encodeURIComponent(workspaceId)}/verified-pages`;
  const items = await readEveryWindow(async (cursor) => {
    const page = await request<WorkspaceVerifiedPages>(cursor ? `${address}?cursor=${encodeURIComponent(cursor)}` : address, { signal }, session);
    return { items: page.items, nextCursor: page.nextCursor };
  }, signal);
  return { items, hasMore: false };
}
