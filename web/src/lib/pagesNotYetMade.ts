/**
 * The pages made on this device first that the server hasn't made yet, as the
 * outbox keeping them tells (nativeGraphPagesWaiting.ts). What asks the
 * server about such a page waits until the server has it, wherever a request
 * goes out: the app's requests (apiTransport.ts), a page's live room
 * (collaboration.ts) and its sign-in wall (usePageLoginWall.ts). A page made
 * here asked for its comments, access, sharing, room, links and analytics
 * before the server had it, and each was refused (2026-11-08: "댓글을
 * 불러오지 못했습니다" showed, and the page was missing from "최근 방문").
 * A page not made here, or made already, goes at once. So does what asks
 * about a data source a change made here makes or changes, once that change
 * is answered: a database just made, asked for its ID property before the
 * server had it, was refused (2026-11-14: 404 for "+ 속성 추가").
 */
export interface PagesNotYetMade {
  /** Whether the page (or the database its address names) was made here and the server hasn't answered its making. */
  has(pageId: string): boolean;
  /** Whether a change made here making or changing the data source hasn't been answered. */
  hasDataSource?(dataSourceId: string): boolean;
  /** Settles once the pages kept before this tab opened are known. */
  ready(): Promise<void>;
  subscribe(listener: () => void): () => void;
}

const told = new Set<PagesNotYetMade>();

/** Tells which pages an account's outbox keeps until made; the returned function stops it. */
export function tellPagesNotYetMade(pages: PagesNotYetMade): () => void {
  told.add(pages);
  return () => { told.delete(pages); };
}

type Kept = (pages: PagesNotYetMade) => boolean;
const pageKept = (pageId: string): Kept => (pages) => pages.has(pageId);
const dataSourceKept = (dataSourceId: string): Kept => (pages) => pages.hasDataSource?.(dataSourceId) === true;

// The outbox that keeps the page (or the data source's change) until the server answered it, if one does.
async function keeperOf(kept: Kept): Promise<PagesNotYetMade | null> {
  for (const pages of [...told]) {
    await pages.ready();
    if (told.has(pages) && kept(pages)) return pages;
  }
  return null;
}

/** Whether the page was made here and the server hasn't made it yet. */
export async function pageNotYetMade(pageId: string): Promise<boolean> {
  return (await keeperOf(pageKept(pageId))) !== null;
}

/**
 * Settles once the server has made the page, when it was made here and not
 * yet made, or once signal is aborted; at once for any other page. An outbox
 * let go meanwhile (its account's session renewed) leaves the wait to the
 * one that follows it.
 */
export function afterPageMade(pageId: string, signal?: AbortSignal | null): Promise<void> {
  return afterKept(pageKept(pageId), signal);
}

/**
 * Settles once the changes made here that make or change the data source
 * are answered, or once signal is aborted; at once for any other.
 */
export function afterDataSourceMade(dataSourceId: string, signal?: AbortSignal | null): Promise<void> {
  return afterKept(dataSourceKept(dataSourceId), signal);
}

async function afterKept(keeps: Kept, signal?: AbortSignal | null): Promise<void> {
  for (;;) {
    const pages = await keeperOf(keeps);
    if (!pages || signal?.aborted) return;
    const kept = () => told.has(pages) && keeps(pages);
    if (!kept()) continue;
    await new Promise<void>((resolve) => {
      const done = () => {
        stop();
        signal?.removeEventListener("abort", done);
        resolve();
      };
      const stop = pages.subscribe(() => { if (!kept()) done(); });
      signal?.addEventListener("abort", done);
    });
  }
}
