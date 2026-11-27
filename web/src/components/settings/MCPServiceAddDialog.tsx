import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { CheckIcon, ChevronDown, ChevronRight, Plus, Trash, XCircleIcon } from "@/components/icons";
import { useOverlayLayer } from "@/lib/overlayLayer";
import {
  addMCPService, probeMCPService, readMCPService, signInMCPService,
  type MCPService, type MCPServiceAddition, type MCPServiceHeaderAuth, type MCPServiceProbe,
} from "@/mcpServicesApi";
import type { Session } from "@/types";
import admin from "@/components/admin/AdministratorApps.module.css";
import dialog from "./MCPServices.module.css";
import { CLIENT_CODES, serviceError, serviceName } from "./MCPServices";
import { openServiceSignInWindow, serviceSignInFailure, serviceSignInWord } from "./mcpServiceSignIn";
import { hearSignIn } from "@/lib/signInWindow";
import { AppSelect } from "@/components/AppSelect";

// Answers after which the service is connected with headers the person gives.
const TOKEN_CODES = new Set(["LEAFEN_MCP_SERVICE_TOKEN_NEEDED", "LEAFEN_MCP_SERVICE_TOKEN_REFUSED", "LEAFEN_MCP_SERVICE_UNSAFE_SIGN_IN"]);
// The address is looked at once the person stops typing.
const PROBE_DELAY = 500;

type Looked = { url: string } & ({ state: "looking" } | { state: "found"; probe: MCPServiceProbe } | { state: "wrong"; message: string });
type AuthType = MCPServiceHeaderAuth["type"];
type HeaderRow = { key: number; name: string; value: string };

/**
 * "사용자 지정 MCP 서버", as Notion's (seen 2026-10-13): the address, looked
 * at as it is typed — a check mark, or a line under it saying what is wrong
 * — then the connection's name and how the server signs in. A server with a
 * sign-in page opens it in a window of its own (or, where windows can't be
 * opened, from a link the person follows), asking for the scopes kept, and
 * under "고급 설정" takes an OAuth client of the person's own; any other
 * server takes a "Bearer token", "Basic" or "API Key" and, under "고급 설정",
 * headers of the person's own. The dialog closes once the server reads the
 * service as connected. Given a service, it connects that one again. A
 * service Leafen can't be a client of by itself (it registers no clients, or
 * reads Leafen at an address it can't reach) is said before signing in, with
 * "고급 설정" open for the client the person makes there; one that refused
 * Leafen's registration is said the same way once connecting tried it;
 * signing in again to one shows the form for that too. A sign-in server the
 * service named that isn't answering now is said under the sign-in form as
 * the address is looked at, and in place of that once an attempt failed.
 */
export function MCPServiceAddDialog({ session, again, onChanged, onClose }: {
  session: Session;
  again: MCPService | null;
  onChanged: () => Promise<unknown>;
  onClose: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const titleId = useId();
  const formId = useId();
  const urlErrorId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const urlRef = useRef<HTMLInputElement>(null);
  const [url, setUrl] = useState(again?.url ?? "");
  const [looked, setLooked] = useState<Looked | null>(null);
  const [name, setName] = useState(again?.name ?? "");
  const nameTyped = useRef(again !== null);
  const [authType, setAuthType] = useState<AuthType>("bearer");
  const [prefix, setPrefix] = useState<"Bearer" | "Token">("Bearer");
  const [secret, setSecret] = useState({ token: "", username: "", password: "", key: "", value: "" });
  const [rows, setRows] = useState<HeaderRow[]>([]);
  const [advanced, setAdvanced] = useState(false);
  const [scopes, setScopes] = useState<string[]>([]);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  // A sign-in page the service refused Leafen: its headers instead.
  const [headersInstead, setHeadersInstead] = useState(false);
  // Refused for want of the person's own client (CLIENT_CODES), when the
  // look at the address didn't already say so; signing in again then shows
  // the form.
  const [refused, setRefused] = useState<{ url: string; code: string } | null>(null);
  const formShown = again?.auth !== "oauth" || refused !== null;
  const [signing, setSigning] = useState<{ service: MCPService; authorizeUrl: string } | null>(null);
  const [busy, setBusy] = useState(again?.auth === "oauth");
  const [error, setError] = useAttemptNotice<string>(url, name, authType, prefix, secret, rows, scopes, clientId, clientSecret, headersInstead);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: urlRef,
  });

  // Signing in again asks the server for a new sign-in page at once.
  useEffect(() => {
    if (again?.auth !== "oauth") return;
    let live = true;
    signInMCPService(session, again.id).then(
      (answer) => { if (live && answer.authorizeUrl) setSigning({ service: again, authorizeUrl: answer.authorizeUrl }); },
      (failure) => {
        if (!live) return;
        if (failure instanceof LeafenApiError && CLIENT_CODES.has(failure.code)) {
          setRefused({ url: again.url, code: failure.code });
          setAdvanced(true);
        } else setError(serviceError(t, failure, t("mcpService.signInFailed")));
      },
    ).finally(() => { if (live) setBusy(false); });
    return () => { live = false; };
  }, [again, session, t]);

  // The address is looked at once typing stops; a new one forgets the last.
  const address = url.trim();
  useEffect(() => {
    if (!address || !formShown) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      let parsed: URL | null = null;
      try {
        parsed = new URL(address);
      } catch {
        parsed = null;
      }
      if (!parsed?.host || !/^https?:$/u.test(parsed.protocol)) {
        setLooked({ url: address, state: "wrong", message: t("mcpService.errorAddress") });
        return;
      }
      setLooked({ url: address, state: "looking" });
      probeMCPService(session, address, controller.signal).then((probe) => {
        setLooked({ url: address, state: "found", probe });
        setScopes(probe.scopes);
        if (probe.clientNeeded) setAdvanced(true);
        if (probe.name && !nameTyped.current) setName(probe.name);
      }, (failure) => {
        if (controller.signal.aborted) return;
        const invalid = failure instanceof LeafenApiError && failure.code === "LEAFEN_MCP_SERVICE_INVALID";
        setLooked({ url: address, state: "wrong", message: invalid ? t(parsed?.protocol === "http:" ? "mcpService.errorHttps" : "mcpService.errorAddress")
          : serviceError(t, failure, t("mcpService.errorFailed")) });
      });
    }, PROBE_DELAY);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [address, formShown, session, t]);

  // The sign-in completes on the server, wherever the page was opened; the
  // page it comes back to says so at once and closes (lib/signInWindow.ts).
  // One that says why it failed (the service's sign-in server down for now)
  // is said here, the form back to try again.
  const signingId = signing?.service.id;
  useEffect(() => {
    if (!signingId) return;
    let done = false;
    const look = () => {
      void readMCPService(session, signingId).then(async ({ service }) => {
        if (done || service.status !== "connected") return;
        done = true;
        await onChanged();
        onClose();
      }, () => undefined);
    };
    const watch = window.setInterval(look, 2000);
    const stop = hearSignIn(serviceSignInWord(signingId), (word) => {
      const failure = serviceSignInFailure(word);
      if (!failure) return look();
      done = true;
      setSigning(null);
      setError(serviceError(t, failure, t("mcpService.signInFailed")));
    });
    return () => { window.clearInterval(watch); stop(); };
  }, [onChanged, onClose, session, setError, signingId, t]);

  const current = looked?.url === address ? looked : null;
  const probe = current?.state === "found" ? current.probe : null;
  const signInPage = probe?.signIn === "oauth" && !headersInstead;
  // What Leafen needs to sign in here: a client the person makes on the
  // service, and why — said before the sign-in.
  const need = !signInPage ? "" : probe?.clientNeeded ?? (refused?.url === address ? refused.code : "");
  const needText = need === "LEAFEN_MCP_SERVICE_ADDRESS_UNREACHABLE"
    ? probe?.publicUrl ? t("mcpService.unreachablePublic", { url: probe.publicUrl }) : t("mcpService.unreachable")
    : need === "LEAFEN_MCP_SERVICE_REGISTRATION_REFUSED" ? t("mcpService.registrationRefused")
    : need ? t("mcpService.errorClientNeeded") : "";
  const credentials: MCPServiceHeaderAuth = authType === "basic" ? { type: "basic", username: secret.username, password: secret.password }
    : authType === "api_key" ? { type: "api_key", key: secret.key.trim(), value: secret.value.trim() }
    : { type: "bearer", prefix, token: secret.token.trim() };
  const credentialsGiven = authType === "basic" ? secret.username !== "" && secret.password !== ""
    : authType === "api_key" ? secret.key.trim() !== "" && secret.value.trim() !== "" : secret.token.trim() !== "";
  const credentialsStarted = authType === "basic" ? secret.username !== "" || secret.password !== ""
    : authType === "api_key" ? secret.key.trim() !== "" || secret.value.trim() !== "" : secret.token.trim() !== "";
  const rowsReady = rows.every((row) => (row.name.trim() === "") === (row.value.trim() === ""));
  const ready = probe !== null && name.trim() !== "" && (signInPage
    ? (probe.scopes.length === 0 || scopes.length > 0) && (!need || clientId.trim() !== "")
    : rowsReady && (credentialsGiven || probe.open && !credentialsStarted));

  async function connect(event: FormEvent) {
    event.preventDefault();
    if (!ready || !probe) return;
    const given: MCPServiceAddition = { name: name.trim() };
    if (signInPage) {
      if (scopes.length < probe.scopes.length) given.scopes = scopes;
      if (clientId.trim()) given.client = { clientId: clientId.trim(), clientSecret: clientSecret.trim() };
    } else {
      if (credentialsStarted) given.auth = credentials;
      given.headers = rows.filter((row) => row.name.trim()).map((row) => ({ name: row.name.trim(), value: row.value.trim() }));
    }
    // Opened now, while the click counts; closed again when no sign-in is asked.
    const popup = signInPage ? openServiceSignInWindow() : null;
    setBusy(true);
    setError("");
    try {
      const answer = await addMCPService(session, address, given);
      await onChanged();
      if (!answer.authorizeUrl) {
        popup?.close();
        onClose();
        return;
      }
      setSigning({ service: answer.service, authorizeUrl: answer.authorizeUrl });
      if (popup) popup.location.href = answer.authorizeUrl;
    } catch (failure) {
      popup?.close();
      if (failure instanceof LeafenApiError && TOKEN_CODES.has(failure.code)) setHeadersInstead(true);
      if (failure instanceof LeafenApiError && CLIENT_CODES.has(failure.code)) {
        // Said once, beside the client's fields, rather than again below.
        setRefused({ url: address, code: failure.code });
        setAdvanced(true);
      } else setError(serviceError(t, failure, t("mcpService.addFailed")));
    } finally {
      setBusy(false);
    }
  }

  const field = (label: string, value: string, change: (value: string) => void, options: { secret?: boolean; placeholder?: string; mark?: string } = {}) => (
    <label className={dialog.field}>
      <span>{label}</span>
      <input type={options.secret ? "password" : "text"} autoComplete="off" spellCheck={false} value={value} placeholder={options.placeholder}
        data-mcp-service-field={options.mark} onChange={(event) => change(event.target.value)} />
    </label>
  );
  const typed = (key: keyof typeof secret) => (value: string) => setSecret((shown) => ({ ...shown, [key]: value }));

  return createPortal(
    <div ref={overlayRef} className={admin.overlay} style={{ zIndex }} data-mcp-service-add>
      <button type="button" className={admin.backdrop} tabIndex={-1} aria-label={t("mcpService.cancel")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={admin.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <div>
          <h2 id={titleId} className={admin.dialogTitle}>{t("mcpService.title")}</h2>
          <p className={admin.note}>{t("mcpService.subtitle")}</p>
        </div>
        {signing ? (
          <>
            <p className={admin.note} role="status">{t("mcpService.signingIn", { name: serviceName(signing.service) })}</p>
            <a className={dialog.openSignIn} href={signing.authorizeUrl} target="_blank" rel="noreferrer" data-mcp-service-sign-in>
              {t("mcpService.openSignIn")}
            </a>
          </>
        ) : !formShown ? null : (
          <form id={formId} className={dialog.form} onSubmit={(event) => void connect(event)}>
            <label className={dialog.field}>
              <span>{t("mcpService.urlLabel")}</span>
              <span className={dialog.urlBox} data-state={current?.state ?? "idle"}>
                <input ref={urlRef} type="url" inputMode="url" autoComplete="off" spellCheck={false} required value={url}
                  readOnly={again !== null} placeholder="https://example.com/mcp" aria-invalid={current?.state === "wrong" || undefined}
                  aria-describedby={current?.state === "wrong" ? urlErrorId : undefined} onChange={(event) => setUrl(event.target.value)} />
                {current?.state === "found" ? <span className={dialog.urlFound} aria-hidden="true"><CheckIcon size="small" /></span>
                  : current?.state === "wrong" ? <XCircleIcon className={dialog.urlWrong} size="small" aria-hidden="true" /> : null}
              </span>
              {current?.state === "wrong" ? <small id={urlErrorId} className={dialog.urlError} role="alert">{current.message}</small> : null}
            </label>
            {probe ? (
              <>
                {field(t("mcpService.nameLabel"), name, (value) => { nameTyped.current = true; setName(value); },
                  { placeholder: t("mcpService.namePlaceholder"), mark: "name" })}
                {signInPage ? (
                  <>
                    <p className={dialog.clientNote}>{t("mcpService.redirectNote")}</p>
                    {needText ? <p className={dialog.clientNeed} role="status" data-mcp-service-client-needed={need}>{needText}</p> : null}
                    {probe.signInUnavailable && !error ? (
                      <p className={dialog.clientNeed} role="status" data-mcp-service-sign-in-unavailable>{t("mcpService.signInUnavailable")}</p>
                    ) : null}
                    {probe.scopes.length > 0 ? (
                      <div className={dialog.choice}>
                        <span className={dialog.choiceText}>
                          <strong>{t("mcpService.scopesLabel")}</strong>
                          <small>{t("mcpService.scopesHint")}</small>
                        </span>
                        <details className={dialog.scopes} data-mcp-service-scopes>
                          <summary>
                            {scopes.length === probe.scopes.length ? t("mcpService.scopesAll") : t("mcpService.scopesSome", { count: scopes.length })}
                            <ChevronDown size="small" aria-hidden="true" />
                          </summary>
                          <div className={dialog.scopeList}>
                            {probe.scopes.map((scope) => (
                              <label key={scope}>
                                <input type="checkbox" checked={scopes.includes(scope)} onChange={(event) => setScopes((kept) =>
                                  event.target.checked ? probe.scopes.filter((each) => each === scope || kept.includes(each)) : kept.filter((each) => each !== scope))} />
                                <span>{scope}</span>
                              </label>
                            ))}
                          </div>
                        </details>
                      </div>
                    ) : null}
                  </>
                ) : (
                  <>
                    <label className={dialog.choice}>
                      <strong>{t("mcpService.authLabel")}</strong>
                      <AppSelect look="value" value={authType} aria-label={t("mcpService.authLabel")} data-mcp-service-auth onChange={(event) => setAuthType(event.target.value as AuthType)}>
                        <option value="bearer">Bearer token</option>
                        <option value="basic">Basic</option>
                        <option value="api_key">API Key</option>
                      </AppSelect>
                    </label>
                    {authType === "bearer" ? (
                      <>
                        <label className={dialog.choice}>
                          <strong>{t("mcpService.prefixLabel")}</strong>
                          <AppSelect look="value" value={prefix} aria-label={t("mcpService.prefixLabel")} data-mcp-service-prefix onChange={(event) => setPrefix(event.target.value as "Bearer" | "Token")}>
                            <option value="Bearer">Bearer</option>
                            <option value="Token">Token</option>
                          </AppSelect>
                        </label>
                        {field("Token", secret.token, typed("token"), { secret: true, mark: "token" })}
                        {secret.token.trim() ? <small className={dialog.sent}>{`Authorization: ${prefix} *`}</small> : null}
                      </>
                    ) : authType === "basic" ? (
                      <>
                        {field("Username", secret.username, typed("username"), { mark: "username" })}
                        {field("Password", secret.password, typed("password"), { secret: true, mark: "password" })}
                      </>
                    ) : (
                      <>
                        {field("Key", secret.key, typed("key"), { mark: "key" })}
                        {field("Value", secret.value, typed("value"), { secret: true, mark: "value" })}
                      </>
                    )}
                  </>
                )}
                <div className={dialog.advanced} data-open={advanced || undefined}>
                  <button type="button" className={dialog.advancedToggle} aria-expanded={advanced} onClick={() => setAdvanced((open) => !open)}>
                    <span>{t("mcpService.advanced")}</span>
                    {advanced ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}
                  </button>
                  {!advanced ? null : signInPage ? (
                    <div className={dialog.client} data-mcp-service-client>
                      <p className={dialog.clientNote}>{t("mcpService.clientNote")}</p>
                      <label className={dialog.field}>
                        <span>{t("mcpService.callbackLabel")}</span>
                        <input type="text" readOnly value={probe.callbackUrl ?? ""} spellCheck={false} data-mcp-service-callback
                          onFocus={(event) => event.currentTarget.select()} />
                      </label>
                      {field(t("mcpService.clientIdLabel"), clientId, setClientId, { mark: "client-id" })}
                      {field(t("mcpService.clientSecretLabel"), clientSecret, setClientSecret, { secret: true, mark: "client-secret" })}
                      <button type="button" className={dialog.useToken} onClick={() => setHeadersInstead(true)}>{t("mcpService.useToken")}</button>
                    </div>
                  ) : (
                    <div className={dialog.client} data-mcp-service-headers>
                      <p className={dialog.clientNote}>{t("mcpService.headersNote")}</p>
                      {rows.map((row) => (
                        <div key={row.key} className={dialog.headerRow}>
                          <input type="text" autoComplete="off" spellCheck={false} value={row.name} placeholder={t("mcpService.headerName")}
                            aria-label={t("mcpService.headerName")} onChange={(event) => setRows((shown) =>
                              shown.map((each) => each.key === row.key ? { ...each, name: event.target.value } : each))} />
                          <input type="text" autoComplete="off" spellCheck={false} value={row.value} placeholder={t("mcpService.headerValue")}
                            aria-label={t("mcpService.headerValue")} onChange={(event) => setRows((shown) =>
                              shown.map((each) => each.key === row.key ? { ...each, value: event.target.value } : each))} />
                          <button type="button" aria-label={t("mcpService.headerRemove")}
                            onClick={() => setRows((shown) => shown.filter((each) => each.key !== row.key))}>
                            <Trash size="small" aria-hidden="true" />
                          </button>
                        </div>
                      ))}
                      {rows.length < 20 ? (
                        <button type="button" className={dialog.addHeader}
                          onClick={() => setRows((shown) => [...shown, { key: Date.now() + shown.length, name: "", value: "" }])}>
                          <Plus size="small" aria-hidden="true" />
                          {t("mcpService.headerAdd")}
                        </button>
                      ) : null}
                    </div>
                  )}
                </div>
                <div className={dialog.warning}>
                  <strong>{t("mcpService.warningTitle")}</strong>
                  <span>{t("mcpService.warningBody")}</span>
                </div>
              </>
            ) : null}
          </form>
        )}
        {error ? <div className={admin.error} role="alert">{error}</div> : null}
        <div className={admin.dialogActions}>
          <button type="button" className={admin.cancel} disabled={busy} onClick={onClose}>{t("mcpService.cancel")}</button>
          {signing ? null : (
            <button type="submit" form={formId} className={admin.primary} disabled={busy || !ready}>
              {busy ? t("mcpService.connecting") : t("mcpService.connect")}
            </button>
          )}
        </div>
      </section>
    </div>,
    document.body,
  );
}
