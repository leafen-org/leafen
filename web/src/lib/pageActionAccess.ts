import { request } from "@/apiTransport";
import { madeHereBy } from "@/lib/pageMadeHere";
import { localFirstProductCache } from "@/lib/localFirstProductCache";
import { serverIdentity } from "@/lib/serverOrigin";
import { isDefinitiveReadDenial } from "@/lib/readDenial";
import type { PageActionAccess } from "@/workspaceMembershipWire.generated";
import type { Session } from "@/types";

/** The server's own answer that the person has no such page, not a failed read (lib/readDenial.ts). */
const refused = isDefinitiveReadDenial;

// A sidebar reads access only for a row the person approaches. Copies of a
// row share this read. A new account's snapshot gets a new owner; a new
// snapshot of the same account has each page read again when next asked, what
// was known standing until the answer comes, so a window open on the page
// stays as it is unless the answer changes it (2026-11-04: it read as
// read-only for that round trip, and what was typed then was lost).
export function createPageActionAccess(session: Session) {
  const reads = new Map<string, { asked: number; promise: Promise<boolean>; editable: boolean; suggests: boolean; manages: boolean; known: boolean; busy?: string; copyPercent?: number }>();
  const listeners = new Set<() => void>();
  const controllers = new Set<AbortController>();
  let retired = false, snapshot = 0;
  const changed = () => { for (const listener of listeners) listener(); };
  // Whether this device keeps a copy of the page that may be changed (a copy
  // read before that said nothing allows it, as the copy itself does).
  const copyAllowsEditing = async (pageId: string) => {
    try {
      const copy = await localFirstProductCache.graph.readCache({ key: pageId, principalId: session.user.id, serverKey: serverIdentity() });
      return copy !== null && copy.value.canEdit !== false;
    } catch {
      return false;
    }
  };
  // A page the person made on this device is theirs until the server answers for it.
  const own = (pageId: string) => reads.get(pageId)?.known !== true && madeHereBy(pageId, session.user.id);
  return {
    activate() { retired = false; },
    canEdit: (pageId: string) => reads.get(pageId)?.editable === true || own(pageId),
    /** Comments take edit or suggest access, as the open page's do. */
    canComment: (pageId: string) => reads.get(pageId)?.editable === true || reads.get(pageId)?.suggests === true || own(pageId),
    /** Full access, which moving the page asks, as Notion's move does. */
    canManage: (pageId: string) => reads.get(pageId)?.manages === true || own(pageId),
    /** What is running on the page and holds its changes back for now, as its answer named it. */
    busy: (pageId: string) => reads.get(pageId)?.busy ?? "",
    /** How far the copy this page is the top of has come, as its answer said; undefined if none is being made. */
    copyPercent: (pageId: string) => reads.get(pageId)?.copyPercent,
    /** Whether the page's access has been read, so a false canEdit means the person can't edit. */
    knows: (pageId: string) => reads.get(pageId)?.known === true || own(pageId),
    /** Which snapshot reads answer for: those showing a page read it again when this changes. */
    snapshot: () => snapshot,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    read(pageId: string): Promise<boolean> {
      if (retired || !pageId) return Promise.resolve(false);
      const previous = reads.get(pageId);
      if (previous?.asked === snapshot) return previous.promise;
      const controller = new AbortController();
      controllers.add(controller);
      const entry = { asked: snapshot, promise: Promise.resolve(false), editable: previous?.editable ?? false, suggests: previous?.suggests ?? false,
        manages: previous?.manages ?? false, busy: previous?.busy ?? "", copyPercent: previous?.copyPercent, known: previous?.known ?? false };
      entry.promise = request<PageActionAccess>(`/pages/${encodeURIComponent(pageId)}?accessOnly=1`, { signal: controller.signal }, session)
        .then((access) => {
          if (retired || controller.signal.aborted) return false;
          const current = reads.get(pageId);
          if (current !== entry) return current?.promise ?? false;
          entry.editable = access.canEdit === true;
          entry.suggests = access.canSuggest === true;
          entry.manages = access.canManage === true;
          entry.busy = access.busy ?? "";
          entry.copyPercent = access.copyPercent;
          entry.known = true;
          changed();
          return entry.editable;
        }).catch(async (error) => {
          if (reads.get(pageId) === entry) {
            // A read that failed says nothing of the person's access: what
            // was known stands, and is read again when next asked. Never
            // read and the server out of reach, this device's copy of the
            // page says what they may do there until the server answers, so
            // a page that can be written offline takes new pages under it,
            // as Notion's (2026-11-07: its row had no "+" offline).
            if (entry.known && !refused(error)) entry.asked = -1;
            else if (!refused(error) && await copyAllowsEditing(pageId)) {
              if (reads.get(pageId) === entry) {
                entry.editable = true;
                entry.asked = -1;
                changed();
              }
            } else {
              reads.delete(pageId);
              if (entry.known) changed();
            }
          }
          throw error;
        }).finally(() => { controllers.delete(controller); });
      reads.set(pageId, entry);
      return entry.promise;
    },
    /** A new snapshot may have changed what the person may do: see the note above. */
    invalidate() {
      snapshot += 1;
      changed();
    },
    cancel() {
      retired = true;
      for (const controller of controllers) controller.abort();
      controllers.clear();
      reads.clear();
    },
  };
}

export type PageActionAccessReader = ReturnType<typeof createPageActionAccess>;
