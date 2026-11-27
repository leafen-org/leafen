import { LeafenApiError } from "@/apiTransport";
import { databaseRowPageBounds, type DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import type { Session } from "@/types";
import { localFirstProductCache, type KeptRowPage } from "./localFirstProductCache";
import { isServerUnreachable } from "./serverReach";
import { serverIdentity } from "./serverOrigin";

/**
 * A database page opened here is kept as any page is (2026-11-11: offline, a
 * row page opened online said it couldn't open while its database page
 * opened): its body as its own copy (useLocalFirstContentGraph), and here the
 * reads that draw the rest — the page itself (its row, properties and what
 * the reader may do), its database's page layout and its verification — each
 * time the server answers one. Without the server the page opens from what
 * was kept, as it was last drawn, and is read again once it answers; a page
 * the server says is gone or not the reader's lets its copy go. Of the body's
 * blocks the page's read keeps the first window only, which says whether
 * there is a body: the body itself shows from its own copy. Any page keeps
 * here too how far it reaches, which its top bar shows beside "공유"
 * (usePageOnlyReader.ts), and the path above it the server named — the
 * database a row page sits in — which its top bar draws (usePagePathsAbove.ts).
 * The copy goes with the page's other copies (recordCacheKeys `rowPage`).
 */

function identity(session: Pick<Session, "user">, rowId: string) {
  return { key: rowId, principalId: session.user.id, serverKey: serverIdentity() };
}

/** Keeps these reads of a page as its copy here. */
export function keepRowPageReads(session: Pick<Session, "user">, rowId: string, reads: KeptRowPage): void {
  void localFirstProductCache.rowPage.keep(identity(session, rowId), reads).catch(() => undefined);
}

/** Keeps a row page's own read as its copy here. */
export function keepRowPage(session: Session, snapshot: DatabaseRowPageSnapshot): void {
  keepRowPageReads(session, snapshot.row.id, { page: {
    ...snapshot,
    body: snapshot.body.slice(0, databaseRowPageBounds.bodyPageSize),
    hasMore: false,
    nextCursor: null,
  } });
}

/** A page's reads as kept here, or none. */
export function readKeptRowPage(session: Pick<Session, "user">, rowId: string): Promise<KeptRowPage | null> {
  return localFirstProductCache.rowPage.read(identity(session, rowId)).catch(() => null);
}

/**
 * What a row page's failed read leaves to show: its copy here when the server
 * couldn't be reached, else nothing — and a page the server says is gone or
 * not the reader's lets its copy go.
 */
export async function rowPageInPlaceOf(session: Session, rowId: string, error: unknown): Promise<DatabaseRowPageSnapshot | null> {
  if (error instanceof LeafenApiError && (error.status === 403 || error.status === 404)) {
    void localFirstProductCache.rowPage.drop(identity(session, rowId)).catch(() => undefined);
    return null;
  }
  return isServerUnreachable(error) ? (await readKeptRowPage(session, rowId))?.page ?? null : null;
}
