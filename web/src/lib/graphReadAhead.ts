import { collectOpenedPageGraph } from "./authenticatedContentGraph";
import { cacheBehind, pageGraphCache } from "./cacheStaleness";
import type { Session } from "@/types";

type GraphRead = Awaited<ReturnType<typeof collectOpenedPageGraph>>;

/**
 * A body read on the press that opens its page — a row's, as its peek or
 * page opens — rather than once the page's own read has come and its editor
 * is drawn: read after it, a row peek's body came some 300 ms after its page
 * (2026-11-12, a NAS-class server 100 ms away: the row's read ending at
 * ~600 ms, the body's starting only then). The editor's own read takes it
 * (useLocalFirstContentGraph) if it comes within a few seconds and nothing
 * was changed here meanwhile, which the copy marked behind says
 * (cacheStaleness.ts); else it reads as before.
 */
const AHEAD_FOR_MS = 5_000;
const ahead = new Map<string, { at: number; read: Promise<GraphRead> }>();
// The pages whose content goes to the server right after it made them, till that change went (readGraphAfter).
const making = new Map<string, Promise<unknown>>();

function key(session: Pick<Session, "user">, pageId: string) {
  return `${session.user.id}\u0000${pageId}`;
}

/** Starts reading `pageId`'s graph now, for its editor to take in a moment. */
export function readGraphAhead(session: Session, pageId: string) {
  if (cacheBehind(session.user.id, pageGraphCache(pageId))) return;
  const held = ahead.get(key(session, pageId));
  if (held && Date.now() - held.at < AHEAD_FOR_MS) return;
  const read = collectOpenedPageGraph(session, pageId);
  // One that fails is read again by the editor, as if not read ahead.
  read.catch(() => undefined);
  ahead.set(key(session, pageId), { at: Date.now(), read });
  window.setTimeout(() => {
    if (ahead.get(key(session, pageId))?.read === read) ahead.delete(key(session, pageId));
  }, AHEAD_FOR_MS);
}

/**
 * Reads `pageId`'s graph once `after` settles, for its editor to take as
 * readGraphAhead's: a page made here whose content goes to the server in a
 * change right after the server made it (a saved view's page) is read with
 * that change, not as the empty page the server holds in between — every read
 * of it till then, not only the first: one read again as the page settled
 * read the empty page and kept it, a full-page database made from "/" staying
 * blank (2026-11-16: three of seven).
 */
export function readGraphAfter(session: Session, pageId: string, after: Promise<unknown>) {
  const settled = after.catch(() => undefined);
  making.set(key(session, pageId), settled);
  void settled.then(() => { if (making.get(key(session, pageId)) === settled) making.delete(key(session, pageId)); });
  const read = settled.then(() => collectOpenedPageGraph(session, pageId));
  read.catch(() => undefined);
  ahead.set(key(session, pageId), { at: Date.now(), read });
  window.setTimeout(() => {
    if (ahead.get(key(session, pageId))?.read === read) ahead.delete(key(session, pageId));
  }, AHEAD_FOR_MS);
}

/** The read started ahead for `pageId`, taken once; else `read` now, or once the change it waits for went (readGraphAfter). */
export function graphReadAheadOr(session: Session, pageId: string, read: () => Promise<GraphRead>): Promise<GraphRead> {
  const held = ahead.get(key(session, pageId));
  ahead.delete(key(session, pageId));
  const waiting = making.get(key(session, pageId));
  const readNow = () => waiting ? waiting.then(read) : read();
  if (!held || Date.now() - held.at >= AHEAD_FOR_MS || cacheBehind(session.user.id, pageGraphCache(pageId))) return readNow();
  return held.read.catch(readNow);
}
