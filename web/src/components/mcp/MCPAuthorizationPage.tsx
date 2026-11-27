import { useEffect, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";

import { api } from "../../api";
import { mcpConsentService, runMCPConsentAction } from "../../lib/mcpConsentService";
import { useReadGeneration, useRequestGeneration } from "../../lib/useReadGeneration";
import { mcpOAuthErrorCodes, type MCPAuthorizationConsent } from "../../mcpOAuth";
import type { Session, Workspace } from "../../types";
import styles from "./MCPAuthorizationPage.module.css";
import { keysFocusHolder } from "@/lib/lastPress";

// A workspace's box the keys' focus is in takes the keyboard's ring (data-keys-within).
keysFocusHolder(styles.workspace);

type MCPAuthorizationPageProps = {
  authorizationRequest: string;
  session: Session;
};

// How few connections left before the screen says so.
const CONNECTIONS_NOTICE_LEFT = 4;

function mergeWorkspaces(current: Workspace[], next: Workspace[]) {
  const byId = new Map(current.map((workspace) => [workspace.id, workspace]));
  for (const workspace of next) byId.set(workspace.id, workspace);
  return [...byId.values()];
}

// A workspace an app keeps its data in, one an import is staging in, or the person's own.
function consentWorkspaceKind(workspace: Workspace): "own" | "app" | "staging" {
  if (workspace.appInstallId) return "app";
  return workspace.staging ? "staging" : "own";
}

export function MCPAuthorizationPage({ authorizationRequest, session }: MCPAuthorizationPageProps) {
  const { t } = useTranslation("authGate");
  const headingRef = useRef<HTMLHeadingElement>(null);
  const [consent, setConsent] = useState<MCPAuthorizationConsent | null>(null);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  // An app that acts as itself is installed in one workspace as a guest
  // (it sees the pages shared with it) or a member.
  const [role, setRole] = useState<"guest" | "member">("guest");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The answer to a web agent waiting at the tunnel's address.
  const [answered, setAnswered] = useState<"allowed" | "denied" | null>(null);
  const beginConsentRead = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `mcp-consent:${authorizationRequest}`,
  });
  const beginConsentAction = useRequestGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `mcp-consent-action:${authorizationRequest}`,
  });

  useEffect(() => {
    const read = beginConsentRead();
    setConsent(null);
    setWorkspaces([]);
    setSelected([]);
    setBusy(false);
    setError("");
    mcpConsentService.initial(session, authorizationRequest, read.signal)
      .then((next) => {
        if (!read.isCurrent()) return;
        setConsent(next);
        setWorkspaces(next.workspaces);
        // The person's own workspaces start chosen; an app's or an import's
        // space only when they choose it (2026-10-16: nothing was chosen,
        // and those looked like the rest).
        if (!next.integration) setSelected(next.workspaces.filter((place) => consentWorkspaceKind(place) === "own").map((place) => place.id).sort());
        window.requestAnimationFrame(() => headingRef.current?.focus());
      })
      .catch(() => {
        if (read.isCurrent()) setError(t("mcpConsent.loadFailed"));
      });
  }, [authorizationRequest, beginConsentRead, session]);

  // The person's connections are full: approving would fail, so the screen
  // says why and reads the count again when they come back from settings.
  const connectionsFull = Boolean(consent && consent.connectionLimit > 0
    && consent.activeConnections >= consent.connectionLimit);
  const connectionsLeft = consent ? consent.connectionLimit - consent.activeConnections : 0;
  useEffect(() => {
    if (!connectionsFull) return;
    const recount = () => {
      mcpConsentService.initial(session, authorizationRequest)
        .then((next) => setConsent((current) => current ? { ...current, activeConnections: next.activeConnections } : current))
        .catch(() => undefined);
    };
    window.addEventListener("focus", recount);
    return () => window.removeEventListener("focus", recount);
  }, [authorizationRequest, connectionsFull, session]);

  function toggleWorkspace(workspaceId: string, checked: boolean) {
    if (consent?.integration) {
      setSelected(checked ? [workspaceId] : []);
      return;
    }
    setSelected((current) => checked
      ? [...current, workspaceId].sort()
      : current.filter((id) => id !== workspaceId));
  }

  function navigateToClient(redirectUrl: string) {
    window.location.assign(redirectUrl);
  }

  async function loadMore() {
    if (!consent?.hasMore || !consent.nextCursor || busy) return;
    const read = beginConsentRead();
    setBusy(true);
    setError("");
    try {
      const result = await mcpConsentService.continueOrRestart(
        session,
        authorizationRequest,
        consent.nextCursor,
        consent.membershipSnapshot,
        read.signal,
      );
      if (!read.isCurrent()) return;
      const next = result.consent;
      const nextWorkspaces = result.restarted
        ? next.workspaces
        : mergeWorkspaces(workspaces, next.workspaces);
      setWorkspaces(nextWorkspaces);
      setSelected((current) => current.filter((id) => (
        nextWorkspaces.some((workspace) => workspace.id === id)
      )));
      setConsent(next);
    } catch {
      if (read.isCurrent()) setError(t("mcpConsent.moreWorkspacesFailed"));
    } finally {
      if (read.isCurrent()) setBusy(false);
    }
  }

  async function approve() {
    if (selected.length === 0 || busy) return;
    const action = beginConsentAction();
    setBusy(true);
    setError("");
    await runMCPConsentAction({
      execute: (signal) => api.approveMCPAuthorization(
        session,
        authorizationRequest,
        selected,
        { signal, integrationRole: consent?.integration ? role : undefined },
      ),
      generation: action,
      navigate: navigateToClient,
      answeredRemotely: () => setAnswered("allowed"),
      publishFailure: (cause) => {
        const code = (cause as { code?: unknown } | null)?.code;
        if (code === mcpOAuthErrorCodes.connectionCapacity) {
          setConsent((current) => current ? { ...current, activeConnections: Math.max(current.activeConnections, current.connectionLimit) } : current);
        } else {
          setError(t(code === mcpOAuthErrorCodes.requestCapacity ? "mcpConsent.waitingFull" : "mcpConsent.approveFailed"));
        }
        setBusy(false);
      },
    });
  }

  async function deny() {
    if (busy) return;
    const action = beginConsentAction();
    setBusy(true);
    setError("");
    await runMCPConsentAction({
      execute: (signal) => api.denyMCPAuthorization(session, authorizationRequest, { signal }),
      generation: action,
      navigate: navigateToClient,
      answeredRemotely: () => setAnswered("denied"),
      publishFailure: () => {
        setError(t("mcpConsent.denyFailed"));
        setBusy(false);
      },
    });
  }

  // An app asking for the native API names the key scope it would get.
  const app = Boolean(consent?.resource.endsWith("/api/leafen/v1"));
  // An agent's program asking for Leafen's MCP makes an agent connection: it
  // works as the person in all their workspaces, so none are chosen here
  // (internal/app/connection_person_rights.go). An app registered with
  // Leafen (lapp_) still chooses them.
  const agent = Boolean(consent && !app && !consent.integration && !consent.client.id.startsWith("lapp_"));
  if (answered) {
    return (
      <main className={styles.layout}>
        <section className={styles.card} aria-labelledby="mcp-authorization-title" data-mcp-consent-remote={answered}>
          <img className={styles.mark} src="/mark-128.png" alt="" width="48" height="48" />
          <h1 id="mcp-authorization-title" tabIndex={-1}>{app ? t("mcpConsent.appTitle") : t("mcpConsent.title")}</h1>
          <p className={styles.intro} role="status">{t(answered === "allowed" ? "mcpConsent.remoteAllowed" : "mcpConsent.remoteDenied")}</p>
          <div className={styles.actions}>
            <button className={styles.allow} type="button" onClick={() => window.location.assign("/")}>{t("mcpConsent.backToWorkspace")}</button>
          </div>
        </section>
      </main>
    );
  }
  return (
    <main className={styles.layout}>
      <section className={styles.card} aria-labelledby="mcp-authorization-title">
        <img className={styles.mark} src="/mark-128.png" alt="" width="48" height="48" />
        <h1 id="mcp-authorization-title" ref={headingRef} tabIndex={-1}>
          {app ? t("mcpConsent.appTitle") : t("mcpConsent.title")}
        </h1>
        {consent ? (
          <>
            <p className={styles.intro}>
              <Trans t={t} i18nKey={agent ? "mcpConsent.agentIntro" : "mcpConsent.chooseWorkspaces"} values={{ client: consent.client.name }}
                components={{ client: <span className={styles.clientName} /> }} />
            </p>
            <p className={styles.permissionNote} data-app-scope={app ? consent.scopes[0] : undefined}>
              {consent.integration ? t(`mcpConsent.integrationAccess.${consent.scopes[0]}`)
                : app ? t(`mcpConsent.appAccess.${consent.scopes[0]}`) : agent ? t("mcpConsent.agentAccess") : t("mcpConsent.access")}
            </p>
            {connectionsFull ? (
              <p className={styles.error} role="alert" data-mcp-consent-capacity="full">
                {t("mcpConsent.connectionsFull", { limit: consent.connectionLimit })}{" "}
                <a href="/settings?section=connections" target="_blank" rel="noopener">{t("mcpConsent.manageConnections")}</a>
              </p>
            ) : consent.connectionLimit > 0 && connectionsLeft <= CONNECTIONS_NOTICE_LEFT ? (
              <p className={styles.permissionNote} data-mcp-consent-capacity="near">
                {t("mcpConsent.connectionsNear", { left: connectionsLeft, limit: consent.connectionLimit })}
              </p>
            ) : null}
            {consent.integration ? (
              <fieldset className={styles.workspaceSet} disabled={busy} data-integration-role>
                <legend>{t("mcpConsent.integrationLegend")}</legend>
                <p className={styles.permissionNote}>{t("mcpConsent.integrationNote")}</p>
                <label className={styles.workspace}>
                  <input type="radio" name="integration-role" checked={role === "guest"} onChange={() => setRole("guest")} />
                  <span>{t("mcpConsent.integrationGuest")}</span>
                </label>
                <label className={styles.workspace}>
                  <input type="radio" name="integration-role" checked={role === "member"} onChange={() => setRole("member")} />
                  <span>{t("mcpConsent.integrationMember")}</span>
                </label>
              </fieldset>
            ) : null}
            {agent ? null : <fieldset className={styles.workspaceSet} disabled={busy}>
              <legend>{t("mcpConsent.workspacesLegend")}</legend>
              {(["own", "app", "staging"] as const).map((kind) => {
                const places = workspaces.filter((workspace) => consentWorkspaceKind(workspace) === kind);
                if (places.length === 0) return null;
                return (
                  <div key={kind} data-mcp-consent-workspaces={kind}>
                    {kind !== "own" ? <p className={styles.permissionNote}>{t(`mcpConsent.${kind}SpacesNote`)}</p> : null}
                    {places.map((workspace) => (
                      <label className={styles.workspace} key={workspace.id}>
                        <input
                          type={consent.integration ? "radio" : "checkbox"}
                          name={consent.integration ? "integration-workspace" : undefined}
                          checked={selected.includes(workspace.id)}
                          onChange={(event) => toggleWorkspace(workspace.id, event.target.checked)}
                        />
                        <span>{workspace.name}</span>
                      </label>
                    ))}
                  </div>
                );
              })}
              {consent.hasMore && (
                <button className={styles.more} type="button" onClick={() => void loadMore()} disabled={busy}>
                  {t("mcpConsent.moreWorkspaces")}
                </button>
              )}
            </fieldset>}
            <div className={styles.actions}>
              <button className={styles.deny} data-mcp-consent-deny type="button" onClick={() => void deny()} disabled={busy}>
                {t("mcpConsent.deny")}
              </button>
              <button className={styles.allow} data-mcp-consent-allow type="button" onClick={() => void approve()} disabled={busy || selected.length === 0 || connectionsFull}>
                {busy ? t("mcpConsent.processing") : t("mcpConsent.allow")}
              </button>
            </div>
          </>
        ) : !error ? (
          <p className={styles.status} role="status">{t("mcpConsent.loading")}</p>
        ) : null}
        {error && <p className={styles.error} role="alert">{error}</p>}
      </section>
    </main>
  );
}
