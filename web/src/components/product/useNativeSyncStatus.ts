import type { ProductStatusSink } from "@/lib/productStatus";
import { i18next } from "@/i18n";
import { useEffect, useMemo, useRef, useState } from "react";

import type { NativePageMutationBrowser } from "@/lib/nativePageMutationBrowser";
import { nativePageMutationFailure } from "@/lib/nativePageMutationFailure";
import { createNativePageMutationRecovery } from "@/lib/nativePageMutationRecovery";
import { isDeviceStoreLockWaitExceeded } from "@/lib/deviceStoreLock";
import { retryWhenServerMayAnswer } from "@/lib/serverReach";
import { onServerAnswersAgain, onServerAway, serverAway } from "@/lib/serverAnswers";
import { sendKeptPageText } from "@/lib/keptPageTextSend";
import { sendKeptTemplateDrafts } from "@/lib/templateDraftSend";
import {
  createNativeSyncStatusService,
  NATIVE_SYNC_STATUS_BOUNDS,
} from "@/lib/nativeSyncStatusService";
import type { Session } from "@/types";

import { useCommittedResourceDisposal } from "./useCommittedResourceDisposal";

// Offline to the badge is the server not answering the app (serverAnswers.ts).
const serverConnectivity = {
  away: serverAway,
  subscribe(handlers: { onOnline(): void; onOffline(): void }) {
    const stopAway = onServerAway(handlers.onOffline);
    const stopAnswers = onServerAnswersAgain(() => handlers.onOnline());
    return () => {
      stopAway();
      stopAnswers();
    };
  },
};

export function useNativeSyncStatus<TResult>({
  blocked,
  mutationOutbox,
  onStatus,
  refresh,
  session,
}: {
  blocked: boolean;
  mutationOutbox: NativePageMutationBrowser<TResult>;
  onStatus: ProductStatusSink;
  refresh(activeSession?: Session): Promise<unknown>;
  session: Session;
}) {
  const [degradedState, setDegradedState] = useState({
    sessionToken: session.token,
    userId: session.user.id,
    value: false,
  });
  const refreshRef = useRef(refresh);
  const statusRef = useRef(onStatus);
  const currentRef = useRef({
    mutationOutbox: mutationOutbox as NativePageMutationBrowser<TResult> | null,
    sessionToken: session.token,
    userId: session.user.id,
  });
  refreshRef.current = refresh;
  statusRef.current = onStatus;
  currentRef.current = {
    mutationOutbox,
    sessionToken: session.token,
    userId: session.user.id,
  };
  useEffect(() => {
    const context = { mutationOutbox, sessionToken: session.token, userId: session.user.id };
    currentRef.current = context;
    return () => {
      if (
        currentRef.current.mutationOutbox === context.mutationOutbox
        && currentRef.current.sessionToken === context.sessionToken
        && currentRef.current.userId === context.userId
      ) {
        currentRef.current = { mutationOutbox: null, sessionToken: "", userId: "" };
      }
    };
  }, [mutationOutbox, session.token, session.user.id]);

  const recovery = useMemo(() => {
    const context = {
      mutationOutbox,
      sessionToken: session.token,
      userId: session.user.id,
    };
    const isCurrent = () => (
      currentRef.current.mutationOutbox === context.mutationOutbox
      && currentRef.current.sessionToken === context.sessionToken
      && currentRef.current.userId === context.userId
    );
    const publishDegraded = (value: boolean) => setDegradedState({
      sessionToken: context.sessionToken,
      userId: context.userId,
      value,
    });
    return createNativePageMutationRecovery({
      isOnline: () => !serverAway(),
      migrateLegacy: () => mutationOutbox.migrateLegacy(),
      async onCommitted() {
        if (!isCurrent()) return;
        await refreshRef.current(session);
        if (isCurrent()) publishDegraded(false);
      },
      // Nothing waits here any more, whoever sent it: another tab may have
      // (2026-11-05: tabs that kept changes through a short outage said the
      // connection was lost for good once one of them had sent them). The
      // page text kept here goes after the block changes it was written with,
      // a page's only once none of them is kept (lib/keptPageTextSend.ts).
      onCaughtUp() {
        if (!isCurrent()) return;
        publishDegraded(false);
        sendKeptPageText(session);
        // A database template's changes kept here go too (templateDraftSend.ts).
        sendKeptTemplateDrafts(session);
      },
      onDeferred() {
        if (!isCurrent()) return;
        publishDegraded(true);
        statusRef.current(i18next.t("syncStatusBadge:status.offlineRetryPending"));
      },
      onError(error, stage) {
        // Another tab holding this device's store is no failure here: it
        // sends what it keeps, and this turn is taken again.
        if (!isCurrent() || isDeviceStoreLockWaitExceeded(error)) return;
        publishDegraded(true);
        const contextName = stage === "legacy"
          ? "pageMutation.legacy"
          : stage === "refresh"
            ? "pageMutation.refresh"
            : "pageMutation.replay";
        statusRef.current(nativePageMutationFailure(error, contextName));
      },
      replay: () => mutationOutbox.replay(),
    });
  }, [mutationOutbox, session]);
  useCommittedResourceDisposal(recovery);
  // A page change kept because the server couldn't be reached, though the
  // browser is online: try the outbox once, which shows the changes waiting.
  // One kept nowhere (no room on the device while another tab keeps its
  // store) is said so: it is on screen only, sent again until it lands.
  useEffect(() => mutationOutbox.subscribeGraphLane((event) => {
    if (event.kind === "kept" && !serverAway()) recovery.trigger();
    if (event.kind === "unsaved") statusRef.current(i18next.t("syncStatusBadge:unsavedChanges"), "error");
  }), [mutationOutbox, recovery]);

  const service = useMemo(() => createNativeSyncStatusService({
    bounds: NATIVE_SYNC_STATUS_BOUNDS,
    connectivity: serverConnectivity,
    sources: [{
      flushAllPending: () => recovery.flush(),
      readPendingCount: () => mutationOutbox.readPendingCount(),
      // The badge's "다시 시도" goes at once, past the pause (retryPacing.ts).
      retrySyncHealth: () => recovery.flush(true),
      subscribeChanges: (listener) => mutationOutbox.subscribeChanges(listener),
      subscribePending: (listener) => mutationOutbox.subscribePending(listener),
    }],
    userId: session.user.id,
  }), [mutationOutbox, recovery, session.user.id]);

  useEffect(() => {
    if (blocked) return;
    let current = true;
    void (async () => {
      try {
        await refreshRef.current(session);
      } catch (error) {
        if (current) statusRef.current(nativePageMutationFailure(error, "page.bootstrap"));
        return;
      }
      if (!current) return;
      await recovery.flush().catch(() => undefined);
    })();
    return () => { current = false; };
  }, [blocked, recovery, session]);

  const degraded = degradedState.sessionToken === session.token
    && degradedState.userId === session.user.id
    && degradedState.value;
  // What waits for the server goes once it may answer, without anyone asking,
  // and once it answers again what another tab kept goes too.
  useEffect(() => {
    if (!degraded) return;
    return retryWhenServerMayAnswer(() => recovery.trigger());
  }, [degraded, recovery]);
  useEffect(() => onServerAnswersAgain(() => recovery.trigger()), [recovery]);
  return { bounds: NATIVE_SYNC_STATUS_BOUNDS, degraded, service };
}
