"use client";

import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { RichTextCellText } from "@/components/database/RichTextCellText";
import type { ButtonConfirmationChallenge } from "@/lib/buttonExecutionContract";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "./buttonConfirmationDialog.module.css";

const MAX_BUTTON_CONFIRMATIONS = 20;

export function ButtonConfirmationDialog({
  challenge,
  busy = false,
  onCancel,
  onConfirm,
}: {
  challenge: ButtonConfirmationChallenge;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const titleId = useId();
  const confirmRef = useRef<HTMLButtonElement>(null);
  const first = challenge.confirmations[0];

  useEffect(() => {
    confirmRef.current?.focus();
  }, [challenge.confirmationToken]);

  // A modal layer: Escape cancels it, and only it, unless it is running.
  const dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "alertdialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!busy) onCancel(); } });

  if (
    !first
    || challenge.confirmations.length > MAX_BUTTON_CONFIRMATIONS
    || typeof document === "undefined"
  ) return null;

  return createPortal(
    <div
      className={styles.backdrop}
      style={{ zIndex }}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) onCancel();
      }}
    >
      <section
        ref={dialogRef}
        className={styles.dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={busy || undefined}
      >
        <div className={styles.content}>
          {challenge.confirmations.map((confirmation, index) => (
            <div className={styles.confirmation} key={confirmation.actionId}>
              <p id={index === 0 ? titleId : undefined}>
                {confirmation.messageSpans?.length ? <RichTextCellText spans={confirmation.messageSpans} interactive /> : confirmation.message}
              </p>
            </div>
          ))}
        </div>
        <div className={styles.actions}>
          <button
            ref={confirmRef}
            type="button"
            className={styles.primary}
            disabled={busy}
            onClick={onConfirm}
          >
            {first.confirmLabel}
          </button>
          <button type="button" disabled={busy} onClick={onCancel}>{first.cancelLabel}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
