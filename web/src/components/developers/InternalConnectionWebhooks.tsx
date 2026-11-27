import { useEffect, useState } from "react";
import { useMadeWithoutAnswer } from "@/lib/uncertainWrites";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { Plus, WarningCircleIcon } from "@/icons/leafen";
import { WebhookIcon } from "@/icons/leafen/automationIcons";
import {
  deleteInternalConnectionWebhook, listInternalConnectionWebhooks,
  type InternalConnection, type InternalConnectionWebhook, type InternalConnectionWebhooks as Webhooks,
} from "@/lib/internalConnections";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { InternalConnectionWebhookCard } from "./InternalConnectionWebhookCard";
import { InternalConnectionWebhookDialog } from "./InternalConnectionWebhookDialog";
import { InternalConnectionWebhookFailures } from "./InternalConnectionWebhookFailures";
import { InternalConnectionWebhookVerifyDialog } from "./InternalConnectionWebhookVerifyDialog";
import styles from "./DeveloperTools.module.css";
import own from "./InternalConnectionWebhooks.module.css";

/**
 * "웹훅", as Notion's tab on an "API 토큰" connection (seen 2026-10-31): with
 * no subscription a card — the webhook mark, "웹훅 구독 없음", what they are
 * for and "+ 구독 생성하기" — whose dialog asks the address, the API version
 * and the events. A subscription hears nothing until the token its address
 * was sent is given back (notion_internal_connection_webhooks.go). Once one
 * is made the tab is that subscription's card and nothing to make another,
 * as Notion's (seen 2026-11-21); one made before then by Leafen shows its own
 * card under it. Its delete asks first, as Notion's: a red mark, the
 * question, what it ends and a filled red button. Closing the verify window
 * reads the list again, so a resend that failed is among its failed events.
 */
export function InternalConnectionWebhooks({ session, workspaceId, connection }: {
  session: Session; workspaceId: string; connection: InternalConnection;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [data, setData] = useState<Webhooks | null>(null);
  const [editing, setEditing] = useState<InternalConnectionWebhook | "new" | null>(null);
  const [verifying, setVerifying] = useState<InternalConnectionWebhook | null>(null);
  const [failures, setFailures] = useState<InternalConnectionWebhook | null>(null);
  const [error, setError] = useState("");
  const [generation, setGeneration] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void listInternalConnectionWebhooks(session, workspaceId, connection.id, controller.signal)
      .then(setData)
      .catch((failure) => { if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("appLoadFailed"))); });
    return () => controller.abort();
  }, [connection.id, generation, session, t, workspaceId]);
  const reload = () => setGeneration((value) => value + 1);
  // A subscription made without the server's answer and found in the list
  // read again closes its window as the answer would have (lib/uncertainWrites.ts).
  const makeWebhook = useMadeWithoutAnswer({
    items: data?.items,
    idOf: (webhook) => webhook.id,
    readAgain: reload,
    landed: () => setEditing((open) => (open === "new" ? null : open)),
  });
  async function remove(webhook: InternalConnectionWebhook) {
    if (!await confirmAction({ message: t("developerTools.internal.webhooks.deleteConfirm"),
      detail: t("developerTools.internal.webhooks.deleteDetail"), confirmLabel: t("developerTools.internal.webhooks.deleteSubmit"),
      danger: "filled", roomy: true, icon: <WarningCircleIcon size="large" className={own.deleteMark} aria-hidden="true" /> })) return;
    try {
      await deleteInternalConnectionWebhook(session, workspaceId, connection.id, webhook.id);
      reload();
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }
  if (!data) return error ? <p className={styles.error} role="alert">{error}</p> : null;
  const create = (
    <button type="button" className={own.create} data-connection-webhook-create onClick={() => setEditing("new")}>
      <Plus size="small" aria-hidden="true" />{t("developerTools.internal.webhooks.create")}
    </button>
  );
  return (
    <>
      {data.items.length === 0 ? (
        <section className={own.empty} data-connection-card="webhooks">
          <WebhookIcon size="wide" aria-hidden="true" />
          <h2>{t("developerTools.internal.webhooks.none")}</h2>
          <p>{t("developerTools.internal.webhooks.noneDescription")}</p>
          {create}
        </section>
      ) : data.items.map((webhook) => (
        <InternalConnectionWebhookCard key={webhook.id} webhook={webhook} eventTypes={data.eventTypes}
          onFailures={() => setFailures(webhook)} onVerify={() => setVerifying(webhook)} onEdit={() => setEditing(webhook)} onDelete={() => void remove(webhook)} />
      ))}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {editing ? (
        <InternalConnectionWebhookDialog session={session} workspaceId={workspaceId} connection={connection} options={data}
          webhook={editing === "new" ? undefined : editing} make={makeWebhook}
          onSaved={() => { setEditing(null); reload(); }}
          onClose={() => setEditing(null)} />
      ) : null}
      {verifying ? (
        <InternalConnectionWebhookVerifyDialog session={session} workspaceId={workspaceId} connectionId={connection.id} webhook={verifying}
          onVerified={() => { setVerifying(null); reload(); }} onClose={() => { setVerifying(null); reload(); }} />
      ) : null}
      {failures ? <InternalConnectionWebhookFailures webhook={failures} onClose={() => setFailures(null)} /> : null}
    </>
  );
}
