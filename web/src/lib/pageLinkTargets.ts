import { useEffect } from "react";
import { api } from "@/api";
import type { Session } from "@/types";
import { isServerUnreachable, readUntilServerAnswers } from "./serverReach";
import { useStateUnlessSame } from "./useStateUnlessSame";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

// What a link block's target is for the person reading: a page, a database
// or a comment, the page that holds it, and its current name, or that they
// cannot open it. The editor asks the server once per page and target.

export type PageLinkTargetKind = "page" | "database" | "comment";

export type PageLinkTarget = {
  kind: PageLinkTargetKind;
  id: string;
  available: boolean;
  /** A page of a copy not yet made: it shows when it is. */
  copying?: boolean;
  /** The page holding the target is in the trash; it still opens there. */
  inTrash?: boolean;
  pageId?: string;
  blockId?: string;
  title?: string;
  icon?: string;
  iconType?: string;
  /** A database without a name of several sources: their titles, which name it. */
  sourceTitles?: DatabaseSourceTitle[];
};

type Pending = { resolve(target: PageLinkTarget | undefined): void; refuse(error: unknown): void; kind: PageLinkTargetKind; id: string };

// A target is read again after a short while, so a renamed, moved or
// trashed target shows as it is now; a page of a copy still being made is
// asked again sooner, so it shows once made while the copy goes on, as
// Notion's copy fills (2026-11-11: a large copy's sub-pages came only at its
// end, waiting for a page list that a copy's changes kept from finishing).
const CACHE_MS = 30_000;
const COPYING_ASKED_AGAIN_MS = 2_000;
// A read answers this many (the server's maxPageLinkTargets); a page with more
// links asks in parts (2026-11-05: 120 sub-pages were refused, each of them).
const TARGETS_PER_READ = 64;
const cache = new Map<string, { at: number; promise: Promise<PageLinkTarget | undefined> }>();
const batches = new Map<string, { session: Session; pageId: string; pending: Pending[] }>();

function flush(batchKey: string) {
  const batch = batches.get(batchKey);
  batches.delete(batchKey);
  if (!batch) return;
  for (let start = 0; start < batch.pending.length; start += TARGETS_PER_READ) {
    const part = batch.pending.slice(start, start + TARGETS_PER_READ);
    api.readPageLinkTargets(batch.session, batch.pageId, part.map(({ kind, id }) => ({ kind, id }))).then((response) => {
      for (const pending of part) {
        pending.resolve(response.targets.find((target) => target.kind === pending.kind && target.id === pending.id));
      }
    }, (error: unknown) => {
      // Unanswered, each is asked again once the server may answer (usePageLinkTarget).
      for (const pending of part) {
        if (isServerUnreachable(error)) pending.refuse(error);
        else pending.resolve(undefined);
      }
    });
  }
}

/** Reads a link target once for the session and page, with others asked in the same moment. */
const targetKey = (session: Session, pageId: string, kind: PageLinkTargetKind, id: string) =>
  `${session.token}\u0000${pageId}\u0000${kind}\u0000${id}`;

function readPageLinkTarget(session: Session, pageId: string, kind: PageLinkTargetKind, id: string) {
  const key = targetKey(session, pageId, kind, id);
  const cached = cache.get(key);
  if (cached && Date.now() - cached.at < CACHE_MS) return cached.promise;
  const promise = new Promise<PageLinkTarget | undefined>((resolve, refuse) => {
    const batchKey = `${session.token}\u0000${pageId}`;
    let batch = batches.get(batchKey);
    if (!batch) {
      batch = { session, pageId, pending: [] };
      batches.set(batchKey, batch);
      queueMicrotask(() => flush(batchKey));
    }
    batch.pending.push({ resolve, refuse, kind, id });
  });
  cache.set(key, { at: Date.now(), promise });
  // A failed read is asked again next time.
  void promise.then((target) => { if (!target) cache.delete(key); }, () => cache.delete(key));
  return promise;
}

/**
 * The target of a link on a page, once known; undefined while it is read or
 * where it cannot be. Read while the server doesn't answer, it is read again
 * once the server may (readUntilServerAnswers), what the link showed standing
 * meanwhile: taken for a target that can't be opened, a link read offline
 * kept its old name after the server was back (2026-11-25).
 */
export function usePageLinkTarget(
  session: Session | null | undefined,
  pageId: string | undefined,
  kind: PageLinkTargetKind,
  id: string,
): PageLinkTarget | undefined {
  // A link drawn for the first time has nothing to clear, so it queues no update.
  const [target, setTarget] = useStateUnlessSame<PageLinkTarget | undefined>(undefined);
  useEffect(() => {
    setTarget(undefined);
    if (!session || !pageId || !id) return undefined;
    let again = 0;
    let stop = () => {};
    const read = () => {
      stop = readUntilServerAnswers(() => readPageLinkTarget(session, pageId, kind, id), (next) => {
        setTarget(next);
        if (next?.copying) again = window.setTimeout(() => {
          cache.delete(targetKey(session, pageId, kind, id));
          read();
        }, COPYING_ASKED_AGAIN_MS);
      });
    };
    read();
    return () => {
      stop();
      window.clearTimeout(again);
    };
  }, [id, kind, pageId, session]);
  return target;
}
