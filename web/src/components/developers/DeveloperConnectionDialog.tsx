import { type FormEvent, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { appRegistrationOperations, type AppRegistration } from "@/appRegistrationWire.generated";
import { AppSelect } from "@/components/AppSelect";
import { useProductPages } from "@/components/product/productPagesContext";
import { newId } from "@/lib/ids";
import { createInternalConnection, rememberInternalConnectionSecret } from "@/lib/internalConnections";
import { developerConnectionHref, pushRoute } from "@/lib/router";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { serverUrl } from "@/lib/serverOrigin";
import { settingsErrorMessage, writeUncertainMark } from "@/lib/settingsErrors";
import { type MakeOnce, useSendAgainWhenAnswered } from "@/lib/uncertainWrites";
import type { Session } from "@/types";
import styles from "@/components/settings/DeveloperSettings.module.css";
import portal from "./DeveloperSelect.module.css";

/**
 * "신규 연결", as Notion's (seen 2026-10-30): its name, then "인증 방법" —
 * "API 토큰", first, an internal connection of this workspace with its own
 * token, opened on its page with the token shown once; or "OAuth", an app
 * with its return addresses and whom it acts as, which sends people to
 * /oauth/authorize for the native API with a key's scope, the
 * address the window shows.
 */
export function DeveloperConnectionDialog({ session, makeInternal, onCreated, onClose }: {
  session: Session; onCreated(): void; onClose(): void;
  /** Runs a token connection's make so one sent without the answer isn't made twice. */
  makeInternal: MakeOnce;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null), dialogRef = useRef<HTMLElement>(null), nameRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");
  const [redirects, setRedirects] = useState("");
  const [actsAs, setActsAs] = useState<AppRegistration["actsAs"]>("person");
  const [method, setMethod] = useState<"token" | "oauth">("token");
  const pages = useProductPages();
  const workspaceId = pages?.workspaceId ?? "";
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: nameRef,
  });
  const appRequest = useRef<{ intent: string; requestId: string } | null>(null);
  const sendAgain = useSendAgainWhenAnswered();
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await send();
  }
  async function send() {
    if (busy || !name.trim() || (method === "oauth" && !redirects.trim()) || (method === "token" && !workspaceId)) return;
    setBusy(true);
    setError("");
    try {
      if (method === "token") {
        const made = await makeInternal(() => createInternalConnection(session, workspaceId, name.trim()));
        if (made.secret) rememberInternalConnectionSecret(made.id, made.secret);
        onCreated();
        pushRoute(developerConnectionHref(made.id));
        return;
      }
      // The same app asked for again goes as the same request, which the
      // server answers with the one it made (lib/uncertainWrites.ts).
      const redirectUris = redirects.split(/\s+/u).filter(Boolean);
      const intent = JSON.stringify([name.trim(), redirectUris, actsAs]);
      if (appRequest.current?.intent !== intent) appRequest.current = { intent, requestId: newId() };
      await request<AppRegistration>(appRegistrationOperations.create.path, {
        method: appRegistrationOperations.create.method,
        body: JSON.stringify({ requestId: appRequest.current.requestId, name: name.trim(), redirectUris, actsAs }),
      }, session);
      onCreated();
    } catch (failure) {
      // An app's registration that went unanswered is sent again, the same,
      // once the server answers (an internal connection is found in the list).
      if (method === "oauth") sendAgain(failure, () => send());
      setError(settingsErrorMessage(failure, t("appFailed")));
    } finally {
      setBusy(false);
    }
  }
  const authorize = serverUrl("/oauth/authorize"), resource = serverUrl("/api/leafen/v1");
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-developer-connection-dialog>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        {/* The title and the line under it stay while the rest scrolls, as Notion's. */}
        <h2 id={titleId} className={styles.dialogTitle}>{t("developerTools.newConnection")}</h2>
        <p className={styles.dialogLead}>{t("developerTools.newConnectionDescription")}</p>
        <form className={styles.dialogBody} onSubmit={(event) => void submit(event)}>
          <label className={styles.field}>
            <span className={styles.fieldLabel}>{t("developerTools.connection.name")}</span>
            <input ref={nameRef} type="text" value={name} maxLength={method === "token" ? 100 : 200} data-app-name
              placeholder={t("developerTools.namePlaceholder", { name: session.user.name || session.user.email })}
              onChange={(event) => setName(event.target.value)} />
          </label>
          <div className={styles.field} role="radiogroup" aria-label={t("developerTools.authMethod")}>
            <span className={styles.fieldLabel}>{t("developerTools.authMethod")}</span>
            {(["token", "oauth"] as const).map((choice) => (
              <label key={choice} className={styles.methodChoice} data-checked={method === choice ? "true" : undefined} data-auth-method={choice}>
                <input type="radio" name={`${titleId}-method`} checked={method === choice} onChange={() => setMethod(choice)} />
                <span><strong>{t(`developerTools.authMethods.${choice}`)}</strong><small>{t(`developerTools.authMethods.${choice}Note`)}</small></span>
              </label>
            ))}
          </div>
          {method === "oauth" ? <>
          <p className={styles.dialogNote}>{t("appsDescription")}</p>
          <code className={styles.secret} data-app-authorize-template>
            {`${authorize}?response_type=code&client_id=<id>&redirect_uri=<address>&scope=read|content|structure|admin&resource=${resource}&state=…&code_challenge=…&code_challenge_method=S256`}
          </code>
          <label className={styles.field}>
            <span className={styles.fieldLabel}>{t("appRedirects")}</span>
            <input type="text" value={redirects} placeholder="https://example.com/callback" data-app-redirects
              onChange={(event) => setRedirects(event.target.value)} />
          </label>
          <label className={styles.field}>
            <span className={styles.fieldLabel}>{t("appActsAs")}</span>
            <AppSelect aria-label={t("appActsAs")} value={actsAs} data-app-acts-as
              className={portal.select} settingsList menuWidth={200} menuGap={0} menuAlign="start"
              onChange={(event) => setActsAs(event.target.value as AppRegistration["actsAs"])}>
              <option value="person">{t("appActsAsPerson")}</option>
              <option value="integration">{t("appActsAsIntegration")}</option>
            </AppSelect>
          </label>
          </> : null}
          {error ? <div className={styles.error} role="alert" {...writeUncertainMark(error)}>{error}</div> : null}
          <div className={styles.dialogActions}>
            <button type="button" className={styles.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
            <button type="submit" className={styles.primary}
              disabled={busy || !name.trim() || (method === "oauth" && !redirects.trim())}>{t("developerTools.createConnection")}</button>
          </div>
        </form>
      </section>
    </div>,
    document.body,
  );
}
