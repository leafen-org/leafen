/*
 * A write that changes what a page is — a page turned into a wiki or back,
 * moved into a database as its row, a database's page moved out as a page —
 * goes after the page titles and settings this tab wrote before it: a title
 * still waiting for its typing to pause (usePageTitleSave) is sent at once,
 * and the title and settings writes on their way are waited for, landed or
 * refused. Sent after it, such a write met a page of another kind — one held
 * while its wiki was made, or a database's page that takes its title as its
 * row's — and the title typed was lost (2026-11-21: a title typed and at once
 * turned into a wiki, its save going 600 ms after the typing, was gone 4 times
 * in 14). One kept for a server out of reach and sent after the change is
 * sent again as the page's new kind takes it (sendKeptPageMetadata).
 */

const typed = new Set<() => void>();
const onTheirWay = new Set<Promise<void>>();

/** Holds a page title's save that waits for its typing to pause; gives what lets it go. */
export function holdTypedPageTitle(sendNow: () => void): () => void {
  typed.add(sendNow);
  return () => { typed.delete(sendNow); };
}

/** A page's title or settings write on its way to the server, until it is answered. */
export function notePageWriteOnItsWay(write: Promise<unknown>) {
  const answered = write.then(() => undefined, () => undefined);
  onTheirWay.add(answered);
  void answered.then(() => { onTheirWay.delete(answered); });
}

/** Sends every page title still waiting for its typing to pause, now, in the order typed. */
export function sendTypedPageTitles() {
  for (const send of [...typed]) send();
}

/** What a write changing a page's kind waits for: the titles typed, sent now, and every page write on its way answered. */
export async function pageWritesSent(): Promise<void> {
  sendTypedPageTitles();
  await Promise.all([...onTheirWay]);
}
