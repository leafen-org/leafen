import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import dialogStyles from "@/components/WorkspaceCreateDialog.module.css";
import {
  onServerRestored,
  readServerDataEpoch,
  rememberDataEpoch,
  restoredServerEpoch,
  serverWasRestored,
  setRestoredQuestionShown,
  takeRestoreChosenHere,
} from "@/lib/dataEpoch";
import { forgetDevicePageCopies, readUnsyncedDeviceChangeCount } from "@/lib/deviceSignOutData";
import { localFirstProductCache as recordCache } from "@/lib/localFirstProductCache";
import type { NativePageMutationBrowser } from "@/lib/nativePageMutationBrowser";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { openRestoredAfresh } from "@/lib/restoredPlace";
import { logProductError } from "@/lib/safeErrorLog";
import { useAfterTyping } from "@/lib/typingHold";
import type { Session } from "@/types";

/**
 * Says so when the server's data was replaced since this device took its
 * copies — a restore, a restore undone, a server brought from another —
 * without saying which, as the server records none (lib/dataEpoch.ts;
 * 2026-10-29: undoing a restore said the data went to an earlier point):
 * the copies, and the changes made here the server never got, are from a
 * history the server no longer has, so
 * nothing more is sent; taking the server's anew lets them go, once the
 * person agrees. "Later" leaves the copies to read, and to copy from. The
 * window that had the server restored takes it anew without asking when
 * this device holds nothing the server never got; with anything unsent, or
 * what is unsent not known, it asks as any other (lib/dataEpoch.ts).
 */
export function ServerRestoredDialog<TResult>({
  mutationOutbox,
  session,
  held = false,
}: {
  mutationOutbox: NativePageMutationBrowser<TResult>;
  session: Session;
  /** Waits behind this device's local lock, still to be answered. */
  held?: boolean;
}) {
  const { t } = useTranslation("syncStatusBadge");
  const [open, setOpen] = useState(serverWasRestored);
  const [chosenHere, setChosenHere] = useState(() => serverWasRestored() && takeRestoreChosenHere(session.user.id));
  const shown = useAfterTyping(open && !held && !chosenHere);
  const [count, setCount] = useState<number | null | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(shown, { kind: "alertdialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) setOpen(false); } });

  useEffect(() => onServerRestored(() => {
    if (takeRestoreChosenHere(session.user.id)) setChosenHere(true);
    setOpen(true);
  }), [session.user.id]);
  // While it is taken anew unasked, the paused notice waits as it does behind the question.
  const answering = shown || (open && chosenHere);
  useEffect(() => {
    setRestoredQuestionShown(answering);
    return () => setRestoredQuestionShown(false);
  }, [answering]);

  // What this device holds that the server never got, counted as sign-out
  // counts it.
  useEffect(() => {
    if (!open || held) return undefined;
    let current = true;
    setCount(undefined);
    void Promise.all([mutationOutbox.readPendingCount(), readUnsyncedDeviceChangeCount(session)])
      .then(([queued, edited]) => { if (current) setCount(queued + edited); })
      .catch(() => { if (current) setCount(null); });
    return () => { current = false; };
  }, [mutationOutbox, open, held, session]);

  // Counted, the window that had the server restored takes it anew, or asks.
  useEffect(() => {
    if (!chosenHere || count === undefined) return;
    if (count === 0) void takeAnew();
    else setChosenHere(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- takeAnew reads only the session and outbox, as the count does
  }, [chosenHere, count]);


  async function takeAnew() {
    setBusy(true);
    setFailed(false);
    try {
      const epoch = restoredServerEpoch() ?? await readServerDataEpoch();
      if (!epoch) throw new Error("The server's data epoch couldn't be read.");
      await mutationOutbox.discardAll();
      await recordCache.clear(session.user.id);
      await forgetDevicePageCopies(session.user.id);
      rememberDataEpoch(session.user.id, epoch);
      await openRestoredAfresh(session);
    } catch (error) {
      logProductError("warn", "outbox.persistence", error, { causeStage: "outbox.serverRestored" });
      setFailed(true);
      setBusy(false);
      setChosenHere(false);
    }
  }

  if (!shown) return null;
  const dialog = (
    <div ref={overlayRef} className={dialogStyles.overlay} style={{ zIndex }} data-server-restored>
      <div className={dialogStyles.backdrop} />
      <section ref={dialogRef} className={dialogStyles.dialog} role="alertdialog" aria-modal="true" aria-labelledby={titleId}
        aria-busy={busy || undefined} tabIndex={-1}>
        <header className={dialogStyles.header}>
          <h2 id={titleId}>{t("restored.title")}</h2>
        </header>
        <div className={dialogStyles.body}>
          <p className={dialogStyles.message}>{t("restored.body")}</p>
          <p className={dialogStyles.message} data-server-restored-count>
            {count === undefined ? t("restored.counting")
              : count === null ? t("restored.countFailed")
                : count > 0 ? t("restored.unsent", { count }) : t("restored.nothingUnsent")}
          </p>
          {failed ? <p className={dialogStyles.message} role="alert">{t("restored.failed")}</p> : null}
          <div className={dialogStyles.actions}>
            <button type="button" className={dialogStyles.secondary} disabled={busy} onClick={() => setOpen(false)}>
              {t("restored.later")}
            </button>
            <button type="button" className={dialogStyles.primary} disabled={busy} onClick={() => void takeAnew()}
              data-server-restored-take>
              {busy ? t("restored.taking") : t("restored.take")}
            </button>
          </div>
        </div>
      </section>
    </div>
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
