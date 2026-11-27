import { useEffect, useState } from "react";
import { PUBLIC_CONTENT_CHANGED_EVENT } from "@/components/page/pageShareEvents";
import { publicMutationService } from "./publicMutationService";
import { LeafenApiError } from "@/apiTransport";
import { isServerUnreachable } from "./serverReach";
import { onServerAnswersAgain } from "./serverAnswers";
import { keepRowPageReads, readKeptRowPage } from "./keptRowPage";
import type { Session } from "@/types";

/** How far a page reaches, for its share button: only its reader, people it is shared with, everyone in its workspace, anyone with a web link, or a published site. */
export type PageReach = "private" | "people" | "workspace" | "web" | "site";

// What this tab last heard, for a page come back to while the server is
// silent; and kept with the page's copy here (keptRowPage.ts), for a page
// opened without the server (2026-11-11: offline, a page's top bar lost the
// lock beside "공유" and its "개인 페이지" once the tab was reloaded).
const lastKnown = new Map<string, PageReach>();

/**
 * A page made on this device reaches, until the server says, as far as it
 * was made to: its parent's reach, or only its maker for a page made among
 * their private pages; its top bar shows that at once, "개인 페이지" and the
 * lock beside "공유" with it, as Notion's (2026-11-12: on a stalled network a
 * page made here showed neither until the server could answer). Kept with its
 * copy here, as any page's reach (keptRowPage.ts).
 */
export function notePageMadeHereReach(session: Session, page: { id: string; parentId?: string | null; teamspaceId?: string | null }) {
  const reach = page.parentId ? lastKnown.get(page.parentId) : page.teamspaceId ? undefined : "private";
  if (!reach || lastKnown.has(page.id)) return;
  lastKnown.set(page.id, reach);
  keepRowPageReads(session, page.id, { reach });
}

/**
 * Whether only the reader opens a page — a private page no one else can
 * see — from the page's access summary: undefined until it is known, false
 * when it can't be read (a reader who may not see the audience). A server that
 * doesn't answer says nothing about it, and it is read again once it does.
 * `revision` reads it again (the share window closing).
 */
export function usePageOnlyReader(session: Session | undefined | null, pageId: string | undefined, revision = 0) {
  const reach = usePageReach(session, pageId, revision);
  return reach === undefined ? undefined : reach === "private";
}

/** The page's reach from the same summary, read and kept as usePageOnlyReader says. */
export function usePageReach(session: Session | undefined | null, pageId: string | undefined, revision = 0) {
  const [onlyReader, setOnlyReaderNow] = useState<{ pageId: string; value: PageReach } | null>(null);
  const setOnlyReader = (answer: { pageId: string; value: PageReach }) => { lastKnown.set(answer.pageId, answer.value); setOnlyReaderNow(answer); };
  // The server's answer, kept with the page's copy when it says something new.
  const heard = (session: Session, answer: { pageId: string; value: PageReach }) => {
    if (lastKnown.get(answer.pageId) !== answer.value) keepRowPageReads(session, answer.pageId, { reach: answer.value });
    setOnlyReader(answer);
  };
  // A share or a publish changes the answer at once, with the window still open.
  const [changes, setChanges] = useState(0);
  useEffect(() => {
    const changed = () => setChanges(count => count + 1);
    window.addEventListener(PUBLIC_CONTENT_CHANGED_EVENT, changed);
    const stop = onServerAnswersAgain(changed);
    return () => {
      window.removeEventListener(PUBLIC_CONTENT_CHANGED_EVENT, changed);
      stop();
    };
  }, []);
  useEffect(() => {
    if (!session || !pageId) return undefined;
    const controller = new AbortController();
    let answered = false;
    // Until the server answers, what this device kept.
    if (!lastKnown.has(pageId)) {
      void readKeptRowPage(session, pageId).then((kept) => {
        if (!controller.signal.aborted && !answered && kept?.reach && !lastKnown.has(pageId)) setOnlyReader({ pageId, value: kept.reach });
      });
    }
    // With the people, which its share window opens with (publicMutationService.lastKnown).
    publicMutationService.summary(session, pageId, controller.signal, true).then(
      ({ state }) => {
        if (controller.signal.aborted) return;
        answered = true;
        heard(session, { pageId, value: state.site?.published ? "site" : state.webSharingEnabled || state.inheritedWebLink ? "web"
          : state.workspacePermission || state.workspaceInheritedRole ? "workspace" : state.onlyReaderOpens === true ? "private" : "people" });
      },
      (error: unknown) => {
        if (controller.signal.aborted || isServerUnreachable(error)) return;
        if (error instanceof LeafenApiError && error.status === 429) return;
        answered = true;
        heard(session, { pageId, value: "people" });
      },
    );
    return () => controller.abort();
  }, [session, pageId, revision, changes]);
  return onlyReader && onlyReader.pageId === pageId ? onlyReader.value : pageId ? lastKnown.get(pageId) : undefined;
}
