import { useEffect, useRef, type MutableRefObject } from "react";
import * as Y from "yjs";
import { listYgoRecoveryLocalEdits } from "@/offline";
import type { Session } from "@/types";
import { ownLockFlip } from "./ownLockFlips";
import { usePageLockedForMe } from "./pageLockedForMe";
import { serverLacks } from "./serverTextLacks";

// How long a page that turned read-only waits for the server to store what
// it already took before asking what of this device's text it doesn't have.
const REFUSED_EDITS_SETTLE_MS = 1500;

/** Told, with the page as it was here, when text written here was let go; with the page's ID when its lock refused it. */
export type RefusedEditsHeard = (document: Y.Doc, lockedPageId?: string) => void;

/**
 * Lets a page's copy here go when the server takes none of the person's text
 * — the page turned read-only for them, or is shown locked for them — and
 * says what was let go (usePageCollaboration).
 */
export function useRefusedTextLetGo(input: {
  session: Session | null;
  pageId: string | null;
  document: Y.Doc | null;
  editable: boolean | undefined;
  held: boolean;
  heldRef: MutableRefObject<boolean>;
  isHeld: MutableRefObject<(() => Promise<boolean>) | undefined>;
  onRefusedEdits: MutableRefObject<RefusedEditsHeard | undefined>;
  onTurnedReadOnly: MutableRefObject<(() => void) | undefined>;
  /** Lets the copy here go: the page opens again from the server's. */
  letGo: () => void;
}) {
  const { session, pageId, document, editable, held, heldRef, isHeld, onRefusedEdits, onTurnedReadOnly } = input;
  const letGo = useRef(input.letGo);
  letGo.current = input.letGo;
  const wasEditable = useRef(false);
  // A page that turned read-only for the person, while open or since this
  // device last edited it, takes none of what they wrote: the server refuses
  // it. Their copy here is let go so the page shows what everyone else sees,
  // and they are told — that their changes were let go when the server lacks
  // any of them, text or deletions, or else that the page turned read-only.
  useEffect(() => {
    if (editable === true) wasEditable.current = true;
  }, [editable, pageId]);
  useEffect(() => {
    wasEditable.current = false;
  }, [pageId]);
  useEffect(() => {
    if (editable !== false || held || !session || !pageId || !document) return;
    let stopped = false;
    // Whether it was editable here stays known until this check finishes: a
    // re-run before then (a renewed session, a new copy) asks again.
    const timer = window.setTimeout(() => void (async () => {
      if (heldRef.current || await isHeld.current?.() || stopped) return;
      const turnedReadOnly = wasEditable.current;
      const edited = turnedReadOnly
        ? [Y.encodeStateAsUpdate(document)]
        : (await listYgoRecoveryLocalEdits(session.user.id)).find((page) => page.pageId === pageId)?.updates ?? [];
      if (stopped || edited.length === 0) return;
      const lacking = await serverLacks(session, pageId, Y.mergeUpdates(edited));
      if (stopped || heldRef.current) return;
      wasEditable.current = false;
      if (!lacking && !turnedReadOnly) return;
      if (lacking) onRefusedEdits.current?.(document);
      else onTurnedReadOnly.current?.();
      letGo.current();
    })().catch(() => {
      // The next turn to read-only asks again.
    }), REFUSED_EDITS_SETTLE_MS);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [editable, held, document, session, pageId, heldRef, isHeld, onRefusedEdits, onTurnedReadOnly]);

  // A page shown locked for the person (lib/pageLockedForMe.ts) keeps their
  // access, but its room takes none of their text (ygo_access_recheck.go):
  // what this device wrote and the server doesn't hold — typed while away
  // as someone locked it, say — is let go and offered to copy, as the lock's
  // refusal of kept changes is (refusedEditsNotice.ts). Only text this
  // device may not have sent is asked of the server, so a locked page opened
  // or locked here asks nothing; a lock this tab just changed held only what
  // its room had taken.
  const locked = usePageLockedForMe(pageId ?? "");
  useEffect(() => {
    if (!locked || editable !== true || held || !session || !pageId || !document || ownLockFlip(pageId)) return;
    let stopped = false;
    const timer = window.setTimeout(() => void (async () => {
      const unsent = (await listYgoRecoveryLocalEdits(session.user.id, { unsent: true })).find((page) => page.pageId === pageId)?.updates ?? [];
      if (stopped || heldRef.current || unsent.length === 0) return;
      const lacking = await serverLacks(session, pageId, Y.mergeUpdates(unsent));
      if (stopped || heldRef.current || !lacking) return;
      onRefusedEdits.current?.(document, pageId);
      letGo.current();
    })().catch(() => {
      // Asked again as the page shows locked again.
    }), REFUSED_EDITS_SETTLE_MS);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [locked, editable, held, document, session, pageId, heldRef, onRefusedEdits]);
}
