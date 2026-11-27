import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useTranslation } from "react-i18next";
import { LeafenApiError, request } from "@/apiTransport";
import {
  eventWebhookConstants,
  eventWebhookErrorCodes,
  eventWebhookOperations,
  eventWebhookTypes,
  type EventWebhook,
  type EventWebhookCreateResponse,
  type EventWebhookDelivery,
  type EventWebhookDeliveryListResponse,
  type EventWebhookListResponse,
  type EventWebhookType,
} from "@/eventWebhookWire.generated";
import { newId } from "@/lib/ids";
import { useCopyNotice } from "@/lib/copyNotice";
import { activeDateLocale } from "@/lib/i18n";
import { settingsErrorMessage, writeUncertainMark } from "@/lib/settingsErrors";
import { useSendAgainWhenAnswered } from "@/lib/uncertainWrites";
import type { Session, Workspace } from "@/types";
import { confirmAction } from "@/components/ConfirmActionDialog";
import styles from "./DeveloperSettings.module.css";
import { SettingsRowMenu } from "./SettingsRowMenu";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { ListNotice } from "./SettingsRowNotice";
import { displayDateTime } from "@/lib/displayDate";

function webhookPath(path: string, workspaceId: string, webhookId = "", deliveryId = "") {
  return path.replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{webhookId}", encodeURIComponent(webhookId)).replace("{deliveryId}", encodeURIComponent(deliveryId));
}

// The secret a webhook made here showed, kept beside the section while one
// is open: the settings are drawn anew once the server answers after a write
// whose outcome wasn't known, and a secret shown once must outlive that
// (2026-11-06: a webhook sent again landed with the secret never seen). It
// goes once no section is open.
type ShownSecret = { secret: string; webhookId: string };
const shownSecrets = new Map<string, ShownSecret>();
const shownSecretListeners = new Set<() => void>();
const sectionsOpen = new Map<string, number>();

function setShownSecret(workspaceId: string, value: ShownSecret | null) {
  if (value && !sectionsOpen.get(workspaceId)) return;
  if (value) shownSecrets.set(workspaceId, value);
  else shownSecrets.delete(workspaceId);
  shownSecretListeners.forEach((listener) => listener());
}

function useShownSecret(workspaceId: string) {
  useEffect(() => {
    sectionsOpen.set(workspaceId, (sectionsOpen.get(workspaceId) ?? 0) + 1);
    return () => {
      sectionsOpen.set(workspaceId, (sectionsOpen.get(workspaceId) ?? 1) - 1);
      // Drawn anew, the next section is open again by now.
      window.setTimeout(() => { if (!sectionsOpen.get(workspaceId)) shownSecrets.delete(workspaceId); }, 0);
    };
  }, [workspaceId]);
  return useSyncExternalStore((listener) => {
    shownSecretListeners.add(listener);
    return () => { shownSecretListeners.delete(listener); };
  }, () => shownSecrets.get(workspaceId) ?? null);
}

/**
 * The workspace's event webhooks under "개발자": each one's address, the
 * events it takes and how its last delivery went, with a test, its recent
 * deliveries and removal, and a form to add one whose signing secret shows
 * once. Notion keeps its integrations' webhooks in its developer portal,
 * which Leafen doesn't have, so the item holds them.
 */
export function EventWebhookSection({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [webhooks, setWebhooks] = useState<EventWebhook[] | null>(null);
  const [url, setUrl] = useState("");
  const [types, setTypes] = useState<EventWebhookType[]>(["page.created", "page.content_updated", "comment.created"]);
  // The secret shown once, and which webhook it signs, so it leaves with that webhook.
  const shown = useShownSecret(workspace.id);
  const secret = shown?.secret ?? "", secretFor = shown?.webhookId ?? "";
  const [open, setOpen] = useState("");
  const [deliveries, setDeliveries] = useState<EventWebhookDelivery[]>([]);
  const [message, setMessage] = useAttemptNotice<string>(url, types);
  // A failed read of the list, said where it shows with a way to read it again.
  const [readFailed, setReadFailed] = useState("");
  const notice = useCopyNotice();
  // What is wrong with the address, under its field.
  const [addressError, setAddressError] = useState("");
  const [busy, setBusy] = useState(false);
  // The form to add one shows once its button is pressed, as a settings list's does.
  const [adding, setAdding] = useState(false);
  const locale = activeDateLocale();
  const time = (value: number) => displayDateTime(value, locale);

  const load = useCallback(async () => {
    try {
      const listed = await request<EventWebhookListResponse>(
        webhookPath(eventWebhookOperations.list.path, workspace.id), { method: eventWebhookOperations.list.method }, session);
      setWebhooks(listed.webhooks);
      setReadFailed("");
    } catch (failure) {
      setReadFailed(settingsErrorMessage(failure, t("webhookLoadFailed")));
    }
  }, [session, t, workspace.id]);
  useEffect(() => { void load(); }, [load]);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setMessage("");
    try {
      await action();
    } catch (failure) {
      setMessage(settingsErrorMessage(failure, t("webhookFailed")));
    } finally {
      setBusy(false);
    }
  }
  // The same webhook asked for again (a make that went without the server's
  // answer, then pressed again) goes as the same request, which the server
  // answers with the one it made, secret and all (lib/uncertainWrites.ts).
  const making = useRef<{ intent: string; requestId: string } | null>(null);
  const sendAgain = useSendAgainWhenAnswered();
  const create = (): Promise<void> => run(async () => {
    setAddressError("");
    const intent = JSON.stringify([url, types]);
    if (making.current?.intent !== intent) making.current = { intent, requestId: newId() };
    let created: EventWebhookCreateResponse;
    try {
      created = await request<EventWebhookCreateResponse>(webhookPath(eventWebhookOperations.create.path, workspace.id), {
        method: eventWebhookOperations.create.method, body: JSON.stringify({ requestId: making.current.requestId, url, eventTypes: types }),
      }, session);
    } catch (failure) {
      const code = failure instanceof LeafenApiError ? failure.code : "";
      if (code === eventWebhookErrorCodes.addressNotPublic) { setAddressError(t("webhookAddressNotPublic")); return; }
      if (code === eventWebhookErrorCodes.addressInvalid) { setAddressError(t("webhookAddressInvalid")); return; }
      // Unanswered, it is sent again, the same, once the server answers.
      sendAgain(failure, () => create());
      throw failure;
    }
    making.current = null;
    setShownSecret(workspace.id, { secret: created.secret, webhookId: created.webhook.id });
    setUrl("");
    setAdding(false);
    await load();
  });
  const showDeliveries = (webhook: EventWebhook) => run(async () => {
    if (open === webhook.id) {
      setOpen("");
      return;
    }
    const listed = await request<EventWebhookDeliveryListResponse>(
      webhookPath(eventWebhookOperations.deliveries.path, workspace.id, webhook.id),
      { method: eventWebhookOperations.deliveries.method }, session);
    setDeliveries(listed.deliveries);
    setOpen(webhook.id);
  });
  const act = (operation: { method: string; path: string }, webhook: EventWebhook, body?: unknown, deliveryId = "") => run(async () => {
    await request<unknown>(webhookPath(operation.path, workspace.id, webhook.id, deliveryId), {
      method: operation.method, body: body === undefined ? undefined : JSON.stringify(body),
    }, session);
    if (operation === eventWebhookOperations.test) setMessage(t("webhookTestQueued"));
    await load();
    if (open === webhook.id) {
      const listed = await request<EventWebhookDeliveryListResponse>(
        webhookPath(eventWebhookOperations.deliveries.path, workspace.id, webhook.id),
        { method: eventWebhookOperations.deliveries.method }, session);
      setDeliveries(listed.deliveries);
    }
  });

  return (
    <section className={settingsStyles.section} role="region" aria-label={t("webhooksSection")} data-event-webhooks>
      <div className={settingsStyles.sectionTitle}>{t("webhooksSection")}</div>
      <p className={styles.note}>{t("webhooksDescription", { header: eventWebhookConstants.signatureHeader })}</p>
      {adding ? (
        <div className={styles.webhookForm} data-webhook-form>
          <div className={styles.webhookField}>
            <label htmlFor="event-webhook-address">{t("webhookUrl")}</label>
            <input id="event-webhook-address" type="url" value={url} placeholder="https://example.com/leafen" data-webhook-url autoFocus
              aria-invalid={addressError ? true : undefined} aria-describedby={addressError ? "event-webhook-address-error" : undefined}
              onChange={(event) => { setUrl(event.target.value); setAddressError(""); }} />
            {/* What is wrong sits right under the field, as a settings field's refusal does. */}
            {addressError ? <p id="event-webhook-address-error" role="alert" data-webhook-address-error>{addressError}</p> : null}
          </div>
          <fieldset className={styles.webhookTypes}>
            <legend>{t("webhookEvents")}</legend>
            {eventWebhookTypes.map((type) => (
              <label key={type}>
                <input type="checkbox" checked={types.includes(type)} data-webhook-type={type} aria-label={type}
                  onChange={(event) => setTypes((current) => event.target.checked
                    ? [...current, type] : current.filter((value) => value !== type))} />
                <code>{type}</code>
              </label>
            ))}
          </fieldset>
          <div className={styles.webhookFormActions}>
            <button type="button" className={styles.secondary} onClick={() => { setAdding(false); setUrl(""); setAddressError(""); }}>
              {t("common:actions.cancel")}
            </button>
            <button type="button" className={styles.primary} disabled={busy || !url.trim() || types.length === 0}
              onClick={() => void create()}>
              {t("webhookAdd")}
            </button>
          </div>
        </div>
      ) : (
        <div className={styles.webhookFormActions}>
          <button type="button" className={settingsStyles.secondaryButton} data-webhook-add onClick={() => { setShownSecret(workspace.id, null); setAdding(true); }}>
            {t("webhookAdd")}
          </button>
        </div>
      )}
      {secret ? (
        <div className={settingsStyles.recoveryCodes} data-webhook-secret>
          <div className={settingsStyles.recoveryHeader}>
            <div>
              <strong>{t("webhookSecretTitle")}</strong>
              <span>{t("webhookSecretDescription")}</span>
            </div>
            {/* Shown once, so it is one press to take, as a made key is. */}
            <button type="button" className={settingsStyles.secondaryButton} data-webhook-secret-copy
              onClick={() => void notice(secret, t("webhookSecretCopied"))}>
              {t("copy")}
            </button>
          </div>
          <code>{secret}</code>
        </div>
      ) : null}
      {readFailed ? <ListNotice role="alert" onRetry={() => { setReadFailed(""); void load(); }} data-webhooks-read-failed>{readFailed}</ListNotice> : null}
      {webhooks && webhooks.length > 0 ? (
        <table className={styles.tokens} data-event-webhook-list>
          <thead>
            <tr>
              <th scope="col">{t("webhookUrl")}</th>
              <th scope="col">{t("webhookEvents")}</th>
              <th scope="col">{t("webhookLastDelivery")}</th>
              <th scope="col"><span className={styles.hidden}>{t("webhookActions")}</span></th>
            </tr>
          </thead>
          <tbody>
            {webhooks.map((webhook) => (
              <tr key={webhook.id} data-event-webhook={webhook.id}>
                <td className={styles.webhookAddress}>
                  {webhook.url}
                  {webhook.connectionName ? <small> · {t("webhookByConnection", { connection: webhook.connectionName })}</small> : null}
                </td>
                <td>{webhook.eventTypes.length}</td>
                <td>{webhook.lastDelivery
                  ? `${t(`webhookState.${webhook.lastDelivery.state}`)} · ${time(webhook.lastDelivery.at)}` : t("webhookNoDeliveries")}</td>
                <td className={styles.webhookActionsCell}>
                  {/* A row's actions sit behind one "…" button, as a settings table's do. */}
                  <SettingsRowMenu label={t("organizationMemberMenu", { name: webhook.url })} data={{ "data-webhook-menu": webhook.id }} rows={[
                    { key: "active", label: webhook.active ? t("webhookPause") : t("webhookResume"), disabled: busy,
                      run: () => void act(eventWebhookOperations.update, webhook, { active: !webhook.active }) },
                    { key: "test", label: t("webhookTest"), disabled: busy || !webhook.active, run: () => void act(eventWebhookOperations.test, webhook) },
                    { key: "deliveries", label: t("webhookDeliveries"), disabled: busy, run: () => void showDeliveries(webhook) },
                    { key: "remove", label: t("webhookRemove"), danger: true, separatorBefore: true, disabled: busy,
                      run: () => void confirmAction({ message: t("webhookRemoveConfirm", { url: webhook.url }), detail: t("webhookRemoveConfirmDetail"),
                        confirmLabel: t("webhookRemove"), danger: true })
                        .then((confirmed) => {
                          if (!confirmed) return;
                          if (secretFor === webhook.id) setShownSecret(workspace.id, null);
                          void act(eventWebhookOperations.remove, webhook);
                        }) },
                  ]} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {open ? (
        <ul className={styles.webhookDeliveries} data-webhook-deliveries>
          {deliveries.length === 0 ? <li>{t("webhookNoDeliveries")}</li> : deliveries.map((delivery) => (
            <li key={delivery.id} data-state={delivery.state}>
              <code>{delivery.eventType}</code>
              <span>{t(`webhookState.${delivery.state}`)}{delivery.statusCode ? ` · ${delivery.statusCode}` : ""}
                {delivery.errorCode ? ` · ${delivery.errorCode}` : ""} · {t("webhookAttempts", { count: delivery.attempts })}</span>
              <time>{time(delivery.createdAt)}</time>
              {delivery.state === "failed" ? (
                <button type="button" className={styles.secondary} disabled={busy}
                  onClick={() => {
                    const webhook = webhooks?.find((item) => item.id === open);
                    if (webhook) void act(eventWebhookOperations.retry, webhook, undefined, delivery.id);
                  }}>{t("webhookRetry")}</button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {message ? <div className={settingsStyles.noticeText} role="status" {...writeUncertainMark(message)}>{message}</div> : null}
    </section>
  );
}
