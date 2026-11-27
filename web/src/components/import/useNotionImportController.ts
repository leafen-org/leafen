import { api } from "@/api";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { newId } from "@/lib/ids";
import { createNotionImportActionIdentityOwner } from "@/lib/notionImportActionIdentity";
import { notionRequestFailureText, notionTokenRejected } from "@/lib/notionImportFailure";
import { useReadGeneration } from "@/lib/useReadGeneration";
import { forgetRememberedWorkspaceImportJob, readRememberedWorkspaceImportJob } from "@/lib/workspaceImportMemory";
import {
  nativeNotionImportStatuses,
  type NativeNotionImportCapabilities,
  type NativeNotionImportConnection,
  type NativeNotionImportJob,
} from "@/notionImportWire.generated";
import type { Session } from "@/types";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNotionImportScope } from "./useNotionImportScope";

export type NotionImportStep = "connect" | "scope" | "apply";

/**
 * How Notion's window was come to: by the link an agent gives for the token
 * ("agent": `import_screen`, `&via=agent`, and `leafen setup notion`), to put
 * a new token in for a run Notion stopped ("token"), or from Leafen's own
 * entries ("").
 */
export type NotionImportArrival = "" | "agent" | "token";

/** Whether an import job still goes: waiting its turn or running. */
export function notionImportJobGoing(job: Pick<NativeNotionImportJob, "status">): boolean {
  return job.status === nativeNotionImportStatuses.queued || job.status === nativeNotionImportStatuses.running;
}

export function useNotionImportController({
  active,
  onRecover,
  open,
  session,
  workspaceId,
}: {
  active: boolean;
  onRecover: () => void;
  open: boolean;
  session: Session;
  workspaceId: string;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const [step, setStep] = useState<NotionImportStep>("connect");
  const [capabilities, setCapabilities] = useState<NativeNotionImportCapabilities | null>(null);
  const [capabilityError, setCapabilityError] = useState("");
  const [token, setToken] = useState("");
  const [connection, setConnection] = useState<NativeNotionImportConnection | null>(null);
  // The person's latest saved Notion connection, to use without its token again.
  const [savedConnection, setSavedConnection] = useState<NativeNotionImportConnection | null>(null);
  // Notion refuses the saved connection's token (revoked or replaced since): it needs a new one.
  const [savedConnectionRejected, setSavedConnectionRejected] = useState(false);
  // Whether the saved connections were read: until then the window can't tell the token's step from the saved one.
  const [connectionsRead, setConnectionsRead] = useState(false);
  // The saved connection's check, which reads what it reaches at the top, while it still reads.
  const savedTopRead = useRef<{
    connectionId: string;
    read: ReturnType<typeof api.notionImportScope>;
    reading: boolean;
  } | null>(null);
  const [job, setJob] = useState<NativeNotionImportJob | null>(null);
  const [busy, setBusy] = useState(false);
  const [notionError, setNotionError] = useState("");
  const [actionIdentities] = useState(() => createNotionImportActionIdentityOwner(newId));
  // The roots' step: the list, the search, the roots chosen.
  const scope = useNotionImportScope({
    confirm: (chosen) => setConnection(chosen),
    open,
    refuse: (chosen) => { setConnection(null); setSavedConnection(chosen); setSavedConnectionRejected(true); setStep("connect"); },
    session,
    workspaceId,
  });
  const selectedRoot = scope.selectedRoots[0] ?? null;
  const selectedRootId = selectedRoot?.id ?? "";
  const beginCapabilityRead = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `notion-capability:${open ? "open" : "closed"}:${workspaceId}`,
  });
  const beginRecoveryRead = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `notion-recovery:${open ? "open" : "closed"}:${workspaceId}`,
  });
  const beginSavedCheck = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `notion-saved-check:${active ? "active" : "idle"}:${workspaceId}:${savedConnection?.id ?? ""}`,
  });
  const beginJobRead = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `notion-job:${open ? "open" : "closed"}:${workspaceId}:${job?.id ?? ""}:${job?.status ?? ""}`,
  });

  // Once Notion's window shows a saved connection, asks Notion whether it still takes its token, so a refused
  // one shows as needing a new token before anyone goes on with it; Notion not answering leaves it as it is.
  // The check reads what the connection reaches at the top: "옮길 항목 선택" pressed while it still reads
  // waits for that read (2026-11-13: the browser holds a second identical request until the first is
  // answered, so the roots came after two whole reads, 30–39 seconds on a NAS).
  useEffect(() => {
    if (!active || !savedConnection) return;
    const check = beginSavedCheck();
    const read = api.notionImportScope(session, workspaceId, savedConnection.id, "", { signal: check.signal });
    const checking = { connectionId: savedConnection.id, read, reading: true };
    savedTopRead.current = checking;
    void read.then(() => { checking.reading = false; }, (error) => {
      checking.reading = false;
      if (check.isCurrent() && notionTokenRejected(error)) setSavedConnectionRejected(true);
    });
  }, [active, savedConnection, beginSavedCheck, session, workspaceId]);

  useEffect(() => {
    if (!open) return;
    actionIdentities.reset();
    const capabilityRead = beginCapabilityRead();
    setStep("connect");
    setCapabilities(null);
    setCapabilityError("");
    setConnection(null);
    scope.resetScope();
    setJob(null);
    setBusy(false);
    setNotionError("");
    setSavedConnection(null);
    setSavedConnectionRejected(false);
    setConnectionsRead(false);
    api.notionImportConnections(session, workspaceId, "", { signal: capabilityRead.signal })
      .then((page) => {
        if (capabilityRead.isCurrent()) setSavedConnection(page.items.find((item) => item.active) ?? null);
      })
      .catch(() => undefined)
      .finally(() => {
        if (capabilityRead.isCurrent()) setConnectionsRead(true);
      });
    api.notionImportCapabilities(session, workspaceId, { signal: capabilityRead.signal })
      .then((next) => {
        if (capabilityRead.isCurrent()) setCapabilities(next);
      })
      .catch(() => {
        if (capabilityRead.isCurrent()) setCapabilityError(t("importDialog:cantStartImport"));
      });
    // An import job this browser remembered from before an import by hand
    // ran as a migration (2026-10-12) opens Notion's window at it only while
    // it still goes; one that ended is forgotten, so the window opens as it
    // does without one (2026-11-17: an ended job remembered there took the
    // import screen over each time it opened, saying the server handled it).
    // A read that fails leaves it for the next time.
    const recovery = readRememberedWorkspaceImportJob(session.user.id, workspaceId);
    if (recovery) {
      const recoveryRead = beginRecoveryRead();
      api.notionImportJob(session, workspaceId, recovery.jobId, { signal: recoveryRead.signal })
        .then((next) => {
          if (!recoveryRead.isCurrent()) return;
          if (!notionImportJobGoing(next)) {
            forgetRememberedWorkspaceImportJob(session.user.id, workspaceId);
            return;
          }
          onRecover();
          setStep("apply");
          setJob(next);
        })
        .catch(() => undefined);
    }
    return () => actionIdentities.reset();
  }, [
    actionIdentities,
    beginCapabilityRead,
    beginRecoveryRead,
    onRecover,
    open,
    session,
    t,
    workspaceId,
  ]);

  // The remembered job, once it ends, is forgotten.
  useEffect(() => {
    if (job && !notionImportJobGoing(job)) forgetRememberedWorkspaceImportJob(session.user.id, workspaceId);
  }, [job, session.user.id, workspaceId]);

  useEffect(() => {
    if (!active || !job || job.status === nativeNotionImportStatuses.completed
      || job.status === nativeNotionImportStatuses.canceled) return;
    const activeJob = job;
    const jobRead = beginJobRead();
    void waitForDurableJob({
      initial: activeJob,
      isSameJob: (next) => next.id === activeJob.id,
      isTerminal: (next) => next.status === nativeNotionImportStatuses.completed
        || next.status === nativeNotionImportStatuses.canceled
        || next.status === nativeNotionImportStatuses.failed,
      onProgress: (next) => {
        if (jobRead.isCurrent()) setJob(next);
      },
      read: () => api.notionImportJob(session, workspaceId, activeJob.id, { signal: jobRead.signal }),
      signal: jobRead.signal,
    }).catch(() => {
      if (jobRead.isCurrent()) setNotionError(t("importDialog:cantStartImport"));
    });
  }, [active, beginJobRead, job, session, t, workspaceId]);

  function setTokenValue(value: string) {
    actionIdentities.invalidate("connection");
    setToken(value);
    // A token typed again puts away what the last connection try said.
    setNotionError("");
  }

  /**
   * "옮길 항목 선택": the roots' step at once, which says it reads what the
   * connection reaches at the top until Notion gives the list (from the
   * saved connection's check while that still reads); a token Notion
   * refuses goes back to ask for a new one in its place.
   */
  function chooseRoots() {
    const chosen = connection ?? savedConnection;
    if (!chosen || busy) return;
    setStep("scope");
    // The roots' step says its own failures; what the connection's step said is put away.
    setNotionError("");
    const checking = savedTopRead.current?.connectionId === chosen.id && savedTopRead.current.reading
      ? savedTopRead.current.read
      : null;
    return scope.chooseScope(chosen, checking);
  }

  /**
   * Saves the token as the workspace's Notion connection: Leafen asks Notion
   * once whom the token is, and nothing more (2026-11-13: reading what it
   * reaches as well kept "토큰 저장" dim for 9–10 seconds on a NAS). The
   * window stays on the saved connection: the agent goes on from it by
   * itself, and by hand "옮길 항목 선택" reads the roots (chooseRoots). The
   * card says it was saved; no notice says it again.
   */
  async function connectNotion() {
    if (!token.trim() || busy) return;
    const connectionAction = actionIdentities.begin("connection");
    setBusy(true);
    setNotionError("");
    try {
      const next = await api.createNotionImportConnection(session, workspaceId, {
        requestId: connectionAction.requestId,
        token: token.trim(),
        label: t("importDialog:notionConnectionDefault"),
      }, { signal: connectionAction.signal });
      if (!connectionAction.isCurrent()) return;
      scope.resetScope();
      setConnection(next);
      setSavedConnectionRejected(false);
      setToken("");
    } catch (error) {
      if (connectionAction.isCurrent()) {
        setNotionError(notionRequestFailureText(t, error, t("importDialog:cantSaveConnection")));
      }
    } finally {
      if (connectionAction.isCurrent()) setBusy(false);
    }
  }

  return {
    busy: busy || scope.readingScope,
    capabilities,
    capabilityError,
    connectNotion,
    connection,
    connectionsRead,
    job,
    listQuery: scope.listQuery,
    loadMoreScope: scope.loadMoreScope,
    notionError,
    retryScope: scope.retryScope,
    roots: scope.roots,
    savedConnection,
    savedConnectionRejected,
    // "다른 토큰 사용": the token's field again, in place of the connection saved before or just now.
    forgetSavedConnection: () => { scope.resetScope(); setConnection(null); setSavedConnection(null); setSavedConnectionRejected(false); },
    chooseRoots,
    scopeFailure: scope.scopeFailure,
    scopeHasMore: scope.scopeHasMore,
    scopeQuery: scope.scopeQuery,
    searchScope: scope.searchScope,
    selectRoot: scope.selectRoot,
    selectedRoot,
    selectedRootId,
    selectedRoots: scope.selectedRoots,
    setStep,
    setTokenValue,
    step,
    token,
  };
}

export type NotionImportController = ReturnType<typeof useNotionImportController>;
