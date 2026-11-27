import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight } from "@/icons/leafen";
import { WarningIcon } from "@/icons/leafen/automationIcons";
import type { InternalConnectionWebhook } from "@/lib/internalConnections";
import styles from "./InternalConnectionWebhookCard.module.css";

/**
 * A subscription as Notion's "웹훅" tab shows its one subscription (seen
 * 2026-11-21, made to an address that never answers and deleted at once,
 * measured live at 1440): a card of the page's width — "웹훅 URL" and "API
 * 버전" each a bold name, a gray line and the value in a dimmed field;
 * "구독한 이벤트", a 40px row per kind of event with "chosen/all" and ›,
 * which opens the kind's chosen events here; while it waits for its token, a
 * yellow card under a yellow ⚠ saying the endpoint must be verified; under a
 * line, "⚠ 인증" (its ⚠ yellow too) at the start and "삭제하기" in red and the
 * blue "구독 편집" at the end. Under the address,
 * at its end, the small blue "실패한 이벤트 보기" opens what failed to reach
 * it (InternalConnectionWebhookFailures). Notion's "웹훅에 대해 자세히
 * 알아보기" line isn't here: it points at Notion's own documentation.
 */
export function InternalConnectionWebhookCard({ webhook, eventTypes, onFailures, onVerify, onEdit, onDelete }: {
  webhook: InternalConnectionWebhook; eventTypes: readonly string[];
  onFailures(): void; onVerify(): void; onEdit(): void; onDelete(): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const label = (key: string, values?: Record<string, unknown>) => t(`developerTools.internal.webhooks.${key}`, values);
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const groups = new Map<string, string[]>();
  for (const type of eventTypes) {
    const kind = type.slice(0, type.indexOf("."));
    groups.set(kind, [...(groups.get(kind) ?? []), type]);
  }
  const chosen = new Set(webhook.eventTypes);
  return (
    <section className={styles.card} data-connection-card="webhooks" data-connection-webhook={webhook.id}
      data-verified={webhook.verified ? "true" : "false"}>
      <div className={styles.body}>
        <div className={styles.section}>
          <span className={styles.label}>{label("url")}</span>
          <span className={styles.note}>{label("urlNote")}</span>
          <input className={styles.value} value={webhook.url} readOnly disabled aria-label={label("url")} />
          <button type="button" className={styles.failures} data-connection-webhook-failures-open onClick={onFailures}>
            {label("failedEvents")}</button>
        </div>
        <div className={styles.section}>
          <span className={styles.label}>{label("version")}</span>
          <span className={styles.note}>{label("versionHelp")}</span>
          <input className={styles.value} value={webhook.apiVersion} readOnly disabled aria-label={label("version")} />
        </div>
        <div className={styles.section}>
          <span className={styles.label}>{label("subscribed")}</span>
          <span className={styles.note}>{label("subscribedNote")}</span>
          <div className={styles.groups}>
            {[...groups].map(([kind, types]) => {
              const picked = types.filter((type) => chosen.has(type));
              const count = label("groupChosen", { chosen: picked.length, total: types.length });
              const expanded = open.has(kind);
              return (
                <div key={kind} data-connection-webhook-group={kind}>
                  <button type="button" className={styles.group} aria-expanded={expanded} onClick={() => setOpen((current) => {
                    const next = new Set(current);
                    if (!next.delete(kind)) next.add(kind);
                    return next;
                  })}>
                    <span className={styles.groupName}>{label(`group.${kind}`)}</span>
                    <span className={styles.groupCount} title={count}>{count}</span>
                    {expanded ? <ChevronDown size="tiny" aria-hidden="true" /> : <ChevronRight size="tiny" aria-hidden="true" />}
                  </button>
                  {expanded ? (
                    <ul className={styles.events}>
                      {picked.map((type) => <li key={type}>{label(`event.${type}`)}</li>)}
                    </ul>
                  ) : null}
                </div>
              );
            })}
          </div>
        </div>
        {webhook.verified ? null : (
          <div className={styles.warning} role="note">
            <WarningIcon size="page" aria-hidden="true" />
            <strong>{label("verifyNeeded")}</strong>
            <span>{label("verifyNeededNote")}</span>
          </div>
        )}
      </div>
      <div className={styles.footer}>
        {webhook.verified ? null : (
          <button type="button" className={styles.verify} data-connection-webhook-verify onClick={onVerify}>
            <WarningIcon size="small" aria-hidden="true" />{label("verify")}</button>
        )}
        <span className={styles.actions}>
          <button type="button" className={styles.delete} data-connection-webhook-delete onClick={onDelete}>{label("delete")}</button>
          <button type="button" className={styles.edit} data-connection-webhook-edit onClick={onEdit}>{label("editTitle")}</button>
        </span>
      </div>
    </section>
  );
}
