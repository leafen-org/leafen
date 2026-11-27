import { newId } from "@/lib/ids";
import {
  deleteDatabaseRowPagePermanently,
  duplicateDatabaseRowPage,
  moveDatabaseRowPage,
  readDatabaseRowPage,
  restoreDatabaseRowPage,
  setDatabaseRowPageArchive,
  setDatabaseRowPageLock,
  setDatabaseRowPageLockForMe,
  setDatabaseRowPageFavorite,
  trashDatabaseRowPage,
} from "@/databaseRowPageApi";
import {
  type DatabaseRowPageActionInput,
  type DatabaseRowPageActionResponse,
  type DatabaseRowPagePresentationInput,
  type DatabaseRowPageSnapshot,
} from "@/databaseRowPageWire.generated";
import type { Session } from "@/types";
import { lastRead } from "./contentGraphCollection";
import { readGraphAhead } from "./graphReadAhead";
import { keepRowPage, rowPageInPlaceOf } from "./keptRowPage";
import { keptSentFor } from "./keptSentFor";
import { lockForMeShownFirst, withUnlockedForMe } from "./lockForMeShownFirst";
import { pageWritesSent } from "./pageKindChange";
import { sendRowPresentation } from "./rowValuesKept";

export type DatabaseRowPageOpenMode = "side" | "center" | "full";

export type DatabaseRowPageService = ReturnType<typeof createDatabaseRowPageService>;

function sessionIdentity(session: Session) {
  return session.sessionId ?? `${session.user.id}:${session.token}`;
}

function actionRequestId(action: string, rowId: string) {
  return `${action}:${rowId}:${newId()}`;
}

/**
 * Where a database page moves: another database, or out as a sub-page; a
 * page without an ID is the top of `teamspaceId`'s teamspace, or of the
 * mover's private pages when it is "" — with neither, of the place its
 * database is in.
 */
export type DatabaseRowMoveDestination = { kind: "database" | "page"; id: string; teamspaceId?: string };

function actionKey(action: string, rowId: string, target?: DatabaseRowMoveDestination, accepted: string[] = []) {
  const destination = target ? `${target.kind}:${target.id}:${target.teamspaceId ?? "\u0000"}` : "";
  return `${action}\u0000${rowId}\u0000${destination}\u0000${[...accepted].sort().join("\u0000")}`;
}

// A page target without an ID is the top of the place it names. A page chosen
// in the Move to menu takes the row out of its database, as Notion's does.
function moveTargetInput(target: DatabaseRowMoveDestination) {
  if (target.kind === "page") {
    if (target.id) return { targetPageId: target.id, leaveDatabase: true };
    return { targetTopLevel: true, leaveDatabase: true, ...(target.teamspaceId === undefined ? {} : { targetTeamspaceId: target.teamspaceId }) };
  }
  return { targetDataSourceId: target.id };
}

function isUncertainActionFailure(error: unknown) {
  return error instanceof Error
    && (error.name === "LeafenTransportError" || error.name === "AbortError");
}

/**
 * A row page's next window with what was read of it before. A change to the
 * row, its body or its database while it is read doesn't start the read over,
 * as a page's graph's doesn't (contentGraphCollection.ts): the window after it
 * reads the page as it is then, a block met twice (moved past the cursor)
 * counts as last read, the row and what may be done with it are as last read,
 * and the read keeps its first window's revisions, behind the change
 * (2026-11-14: a row page of 161 blocks opened while another person renamed a
 * row of its database every 60 ms failed six reads in a row and closed).
 */
function appendSnapshot(
  first: DatabaseRowPageSnapshot,
  next: DatabaseRowPageSnapshot,
) {
  if (next.row.id !== first.row.id) {
    throw new Error("Database row page continuation read another row.");
  }
  return {
    ...next,
    snapshotIdentity: first.snapshotIdentity,
    containerSnapshotRevision: Math.min(first.containerSnapshotRevision, next.containerSnapshotRevision),
    bodySnapshotRevision: Math.min(first.bodySnapshotRevision, next.bodySnapshotRevision),
    body: lastRead([...first.body, ...next.body], (block) => block.id),
  };
}

export function createDatabaseRowPageService(
  session: Session,
  publish: (snapshot: DatabaseRowPageSnapshot) => void,
) {
  const ownerSessionIdentity = sessionIdentity(session);
  // Every read is numbered; a row's newest read is the one that counts for it.
  let requestGeneration = 0;
  let disposed = false;
  const actionControllers = new Set<AbortController>();
  const actionInflight = new Map<string, Promise<DatabaseRowPageActionResponse | null>>();
  const pendingActionInputs = new Map<string, DatabaseRowPageActionInput>();
  // Reads by row: a row's read leaves another row's alone, as a newer read of
  // the same row supersedes an older one. A row's menu, a comment on it and
  // what its menu does read that row beside the opened row's read, which goes
  // on (2026-11-22: a right-click on another row, and its menu's favorite,
  // aborted the open peek's read and took its place, the peek drawn as being
  // read again). The view draws its opened row from that row's own read
  // (useRowPageReads).
  const reads = new Map<string, { controller: AbortController; promise: Promise<DatabaseRowPageSnapshot | null> }>();
  const newestRead = new Map<string, number>();
  const newestOf = (rowId: string) => newestRead.get(rowId) ?? 0;
  const alive = () => !disposed && ownerSessionIdentity === sessionIdentity(session);
  let currentSnapshot: DatabaseRowPageSnapshot | null = null;
  // The person's own unlock as last pressed, drawn over every read of its
  // page until the server has it, and the page as last drawn here.
  let held: { rowId: string; unlocked: boolean; press: object } | null = null;
  let lastShown: DatabaseRowPageSnapshot | null = null;
  const show = (snapshot: DatabaseRowPageSnapshot) => {
    lastShown = snapshot;
    publish(held?.rowId === snapshot.row.id ? withUnlockedForMe(snapshot, held.unlocked) : snapshot);
  };

  const currentGeneration = () => requestGeneration;

  async function readCompleteSnapshot(
    rowId: string,
    signal: AbortSignal,
    isCurrent: () => boolean,
  ) {
    // A row page's body is read window by window to its end, however long
    // (2026-10-13: past 1 MiB together it failed to open).
    let snapshot = await readDatabaseRowPage(session, rowId, { signal });
    if (!isCurrent()) return null;
    while (snapshot.hasMore) {
      if (!snapshot.nextCursor) throw new Error("Database row page continuation is missing.");
      const next = await readDatabaseRowPage(session, rowId, {
        cursor: snapshot.nextCursor,
        signal,
      });
      if (!isCurrent()) return null;
      snapshot = appendSnapshot(snapshot, next);
    }
    return snapshot;
  }

  function refresh(rowId: string) {
    const inflight = reads.get(rowId);
    if (inflight) return inflight.promise;
    requestGeneration += 1;
    const generation = requestGeneration;
    newestRead.set(rowId, generation);
    const controller = new AbortController();
    const isCurrent = () => !controller.signal.aborted && alive() && newestOf(rowId) === generation;
    const promise = readCompleteSnapshot(rowId, controller.signal, isCurrent)
      .then((snapshot) => {
        if (!snapshot || !isCurrent()) return null;
        currentSnapshot = snapshot;
        show(snapshot);
        keepRowPage(session, snapshot);
        return snapshot;
      }, async (error: unknown) => {
        // Without the server the page shows as kept here, and the read still
        // fails, so it is read again once the server answers (useRowPageRead).
        const kept = await rowPageInPlaceOf(session, rowId, error);
        if (kept && isCurrent()) show(kept);
        throw error;
      })
      .finally(() => {
        if (reads.get(rowId)?.promise === promise) reads.delete(rowId);
      });
    reads.set(rowId, { controller, promise });
    return promise;
  }

  async function setFavorite(rowId: string, favorite: boolean) {
    const snapshot = await refresh(rowId);
    if (!snapshot || !alive()) return null;
    const generation = newestOf(rowId);
    const controller = new AbortController();
    actionControllers.add(controller);
    try {
      await setDatabaseRowPageFavorite(session, rowId, {
        favorite,
        expectedAuthorityRevision: snapshot.row.authorityRevision,
      }, controller.signal);
    } finally {
      actionControllers.delete(controller);
    }
    if (newestOf(rowId) !== generation || !alive()) return null;
    currentSnapshot = { ...snapshot, row: { ...snapshot.row, favorite } };
    show(currentSnapshot);
    return currentSnapshot;
  }

  // Archiving changes the row's authority, so the row page is read again.
  async function setArchived(rowId: string, archived: boolean) {
    const snapshot = await refresh(rowId);
    if (!snapshot || !alive()) return null;
    const controller = new AbortController();
    actionControllers.add(controller);
    try {
      await setDatabaseRowPageArchive(session, rowId, {
        archived,
        expectedAuthorityRevision: snapshot.row.authorityRevision,
      }, controller.signal);
    } finally {
      actionControllers.delete(controller);
    }
    if (!alive()) return null;
    currentSnapshot = null;
    return refresh(rowId);
  }

  // An icon or cover shows on the open page at the press and is kept in the
  // outbox behind the row's other changes, its making among them, on the page
  // holding its database (rowValuesKept.ts); the page is read again once it went.
  async function setPresentation(rowId: string, change: Omit<DatabaseRowPagePresentationInput, "expectedAuthorityRevision">, pageId?: string | null) {
    const drawn = lastShown?.row.id === rowId ? lastShown : null;
    if (drawn) show({ ...drawn, row: { ...drawn.row, ...change } as DatabaseRowPageSnapshot["row"] });
    const controller = new AbortController();
    actionControllers.add(controller);
    try {
      await sendRowPresentation(session, rowId, change, pageId ?? drawn?.row.containerPageId, controller.signal);
    } finally {
      actionControllers.delete(controller);
    }
    if (!alive()) return null;
    currentSnapshot = null;
    return refresh(rowId);
  }

  // Locking the page for everyone, or unlocking it only for oneself, reads
  // the page again with its new lock.
  async function setLock(rowId: string, locked: boolean) {
    const snapshot = await refresh(rowId);
    if (!snapshot || !alive()) return null;
    await setDatabaseRowPageLock(session, rowId, { locked, expectedAuthorityRevision: snapshot.row.authorityRevision });
    if (!alive()) return null;
    currentSnapshot = null;
    return refresh(rowId);
  }

  // Unlocking the page for oneself, or locking it again, shows at the press
  // over `shown`, the page as the view draws it (lockForMeShownFirst.ts); the
  // page is read again once the server has it, after any read begun before.
  async function setLockForMe(rowId: string, unlocked: boolean, shown?: DatabaseRowPageSnapshot | null) {
    const press = {};
    if (shown?.row.id === rowId) lastShown = shown;
    try {
      const done = await lockForMeShownFirst(rowId, unlocked, {
        show: (value) => {
          held = { rowId, unlocked: value, press };
          if (lastShown?.row.id === rowId) show(lastShown);
        },
        write: () => setDatabaseRowPageLockForMe(session, rowId, { unlocked }),
      });
      if (!done || !alive()) return null;
      const going = reads.get(rowId);
      if (going) await going.promise.catch(() => null);
      currentSnapshot = null;
      return await refresh(rowId);
    } finally {
      if (held?.press === press) held = null;
    }
  }

  // A move previews the values the target cannot hold; the mover accepts
  // dropping them by naming their properties in the move itself.
  async function previewMove(rowId: string, target: DatabaseRowMoveDestination) {
    const snapshot = await refresh(rowId);
    if (!snapshot || !alive()) throw new Error("Database row move preview was superseded.");
    const controller = new AbortController();
    actionControllers.add(controller);
    try {
      const result = await moveDatabaseRowPage(session, rowId, {
        requestId: actionRequestId("move-preview", rowId),
        expectedAuthorityRevision: snapshot.row.authorityRevision,
        ...moveTargetInput(target),
        preview: true,
      }, controller.signal);
      return result.droppedProperties ?? [];
    } finally {
      actionControllers.delete(controller);
    }
  }

  function runAction(
    rowId: string,
    action: "duplicate" | "move" | "trash" | "restore" | "deletePermanently",
    target?: DatabaseRowMoveDestination,
    acceptDroppedPropertyIds: string[] = [],
  ) {
    const key = actionKey(action, rowId, target, acceptDroppedPropertyIds);
    const inflight = actionInflight.get(key);
    if (inflight) return inflight;
    const request = (async () => {
      // Moved out as a page, or into another database, a wiki's page goes once its title typed before has (pageKindChange.ts).
      if (action === "move") await pageWritesSent();
      let snapshot = await refresh(rowId);
      // A copy is of the row as the server holds it, once what this device wrote of it went (keptSentFor).
      if (snapshot && action === "duplicate" && await keptSentFor(session, snapshot.row.containerPageId)) snapshot = await refresh(rowId);
      if (!snapshot || !alive()) return null;
      const generation = newestOf(rowId);
      const controller = new AbortController();
      actionControllers.add(controller);
      const input = pendingActionInputs.get(key) ?? {
        requestId: actionRequestId(action, rowId),
        expectedAuthorityRevision: snapshot.row.authorityRevision,
        ...(target ? moveTargetInput(target) : {}),
        ...(acceptDroppedPropertyIds.length > 0 ? { acceptDroppedPropertyIds } : {}),
      };
      pendingActionInputs.set(key, input);
      const operation = {
        duplicate: duplicateDatabaseRowPage,
        move: moveDatabaseRowPage,
        trash: trashDatabaseRowPage,
        restore: restoreDatabaseRowPage,
        deletePermanently: deleteDatabaseRowPagePermanently,
      }[action];
      try {
        const result = await operation(session, rowId, input, controller.signal);
        pendingActionInputs.delete(key);
        return newestOf(rowId) === generation && alive() ? result : null;
      } catch (error) {
        if (!isUncertainActionFailure(error)) pendingActionInputs.delete(key);
        throw error;
      } finally {
        actionControllers.delete(controller);
      }
    })();
    actionInflight.set(key, request);
    const clear = () => {
      if (actionInflight.get(key) === request) actionInflight.delete(key);
    };
    request.then(clear, clear);
    return request;
  }

  return Object.freeze({
    identity: ownerSessionIdentity,
    currentGeneration,
    refresh,
    /** Starts reading the row page's body as its page is read (graphReadAhead.ts). */
    readBodyAhead: (rowId: string) => readGraphAhead(session, rowId),
    setFavorite,
    setArchived,
    setPresentation,
    setLock,
    setLockForMe,
    duplicate: (rowId: string) => runAction(rowId, "duplicate"),
    previewMove,
    move: (rowId: string, target: DatabaseRowMoveDestination, acceptDroppedPropertyIds: string[] = []) => (
      runAction(rowId, "move", target, acceptDroppedPropertyIds)
    ),
    // Trash state changes the row page's revision, so it is read again.
    trash: async (rowId: string) => {
      const result = await runAction(rowId, "trash");
      if (currentSnapshot?.row.id === rowId) currentSnapshot = null;
      return result;
    },
    restore: async (rowId: string) => {
      const result = await runAction(rowId, "restore");
      if (currentSnapshot?.row.id === rowId) currentSnapshot = null;
      if (newestRead.has(rowId)) await refresh(rowId);
      return result;
    },
    deletePermanently: (rowId: string) => runAction(rowId, "deletePermanently"),
    dispose() {
      disposed = true;
      requestGeneration += 1;
      for (const { controller } of reads.values()) controller.abort();
      reads.clear();
      newestRead.clear();
      for (const controller of actionControllers) controller.abort();
      actionControllers.clear();
      actionInflight.clear();
      pendingActionInputs.clear();
      currentSnapshot = null;
      held = null;
      lastShown = null;
    },
  });
}
