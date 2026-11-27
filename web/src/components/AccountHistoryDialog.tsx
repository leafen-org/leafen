import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/api";
import { organizationAuditErrorCodes, type AccountHistory, type AccountHistoryItem } from "@/organizationAuditWire.generated";
import type { OrganizationSettingsAuditEvent } from "@/organizationWire.generated";
import { activeDateLocale } from "@/lib/i18n";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { OPEN_ACCOUNT_HISTORY_EVENT, readAccountHistory, type OpenAccountHistoryDetail } from "@/lib/accountHistory";
import { organizationAuditLabel } from "@/components/settings/organizationAuditModel";
import { sessionDevice } from "@/components/account/AccountDevicesSection";
import { authAuditMethodText, authAuditReasonText, authAuditSignInText } from "@/lib/authAuditText";
import styles from "./ReadLogDialog.module.css";

// An account's history (the audit decision of 2026-10-21 20:05, 1; Leafen's
// own): a person's sign-ins, reads, edits, permission
// changes and work through their connections, newest first, as much as the server
// lets the reader see — and a line saying how much that is.

function moment(at: number) {
  return new Date(at).toLocaleString(activeDateLocale(), { year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

/** An audit item as the audit log's names read it. */
function historyAuditEvent(item: AccountHistoryItem): OrganizationSettingsAuditEvent {
  return { id: item.id, actorId: item.actorId ?? "", action: item.action, targetType: item.targetType ?? "", targetId: item.targetId ?? "",
    metadata: item.metadata ?? {}, occurredAt: new Date(item.at).toISOString(), actorName: item.actorName, targetName: item.targetName ?? null };
}

function ItemLine({ item }: { item: AccountHistoryItem }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  let what = "";
  let detail = "";
  if (item.kind === "sign_in") {
    what = `${authAuditSignInText(t, item.outcome ?? "", item.phase)} · ${authAuditMethodText(t, item.method ?? "")}`;
    detail = [item.reason ? t("accountHistory.reason", { reason: authAuditReasonText(t, item.method ?? "", item.reason, item.reasonStatus) }) : "", sessionDevice(item.device ?? "")?.name ?? ""].filter(Boolean).join(" · ");
  } else {
    what = organizationAuditLabel(historyAuditEvent(item), t);
    detail = item.masked ? t("accountHistory.masked") : item.targetName || "";
    if (item.kind === "to_me") detail = [t("accountHistory.by", { name: item.actorName || t("auditUnknownPerson") }), detail].filter(Boolean).join(" · ");
  }
  return (
    <li className={styles.event} data-account-history-item={item.kind} data-action={item.action}>
      <span className={styles.time}>{moment(item.at)}</span>
      <span className={styles.line}><span className={styles.who}>{what}</span></span>
      {detail ? <span className={styles.how}>{detail}</span> : null}
    </li>
  );
}

function AccountHistoryDialog({ detail, onClose }: { detail: OpenAccountHistoryDetail; onClose(): void }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [history, setHistory] = useState<AccountHistory | null>(null);
  const [items, setItems] = useState<AccountHistoryItem[]>([]);
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
  });
  const failed = (error: unknown) => setFailure(error instanceof LeafenApiError && error.code === organizationAuditErrorCodes.notFound
    ? "accountHistory.notAllowed" : "accountHistory.loadFailed");

  useEffect(() => {
    const controller = new AbortController();
    readAccountHistory(detail.organizationId, detail.userId, "", controller.signal).then((next) => {
      setHistory(next);
      setItems(next.items);
    }).catch((error) => { if (!controller.signal.aborted) failed(error); });
    return () => controller.abort();
  }, [detail.organizationId, detail.userId]);

  async function more() {
    if (!history?.nextCursor || busy) return;
    setBusy(true);
    try {
      const next = await readAccountHistory(detail.organizationId, detail.userId, history.nextCursor);
      setHistory(next);
      setItems((current) => [...current, ...next.items]);
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  }

  return createPortal(
    <div className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={onClose} />
      <div ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} data-account-history>
        <h2 id={titleId}>{t("accountHistory.title")}</h2>
        <p className={styles.name}>{history?.name || detail.name || ""}</p>
        {failure ? <p className={styles.failure} role="alert">{t(failure)}</p> : null}
        {history ? (
          <>
            <p className={styles.since} data-account-history-scope={history.scope}>{t(`accountHistory.scope.${history.scope}`)}</p>
            <ul className={styles.events} aria-label={t("accountHistory.title")}>
              {items.map((item) => <ItemLine key={item.id} item={item} />)}
              {items.length === 0 ? <li className={styles.empty}>{t("accountHistory.empty")}</li> : null}
            </ul>
            {history.nextCursor ? (
              <button type="button" className={styles.more} disabled={busy} onClick={() => void more()}>{t("accountHistory.more")}</button>
            ) : null}
          </>
        ) : null}
        <div className={styles.actions}>
          <button type="button" className={styles.close} onClick={onClose}>{t("common:actions.close")}</button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

/** Opens the history a person's "History" asks for (requestAccountHistory). */
export function AccountHistoryDialogHost() {
  const [detail, setDetail] = useState<OpenAccountHistoryDetail | null>(null);
  useEffect(() => {
    const listener = (event: Event) => setDetail((event as CustomEvent<OpenAccountHistoryDetail>).detail);
    window.addEventListener(OPEN_ACCOUNT_HISTORY_EVENT, listener);
    return () => window.removeEventListener(OPEN_ACCOUNT_HISTORY_EVENT, listener);
  }, []);
  return detail ? <AccountHistoryDialog key={`${detail.organizationId}:${detail.userId}`} detail={detail} onClose={() => setDetail(null)} /> : null;
}
