import { api } from "@/api";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { newId } from "@/lib/ids";
import { askPageCopy, endPageCopy, setPageCopyProgress } from "@/lib/pageCopyProgress";
import { pageDuplicationErrorCodes, pageDuplicationStatuses, type PageDuplicationResponse } from "@/pageDuplicationWire.generated";
import type { Page, Session } from "@/types";
import { pageTextStored } from "@/lib/pageTextStored";

// A copy goes beside its page, or into the page parentId names ("" for the
// top of the page's place), in another workspace when workspaceId names one,
// at the top of the teamspace teamspaceId names there, or at the top of the
// person's private pages with its own name (a wiki page's "개인 페이지로
// 복제", as Notion's); a database may be copied without its rows.
export type PageDuplicationOptions = {
  /** Called with the copy's page as the server has taken the copy on, and what settles when it ends. */
  onStarted?: (targetPageId: string, finished: Promise<unknown>) => void;
  withoutContent?: boolean; parentId?: string; workspaceId?: string; teamspaceId?: string; toPrivatePages?: boolean;
};

// pageDuplicationDestinationLocked tells whether a duplication failed because
// the page the copy would go in is locked.
export function pageDuplicationDestinationLocked(error: unknown) {
  return (error as { code?: unknown } | null)?.code === pageDuplicationErrorCodes.destinationLocked;
}

/** What a refused duplicate tells the person, by why it was refused. */
export function pageDuplicationRefusal(error: unknown) {
  const code = (error as { code?: unknown } | null)?.code;
  if (code === pageDuplicationErrorCodes.destinationLocked) return "rowMenu:toast.cantDuplicateIntoLockedPage";
  return code === pageDuplicationErrorCodes.conflict ? "rowMenu:toast.alreadyDuplicatingPage" : "rowMenu:toast.couldntDuplicatePage";
}

/**
 * A copy's name, as the server names a copy in its workspace
 * (duplicate_copy_title.go): "Name" becomes "Name (1)", "Name (1)" becomes
 * "Name (2)" and "(5)" "(6)" — a count from 1 closing the name after a space
 * or alone; "Name(1)" or "Name (05)" becomes "… (1)"; an untitled page's copy
 * stays untitled.
 */
export function pageCopyTitle(title: string) {
  if (!title) return "";
  const numbered = /(?:^| )\(([1-9][0-9]*)\)$/.exec(title);
  if (!numbered) return `${title} (1)`;
  return `${title.slice(0, title.length - numbered[1].length - 2)}(${(BigInt(numbered[1]) + 1n).toString()})`;
}

/**
 * The copy of a page as the list shows it until the server has made it
 * (user decision 2026-11-12: a press answers on the next frame): named and
 * placed as the server makes it, right after its page beside it, or last in
 * the page it goes in, under an ID of its own until the server's.
 */
export function shownPageCopy(page: Page, pages: readonly Page[], parentId = page.parentId): Page {
  const siblings = pages.filter((other) => other.workspaceId === page.workspaceId && other.parentId === parentId);
  const beside = parentId === page.parentId;
  const next = beside ? Math.min(...siblings.map(({ position }) => position).filter((position) => position > page.position)) : Infinity;
  const position = beside
    ? Number.isFinite(next) ? page.position + (next - page.position) / 2 : page.position + 1
    : Math.max(0, ...siblings.map((other) => other.position)) + 1;
  const now = new Date().toISOString();
  return {
    ...page, id: newId(), title: pageCopyTitle(page.title), titleSpans: undefined,
    parentId, position, inTrash: false, trashedAt: undefined, archivedAt: undefined, archivedBy: undefined,
    teamspaceId: beside ? page.teamspaceId : pages.find((other) => other.id === parentId)?.teamspaceId ?? page.teamspaceId,
    isWiki: false, wikiRootId: undefined, createdAt: now, updatedAt: now, cursor: 0, revision: 0,
  };
}

export async function duplicateNativePage(
  session: Session,
  page: Page,
  signal: AbortSignal,
  options: PageDuplicationOptions = {},
) {
  signal.throwIfAborted();
  // The copy is of what the server has stored; what was just typed is to be in it.
  if (!(await pageTextStored(session, page.id, signal))) throw new Error("The page's text has not reached the server yet.");
  const initial = await api.startPageDuplication(session, page.id, options.toPrivatePages ? {
    requestId: newId(), destinationParentId: "", title: page.title, withoutContent: false, toPrivatePages: true,
  } : {
    requestId: newId(),
    destinationParentId: options.parentId ?? page.parentId,
    title: "",
    withoutContent: options.withoutContent === true,
    ...(options.workspaceId && options.workspaceId !== page.workspaceId ? {
      destinationWorkspaceId: options.workspaceId,
      ...(options.teamspaceId ? { destinationTeamspaceId: options.teamspaceId } : {}),
    } : {}),
  }, signal);
  // The copy opens now; its wait goes on if the menu that asked for it closes,
  // so the page it opened on is told when the copy is ready.
  const targetId = initial.targetRootPageId;
  askPageCopy(targetId);
  const followed = new AbortController().signal;
  const finished = waitForDurableJob({
    initial,
    isSameJob: (next) => next.jobId === initial.jobId
      && next.sourceRootPageId === page.id
      && next.targetRootPageId === initial.targetRootPageId,
    isTerminal: (next) => next.status === pageDuplicationStatuses.completed
      || next.status === pageDuplicationStatuses.failed,
    onProgress(next) {
      const { copied, discovered } = next.progress;
      setPageCopyProgress(targetId, discovered > 0 ? Math.min(99, Math.floor((copied / discovered) * 100)) : 0);
    },
    read: () => api.readPageDuplication(session, initial.jobId, followed),
    signal: followed,
  }).then((result) => {
    if (result.status === pageDuplicationStatuses.completed) endPageCopy(targetId);
    else setPageCopyProgress(targetId, null);
    return result;
  }, (error: unknown) => {
    setPageCopyProgress(targetId, null);
    throw error;
  });
  options.onStarted?.(targetId, finished);
  const result = await new Promise<PageDuplicationResponse>((resolve, reject) => {
    signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
    finished.then(resolve, reject);
  });
  if (result.status !== pageDuplicationStatuses.completed) {
    throw Object.assign(new Error("Page duplication did not complete."), {
      code: result.errorCode || "LEAFEN_PAGE_DUPLICATION_UNAVAILABLE",
    });
  }
  signal.throwIfAborted();
  return result;
}
