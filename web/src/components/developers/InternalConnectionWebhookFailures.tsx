import { useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { WebhookIcon } from "@/icons/leafen/automationIcons";
import { activeDateLocale } from "@/lib/i18n";
import type { InternalConnectionWebhook } from "@/lib/internalConnections";
import { useOverlayLayer } from "@/lib/overlayLayer";
import shared from "./InternalConnectionWebhooks.module.css";
import own from "./InternalConnectionWebhookFailures.module.css";

const failedAt = (value: number) => new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long", timeStyle: "medium" }).format(value);

/**
 * "실패한 이벤트 보기", as Notion's (seen 2026-11-21 at 1440, dpr 1, a
 * subscription made to an address that never answers): a 516px window with
 * the webhook mark, its title and a gray line, then two rows of a gray name
 * and its value in two even columns — how many of the subscription's events
 * failed to reach its address and when the latest did, to the second. No
 * list and no resend; Escape or a press outside closes it. The numbers come
 * with the subscription (internalConnectionWebhookFailures), as Notion's
 * came with its page.
 */
export function InternalConnectionWebhookFailures({ webhook, onClose }: {
  webhook: InternalConnectionWebhook; onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null), dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: onClose, initialFocus: dialogRef,
  });
  const label = (key: string) => t(`developerTools.internal.webhooks.${key}`);
  return createPortal(
    <div ref={overlayRef} className={shared.overlay} style={{ zIndex }} data-connection-webhook-failures-dialog>
      <button type="button" className={shared.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={onClose} />
      <section ref={dialogRef} className={own.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}>
        <WebhookIcon size="card" aria-hidden="true" className={own.mark} />
        <h2 id={titleId} className={own.title}>{label("failedEvents")}</h2>
        <p className={own.note}>{label("failedEventsDescription")}</p>
        <dl className={own.facts}>
          <dt>{label("failureCount")}</dt>
          <dd data-connection-webhook-failures>{webhook.failures}</dd>
          <dt>{label("lastFailedAt")}</dt>
          <dd data-connection-webhook-last-failed>{webhook.lastFailedAt ? failedAt(webhook.lastFailedAt) : label("noFailure")}</dd>
        </dl>
      </section>
    </div>,
    document.body,
  );
}
