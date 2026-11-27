import { LeafenApiError, request } from "@/apiTransport";
import { readAndFollow } from "@/lib/followAcrossTabs";
import { pageBusyCopyingDatabases } from "@/lib/pageBusy";
import { pageAccessTopic, setPageFilling } from "@/lib/pageCopyProgress";
import type { Session } from "@/types";
import type { PageActionAccess } from "@/workspaceMembershipWire.generated";

// A copy of a database page's body takes the body's own databases as copies
// of them, after the rest, by a job on the server (database_row_page_database_
// copies.go): a row's duplicate, a template applied to a row or made into
// one. Until the job ends the page says "copyingDatabases": its body shows as
// being filled (usePageBodyEditing), its title editable, and past the moment
// one expects "페이지 복제 중 | 무시하기" says so (PageCopyNotice), as Notion's
// does (seen 2026-11-14: a row made from a template showed its body's
// skeleton until its database's copy came, the notice from 3.2 seconds and
// the body at 5.4; a row's duplicate holding a database said the same while
// slow). The tab that asked follows the page from the server's answer, which
// says whether the copy goes on: one look a second for all of the person's
// tabs (followAcrossTabs) until it ends.

const CHECK_MS = 1_000;
const following = new Map<string, () => void>();

/**
 * Follows the copy of a page's own databases a copy asked for here begins:
 * `copying` as the server answered it, or, when it said nothing of it, as
 * the page's access says at once.
 */
export function followPageDatabaseCopies(session: Session, pageId: string, copying?: boolean): void {
  if (following.has(pageId)) return;
  // One shown as being filled from the press (fillRowFromTemplateAhead) is not, the server said.
  if (copying === false) {
    setPageFilling(pageId, false);
    return;
  }
  if (copying) setPageFilling(pageId, true);
  let ended = false;
  const end = () => {
    if (ended) return;
    ended = true;
    following.get(pageId)?.();
    following.delete(pageId);
    setPageFilling(pageId, false);
  };
  const read = () => request<PageActionAccess>(`/pages/${encodeURIComponent(pageId)}?accessOnly=1`, {}, session)
    .catch((error: unknown) => {
      // A page gone or out of reach for good copies nothing more here.
      if (error instanceof LeafenApiError && (error.status === 404 || error.status === 403)) end();
      throw error;
    });
  const stop = readAndFollow(pageAccessTopic(session.user.id, pageId), CHECK_MS, true, read, (value) => {
    const access = value as PageActionAccess | undefined;
    if (!access || ended) return;
    if (access.busy === pageBusyCopyingDatabases) setPageFilling(pageId, true);
    else end();
  });
  if (ended) stop();
  else following.set(pageId, stop);
}
