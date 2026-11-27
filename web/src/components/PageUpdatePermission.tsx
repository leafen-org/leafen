import type { MouseEvent, ReactNode } from "react";
import { Trans, useTranslation } from "react-i18next";
import { KOREAN_SUBJECT_PAIR } from "@/lib/koreanParticles";
import type { WorkspaceActivityRecord } from "@/workspaceActivityWire.generated";
import { InboxPermissionChange } from "./updates/InboxPermissionChange";
import { permissionLineSentence } from "./updates/permissionLine";
import styles from "./PageAnalyticsPanel.module.css";

/**
 * A permission line of the page in its updates, as Notion's page updates list
 * one among its edits (seen 2026-11-16 and 2026-11-17 in two tabs, by whoever
 * changed them): "본인 〈page〉의 권한을 업데이트했습니다.", or access the
 * pages above gave taken away or restored (permissionLineSentence) — its
 * time and place, and each share it changed under it, as the workspace's
 * updates draw them (WorkspaceActivityFeed.tsx). The reader's own lines, as
 * theirs are there (workspace_activity_permission_notices.go).
 */
export function PageUpdatePermission({ record, page, href, meta, readerId, nameOf, workspaceName, onFollow }: {
  record: WorkspaceActivityRecord;
  /** The page's name, as the panel's other lines name it. */
  page: string;
  href: string;
  /** Its time and place line. */
  meta: ReactNode;
  readerId: string;
  /** A person's name, "" when not known. */
  nameOf(userId: string): string;
  /** The page's workspace's name, for everyone in it. */
  workspaceName: string;
  onFollow(event: MouseEvent<HTMLAnchorElement>): void;
}) {
  const { t, i18n } = useTranslation("updatesPanel");
  const self = record.actorId === readerId;
  const person = self ? t("inbox.activity.self") : nameOf(record.actorId) || t("inbox.someone");
  const changes = record.permissions?.length ? record.permissions : [record];
  return <>
    <p className={styles.updateSummary}>
      <Trans t={t} i18nKey={`inbox.sentences.${permissionLineSentence(record)}`}
        values={{ name: person, title: page, subject: i18n.language.startsWith("ko") ? KOREAN_SUBJECT_PAIR : "" }}
        // Trans supplies the page's name as the link text; the reader in the line's weight, anyone else bold.
        components={{ name: self ? <span /> : <strong className={styles.updateWho} />,
          page: <a href={href} className={styles.updatePageLink} onClick={onFollow} /> }} />
    </p>
    {meta}
    {changes.map((change, at) => <InboxPermissionChange key={at}
      reader={change.entityType === "generalAccess" ? t("inbox.activity.everyoneInWorkspace", { name: workspaceName }) : nameOf(change.entityId)}
      from={change.fromRole ?? ""} to={change.toRole ?? ""} />)}
  </>;
}
