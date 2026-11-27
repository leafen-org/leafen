"use client";

// Boot-time unlock dialog for the local data lock (passphrase key custody).
// Shown only when the encryption mode is "passphrase" and this session has
// not unlocked yet. Skipping keeps the session network-only: the outbox and
// record cache stay disabled, so locked data is neither read nor written.

import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { localLockPending, primeUnlockedGate, skipLocalLock, unlockLocalData } from "@/lib/localLock";
import { readHeldKey } from "@/lib/localLockHold";
import { restoreLocalSecretBoxSession } from "@/lib/localSecretBox";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { thisWindowKey } from "@/lib/thisWindowWords";

import styles from "./LocalLockGate.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

export default function LocalLockGate({
  userId,
  onUnlocked,
  onSettled,
}: {
  userId: string;
  onUnlocked?: () => void;
  /** The lock is out of the way: unlocked, held from before, or skipped. */
  onSettled?: () => void;
}) {
  const [pending, setPending] = useState(() => localLockPending(userId));
  useEffect(() => {
    if (!pending) onSettled?.();
    // Each time it settles, with the callback of that time.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pending]);
  // A reload finds what this tab unlocked (localLockHold.ts): nothing is asked until that is known.
  const [readingHeld, setReadingHeld] = useState(pending);
  const [passphrase, setPassphrase] = useState("");
  const [busy, setBusy] = useState(false);
  // A wrong passphrase is about the one typed, and goes once it changes; a
  // browser that can't unlock stays said.
  const [unsupported, setUnsupported] = useState(false);
  const [wrongPassphrase, setWrongPassphrase] = useAttemptNotice<boolean>(passphrase);
  const error = unsupported ? "unsupported" : wrongPassphrase ? "wrong-passphrase" : "";
  const { t } = useTranslation(["localLockGate", "common"]);
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const titleId = useId();
  const descriptionId = useId();

  // A modal layer with no Escape: the data stays locked until unlocked or
  // skipped; the page stays still and the focus goes back after. It stays
  // above windows opened after it, such as settings opened by its address.
  const { zIndex } = useOverlayLayer(pending && !readingHeld, { kind: "dialog", ref: dialogRef, above: true, modalOwned: () => [dialogRef.current] });

  useEffect(() => {
    if (!pending || readingHeld) return;

    const frame = window.requestAnimationFrame(() => inputRef.current?.focus());
    return () => {
      window.cancelAnimationFrame(frame);

    };
  }, [pending, readingHeld]);

  useEffect(() => {
    if (!readingHeld) return undefined;
    let live = true;
    void readHeldKey(userId).then((snapshot) => {
      if (!live) return;
      const box = restoreLocalSecretBoxSession(snapshot);
      if (box) {
        primeUnlockedGate(userId, box);
        setPending(false);
        onUnlocked?.();
      }
      setReadingHeld(false);
    });
    return () => { live = false; };
    // Once, as the gate opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (!pending || readingHeld) return null;

  async function unlock() {
    if (!passphrase || busy) return;
    setBusy(true);
    setUnsupported(false);
    setWrongPassphrase(null);
    const result = await unlockLocalData(userId, passphrase);
    setBusy(false);
    if (result === "ok") {
      setPending(false);
      onUnlocked?.();
      return;
    }
    if (result === "wrong-passphrase") setWrongPassphrase(true);
    else setUnsupported(true);
  }

  function skip() {
    skipLocalLock(userId);
    setPending(false);
  }

  const dialog = (
    <div
      ref={dialogRef}
      className={styles.backdrop}
      style={{ zIndex }}
      data-testid="local-lock-gate"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      tabIndex={-1}
    >
      <div className={styles.card}>
        <strong id={titleId} className={styles.title}>{t("localLockGate:title")}</strong>
        <p id={descriptionId} className={styles.desc}>{t("localLockGate:description")}</p>
        <input
          ref={inputRef}
          type="password"
          className={styles.input}
          data-testid="local-lock-passphrase"
          aria-label={t("localLockGate:passphrasePlaceholder")}
          placeholder={t("localLockGate:passphrasePlaceholder")}
          value={passphrase}
          onChange={(e) => setPassphrase(e.target.value)}
          onKeyDown={(e) => {
            if (fieldEnter(e)) void unlock();
          }}
        />
        {error ? (
          <p className={styles.error} data-testid="local-lock-error" role="alert">
            {error === "wrong-passphrase"
              ? t("localLockGate:wrongPassphrase")
              // "이 브라우저에서는" or, in Leafen's own app window, "이 앱에서는" (thisWindowWords.ts).
              : t(thisWindowKey("localLockGate:unsupportedBrowser", "localLockGate:unsupportedBrowserApp"))}
          </p>
        ) : null}
        {error === "wrong-passphrase" ? (
          <p className={styles.desc} data-testid="local-lock-continue-hint">
            {t("localLockGate:continueOnlineHint")}
          </p>
        ) : null}
        <div className={styles.actions}>
          <button
            type="button"
            className={styles.primary}
            data-testid="local-lock-unlock"
            disabled={busy || !passphrase}
            onClick={() => void unlock()}
          >
            {t("localLockGate:unlock")}
          </button>
          <button
            type="button"
            className={styles.secondary}
            data-testid="local-lock-skip"
            onClick={skip}
          >
            {t("localLockGate:continueOnline")}
          </button>
        </div>
      </div>
    </div>
  );

  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
