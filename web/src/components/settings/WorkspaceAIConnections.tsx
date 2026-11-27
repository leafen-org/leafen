import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsRowMenu } from "./SettingsRowMenu";
import { ListNotice } from "./SettingsRowNotice";
import { confirmAction } from "@/components/ConfirmActionDialog";
import type { MCPConnectionListItem } from "@/mcpConnectionListWire.generated";
import type { Session, Workspace } from "@/types";
import { styles } from "./workspaceSettingsOverviewStyles";
import { useWorkspaceAIConnections } from "./useWorkspaceAIConnections";
import { WorkspaceUserKeyPolicy } from "./WorkspaceUserKeyPolicy";
import { ConnectionAppsDialog } from "@/components/mcp/ConnectionAppsDialog";
import { ConnectionServicesDialog } from "@/components/mcp/ConnectionServicesDialog";
import { useAppPlatformAvailable } from "@/lib/appPlatformAvailability";
import { serverUrl } from "@/lib/serverOrigin";
import { claudeConnectorBlockingPort } from "@/lib/claudeConnector";
import { AgentConnect } from "./AgentConnect";
import { NATIVE_MCP_PATH, connectionKey } from "@/lib/agentConnection";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { displayDate, displayDateTime } from "@/lib/displayDate";
import { AppSelect } from "@/components/AppSelect";
import { useCopyNotice } from "@/lib/copyNotice";
import { developerMcpTokenHref, pushRoute } from "@/lib/router";
import { PERSONAL_ACCESS_TOKEN_LIFETIMES, type PersonalAccessTokenLifetime } from "@/lib/personalAccessTokens";
import { KEY_LIFETIME_KEYS, keyLifetimeEnd } from "./KeyLifetimeRow";
import { useRowNotice } from "@/lib/useNoticeFor";

// How few places left before the connection limit is said.
const CAPACITY_NOTICE_LEFT = 4;

/** The key a connection URI carries after "#key=", for the native API. */
// The Leafen MCP endpoint, as the agent connect button gives (2026-10-16:
// this section gave the strict /mcp, whose agents had no Leafen memory).
export function currentMCPServerUrl() {
  if (typeof window === "undefined") return "";
  return serverUrl(NATIVE_MCP_PATH);
}

function activeConnections<T extends { status: string }>(connections: T[]): T[] {
  return connections.filter((connection) => connection.status !== "revoked");
}

// Coming back within this long of a refresh reads nothing again.
const SECTION_REFRESH_GAP = 2000;
// While a one-time code may be used, the list is read this often, for as
// long as the code lives (the first run's agents step reads every 3 seconds).
const AWAIT_CONNECTION_EVERY_MS = 3000;
const CODE_LIFETIME_MS = 10 * 60_000;

export function WorkspaceAIConnections({
  session,
  workspace,
  onNavigate,
}: {
  session: Session;
  workspace: Workspace;
  /** Goes to a page outside the settings window, closing it. */
  onNavigate?: (path: string) => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const sectionId = useId();
  const connections = useWorkspaceAIConnections({ session, workspace });
  const copyNotice = useCopyNotice();
  const [keyFormOpen, setKeyFormOpen] = useState(false);
  // Closing the key's choices hands focus back to the button that opened them.
  const keyOpenRef = useRef<HTMLButtonElement>(null);
  // How long the key lives: a year unless chosen, as Notion's personal access tokens.
  const [keyLifetime, setKeyLifetime] = useState<PersonalAccessTokenLifetime>("1y");
  const appPlatform = useAppPlatformAvailable();
  const [appsFor, setAppsFor] = useState<{ id: string; name: string } | null>(null);
  const [servicesFor, setServicesFor] = useState<{ id: string; name: string } | null>(null);
  const mcpServerUrl = currentMCPServerUrl();
  const mcpConnectorPort = claudeConnectorBlockingPort(mcpServerUrl);
  const createdKey = connectionKey(connections.createdConnectionUri);
  const snippets = useMemo(() => [
    {
      key: "claude-code",
      label: t("mcpSnippetClaudeCode"),
      value: `claude mcp add --transport http leafen ${mcpServerUrl}`,
    },
    {
      key: "cursor",
      label: t("mcpSnippetCursor"),
      value: JSON.stringify({ mcpServers: { leafen: { url: mcpServerUrl } } }, null, 2),
    },
    {
      key: "generic",
      label: t("mcpSnippetGeneric"),
      value: JSON.stringify({
        mcpServers: {
          leafen: { command: "npx", args: ["-y", "mcp-remote", mcpServerUrl] },
        },
      }, null, 2),
    },
  ], [mcpServerUrl, t]);

  // The buttons keep their words while a toast says what was copied (lib/copyNotice):
  // a line above the section pushed everything under it down.
  function copyMCPText(value: string, said: string) {
    return copyNotice(value, said);
  }

  // A disconnected agent leaves the list; the ones still connected stay.
  const connected = activeConnections(connections.connections);
  // The connections read again when the person comes back to the window,
  // and every few seconds while a one-time code made here may be used, until
  // a new connection shows: an agent connects in its own (2026-10-16: a
  // connection made in a terminal showed only after "새로고침"; 2026-11-06:
  // Leafen's own screens have no "새로고침", as the first run's agents step
  // reads them the same way).
  const lastRefresh = useRef(0);
  const refreshSection = useCallback(() => {
    lastRefresh.current = Date.now();
    void connections.refresh();
  }, [connections.refresh]);
  useEffect(() => {
    const comeBack = () => {
      if (document.visibilityState === "visible" && Date.now() - lastRefresh.current > SECTION_REFRESH_GAP) refreshSection();
    };
    window.addEventListener("focus", comeBack);
    document.addEventListener("visibilitychange", comeBack);
    return () => {
      window.removeEventListener("focus", comeBack);
      document.removeEventListener("visibilitychange", comeBack);
    };
  }, [refreshSection]);
  const [awaitingUntil, setAwaitingUntil] = useState(0);
  const connectedCount = activeConnections(connections.connections).length;
  const countWhenCodeMade = useRef(0);
  // The connections there were when a text was made here: one beyond them
  // is the agent that took it, which "Copy text" says connected.
  const [idsWhenCodeMade, setIdsWhenCodeMade] = useState<ReadonlySet<string> | null>(null);
  const activeIds = useMemo(() => activeConnections(connections.connections).map((connection) => connection.id), [connections.connections]);
  const awaitConnection = useCallback(() => {
    countWhenCodeMade.current = connectedCount;
    setIdsWhenCodeMade((known) => known ?? new Set(activeIds));
    setAwaitingUntil(Date.now() + CODE_LIFETIME_MS);
  }, [connectedCount, activeIds]);
  useEffect(() => {
    if (!awaitingUntil) return undefined;
    if (connectedCount > countWhenCodeMade.current) { setAwaitingUntil(0); return undefined; }
    const every = window.setInterval(() => {
      if (Date.now() > awaitingUntil) { setAwaitingUntil(0); return; }
      if (document.visibilityState === "visible") refreshSection();
    }, AWAIT_CONNECTION_EVERY_MS);
    return () => window.clearInterval(every);
  }, [awaitingUntil, connectedCount, refreshSection]);
  // Each workspace by its name, as the server reads them for the
  // connection (2026-10-16: workspaces besides this one showed their ids).
  const workspaceNames = (connection: MCPConnectionListItem) => (connection.workspaces ?? [])
    .map((place) => place.name || (place.id === workspace.id ? workspace.name : place.id)).join(", ");
  const blockedNames = (connection: MCPConnectionListItem) => (connection.workspaces ?? [])
    .filter((place) => place.blocked)
    .map((place) => place.name || (place.id === workspace.id ? workspace.name : place.id)).join(", ");
  // A row's action that failed (disconnect, taking back the migration rights)
  // sits under that row until it reads otherwise or the next one starts.
  const [rowNotice, setRowNotice] = useRowNotice<string>((id) => {
    const connection = connected.find((item) => item.id === id);
    return connection && [connection.accessScope, connection.pages, connection.expiresAt,
      connection.reconnect, blockedNames(connection)];
  });
  const rowAction = async (id: string, run: () => Promise<string>) => {
    setRowNotice(id, null);
    const failed = await run();
    if (failed) setRowNotice(id, failed);
  };
  // The limit stays out of sight until a few places are left.
  const capacity = connections.capacity;
  const capacityFull = Boolean(capacity && capacity.limit > 0 && capacity.active >= capacity.limit);
  const capacityNear = Boolean(capacity && capacity.limit > 0 && !capacityFull
    && capacity.limit - capacity.active <= CAPACITY_NOTICE_LEFT);
  // A group as the settings window's own screens keep them: its title over a
  // faint line, rows 24px apart, no box (baseline "리펜 고유 화면"). A count
  // only says something when the list is cut short, which this one never is.
  const connectedList = (
    <section className={styles.rowGroup} data-agent-connected>
      <div className={styles.sectionTitle}>{t("agents.connectedTitle")}</div>
      {capacity && (capacityFull || capacityNear) ? (
        <ListNotice tone={capacityFull ? "error" : "notice"} data-agent-capacity={capacityFull ? "full" : "near"}>
          {capacityFull
            ? t("agents.capacityFull", { limit: capacity.limit })
            : t("agents.capacityNear", { active: capacity.active, limit: capacity.limit, left: capacity.limit - capacity.active })}
        </ListNotice>
      ) : null}
      {connected.some((connection) => connection.reconnect) ? (
        <ListNotice data-agent-reconnect-notice>{t("agents.reconnectNotice")}</ListNotice>
      ) : null}
      {/* A failed read is said here with a way to read again, never as no agents. */}
      {connections.readFailed ? (
        <ListNotice role="alert" onRetry={connections.readAgain} data-agent-connections-read-failed>{connections.readFailed}</ListNotice>
      ) : null}
      {connected.length ? (
        <div className={styles.plainList}>
          {connected.map((connection) => (
            <div className={styles.securityRow} key={connection.id}>
              <div className={styles.securityInstruction}>
                <strong>{connection.clientName || connection.clientId}</strong>
                <span>
                  {/* Its kind before where it works: an agent connection does what its person may in all
                      their workspaces, an MCP token is kept to its one workspace and what its page
                      chooses (internal/app/connection_kind_ways.go). */}
                  {connection.kind ? <><span data-connection-kind={connection.kind}>{t(`agents.connectionKind.${connection.kind}`)}</span> · </> : null}
                  {workspaceNames(connection)}
                </span>
                <span data-connection-key-scope>
                  {/* A person's connection does what they may; an outside app's grant says
                      what it was granted (internal/app/connection_person_rights.go). */}
                  {connection.accessScope ? t("keyScopeShown", { scope: t(`keyScope.${connection.accessScope}`) }) : null}
                  {/* Held to chosen pages, it says so, and leads to its token page, where
                      they are chosen (2026-11-13: both read "권한 전부"). */}
                  {connection.pages !== undefined ? <>
                    {connection.accessScope ? " · " : null}
                    <a href={developerMcpTokenHref(connection.id)} className={styles.auditLink} data-connection-pages={connection.pages}
                      onClick={(event) => { event.preventDefault(); (onNavigate ?? pushRoute)(developerMcpTokenHref(connection.id)); }}>
                      {connection.pages ? t("agents.pagesShown", { count: connection.pages }) : t("agents.noPagesShown")}
                    </a>
                  </> : null}
                  {/* An OAuth connection renews itself while its agent uses it; a key ends on its day. */}
                  {(connection.accessScope || connection.pages !== undefined) && (connection.clientId !== connection.id || connection.expiresAt) ? " · " : ""}
                  {connection.clientId !== connection.id
                    ? t("keyRenewsShown")
                    : connection.expiresAt ? t("keyExpiresShown", { date: displayDate(connection.expiresAt) }) : ""}
                </span>
                {connection.reconnect ? (
                  <span className={styles.errorText} data-connection-reconnect>{t("agents.reconnectNeeded")}</span>
                ) : null}
                {/* Through an earlier address that still forwards, it goes on and may move (2026-11-22). */}
                {connection.moves ? <span data-connection-moves>{t("agents.movesAddress")}</span> : null}
                {blockedNames(connection) ? (
                  // An administrator turned this kind of connection off where
                  // it works: the agent is told why, and so is its person.
                  <span className={styles.errorText} data-connection-blocked>
                    {t("agents.blockedByAdmin", { workspaces: blockedNames(connection) })}
                  </span>
                ) : null}
                {/* When it was made, as Notion's connection list dates each, tells two of one name apart. */}
                <span>
                  <span data-connection-made>{t("agents.made", { date: displayDate(connection.createdAt) })}</span>
                  {" · "}
                  <span data-connection-last-used>
                    {connection.lastUsedAt
                      ? t("agents.lastUsed", { date: displayDateTime(connection.lastUsedAt) })
                      : t("agents.neverUsed")}
                  </span>
                </span>
                {rowNotice(connection.id) ? (
                  <div className={styles.errorText} role="alert" data-connection-error>{rowNotice(connection.id)}</div>
                ) : null}
              </div>
              {/* A row's actions sit behind one "…" button, as the members table keeps them. */}
              <SettingsRowMenu label={t("organizationMemberMenu", { name: connection.clientName || connection.clientId })}
                data={{ "data-connection-menu": connection.id }} rows={[
                  ...(appPlatform ? [{ key: "apps", label: t("connectionApps.open"), data: { "data-connection-apps-open": "" },
                    run: () => setAppsFor({ id: connection.id, name: connection.clientName || connection.clientId }) }] : []),
                  { key: "services", label: t("connectionServices.open"), data: { "data-connection-services-open": "" },
                    run: () => setServicesFor({ id: connection.id, name: connection.clientName || connection.clientId }) },
                  { key: "disconnect", label: t("disconnect"), danger: true, separatorBefore: true, disabled: connections.busyActions.has(`revoke:${connection.id}`),
                    run: () => void confirmAction({ message: t("agentDisconnectConfirm", { name: connection.clientName || connection.clientId }),
                      detail: t("agentDisconnectConfirmDetail"), confirmLabel: t("disconnect"), danger: true })
                      .then((confirmed) => { if (confirmed) void rowAction(connection.id, () => connections.revokeConnection(connection.id)); }) },
                ]} />
            </div>
          ))}
        </div>
      ) : connections.readFailed ? null : (
        <div className={styles.plainEmpty}>
          {connections.loading ? t("loadingConnections") : t("agents.emptyHint")}
        </div>
      )}
      <div className={styles.organizationMeta}>{t("agents.move")}</div>
    </section>
  );
  const currentError = connections.error;
  const currentNotice = connections.notice;
  return (
    <section
      id="mcp"
      className={styles.section}
      aria-labelledby={sectionId}
      aria-busy={connections.loading || connections.busyActions.size > 0}
    >
      {/* The page's line under its title says what it is for (settingsWindowLabels); the list keeps itself current. */}
      <SettingsSectionTitle id={sectionId} className={styles.sectionTitle}>{t("navAiConnections")}</SettingsSectionTitle>

      {currentError ? <ListNotice role="alert">{currentError}</ListNotice> : null}
      {currentNotice ? <ListNotice tone="notice">{currentNotice}</ListNotice> : null}
      {appsFor ? <ConnectionAppsDialog session={session} connectionId={appsFor.id} name={appsFor.name} onClose={() => setAppsFor(null)} /> : null}
      {servicesFor ? (
        <ConnectionServicesDialog session={session} connectionId={servicesFor.id} name={servicesFor.name} onClose={() => setServicesFor(null)} />
      ) : null}

      <AgentConnect session={session} workspaceId={workspace.id} onCodeMade={awaitConnection}
        connectedNow={idsWhenCodeMade ? connected.filter((connection) => !idsWhenCodeMade.has(connection.id))
          .map((connection) => connection.clientName || connection.clientId) : []} />
      {connectedList}
      <WorkspaceUserKeyPolicy session={session} organizationId={workspace.organizationId} />

      <details className={styles.securityDetails} data-agent-advanced>
        <summary>{t("agents.advanced")}</summary>
        <div className={styles.plainGroup}>
          <div className={styles.securityRow}>
            <div className={styles.securityInstruction}>
              <strong>{t("mcpServerUrlTitle")}</strong>
              <span>{t("mcpServerUrlDesc")}</span>
              {/* Claude, named above, doesn't connect a connector at a port (lib/claudeConnector). */}
              {mcpConnectorPort ? <span data-mcp-connector-port={mcpConnectorPort}>{t("agents.connectorPort", { port: mcpConnectorPort })}</span> : null}
            </div>
            <button
              type="button"
              className={styles.secondaryButton}
              onClick={() => void copyMCPText(mcpServerUrl, t("mcpUrlCopied"))}
              disabled={!mcpServerUrl}
            >
              {t("copyUrl")}
            </button>
          </div>
          <code className={styles.securityCode}>{mcpServerUrl || t("mcpUrlUnavailable")}</code>
          <div className={styles.securityInstruction}>
            {/* The workspace a hand-set client names; which connections the list above shows is not this box's to say. */}
            <span>{workspace.name} · <code>workspace_id: {workspace.id}</code></span>
          </div>
        </div>

        {mcpServerUrl ? (
          <div className={styles.plainGroup} data-testid="mcp-client-snippets">
            <div className={styles.securityInstruction}>
              <strong>{t("mcpSnippetsTitle")}</strong>
              <span>{t("mcpSnippetsDesc")}</span>
            </div>
            {snippets.map((snippet) => (
              <div className={styles.securityRow} key={snippet.key}>
                <div className={styles.securityInstruction}>
                  <strong>{snippet.label}</strong>
                  <code className={styles.securityCode}>{snippet.value}</code>
                </div>
                <button
                  type="button"
                  className={styles.secondaryButton}
                  onClick={() => void copyMCPText(snippet.value, t("mcpSnippetCopied"))}
                >
                  {t("copySnippet")}
                </button>
              </div>
            ))}
          </div>
        ) : null}

        <div className={styles.plainGroup}>
          <div className={styles.securityRow}>
            <div className={styles.securityInstruction}>
              <strong>{t("manualTokensTitle")}</strong>
              <span>{t("manualTokensDesc")}</span>
            </div>
            {keyFormOpen ? null : (
              <button ref={keyOpenRef} type="button" className={styles.secondaryButton} data-manual-key-open
                onClick={() => setKeyFormOpen(true)} disabled={capacityFull}>
                {t("createToken")}
              </button>
            )}
          </div>
          {/* The key's choices show once its button is pressed, as a settings list's form does. */}
          {keyFormOpen ? <>
          <div className={styles.securityRow}>
            <label className={styles.securityInstruction}>
              <strong>{t("personalAccessTokenExpiryLabel")}</strong>
              <span>{t("personalAccessTokenExpiryNote", { date: keyLifetimeEnd(keyLifetime) })}</span>
              <AppSelect aria-label={t("personalAccessTokenExpiryLabel")} value={keyLifetime} data-key-lifetime autoFocus
                onChange={(event) => setKeyLifetime(event.target.value as PersonalAccessTokenLifetime)}>
                {PERSONAL_ACCESS_TOKEN_LIFETIMES.map((lifetime) => <option key={lifetime} value={lifetime}>{t(KEY_LIFETIME_KEYS[lifetime])}</option>)}
              </AppSelect>
            </label>
          </div>
          <div className={styles.keyFormActions} data-manual-key-actions>
            <button type="button" className={styles.secondaryButton}
              onClick={() => { setKeyFormOpen(false); requestAnimationFrame(() => keyOpenRef.current?.focus()); }}>{t("common:actions.cancel")}</button>
            <button type="button" className={styles.primaryButton}
              onClick={() => void connections.createManualConnection({ expiresIn: keyLifetime }).then(() => setKeyFormOpen(false))}
              disabled={connections.busyActions.has("manual-token") || capacityFull}>
              {t("createToken")}
            </button>
          </div>
          </> : null}
          {connections.createdConnectionUri ? (
            <div className={styles.recoveryCodes}>
              <div className={styles.recoveryHeader}>
                <div>
                  <strong>{t("newManualTokenTitle")}</strong>
                  <span>{t("newManualTokenDesc")}</span>
                </div>
                <span className={styles.recoveryActions}>
                  <button
                    type="button"
                    className={styles.secondaryButton}
                    onClick={() => void copyMCPText(
                      connections.createdConnectionUri,
                      t("mcpSnippetCopied"),
                    )}
                  >
                    {t("copyUri")}
                  </button>
                </span>
              </div>
              <code>{connections.createdConnectionUri}</code>
              {createdKey ? <div className={styles.recoveryHeader} data-created-api-key>
                <div>
                  <strong>{t("apiKeyTitle")}</strong>
                  <span>{t("apiKeyDesc")}</span>
                </div>
                <span className={styles.recoveryActions}>
                  <button type="button" className={styles.secondaryButton}
                    onClick={() => void copyMCPText(createdKey, t("apiKeyCopied"))}>
                    {t("copyApiKey")}
                  </button>
                </span>
              </div> : null}
              {appPlatform && connections.createdConnectionId ? <div className={styles.recoveryHeader}>
                <div>
                  <strong>{t("connectionApps.open")}</strong>
                  <span>{t("connectionApps.newKey")}</span>
                </div>
                <span className={styles.recoveryActions}>
                  <button type="button" className={styles.secondaryButton} data-created-connection-apps
                    onClick={() => setAppsFor({ id: connections.createdConnectionId, name: t("connectionApps.newKeyName") })}>
                    {t("connectionApps.choose")}
                  </button>
                </span>
              </div> : null}
            </div>
          ) : null}
        </div>
      </details>
    </section>
  );
}
