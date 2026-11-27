import * as Y from "yjs";
import { openCopyRoom } from "@/collaboration";
import type { Session } from "@/types";
import { pageNotYetMade } from "./pagesNotYetMade";
import { isServerUnreachable } from "./serverReach";
import { listYgoRecoveryLocalEdits, noteYgoRecoveryServerHas } from "./ygoRecoveryLog";
import { serverAway } from "./serverAnswers";
import { serverWasRestored } from "./dataEpoch";
import { withPageClosedEverywhere } from "./pageTextOpenLock";
import { serverLacks } from "./serverTextLacks";
import { pagesWithKeptChangesNow } from "./pageTextBehindKeptChanges";

// Page text written on this device that the server may not have goes to it
// once it answers, though no tab has the page open: a tab that wrote a page's
// text while away and was closed before it could send left it only in the
// page's copy here, and everyone else saw the page's new blocks empty until
// the page was opened here again (2026-11-13). A page open in a tab here
// sends its own through its room; the others, among the pages this device
// edited (as sign-out counts them, lib/deviceSignOutData.ts) that the server
// wasn't seen to hold since their last edit here, go one at a time through a
// room of their own that closes once the server holds them. The outbox goes
// first (useNativeSyncStatus.ts), so text reaches the server after its blocks:
// a page whose changes the outbox still keeps — of any app's format, one this
// app leaves for a newer one too — keeps its text here until they went, and
// while the outbox can't be read (another tab keeps it) every page does; the
// tab that sends them sends the text after (lib/pageTextBehindKeptChanges.ts).

const MAX_PAGES_PER_TURN = 20;
// How long a page's room may take to sync, and the waits after it for the
// server to have stored what it took (2026-11-13: some 4 seconds on a
// NAS-class server).
const ROOM_SYNC_WAIT_MS = 20_000;
const STORED_WAITS_MS = [500, 1_500, 3_000, 6_000] as const;

// Opens the page's room with the copy (collaboration.ts, the one owner of
// rooms), so the server takes what it lacks of it, and settles once the room
// synced (true) or it can't (false). A room let go with the sign-in it was
// opened under, or as the server was restored, tells retired: what it took
// is not waited for, and the turn stops.
function syncThroughRoom(session: Session, pageId: string, document: Y.Doc) {
  return new Promise<{ synced: boolean; retired: () => boolean; close: () => void }>((resolve) => {
    let settled = false;
    let gone = false;
    const room = openCopyRoom(session, pageId, document, () => {
      gone = true;
      settle(false);
    });
    const settle = (synced: boolean) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      if (!synced) room.close();
      resolve({ synced, retired: () => gone, close: room.close });
    };
    const timer = window.setTimeout(() => settle(false), ROOM_SYNC_WAIT_MS);
    room.provider.on("sync", (synced: boolean) => { if (synced) settle(true); });
    // A room closed before it synced is tried again at a later turn.
    room.provider.on("connection-close", () => settle(false));
    room.provider.connect();
  });
}

const wait = (ms: number) => new Promise<void>((resolve) => { window.setTimeout(resolve, ms); });

// Copies this tab couldn't send, by where they stood: the server didn't take
// them (a page turned read-only, say, which the page lets go and says once
// opened) or refused to be asked (a page gone for the person). They aren't
// tried again from here until written to.
const notTaken = new Map<string, number>();

async function sendPage(session: Session, pageId: string, updates: Uint8Array[], through: number) {
  const userId = session.user.id;
  const copy = Y.mergeUpdates(updates);
  if (!(await serverLacks(session, pageId, copy))) {
    await noteYgoRecoveryServerHas(userId, pageId, through);
    return true;
  }
  const document = new Y.Doc();
  try {
    Y.applyUpdate(document, copy);
    const room = await syncThroughRoom(session, pageId, document);
    if (!room.synced) return null;
    try {
      for (const ms of STORED_WAITS_MS) {
        await wait(ms);
        if (room.retired()) return null;
        if (!(await serverLacks(session, pageId, copy))) {
          await noteYgoRecoveryServerHas(userId, pageId, through);
          return true;
        }
      }
      return false;
    } finally {
      room.close();
    }
  } finally {
    document.destroy();
  }
}

// One turn: "more" when pages were left for the next, "away" when the server
// stopped answering or the room was let go with the sign-in it went under.
async function sendTurn(session: Session): Promise<"done" | "more" | "away"> {
  const userId = session.user.id;
  let asked = 0;
  for (const page of await listYgoRecoveryLocalEdits(userId, { unsent: true })) {
    const key = JSON.stringify([userId, page.pageId]);
    if (notTaken.get(key) === page.through || page.updates.length === 0) continue;
    if (serverWasRestored() || serverAway()) return "away";
    if (asked >= MAX_PAGES_PER_TURN) return "more";
    if (await pageNotYetMade(page.pageId)) continue;
    // Read before each page, as the outbox goes on meanwhile.
    const held = await pagesWithKeptChangesNow();
    if (held === "every") return "done";
    if (held.has(page.pageId)) continue;
    try {
      // Left undefined when a tab here has the page open, which sends it.
      let sent = undefined as boolean | null | undefined;
      await withPageClosedEverywhere(userId, page.pageId, async () => {
        asked += 1;
        sent = await sendPage(session, page.pageId, page.updates, page.through);
      });
      if (sent === null) return "away";
      if (sent) notTaken.delete(key);
      else if (sent === false) notTaken.set(key, page.through);
    } catch (error) {
      if (isServerUnreachable(error)) return "away";
      notTaken.set(key, page.through);
    }
  }
  return "done";
}

const MORE_PAGES_AFTER_MS = 2_000;
const sending = new Map<string, { again: boolean; session: Session }>();

/**
 * Sends the page text this device kept that the server may lack, of pages
 * open in no tab here and with no change of theirs kept here; once at a time
 * per person, again after a turn under way when asked meanwhile.
 */
export function sendKeptPageText(session: Session): void {
  const userId = session.user.id;
  const running = sending.get(userId);
  if (running) {
    running.again = true;
    running.session = session;
    return;
  }
  const turn = { again: false, session };
  sending.set(userId, turn);
  void (async () => {
    for (;;) {
      const ended = await sendTurn(turn.session).catch(() => "done" as const);
      if (ended === "more") await wait(MORE_PAGES_AFTER_MS);
      else if (!turn.again || ended === "away") break;
      turn.again = false;
    }
  })().finally(() => sending.delete(userId));
}
