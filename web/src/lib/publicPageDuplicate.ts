import { api } from "@/api";
import { request } from "@/apiTransport";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { newId } from "@/lib/ids";
import { pageDuplicationStatuses, type PageDuplicationResponse } from "@/pageDuplicationWire.generated";
import type { Session } from "@/types";

/**
 * A published site's "복제", as Notion's (its help "Duplicate public
 * pages", read 2026-10-12): copies the page the visitor is on, with the
 * pages under it the site shows, into the private pages of a workspace of
 * theirs, and gives the copy's page.
 */
export async function duplicatePublicPage(session: Session, slug: string, pageId: string, workspaceId: string, signal: AbortSignal) {
  const initial = await request<PageDuplicationResponse>(`/public/sites/${encodeURIComponent(slug)}/duplicate`, {
    method: "POST", body: JSON.stringify({ requestId: newId(), pageId, workspaceId }), signal,
  }, session);
  const result = await waitForDurableJob({
    initial,
    isSameJob: (next) => next.jobId === initial.jobId && next.targetRootPageId === initial.targetRootPageId,
    isTerminal: (next) => next.status === pageDuplicationStatuses.completed || next.status === pageDuplicationStatuses.failed,
    onProgress() {},
    read: () => api.readPageDuplication(session, initial.jobId, signal),
    signal,
  });
  if (result.status !== pageDuplicationStatuses.completed) {
    throw Object.assign(new Error("Page duplication did not complete."), { code: result.errorCode || "LEAFEN_PAGE_DUPLICATION_UNAVAILABLE" });
  }
  return result.targetRootPageId;
}
