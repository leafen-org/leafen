import {
  mutateDatabaseToolbarConfig,
  readDatabaseToolbarConfig,
} from "@/databaseToolbarConfigApi";
import {
  type DatabaseToolbarConfigMetadataResponse,
  type DatabaseToolbarConfigMutationInput,
  type DatabaseToolbarConfigMutationResponse,
} from "@/databaseToolbarConfigWire.generated";
import type { Session } from "@/types";

function sessionIdentity(session: Session) {
  return session.sessionId ?? `${session.user.id}:${session.token}`;
}

function configKey(pageId: string, dataSourceId: string) {
  return `${pageId}\u0000${dataSourceId}`;
}

function sameSnapshot(
  first: DatabaseToolbarConfigMetadataResponse,
  next: DatabaseToolbarConfigMetadataResponse,
) {
  return next.pageId === first.pageId
    && next.dataSourceId === first.dataSourceId
    && next.snapshotIdentity === first.snapshotIdentity
    && next.workspaceContentRevision === first.workspaceContentRevision
    && next.configurationRevision === first.configurationRevision;
}

function appendMetadataPage(
  first: DatabaseToolbarConfigMetadataResponse,
  next: DatabaseToolbarConfigMetadataResponse,
) {
  if (!sameSnapshot(first, next)) {
    throw new Error("Database toolbar configuration continuation changed snapshot authority.");
  }
  return {
    ...next,
    taskFeatures: first.taskFeatures,
    templates: [...(first.templates ?? []), ...(next.templates ?? [])],
  };
}

/**
 * A database's toolbar settings and templates, read and changed for one
 * person. A read of the same settings again gives up only on the reads
 * before it; a change sent is never given up by one. A row is not made
 * here: New with a template makes its row in the view and fills it from the
 * template behind (useDefaultTemplateRows.ts fillRowFromTemplate), so no
 * read of these settings can stop a row being made.
 */
export function createDatabaseToolbarConfigService(
  session: Session,
  publish: (snapshot: DatabaseToolbarConfigMetadataResponse) => void,
) {
  const ownerSessionIdentity = sessionIdentity(session);
  let generation = 0;
  let activeKey = "";
  let activeController: AbortController | null = null;
  let currentSnapshot: DatabaseToolbarConfigMetadataResponse | null = null;
  let refreshInflight: { key: string; answered: number; promise: Promise<DatabaseToolbarConfigMetadataResponse | null> } | null = null;
  // How many changes the server has answered: a read begun before the last
  // answer may not hold it, so a read asked for after it is a new one.
  let answered = 0;
  // A change sent stays sent while its settings are read again: only
  // leaving these settings (dispose) gives up on its answer.
  const mutationControllers = new Set<AbortController>();
  let continuationInflight: Promise<DatabaseToolbarConfigMetadataResponse | null> | null = null;
  const mutationInflight = new Map<string, Promise<DatabaseToolbarConfigMutationResponse | null>>();
  // The reads that follow a read (loadMore): a new read gives them up.
  const childControllers = new Set<AbortController>();

  const isCurrent = (key: string, expectedGeneration: number, signal?: AbortSignal) => (
    !signal?.aborted
    && ownerSessionIdentity === sessionIdentity(session)
    && activeKey === key
    && generation === expectedGeneration
  );

  function refresh(pageId: string, dataSourceId: string) {
    const key = configKey(pageId, dataSourceId);
    if (refreshInflight?.key === key && refreshInflight.answered === answered) return refreshInflight.promise;
    generation += 1;
    const expectedGeneration = generation;
    activeKey = key;
    activeController?.abort();
    for (const controller of childControllers) controller.abort();
    childControllers.clear();
    continuationInflight = null;
    const controller = new AbortController();
    activeController = controller;
    const promise = readDatabaseToolbarConfig(session, pageId, dataSourceId, {
      signal: controller.signal,
    }).then((snapshot) => {
      if (!isCurrent(key, expectedGeneration, controller.signal)) return null;
      currentSnapshot = snapshot;
      publish(snapshot);
      return snapshot;
    }).finally(() => {
      if (refreshInflight?.promise === promise) refreshInflight = null;
      if (activeController === controller) activeController = null;
    });
    refreshInflight = { key, answered, promise };
    return promise;
  }

  function loadMore() {
    if (continuationInflight) return continuationInflight;
    const first = currentSnapshot;
    if (!first?.hasMore || !first.nextCursor) return Promise.resolve(first);
    const key = configKey(first.pageId, first.dataSourceId);
    const expectedGeneration = generation;
    const controller = new AbortController();
    childControllers.add(controller);
    const promise = readDatabaseToolbarConfig(session, first.pageId, first.dataSourceId, {
      cursor: first.nextCursor,
      signal: controller.signal,
    }).then((next) => {
      if (!isCurrent(key, expectedGeneration, controller.signal) || currentSnapshot !== first) return null;
      const snapshot = appendMetadataPage(first, next);
      currentSnapshot = snapshot;
      publish(snapshot);
      return snapshot;
    }).finally(() => {
      childControllers.delete(controller);
      if (continuationInflight === promise) continuationInflight = null;
    });
    continuationInflight = promise;
    return promise;
  }

  function mutate(
    pageId: string,
    dataSourceId: string,
    input: DatabaseToolbarConfigMutationInput,
  ) {
    const key = configKey(pageId, dataSourceId);
    const existing = mutationInflight.get(input.requestId);
    if (existing) return existing;
    const controller = new AbortController();
    mutationControllers.add(controller);
    // Each operation names the revision of what it changes, which the server
    // holds it to: content changed elsewhere in the workspace since these
    // settings were read doesn't refuse it, and a change to the same setting
    // does (database_toolbar_config_mutation.go).
    const promise = mutateDatabaseToolbarConfig(
      session,
      pageId,
      dataSourceId,
      input,
      controller.signal,
    ).then((response) => {
      answered += 1;
      return !controller.signal.aborted && ownerSessionIdentity === sessionIdentity(session) && activeKey === key ? response : null;
    }).finally(() => {
      mutationControllers.delete(controller);
      if (mutationInflight.get(input.requestId) === promise) mutationInflight.delete(input.requestId);
    });
    mutationInflight.set(input.requestId, promise);
    return promise;
  }

  return Object.freeze({
    identity: ownerSessionIdentity,
    refresh,
    loadMore,
    mutate,
    snapshot: () => currentSnapshot,
    dispose() {
      generation += 1;
      activeKey = "";
      activeController?.abort();
      activeController = null;
      for (const controller of [...childControllers, ...mutationControllers]) controller.abort();
      childControllers.clear();
      mutationControllers.clear();
      mutationInflight.clear();
      continuationInflight = null;
      refreshInflight = null;
      currentSnapshot = null;
    },
  });
}
