import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { api } from "@/api";
import type { Session } from "@/types";
import { accessRequestBounds, type WorkspaceMembershipRequestStatus } from "@/accessRequestWire.generated";
import { ComposeIcon, Copy, LightningIcon, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useClosedForLink } from "@/lib/productWindows";
import { MembershipAskPicture } from "./MembershipAskPicture";
import styles from "./WorkspaceMembershipRequest.module.css";

/**
 * Whether a guest may ask to become a member of the workspace, and the
 * window that asks, as Notion's guest "새로" opens it where the workspace
 * takes such requests (seen 2026-11-12, a Plus workspace's guest); where it
 * takes none "새로" stays the new workspace's menu. Only the person's own
 * request state is read; the workspace roster stays closed to guests.
 */
export function useWorkspaceMembershipAsk(session: Session, workspaceId: string | null): {
  asks: boolean;
  open(): void;
  dialog: ReactNode;
} {
  const [status, setStatus] = useState<WorkspaceMembershipRequestStatus | null>(null);
  const [shown, setShown] = useState(false);
  useEffect(() => {
    setStatus(null);
    setShown(false);
    if (!workspaceId) return undefined;
    const request = new AbortController();
    void api.workspaceMembershipRequestStatus(session, workspaceId, request.signal).then((value) => {
      if (!request.signal.aborted) setStatus(value);
    }, () => undefined);
    return () => request.abort();
  }, [session, workspaceId]);
  const open = useCallback(() => setShown(true), []);
  const asks = Boolean(workspaceId && (status?.canRequest || status?.pending));
  const dialog = shown && workspaceId && status ? (
    <WorkspaceMembershipRequestDialog session={session} workspaceId={workspaceId} status={status}
      onStatus={setStatus} onClose={() => setShown(false)} />
  ) : null;
  return { asks, open, dialog };
}

// The grey part of a line, and the rest (a particle after the grey part
// stays in the rest, scripts/check-korean-copy.mjs).
const muted = { muted: <span className={styles.muted} />, plain: <span /> };

function WorkspaceMembershipRequestDialog({ session, workspaceId, status, onStatus, onClose }: {
  session: Session;
  workspaceId: string;
  status: WorkspaceMembershipRequestStatus;
  onStatus(status: WorkspaceMembershipRequestStatus): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["sidebar", "workspaceSettingsDialog", "common"]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Why the person asks, as Notion's "메시지 추가 — 이유(선택 사항)"; it goes with the request.
  const [message, setMessage] = useState("");
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const messageId = useId();
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
  });
  // A link going to a place here closes it as Escape does (closeWindowsForLink), unless it is sending.
  useClosedForLink(() => { if (!busy) onClose(); });
  const live = useRef(new AbortController());
  useEffect(() => {
    const request = new AbortController();
    live.current = request;
    return () => request.abort();
  }, []);

  async function send() {
    const request = live.current;
    if (busy || status.pending) return;
    setBusy(true);
    setError("");
    try {
      await api.requestWorkspaceMembership(session, workspaceId, request.signal, message);
      if (!request.signal.aborted) onStatus({ canRequest: true, pending: true });
    } catch (failure) {
      if (!request.signal.aborted) setError(settingsErrorMessage(failure, t("workspaceSettingsDialog:requestDecideFailed")));
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }

  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={onClose} />
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}
        data-workspace-membership-request={status.pending ? "pending" : "open"}>
        <button type="button" className={styles.close} aria-label={t("common:actions.close")} onClick={onClose}>
          <X size="small" aria-hidden="true" />
        </button>
        <div className={styles.body}>
          <MembershipAskPicture />
          <h2 id={titleId} className={styles.title}>{t("membershipAskTitle")}</h2>
          <ul className={styles.points}>
            <li><ComposeIcon size="regular" aria-hidden="true" /><span><Trans t={t} i18nKey="membershipAskCreate" components={muted} /></span></li>
            <li><Copy size="regular" aria-hidden="true" /><span><Trans t={t} i18nKey="membershipAskPages" components={muted} /></span></li>
            <li><LightningIcon size="regular" aria-hidden="true" /><span><Trans t={t} i18nKey="membershipAskFeatures" components={muted} /></span></li>
          </ul>
        </div>
        <div className={styles.foot}>
          <label className={styles.messageLabel} htmlFor={messageId}>{t("membershipAskMessage")}</label>
          <div className={styles.messageField}>
            <textarea id={messageId} className={styles.message} value={message} placeholder={t("membershipAskReason")}
              maxLength={accessRequestBounds.messageRunes} disabled={busy || status.pending} data-membership-request-message
              onChange={(event) => setMessage(event.target.value)} />
          </div>
          {error ? <p className={styles.error} role="alert">{error}</p> : null}
          <div className={styles.actions}>
            <button type="button" className={styles.guest} data-membership-continue-guest onClick={onClose}>{t("membershipAskContinue")}</button>
            <button type="button" className={styles.send} data-membership-request-send disabled={busy || status.pending}
              onClick={() => void send()}>
              {t(status.pending ? "workspaceSettingsDialog:membershipRequestPending" : "membershipAskSend")}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
