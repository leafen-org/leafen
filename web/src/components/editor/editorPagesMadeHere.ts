import { compareGraphOrder, type ContentGraph, type GraphBlock, type GraphOperation } from "@/contentGraph";
import { nextPaint } from "@/lib/afterPress";
import { contentGraphOutboxOf } from "@/lib/contentGraphSend";
import { pageHref } from "@/lib/navigation";
import { hrefOfPageMadeIn, pagePeekHref, peekedPageId, peekModeOf } from "@/lib/pagePeek";
import { pageCreationHere } from "@/lib/pageCreationService";
import { openPageBeforeMade } from "@/lib/pageMadeHere";
import { holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { pushRoute, replaceRoute } from "@/lib/router";
import type { Session } from "@/types";
import type { EditorMoveContext } from "./editorRuntimeTypes";
import { pageMapOf } from "@/lib/pageIndex";

// The pages a change to a page's body makes on the server — a line turned
// into a page ("/페이지", "전환 → 페이지"), a page link's new page — show at
// once, as Notion's (user decision 2026-11-12: a press answers on the next
// frame; "/페이지" waited for the change to be saved and the page list read
// again, 460 ms on a NAS): in the page list, and opening from what this
// device keeps (openPageBeforeMade), what asks the server about them waiting
// until the change reached it. The list is read once it has; refused, the
// list shows what the server holds.

/** How long a page made here waits for its copy here before it opens. */
const COPY_WAIT_MS = 300;

/** The pages a change made, shown, and whether the change reached the server. */
export type PagesMadeHere = { reached: Promise<boolean>; opening: Map<string, Promise<void>> };

/**
 * Whether the change `send` sends to a page reached the server, once it has:
 * once the outbox's lane answered it, whichever turn sent it, or once its own
 * send was answered — at once, or once the outbox sent it where it kept it,
 * for a server out of reach. False when the server refused it. A page it
 * makes waits for it (openPageBeforeMade), and so does a change to that page
 * sent in the lane after it: told by its own send alone, which waited for the
 * lane, the lane waited six seconds for it (2026-11-14: a database made
 * with "/데이터베이스 - 전체 페이지" on a NAS).
 */
export function changeReached(session: Session, pageId: string, send: () => Promise<boolean>): Promise<boolean> {
  const outbox = contentGraphOutboxOf(session), waiting = outbox?.graphPagesWaiting;
  const before = new Set(waiting?.unanswered(pageId) ?? []);
  const flushed = send();
  // The change it kept last, which the lane answers after those before it.
  const sent = waiting?.unanswered(pageId).filter((id) => !before.has(id)).at(-1);
  let stopAnswered = () => {};
  const answered = new Promise<boolean>((done) => {
    if (!outbox || !sent) return;
    stopAnswered = outbox.subscribeGraphLane((event) => {
      if (event.kind === "answered" && event.mutationId === sent) done(!event.refused);
    });
  });
  const reached = flushed.catch(() => false).then((saved) => {
    if (!outbox || !waiting?.has(pageId)) return saved;
    return new Promise<boolean>((done) => {
      let refused = false;
      const stopLane = outbox.subscribeGraphLane((event) => { if (event.kind === "rejected" && !event.settings && event.pageId === pageId) refused = true; });
      const stop = waiting.subscribe(() => {
        if (waiting.has(pageId)) return;
        stop();
        stopLane();
        done(!refused);
      });
    });
  });
  return Promise.race([answered, reached]).finally(() => stopAnswered());
}

/** The page blocks of a body, in the order they stand in it. */
export function pagesInBodyOrder(graph: ContentGraph, isPage: (block: GraphBlock) => boolean) {
  const children = new Map<string | null, GraphBlock[]>();
  for (const block of graph.blocks) children.set(block.parentId, [...children.get(block.parentId) ?? [], block]);
  const order: string[] = [];
  const walk = (parentId: string | null) => {
    for (const block of [...children.get(parentId) ?? []].sort(compareGraphOrder)) {
      if (isPage(block)) order.push(block.id);
      else walk(block.id);
    }
  };
  walk(null);
  return order;
}

/** A place in the page list between the listed pages whose blocks stand before and after the page's. */
export function positionAmong(order: readonly string[], positions: ReadonlyMap<string, number>, pageId: string) {
  const at = order.indexOf(pageId);
  if (at < 0) return null;
  const before = order.slice(0, at).reverse().find((id) => positions.has(id));
  const after = order.slice(at + 1).find((id) => positions.has(id));
  const low = before ? positions.get(before)! : undefined, high = after ? positions.get(after)! : undefined;
  if (low !== undefined && high !== undefined) return (low + high) / 2;
  if (low !== undefined) return low + 1;
  return high !== undefined ? high - 1 : null;
}

/**
 * Shows the pages whose blocks a change to a page's body makes (`made`, a
 * page block's ID being its page's): listed under the page at once, named
 * as the change names them, and opening from here, an empty body their copy
 * where their blocks hold nothing; null when the change makes none.
 */
export function showPagesMadeHere(input: {
  session: Session; context: EditorMoveContext; pageId: string; workspaceId: string;
  before: ContentGraph; after: ContentGraph; operations: readonly GraphOperation[]; isPage: (block: GraphBlock) => boolean;
  flush: () => Promise<boolean>; refresh: () => Promise<unknown>;
}): PagesMadeHere | null {
  const { session, context, before } = input;
  const listed = pageMapOf(context.pages);
  const had = new Set(before.blocks.filter(input.isPage).map((block) => block.id));
  const made = input.operations.flatMap((operation) => operation.kind === "upsertBlock" && input.isPage(operation.block)
    && !had.has(operation.block.id) && !listed.has(operation.block.id) ? [operation.block] : []);
  if (!made.length) return null;
  const parent = context.pages.find((page) => page.id === input.pageId);
  const workspaceId = input.workspaceId || parent?.workspaceId || "";
  const reached = changeReached(session, input.pageId, input.flush);
  const opening = new Map<string, Promise<void>>();
  // The page list orders sub-pages as their blocks stand in the body.
  const order = pagesInBodyOrder(input.after, input.isPage);
  const positions = new Map(context.pages.filter((page) => page.parentId === input.pageId).map((page) => [page.id, page.position]));
  for (const block of made) {
    const title = String(block.content.childPageTitle ?? "");
    const page = { ...pageCreationHere(session, { siblings: context.pages, parentId: input.pageId, title, workspaceId }).page,
      id: block.id, teamspaceId: parent?.teamspaceId ?? "" };
    page.position = positionAmong(order, positions, block.id) ?? page.position;
    positions.set(block.id, page.position);
    // A block holding blocks takes them into its page on the server: that
    // page opens once the server has it.
    const empty = !before.blocks.some((candidate) => candidate.parentId === block.id);
    opening.set(block.id, openPageBeforeMade(session, page, reached, empty));
    context.onPageChange(page);
  }
  void reached.then(() => input.refresh()).catch(() => {});
  return { reached, opening };
}

/**
 * Opens a page made here on the next frame, at its title, what is typed
 * meanwhile going into it: beside the page while the page it was made in
 * shows in a side peek, else whole, as a full-page database (`database`)
 * always does (pagePeek.ts hrefOfPageMadeIn). Refused, its parent shows
 * again where the page shows, whole or in its peek.
 */
export function openPageMadeHere(made: PagesMadeHere, pageId: string, parentId: string, isCurrent: () => boolean, database = false) {
  const kept = made.opening.get(pageId) ?? Promise.resolve();
  void holdTypingWhile(async () => {
    await Promise.all([nextPaint(), Promise.race([kept, new Promise((done) => { setTimeout(done, COPY_WAIT_MS); })])]);
    if (!isCurrent()) return;
    requestPageTitleFocus(pageId);
    pushRoute(hrefOfPageMadeIn(parentId, pageId, database));
  }).catch(() => {});
  void made.reached.then((saved) => {
    if (saved) return;
    const search = new URLSearchParams(window.location.search);
    if (peekedPageId(search.get("p")) === pageId) replaceRoute(pagePeekHref(parentId, peekModeOf(search.get("pm")) === "side" ? "side" : "center"));
    else if (window.location.pathname === pageHref(pageId)) replaceRoute(pageHref(parentId));
  });
}
