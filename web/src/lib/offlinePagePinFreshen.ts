import * as Y from "yjs";
import { request, requestBinaryResponse } from "@/apiTransport";
import type { Session } from "@/types";
import {
  changesShowOtherSources,
  collectAuthenticatedContentGraph,
  collectContentGraphChanges,
  withChangedRows,
} from "./authenticatedContentGraph";
import { cacheBehind, pageGraphCache } from "./cacheStaleness";
import { localFirstProductCache } from "./localFirstProductCache";
import { serverIdentity } from "./serverOrigin";
import { appendYgoRecoveryUpdate, compactYgoRecoveryUpdates, replayYgoRecoveryUpdates } from "./ygoRecoveryLog";
import { serverAway } from "./serverAnswers";
import { isPageBusyRefusal } from "./pageBusy";

// A page made available offline is kept up to date while the device is
// connected, as Notion keeps its offline pages in the background: edits made
// on another device or by another person are there offline though the page
// wasn't opened here since (2026-11-11: they were missing until it was). While
// a screen is in view and online it asks every few seconds, for all its kept
// pages in one request, where each page's changes stand, and reads again only
// the pages that moved: their blocks and databases as the changes after the
// copy kept here, their text as what its copy lacks. Opening a page asks
// nothing more. However many pages are kept, one request asks for at most
// MAX_MARKED_PAGES of them in turn, and one turn reads at most
// MAX_FRESHENED_PAGES of those that moved, one after another.

const FRESHEN_EVERY_MS = 10_000;
const MAX_MARKED_PAGES = 100;
const MAX_FRESHENED_PAGES = 5;

type PageMark = { graph: number; text: number };

// The text marks each kept page's copy was last brought to, by person.
const textMarks = new Map<string, Map<string, number>>();
// Where the last turn's pages ended, by person, for the next to go on from.
const lastMarked = new Map<string, string>();
const walking = new Map<string, Promise<void>>();

async function freshenGraph(session: Session, pageId: string, mark: PageMark, keptRevision: number) {
  if (mark.graph <= keptRevision || cacheBehind(session.user.id, pageGraphCache(pageId))) return;
  const identity = { key: pageId, principalId: session.user.id, serverKey: serverIdentity() };
  const kept = await localFirstProductCache.graph.readCache(identity);
  if (!kept) return;
  const read = await collectContentGraphChanges(session, pageId, kept.value.graphRevision);
  if (!read) return;
  let { graph, snapshotRevision } = read;
  if (read.changes && changesShowOtherSources(kept.value.graph, read.changes)) {
    const whole = await collectAuthenticatedContentGraph(session, pageId, null);
    if (!whole) return;
    ({ graph, snapshotRevision } = whole);
  } else if (read.changes) {
    graph = withChangedRows(kept.value.graph, read.changes);
  }
  if (!graph) return;
  await localFirstProductCache.graph.writeCache({
    ...identity,
    complete: true,
    version: 1,
    value: {
      bootstrapRevision: kept.value.bootstrapRevision,
      graphRevision: snapshotRevision,
      graph,
      canEdit: read.canEdit,
      canSuggest: read.canSuggest,
    },
  });
}

// The page's text copy here takes what the server's has that it lacks, as
// an update from elsewhere; a page with no copy here is read when it opens.
async function freshenText(session: Session, pageId: string) {
  const userId = session.user.id;
  const document = new Y.Doc();
  try {
    let copied = false;
    await replayYgoRecoveryUpdates(userId, pageId, (update) => {
      copied = true;
      Y.applyUpdate(document, update);
    });
    if (!copied) return;
    const response = await requestBinaryResponse(
      `/collaboration-state/${encodeURIComponent(pageId)}/missing`,
      session,
      {
        method: "POST",
        body: Y.encodeStateVector(document) as BodyInit,
        headers: { "content-type": "application/octet-stream" },
      },
    );
    const missing = new Uint8Array(await response.arrayBuffer());
    const before = Y.snapshot(document);
    Y.applyUpdate(document, missing);
    if (Y.equalSnapshots(before, Y.snapshot(document))) return;
    if (await appendYgoRecoveryUpdate(userId, pageId, missing, false)) {
      await compactYgoRecoveryUpdates(userId, pageId, (updates) => Y.mergeUpdates(updates));
    }
  } finally {
    document.destroy();
  }
}

async function freshenTurn(session: Session) {
  const userId = session.user.id;
  const serverKey = serverIdentity();
  const pinned = Object.keys(await localFirstProductCache.pins.getPins(userId)).sort();
  // A page's databases are kept with it; pages are what has a copy of its own.
  const kept = new Map<string, number>();
  const after = pinned.findIndex((pageId) => pageId > (lastMarked.get(userId) ?? ""));
  const start = after < 0 ? 0 : after;
  for (const pageId of [...pinned.slice(start), ...pinned.slice(0, start)]) {
    if (kept.size >= MAX_MARKED_PAGES) break;
    const revision = await localFirstProductCache.graph.keptRevision({ key: pageId, principalId: userId, serverKey });
    if (revision !== null) kept.set(pageId, revision);
  }
  if (kept.size === 0) return;
  const pageIds = [...kept.keys()];
  lastMarked.set(userId, pageIds[pageIds.length - 1]!);
  const { marks } = await request<{ marks: Record<string, PageMark> }>(
    "/offline-page-marks",
    { method: "POST", body: JSON.stringify({ pageIds }) },
    session,
  );
  const textSeen = textMarks.get(userId) ?? new Map<string, number>();
  textMarks.set(userId, textSeen);
  let freshened = 0;
  for (const pageId of pageIds) {
    const mark = marks[pageId];
    if (!mark || freshened >= MAX_FRESHENED_PAGES) continue;
    const graphMoved = mark.graph > kept.get(pageId)!;
    const textMoved = textSeen.get(pageId) !== mark.text;
    if (!graphMoved && !textMoved) continue;
    freshened += 1;
    try {
      if (graphMoved) await freshenGraph(session, pageId, mark, kept.get(pageId)!);
      if (textMoved) {
        await freshenText(session, pageId);
        textSeen.set(pageId, mark.text);
      }
    } catch (error) {
      // A page an operation holds is brought up to date at a later turn, and
      // the pages after it now (lib/pageBusy.ts): its refusal ended the turn.
      if (!isPageBusyRefusal(error)) throw error;
      continue;
    }
    // Pictures and files the page shows now are kept too.
    await localFirstProductCache.pins.warm(userId, pageId).catch(() => undefined);
  }
}

/** Brings the person's kept pages up to date once, unless a turn is under way. */
export function freshenPinnedPages(session: Session): Promise<void> {
  const userId = session.user.id;
  const running = walking.get(userId);
  if (running) return running;
  const turn = freshenTurn(session).catch(() => undefined).finally(() => walking.delete(userId));
  walking.set(userId, turn);
  return turn;
}

// The screens following each person's kept pages here; one turn of asking
// serves them all.
const followers = new Map<string, { count: number; session: Session; stop: () => void }>();

/**
 * Keeps the person's pages made available offline up to date while this
 * screen is in view and online; returns what stops it.
 */
export function followPinnedPages(session: Session): () => void {
  const userId = session.user.id;
  const following = followers.get(userId);
  if (following) {
    following.count += 1;
    following.session = session;
  } else {
    const freshen = () => {
      const current = followers.get(userId);
      if (!current || document.visibilityState !== "visible" || serverAway()) return;
      void freshenPinnedPages(current.session);
    };
    const timer = window.setInterval(freshen, FRESHEN_EVERY_MS);
    window.addEventListener("online", freshen);
    document.addEventListener("visibilitychange", freshen);
    followers.set(userId, {
      count: 1,
      session,
      stop: () => {
        window.clearInterval(timer);
        window.removeEventListener("online", freshen);
        document.removeEventListener("visibilitychange", freshen);
      },
    });
    freshen();
  }
  let stopped = false;
  return () => {
    const current = followers.get(userId);
    if (stopped || !current) return;
    stopped = true;
    current.count -= 1;
    if (current.count > 0) return;
    current.stop();
    followers.delete(userId);
  };
}
