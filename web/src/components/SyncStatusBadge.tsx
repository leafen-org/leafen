import { X } from "@/icons/leafen";
"use client";

import { startTransition, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  admitSyncPendingCount,
  admitSyncStatusUserId,
  syncStatusFailureMessage,
  type SyncStatusBounds,
  type SyncStatusFailureStage,
  type SyncStatusService,
} from "@/lib/syncStatusView";

import { isDeviceStoreLockWaitExceeded } from "@/lib/deviceStoreLock";
import { askAboutRestoredServer, onRestoredQuestionShown, onServerRestored, restoredQuestionShown, serverWasRestored } from "@/lib/dataEpoch";
import styles from "./SyncStatusBadge.module.css";
import { troubleLastedAlready } from "@/lib/connectionQuiet";
import { showOfflinePill, useOfflineShownInPlace } from "@/lib/offlinePill";
import { toastPlacementTarget } from "@/lib/toastStackView";

// Connection trouble shows only once it has lasted this long; a blip that
// heals sooner shows nothing. Once shown it stays a moment, so it doesn't
// flicker. Changes are kept on this device meanwhile, so waiting loses
// nothing.
const WATCH_MS = 10_000;
const MIN_SHOWN_MS = 3_000;

/**
 * One calm notice for a lost connection, whether the browser is offline, the
 * server can't be reached, live editing dropped or sending kept failing. A
 * lost connection shows as Notion's, "오프라인" in the top bar (offlinePill.ts),
 * where the toasts at the window's foot never cover it (2026-11-12: one over
 * a window covered this notice there); with no bar drawn, and for what only
 * Leafen says (a send failing with the connection up, a server restored,
 * moved or paused), it is a notice at the window's foot: the changes waiting
 * here, "다시 시도" and a close button. Closed, it stays away until the
 * connection comes back and is lost again.
 */
export default function SyncStatusBadge({
  bounds,
  degraded,
  realtimeLost,
  readOnly = false,
  serverRefuses = null,
  service,
  userId,
}: {
  bounds: SyncStatusBounds;
  degraded: boolean;
  realtimeLost: boolean;
  /** The page shown can only be read here: there is nothing of its to send. */
  readOnly?: boolean;
  /** The server answers but takes no changes, as the band above says: this is no lost connection. */
  serverRefuses?: "moved" | "paused" | null;
  service: SyncStatusService;
  userId: string | null;
}) {
  const { t } = useTranslation(["syncStatusBadge", "common"]);
  const [offline, setOffline] = useState(() => service.initialOffline());
  const [pending, setPending] = useState(0);
  const [failure, setFailure] = useState("");
  const [shown, setShown] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const shownAt = useRef(0);
  // A server whose data was replaced refuses this device's changes on
  // purpose (dataEpoch.ts): what stopped is said as that, not as a lost
  // connection (2026-10-16: both showed while the server was up), and the
  // notice put off can be brought back from here.
  const [restored, setRestored] = useState(() => serverWasRestored());
  useEffect(() => onServerRestored(() => setRestored(true)), []);
  // While the question about it is on screen the notice waits behind it.
  const [questionShown, setQuestionShown] = useState(restoredQuestionShown);
  useEffect(() => onRestoredQuestionShown(setQuestionShown), []);
  const connectionLost = offline || degraded || realtimeLost;
  // A server that was restored refuses this device's changes everywhere, not
  // only where a page's live connection is refused: the notice stays on home
  // and in settings too (2026-11-06: after "나중에" it went once off the page).
  const trouble = connectionLost || Boolean(failure) || restored;

  const reportFailure = useCallback((
    error: unknown,
    stage: SyncStatusFailureStage,
  ) => {
    // Another tab holding this device's store is no failure here: it sends
    // what it keeps, and this tab's changes go straight to the server.
    if (isDeviceStoreLockWaitExceeded(error)) return;
    setFailure(syncStatusFailureMessage(
      error,
      stage,
    ));
  }, [t]);

  // Only trouble seen counts: a tab in the background whose connection
  // dropped there and comes back on its own once it is in front again says
  // nothing (2026-11-06: brought forward after minutes, it showed the notice
  // for a few seconds; Notion's shows none). It waits its time once seen.
  const [visible, setVisible] = useState(() => typeof document === "undefined" || document.visibilityState !== "hidden");
  useEffect(() => {
    const update = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);

  useEffect(() => {
    if (trouble) {
      if (shown || !visible) return;
      const timer = setTimeout(() => {
        shownAt.current = Date.now();
        setShown(true);
      }, Math.max(0, WATCH_MS - troubleLastedAlready()));
      return () => clearTimeout(timer);
    }
    setDismissed(false);
    if (!shown) return;
    // One put away stays away: it doesn't come back for its moment.
    if (dismissed) {
      setShown(false);
      return;
    }
    const timer = setTimeout(() => setShown(false), Math.max(0, shownAt.current + MIN_SHOWN_MS - Date.now()));
    return () => clearTimeout(timer);
  }, [dismissed, shown, trouble, visible]);

  useEffect(() => service.subscribeConnectivity({
    onOnline: () => {
      setOffline(false);
      void service.flushAllPending()
        .then(() => setFailure(""))
        .catch((error: unknown) => reportFailure(error, "flush"));
    },
    onOffline: () => setOffline(true),
  }), [reportFailure, service]);

  useEffect(() => {
    if (!userId) {
      setPending(0);
      return;
    }
    let activeUserId: string;
    try {
      activeUserId = admitSyncStatusUserId(userId, bounds);
    } catch (error) {
      reportFailure(error, "pending.read");
      return;
    }
    let mounted = true;
    let dirty = true;
    let trailing = false;
    let inFlight: Promise<void> | null = null;
    const isVisible = () => {
      try {
        return service.isVisible();
      } catch (error) {
        reportFailure(error, "pending.read");
        return false;
      }
    };
    const reconcile = () => {
      if (!mounted) return;
      if (!isVisible()) {
        dirty = true;
        return;
      }
      if (inFlight) {
        trailing = true;
        return;
      }
      dirty = false;
      const current = service.readPendingCount(activeUserId)
        .then((count) => {
          if (!mounted) return;
          // Drawn after the frame that shows what was changed (below).
          startTransition(() => { setPending(admitSyncPendingCount(count, bounds)); setFailure(""); });
        })
        .catch((error: unknown) => {
          if (mounted) reportFailure(error, "pending.read");
        });
      inFlight = current;
      void current.finally(() => {
        if (inFlight !== current) return;
        inFlight = null;
        if (!mounted || (!trailing && !dirty)) return;
        trailing = false;
        reconcile();
      });
    };
    const requestReconcile = () => {
      if (!isVisible()) {
        dirty = true;
        return;
      }
      reconcile();
    };
    const unsubscribePending = service.subscribePending((
      changedUserId,
      pendingCount,
    ) => {
      if (!mounted || changedUserId !== activeUserId) return;
      try {
        const admittedPending = admitSyncPendingCount(pendingCount, bounds);
        if (!isVisible()) {
          dirty = true;
          return;
        }
        // A count told inside a press (a row made, a line typed) is drawn after
        // the frame that shows the press, not in it as the press's own: three
        // more drawings in a table's "+ 새 페이지" (2026-11-15).
        startTransition(() => setPending(admittedPending));
        requestReconcile();
      } catch (error) {
        reportFailure(error, "pending.read");
      }
    });
    const unsubscribeChanges = service.subscribeChanges(requestReconcile);
    const unsubscribeVisibility = service.subscribeVisibility(() => {
      if (isVisible()) requestReconcile();
      else dirty = true;
    });
    requestReconcile();
    return () => {
      mounted = false;
      unsubscribePending();
      unsubscribeChanges();
      unsubscribeVisibility();
    };
  }, [bounds, reportFailure, service, userId]);

  const retry = useCallback(() => {
    setFailure("");
    setRetrying(true);
    // Long enough to be seen: the notice reads the same when it fails.
    const shortest = new Promise((resolve) => { window.setTimeout(resolve, 800); });
    void Promise.all([service.retrySyncHealth().catch((error: unknown) => reportFailure(error, "retry")), shortest])
      .finally(() => setRetrying(false));
  }, [reportFailure, service]);

  // The band says what stopped; here only what that means for changes waiting.
  const refused = !offline && !restored ? serverRefuses : null;
  // A connection lost, not a refusal, a restored server or a failure with
  // the connection up: the bar's "오프라인" says it where a bar shows it, as
  // a phone's open drawer does with its cloud.
  const lostOnly = connectionLost && !(restored && !offline) && !refused;
  // The trouble over, what was shown stays its moment (MIN_SHOWN_MS) as it
  // was, where it was: the bar's "오프라인" was drawn for those last frames
  // as the notice at the window's foot, which flashed as the connection came
  // back (2026-11-19: up to 0.9 s, the bar's mark gone; and as a row's peek
  // opened, whose page's room ended a trouble that was only the room's).
  const [lostAsShown, setLostAsShown] = useState(false);
  useEffect(() => { if (trouble) setLostAsShown(lostOnly); }, [lostOnly, trouble]);
  const lostMark = trouble ? lostOnly : lostAsShown;
  const inBar = useOfflineShownInPlace();
  const pillShown = shown && !dismissed && lostMark;
  // Put away with ×, the connection is still known lost (offlineShown); its
  // mark staying its moment says nothing of it.
  const knownLost = shown && lostOnly;
  useEffect(() => {
    showOfflinePill(pillShown ? { pending, readOnly, retrying, retry } : null, knownLost);
  }, [knownLost, pending, pillShown, readOnly, retry, retrying]);
  useEffect(() => () => showOfflinePill(null), []);

  if (!shown || dismissed) return null;
  if (restored && !offline && questionShown) return null;
  if (lostMark && inBar) return null;
  if (refused && pending === 0) return null;
  // A failure to read or send what waits here, with the connection up, says
  // what failed; otherwise the notice is the one for a lost connection, which
  // promises to send changes only where this screen can make them (2026-10-26:
  // a guest who could only read was told changes would be sent).
  const text = restored && !offline ? t("syncStatusBadge:restoredPaused")
    : refused === "moved" ? t("syncStatusBadge:serverMovedPending", { count: pending })
    : refused === "paused" ? t("syncStatusBadge:serverPausedPending", { count: pending })
    : failure && !connectionLost ? failure : pending > 0
    ? t("syncStatusBadge:disconnectedPending", { count: pending })
    : t(readOnly ? "syncStatusBadge:disconnectedReadOnly" : "syncStatusBadge:disconnected");
  return (
    <div ref={toastPlacementTarget} className={styles.badge} data-testid="sync-status-badge" data-react-aria-top-layer="true" role="status">
      <span aria-hidden className={styles.dot} />
      <span className={styles.label} data-sync-status-label>{text}</span>
      {restored && !offline ? <button
        type="button"
        className={styles.retry}
        onClick={askAboutRestoredServer}
      >
        {t("syncStatusBadge:restored.take")}
      </button> : refused === "moved" ? null : <button
        type="button"
        className={styles.retry}
        disabled={retrying}
        onClick={retry}
      >
        {t(retrying ? "syncStatusBadge:retrying" : "syncStatusBadge:retry")}
      </button>}
      <button
        type="button"
        className={styles.close}
        aria-label={t("syncStatusBadge:dismiss")}
        onClick={() => setDismissed(true)}
      >
        <X size="small" aria-hidden="true" />
      </button>
    </div>
  );
}
