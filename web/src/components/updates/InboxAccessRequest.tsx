import { useState } from "react";
import { useTranslation } from "react-i18next";
import { publicMutationRoles, type PublicMutationRole } from "@/publicMutationWire.generated";
import { displayDateTime } from "@/lib/displayDate";
import { TopBarSharePermissionPicker } from "../page/TopBarSharePermissionPicker";
import { ReadFailed } from "../ReadFailed";
import type { useAccessRequestDecisions } from "./useAccessRequestDecisions";
import styles from "../UpdatesPanelFeed.module.css";

type AccessRequests = ReturnType<typeof useAccessRequestDecisions>;

/**
 * An access request's part of its inbox row, as Notion's (seen 2026-11-09, a
 * guest asking for a page). While it waits: a yellow warning when the person
 * is outside the workspace, then a bordered role choice — "편집 허용" unless
 * more was asked for — a bordered "승인" and a plain "거부". Approving marks
 * the row read. "거부" is Notion's ignore (seen 2026-11-09): the row is read
 * and archived, and the request still waits, approvable from 보관됨 (a
 * member's is in Settings › 요청 too). Once decided it says by whom and when
 * ("승인됨 작성자: 본인, 작성 시간: …").
 */
export function InboxAccessRequest({ requestId, requests, readerId, unread, onMarkRead, onArchive }: {
  requestId: string;
  requests: AccessRequests;
  readerId?: string;
  unread: boolean;
  onMarkRead: () => void;
  /** Archives the row: "거부", which an archived row, already put away, doesn't offer. */
  onArchive?: () => void;
}) {
  const { t } = useTranslation("updatesPanel");
  const request = requests.records[requestId];
  const [role, setRole] = useState<PublicMutationRole | null>(null);
  if (!request) {
    return requests.loadFailed ? (
      <div className={styles.requestState}>
        <ReadFailed message={t("rows.requestsLoadFailed")} onRetry={requests.retry} />
      </div>
    ) : null;
  }
  if (request.status !== "pending") {
    const name = request.decidedBy && request.decidedBy === readerId ? t("rows.requestDecidedBySelf") : request.decidedByName || t("inbox.someone");
    return (
      <div className={styles.requestDecision} role="status" data-access-request-outcome={request.status}>
        {t(request.status === "approved" ? "rows.requestApprovedBy" : "rows.requestDeclinedBy", {
          name, time: request.decidedAt ? displayDateTime(request.decidedAt) : "",
        })}
      </div>
    );
  }
  // One's own request waits for others to decide.
  if (request.mine) return null;
  const asked = request.role === publicMutationRoles.fullAccess ? publicMutationRoles.fullAccess : publicMutationRoles.edit;
  const chosen = role ?? asked;
  const busy = Boolean(requests.busy);
  const approve = async () => {
    if (!await requests.decide(requestId, true, chosen)) return;
    if (unread) onMarkRead();
  };
  const ignore = onArchive ? () => {
    if (unread) onMarkRead();
    onArchive();
  } : null;
  return (
    <div className={styles.requestBlock} data-access-request-actions>
      {request.external ? <div className={styles.requestWarning} data-access-request-external>{t("rows.externalWarning")}</div> : null}
      <div className={styles.accessActions}>
        <span className={styles.requestRole} data-access-request-role={chosen}>
          <TopBarSharePermissionPicker value={chosen} disabled={busy} onChange={(next) => { if (next !== "none") setRole(next); }} />
        </span>
        <button type="button" className={styles.accessAction} data-tone="outlined" disabled={busy} onClick={() => void approve()}>
          {t("rows.approve")}
        </button>
        {ignore ? <button type="button" className={styles.accessAction} disabled={busy} onClick={ignore}>
          {t("rows.decline")}
        </button> : null}
        {/* A decision the server refused leaves the request waiting and says so, rather than nothing. */}
        {requestId in requests.failed ? <span className={styles.requestActionNote} role="alert" data-error>{requests.failed[requestId] || t("rows.request_failed")}</span> : null}
      </div>
    </div>
  );
}
