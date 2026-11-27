import { type FormEvent, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { WebhookIcon } from "@/icons/leafen/automationIcons";
import {
  resendInternalConnectionWebhookToken, verifyInternalConnectionWebhook, type InternalConnectionWebhook,
} from "@/lib/internalConnections";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage, writeUncertainMark } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import own from "./InternalConnectionWebhooks.module.css";

/**
 * "구독 인증", as Notion's (seen 2026-11-21, measured live at 1440): the
 * 600px window of "구독 생성하기" — the webhook mark, its title and two gray
 * lines — then "웹훅 URL", the subscription's address in a dimmed 32px field
 * with "토큰 재전송" at its end, and "인증 토큰", a 28px field to paste the
 * token into; the blue "구독 인증", pressable
 * even while the field is empty, over "취소". What goes wrong is said under
 * the field it is about: a token that isn't this subscription's (an empty one
 * too) under the token, a resend the address didn't take — with the status
 * it answered, 502 when nothing answered — under the address.
 */
export function InternalConnectionWebhookVerifyDialog({ session, workspaceId, connectionId, webhook, onVerified, onClose }: {
  session: Session; workspaceId: string; connectionId: string; webhook: InternalConnectionWebhook;
  onVerified(): void; onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null), dialogRef = useRef<HTMLElement>(null), tokenRef = useRef<HTMLInputElement>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState<"verify" | "resend" | null>(null);
  const [sent, setSent] = useState("");
  const [addressError, setAddressError] = useState("");
  const [tokenError, setTokenError] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: tokenRef,
  });
  const label = (key: string, values?: Record<string, unknown>) => t(`developerTools.internal.webhooks.${key}`, values);
  async function verify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    if (!token.trim()) {
      setTokenError(label("tokenMismatch"));
      return;
    }
    setBusy("verify");
    setTokenError("");
    try {
      await verifyInternalConnectionWebhook(session, workspaceId, connectionId, webhook.id, token.trim());
      onVerified();
    } catch (failure) {
      setTokenError(failure instanceof LeafenApiError && failure.code === "LEAFEN_INTERNAL_CONNECTION_WEBHOOK_TOKEN"
        ? label("tokenMismatch") : settingsErrorMessage(failure, t("appFailed")));
    } finally {
      setBusy(null);
    }
  }
  async function resend() {
    if (busy) return;
    setBusy("resend");
    setSent("");
    setAddressError("");
    try {
      const answer = await resendInternalConnectionWebhookToken(session, workspaceId, connectionId, webhook.id);
      if (answer.verified) onVerified();
      else if (answer.tokenDelivered) setSent(label("resent"));
      else setAddressError(label("resendFailed", { status: answer.tokenStatus }));
    } catch (failure) {
      setAddressError(settingsErrorMessage(failure, t("appFailed")));
    } finally {
      setBusy(null);
    }
  }
  return createPortal(
    <div ref={overlayRef} className={own.overlay} style={{ zIndex }} data-connection-webhook-verify-dialog>
      <button type="button" className={own.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={Boolean(busy)} onClick={onClose} />
      <section ref={dialogRef} className={`${own.dialog} ${own.verifyDialog}`} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={Boolean(busy) || undefined}>
        <WebhookIcon size="card" aria-hidden="true" className={own.dialogMark} />
        <h2 id={titleId} className={own.dialogTitle}>{label("verifyTitle")}</h2>
        <p className={`${own.dialogNote} ${own.lines}`}>{label("verifyDescription")}</p>
        <form className={`${own.form} ${own.verifyForm}`} onSubmit={(event) => void verify(event)}>
          <div className={`${own.field} ${own.tightField}`}>
            <span className={own.label}>{label("url")}</span>
            <span className={own.withAction}>
              <input className={own.textField} value={webhook.url} disabled readOnly aria-label={label("url")} />
              <button type="button" className={own.inlineAction} disabled={Boolean(busy)} data-connection-webhook-resend
                onClick={() => void resend()}>{label("resend")}</button>
            </span>
            {addressError ? <span className={own.fieldError} role="alert" {...writeUncertainMark(addressError)}>{addressError}</span>
              : sent ? <span className={own.help} role="status">{sent}</span> : null}
          </div>
          <label className={`${own.field} ${own.tightField}`}>
            <span className={own.label}>{label("token")}</span>
            <input ref={tokenRef} className={own.textField} value={token} placeholder="secret_********" autoComplete="off" spellCheck={false}
              aria-invalid={tokenError ? true : undefined} data-connection-webhook-token
              onChange={(event) => { setToken(event.currentTarget.value); setTokenError(""); }} />
            {tokenError ? <span className={own.fieldError} role="alert" {...writeUncertainMark(tokenError)}>{tokenError}</span> : null}
          </label>
          <button type="submit" className={own.primary} disabled={busy === "verify"} data-connection-webhook-verify-submit>
            {label("verifySubmit")}</button>
          <button type="button" className={`${own.secondary} ${own.closeAfterVerify}`} disabled={Boolean(busy)} onClick={onClose}>{t("common:actions.cancel")}</button>
        </form>
      </section>
    </div>,
    document.body,
  );
}
