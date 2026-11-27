import { emptyContentGraph } from "@/contentGraph";
import { i18next } from "@/i18n";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import { type CachedContentGraph, localFirstProductCache } from "@/lib/localFirstProductCache";
import type { NativePageCreation } from "@/lib/nativePageLaneEntries";
import type { NativePageMutationBrowser } from "@/lib/nativePageMutationBrowser";
import type { ProductStatusSink } from "@/lib/productStatus";
import { serverIdentity } from "@/lib/serverOrigin";
import { notePageMadeHereReach } from "@/lib/usePageOnlyReader";
import { tellPagesNotYetMade } from "@/lib/pagesNotYetMade";
import { forgetPageMadeHereOpening, leaveRefusedPageMadeHere, notePageMadeHereOpening } from "@/lib/pageMadeHereRefused";
import { holdShownGraph } from "@/lib/shownGraphs";
import { tellPageEditedHere } from "@/lib/pageEditedHere";
import type { Page, Session } from "@/types";
import type { PageCreateResponse } from "@/workspaceMembershipWire.generated";
import { keptChangeStatus } from "./serverAnswers";

// The pages made on this device in this window, by who made them: theirs to
// edit before the server can answer for them (2026-11-07: a sub-page made
// while the server couldn't be reached opened read-only, what was typed held
// back).
const madeHere = new Map<string, string>();

/** Whether the person made this page on this device in this window. */
export function madeHereBy(pageId: string, userId: string) {
  return madeHere.get(pageId) === userId;
}

/** Where a page made on this device is kept until the server makes it. */
export type PageMadeHereOutbox = Pick<NativePageMutationBrowser<unknown>, "admitAndSendPageCreation" | "keepPageCreation">;

// An empty page's body as made here.
const emptyCopy = (): CachedContentGraph => ({ bootstrapRevision: 0, graphRevision: 0, graph: emptyContentGraph, canEdit: true, canSuggest: false });

// A page made here opens from here: theirs to edit, its top bar showing how
// far it reaches before the server can say, and its body (empty unless
// given) its copy — without which it opens once the server has it.
async function openFromHere(session: Session, page: Page, copy: CachedContentGraph | null = emptyCopy()) {
  madeHere.set(page.id, session.user.id);
  notePageMadeHereReach(session, page);
  if (!copy) return;
  await localFirstProductCache.graph.writeCache({
    complete: true, key: page.id, principalId: session.user.id, serverKey: serverIdentity(), version: 1, value: copy,
  }).catch(() => undefined);
}

// Pages opened before the server has them and before the outbox holds them:
// made by a change to their parent's body (a line turned into a page) that
// hasn't reached the server, or shown at a press while they are being kept
// here (showPageMadeHere).
const madeByChange = new Set<string>();
const madeByChangeListeners = new Set<() => void>();
tellPagesNotYetMade({
  has: (pageId) => madeByChange.has(pageId),
  ready: () => Promise.resolve(),
  subscribe(listener) {
    madeByChangeListeners.add(listener);
    return () => { madeByChangeListeners.delete(listener); };
  },
});

/**
 * Opens a page that a change to its parent's body makes on the server (a
 * line turned into a page, "/페이지") before the server has it, as a page
 * made here opens (user decision 2026-11-12: a press answers
 * on the next frame — the page waited for the change to be saved and the
 * page list read again, 460 ms on a NAS): what asks the server about it waits
 * until `reached` settles (pagesNotYetMade). Its empty body is its copy unless
 * `empty` is false (its block holds blocks the server puts in it). Resolves
 * once its copy is kept.
 */
export function openPageBeforeMade(session: Session, page: Page, reached: Promise<unknown>, empty = true) {
  madeByChange.add(page.id);
  const told = () => { for (const listener of [...madeByChangeListeners]) listener(); };
  told();
  void reached.catch(() => undefined).then(() => { if (madeByChange.delete(page.id)) told(); });
  return openFromHere(session, page, empty ? emptyCopy() : null);
}

/**
 * A sub-page made here edits its parent, as the server notes it
 * (placeMadeChildPageBlock) and Notion's shows it: the parent last edited
 * now, by this person, first in a sidebar section sorted by last edit (seen
 * 2026-11-17 twice, the sidebar's '+'), past the frame of the press
 * (pageEditedHere.ts).
 */
export function tellParentEditedHere(session: Session, page: Pick<Page, "parentId" | "createdAt">) {
  if (page.parentId) tellPageEditedHere({ id: page.parentId, updatedAt: page.createdAt || new Date().toISOString(), lastEditedBy: session.user.id });
}

/**
 * Shows a page made on this device in the press's own frame, as Notion's new
 * page shows at once, though the server can't be reached (2026-11-07: on a
 * stalled network a new page waited for the server, gave up after 12
 * seconds, and its typed title went; user decision 2026-11-12: a press
 * answers on the next frame — the add window's "빈 페이지" stayed up while
 * the server made the page, some 0.18 s on a NAS, and a page kept here
 * first waited for the keeping): it is the person's to edit, its body draws from `shown` (empty
 * unless given) at once (shownGraphs.ts), and what asks the server about it
 * waits until the server made it (pagesNotYetMade). It is kept here and sent
 * in turn right behind the press (sendPageMadeHere), ahead of anything done
 * in it; where this device can't keep it, it goes to the server at once.
 * Resolves once it can be opened from elsewhere (another tab): kept with its
 * copy, or made by the server; false when it was refused, the person then
 * back where they were (pageMadeHereRefused.ts).
 */
export function showPageMadeHere(outbox: PageMadeHereOutbox, session: Session, creation: NativePageCreation, handlers: {
  announce: ProductStatusSink;
  isCurrent?: () => boolean;
  made: (page: Page) => void;
  refused: () => void;
}, shown: CachedContentGraph = emptyCopy()): Promise<boolean> {
  holdShownGraph(session.user.id, creation.pageId, shown);
  tellParentEditedHere(session, creation.page);
  notePageMadeHereOpening(creation.pageId);
  let keptOrAnswered = () => {};
  void openPageBeforeMade(session, creation.page, new Promise<void>((resolve) => { keptOrAnswered = resolve; }), false);
  const kept = outbox.keepPageCreation(creation).catch(() => false);
  const answered = sendPageMadeHere(outbox, creation, handlers);
  // Kept, the outbox holds what asks about it until the server made it.
  void kept.then((held) => { if (held) keptOrAnswered(); });
  void answered.then(() => keptOrAnswered());
  return kept.then(async (held) => {
    if (!held) return await answered === "made";
    await openFromHere(session, creation.page, shown);
    return true;
  });
}

/**
 * Sends a page made here in turn, now or when the server
 * answers. Waiting for the server, it says so; made, made gets the page as the
 * server holds it; refused, it says why, leads back from the page
 * (pageMadeHereRefused.ts) and refused is called.
 */
export function sendPageMadeHere(outbox: PageMadeHereOutbox, creation: NativePageCreation, { announce, isCurrent = () => true, made, refused }: {
  announce: ProductStatusSink;
  isCurrent?: () => boolean;
  made: (page: Page) => void;
  refused: () => void;
}): Promise<"made" | "waiting" | "refused"> {
  return outbox.admitAndSendPageCreation(creation).then((delivery) => {
    if (delivery.state === "kept") {
      if (isCurrent()) announce(i18next.t(keptChangeStatus()));
      return "waiting" as const;
    }
    forgetPageMadeHereOpening(creation.pageId);
    if (delivery.state === "committed" && isCurrent()) made((delivery.result as PageCreateResponse).page);
    return "made" as const;
  }, (error: unknown) => {
    if (isCurrent()) announce(authenticatedProductErrorMessage(error, "pageCreate"), "error");
    leaveRefusedPageMadeHere(creation.pageId);
    refused();
    return "refused" as const;
  });
}

/**
 * The pages listed, a page made here given the server's place and cursor for
 * it; what was changed in it here since (its title, …) stays as it shows.
 */
export function withServerMadePage(pages: readonly Page[], made: Page) {
  return pages.map((page) => page.id === made.id
    ? { ...page, cursor: Math.max(page.cursor, made.cursor), revision: Math.max(page.revision, made.revision), position: made.position }
    : page);
}
