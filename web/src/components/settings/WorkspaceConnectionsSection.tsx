import { Fragment, useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, Plus, Settings } from "@/icons/leafen";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";
import {
  listWorkspacePersonalAccessTokens,
  revokePersonalAccessToken,
  type PersonalAccessToken,
} from "@/lib/personalAccessTokens";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useNoticeFor, useRowNotice } from "@/lib/useNoticeFor";
import {
  disconnectWorkspaceConnection,
  listAllWorkspaceConnections,
  type WorkspaceConnection,
  type WorkspaceConnections,
} from "@/lib/workspaceConnections";
import type { Session, Workspace } from "@/types";
import table from "./DeveloperSettings.module.css";
import styles from "./SettingsItemRows.module.css";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import { WorkspaceAppAccounts } from "./WorkspaceAppAccounts";
import { WorkspaceConnectionPoliciesSection } from "./WorkspaceConnectionPolicies";
import { WorkspaceAgentKeys } from "./WorkspaceAgentKeys";
import { activeDateLocale } from "@/lib/i18n";
import type { MCPService } from "@/mcpServicesApi";
import { MCPServiceAddDialog } from "./MCPServiceAddDialog";
import { MCPServiceToolsDialog } from "./MCPServiceToolsDialog";
import { MCPServiceRows, useMCPServices } from "./MCPServices";
import { PersonSteps } from "../admin/PersonSteps";
import services from "./MCPServices.module.css";
import menu from "./SidebarPresetsSection.module.css";
import { ListNotice, RowNotice, TableRowNotice } from "./SettingsRowNotice";

type View = "installed" | "manage" | "all-connections" | "all-tokens" | "all-agent-keys";

/**
 * The features' "연결" item, as Notion's (seen 2026-10-02, 2026-10-12):
 * "설치됨" lists the connections added to the workspace and the person's own
 * services, "+ 연결 추가하기" connects a service by its MCP address or opens
 * the developer item, and for its owner "관리" counts the connections and the
 * working personal access tokens, each opening its full list, and opens
 * every agent key that reaches the workspace (WorkspaceAgentKeys). Notion's
 * "둘러보기" is its Marketplace; the permission rows are their own
 * work.
 */
export function WorkspaceConnectionsSection({
  session,
  workspace,
  onOpenMcp,
  onOpenDeveloper,
}: {
  session: Session;
  workspace: Workspace;
  onOpenMcp: () => void;
  onOpenDeveloper: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [view, setView] = useState<View>("installed");
  const [appCount, setAppCount] = useState(0);
  const [state, setState] = useState<WorkspaceConnections | null>(null);
  const [tokens, setTokens] = useState<PersonalAccessToken[] | null>(null);
  // A list that couldn't be read says so in the view it was read for, with a
  // way to read it again.
  const [error, setError] = useNoticeFor<string>(view);
  const [tokensTurn, setTokensTurn] = useState(0);
  // A disconnect or revoke that failed sits under its row, in the view it was
  // pressed in, until that row reads otherwise or the next one starts.
  const [rowNotice, setRowNotice] = useRowNotice<string>((row) => {
    const [rowView, id] = row.split("\u0000");
    if (rowView !== view) return undefined;
    if (rowView === "all-tokens") return tokens?.find((token) => token.id === id)?.status;
    const connection = state?.items.find((item) => item.id === id);
    return connection && [connection.pages, connection.canDisconnect];
  });
  const rowOf = (id: string) => `${view}\u0000${id}`;
  const [busy, setBusy] = useState("");
  const mine = useMCPServices(session);
  const [adding, setAdding] = useState<{ again: MCPService | null } | null>(null);
  const [addMenu, setAddMenu] = useState(false);
  const closeAdding = useCallback(() => setAdding(null), []);
  const [managing, setManaging] = useState<MCPService | null>(null);
  const closeManaging = useCallback(() => setManaging(null), []);

  const load = useCallback((signal?: AbortSignal) => {
    setError("");
    return listAllWorkspaceConnections(session, workspace.id, signal)
      .then(setState)
      .catch((failure) => {
        if (!signal?.aborted) setError(settingsErrorMessage(failure, t("connectionsLoadFailed")));
      });
  }, [session, t, workspace.id]);

  useEffect(() => {
    setView("installed");
    setState(null);
    setTokens(null);
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (view !== "all-tokens") return;
    const controller = new AbortController();
    listWorkspacePersonalAccessTokens(session, workspace.id, controller.signal)
      .then((result) => setTokens(result.items))
      .catch((failure) => {
        if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("personalAccessTokenLoadFailed")));
      });
    return () => controller.abort();
  }, [session, t, tokensTurn, view, workspace.id]);

  async function disconnect(connection: WorkspaceConnection) {
    if (!await confirmAction({ message: t("connectionDisconnectConfirm", { name: connection.name }),
      detail: t("connectionDisconnectConfirmDetail"), confirmLabel: t("connectionDisconnect"), danger: true })) return;
    const row = rowOf(connection.id);
    setBusy(connection.id);
    setRowNotice(row, null);
    try {
      await disconnectWorkspaceConnection(session, workspace.id, connection.id);
      await load();
    } catch (failure) {
      setRowNotice(row, settingsErrorMessage(failure, t("connectionDisconnectFailed", { name: connection.name })));
    } finally {
      setBusy("");
    }
  }

  async function revoke(token: PersonalAccessToken) {
    // The same question as the developer item's token table asks.
    if (!await confirmAction({ message: t("personalAccessTokenRevokeNamed", { name: token.name }), detail: t("personalAccessTokenRevokeHint"),
      confirmLabel: t("personalAccessTokenRevokeConfirm"), danger: true })) return;
    const row = rowOf(token.id);
    setBusy(token.id);
    setRowNotice(row, null);
    try {
      const revoked = await revokePersonalAccessToken(session, workspace.id, token.id);
      setTokens((current) => current?.map((item) => (item.id === revoked.id ? { ...revoked, creator: item.creator } : item)) ?? current);
      await load();
    } catch (failure) {
      setRowNotice(row, settingsErrorMessage(failure, t("personalAccessTokenRevokeFailed")));
    } finally {
      setBusy("");
    }
  }

  const date = (value: number) => new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(value));
  const reach = (connection: WorkspaceConnection) => connection.pages === 0
    ? t("connectionAllPages")
    : t("connectionSomePages", { count: connection.pages });
  // Each asks in the common confirmation first; focus goes back to the button.
  const actionButton = (id: string, onConfirm: () => void, label: string, named: string) => (
    <button type="button" className={table.secondary} aria-label={named} disabled={busy === id} onClick={onConfirm}>{label}</button>
  );
  const back = (
    <button type="button" className={styles.back} onClick={() => setView("manage")}>
      {t("connectionsBack")}
    </button>
  );
  const manage = state?.manage;
  // Installed and, for a workspace's managers, managed connections are tabs.
  const tabListRef = useRef<HTMLDivElement>(null);
  const tabList = useNativeTabs<"installed" | "manage">({
    tabs: [{ key: "installed", text: t("connectionsInstalledTab") },
      ...(manage ? [{ key: "manage" as const, text: t("connectionsManageTab") }] : [])],
    selected: view === "manage" ? "manage" : "installed", onSelect: setView,
    label: t("navIntegrations"), listRef: tabListRef,
  });

  return (
    <div data-workspace-connections>
      {/* The section's line sits under its title as every section's does. One
          tab alone has nothing to choose: a member sees the list without it. */}
      {view === "installed" || view === "manage" ? (manage ? (
        <div {...tabList.tabListProps} ref={tabListRef} className={styles.tabs}>
          <NativeTab state={tabList.state} tabKey="installed">{t("connectionsInstalledTab")}</NativeTab>
          <NativeTab state={tabList.state} tabKey="manage">{t("connectionsManageTab")}</NativeTab>
        </div>
      ) : null) : back}
      {view === "installed" ? (
        <div className={services.addBar}>
          <button type="button" className={services.addButton} aria-haspopup="menu" aria-expanded={addMenu}
            onClick={() => setAddMenu((open) => !open)} data-connection-add>
            <Plus size="small" aria-hidden="true" />{t("mcpService.add")}<ChevronDown size="small" aria-hidden="true" />
          </button>
          {addMenu ? (
            <AddMenu onClose={() => setAddMenu(false)}>
              <button type="button" role="menuitem" className={menu.choice} data-connection-add-mcp
                onClick={() => { setAddMenu(false); setAdding({ again: null }); }}>
                <span className={menu.choiceText}><span>{t("mcpService.addCustom")}</span><span>{t("mcpService.addCustomHint")}</span></span>
              </button>
              <button type="button" role="menuitem" className={menu.choice} data-connection-add-developer
                onClick={() => { setAddMenu(false); onOpenDeveloper(); }}>
                <span className={menu.choiceText}><span>{t("mcpService.addDeveloper")}</span><span>{t("mcpService.addDeveloperHint")}</span></span>
              </button>
            </AddMenu>
          ) : null}
          {/* The agents that use these services are one press away, at the row's end. */}
          <button type="button" className={`${settingsStyles.secondaryButton} ${services.addBarEnd}`} onClick={onOpenMcp}>{t("navAiConnections")}</button>
        </div>
      ) : null}
      {/* An outside service an agent added that waits for the key only the person gives. */}
      {view === "installed" ? <PersonSteps session={session} section="integrations" outsideConsole
        onDone={() => void mine.reload()} /> : null}
      {view === "installed" ? (
        <section className={settingsStyles.section} role="region" aria-label={t("connectionsInstalledTitle")}>
          <div className={settingsStyles.sectionTitle}>{t("connectionsInstalledTitle")}</div>
          <p className={settingsStyles.organizationMeta}>{t("connectionsInstalledDescription")}</p>
          <WorkspaceAppAccounts session={session} workspace={workspace} canManage={Boolean(manage)} onCount={setAppCount} />
          {mine.services ? <MCPServiceRows session={session} services={mine.services} onChanged={mine.reload}
            onReconnect={(again) => setAdding({ again })} onManage={setManaging} /> : null}
          {mine.nextCursor ? (
            <button type="button" className={table.secondary} data-mcp-services-more onClick={() => void mine.loadMore()}>{t("loadMore")}</button>
          ) : null}
          {mine.error ? <ListNotice role="alert" onRetry={() => void mine.reload()}>{mine.error}</ListNotice> : null}
          {state && state.items.length === 0 && appCount === 0 && mine.services?.length === 0
            ? <div className={styles.empty}>{t("connectionsEmpty")}</div> : null}
          {state?.items.map((connection) => (
            <Fragment key={connection.id}>
              <div className={styles.row} data-workspace-connection={connection.id}>
                <span className={styles.icon} aria-hidden="true">{connection.name.slice(0, 1).toUpperCase()}</span>
                <span className={styles.text}>
                  <strong>{connection.name}</strong>
                  <span>{t("connectionAddedBy", { name: connection.installedBy.name || t("connectionUnknownPerson"), date: date(connection.createdAt) })} · {reach(connection)}</span>
                </span>
                <span className={styles.actions}>
                  {connection.canDisconnect
                    ? actionButton(connection.id, () => void disconnect(connection), t("connectionDisconnect"), t("connectionDisconnectNamed", { name: connection.name }))
                    : <span className={styles.badge}>{t("connectionInstalled")}</span>}
                </span>
              </div>
              <RowNotice text={rowNotice(rowOf(connection.id))} />
            </Fragment>
          ))}
        </section>
      ) : null}
      {view === "manage" && manage ? (
        // Notion's 관리 tab (seen 2026-11-06, Business): 44px under the tabs, all
        // connections as a card with its button, all tokens as a row with the count.
        <section className={`${settingsStyles.section} ${styles.afterTabs}`} role="region" aria-label={t("connectionsManageTitle")}>
          <div className={`${settingsStyles.sectionTitle} ${styles.manageHeading}`}>{t("connectionsManageTitle")}</div>
          <div className={styles.manageCard}>
            <strong>{t("allConnectionsTitle")}</strong>
            <span>{t("allConnectionsDescription")}</span>
            <button type="button" className={styles.manageCardButton} onClick={() => setView("all-connections")} data-connections-all>
              <Settings size="small" aria-hidden="true" />{t("allConnectionsOpen")}
            </button>
          </div>
          <button type="button" className={styles.manageRow} onClick={() => setView("all-tokens")} data-personal-access-tokens-all>
            <span className={styles.manageRowText}>
              <strong>{t("allPersonalAccessTokensTitle")}</strong>
              <span>{t("allPersonalAccessTokensDescription")}</span>
            </span>
            <span className={styles.manageRowCount}>
              {t("activePersonalAccessTokensCount", { count: manage.activePersonalAccessTokens })}
              <span className={styles.manageRowChevron}><ChevronRight size="small" aria-hidden="true" /></span>
            </span>
          </button>
          <button type="button" className={styles.manageRow} onClick={() => setView("all-agent-keys")} data-agent-keys-all>
            <span className={styles.manageRowText}>
              <strong>{t("allAgentKeysTitle")}</strong>
              <span>{t("allAgentKeysDescription")}</span>
            </span>
            <span className={styles.manageRowCount}>
              <span className={styles.manageRowChevron}><ChevronRight size="small" aria-hidden="true" /></span>
            </span>
          </button>
        </section>
      ) : null}
      {view === "manage" && manage ? <WorkspaceConnectionPoliciesSection session={session} workspace={workspace} headingClassName={styles.manageHeading} /> : null}
      {view === "all-connections" && state ? (
        <section className={settingsStyles.section} role="region" aria-label={t("allConnectionsTitle")}>
          <div className={settingsStyles.sectionTitle}>{t("allConnectionsTitle")}</div>
          {state.items.length === 0 ? <div className={styles.empty}>{t("connectionsEmpty")}</div> : (
            <table className={table.tokens}>
              <thead>
                <tr>
                  <th scope="col">{t("personalAccessTokenColumnName")}</th>
                  <th scope="col">{t("connectionColumnAddedBy")}</th>
                  <th scope="col">{t("connectionColumnAccess")}</th>
                  <th scope="col">{t("connectionColumnAdded")}</th>
                  <th scope="col"><span className={table.hidden}>{t("connectionDisconnect")}</span></th>
                </tr>
              </thead>
              <tbody>
                {state.items.map((connection) => (
                  <Fragment key={connection.id}>
                    <tr data-workspace-connection={connection.id}>
                      <td className={table.name}>{connection.name}</td>
                      <td>{connection.installedBy.name || t("connectionUnknownPerson")}</td>
                      <td>{reach(connection)}</td>
                      <td>{date(connection.createdAt)}</td>
                      <td className={table.actions}>
                        {actionButton(connection.id, () => void disconnect(connection), t("connectionDisconnect"), t("connectionDisconnectNamed", { name: connection.name }))}
                      </td>
                    </tr>
                    <TableRowNotice text={rowNotice(rowOf(connection.id))} columns={5} />
                  </Fragment>
                ))}
              </tbody>
            </table>
          )}
        </section>
      ) : null}
      {view === "all-tokens" ? (
        <section className={settingsStyles.section} role="region" aria-label={t("allPersonalAccessTokensTitle")}>
          <div className={settingsStyles.sectionTitle}>{t("allPersonalAccessTokensTitle")}</div>
          {tokens && tokens.length === 0 ? <div className={styles.empty}>{t("allPersonalAccessTokensEmpty")}</div> : null}
          {tokens && tokens.length > 0 ? (
            <table className={table.tokens} data-personal-access-tokens>
              <thead>
                <tr>
                  <th scope="col">{t("personalAccessTokenColumnName")}</th>
                  <th scope="col">{t("personalAccessTokenColumnCreator")}</th>
                  <th scope="col">{t("personalAccessTokenColumnStatus")}</th>
                  <th scope="col">{t("personalAccessTokenColumnCreated")}</th>
                  <th scope="col">{t("personalAccessTokenColumnLastUsed")}</th>
                  <th scope="col"><span className={table.hidden}>{t("personalAccessTokenRevoke")}</span></th>
                </tr>
              </thead>
              <tbody>
                {tokens.map((token) => (
                  <Fragment key={token.id}>
                    <tr data-personal-access-token={token.id} data-status={token.status}>
                      <td className={table.name}>{token.name}</td>
                      <td>{token.creator?.name || t("connectionUnknownPerson")}</td>
                      <td>{t(TOKEN_STATUS_KEYS[token.status])}</td>
                      <td>{date(token.createdAt)}</td>
                      <td>{token.lastUsedAt ? date(token.lastUsedAt) : t("personalAccessTokenNeverUsed")}</td>
                      <td className={table.actions}>
                        {token.status === "active"
                          ? actionButton(token.id, () => void revoke(token), t("personalAccessTokenRevoke"), t("personalAccessTokenRevokeNamed", { name: token.name }))
                          : null}
                      </td>
                    </tr>
                    <TableRowNotice text={rowNotice(rowOf(token.id))} columns={6} />
                  </Fragment>
                ))}
              </tbody>
            </table>
          ) : null}
        </section>
      ) : null}
      {view === "all-agent-keys" ? <WorkspaceAgentKeys session={session} workspace={workspace} /> : null}
      {error ? (
        <ListNotice role="alert" data-connections-read-failed onRetry={() => {
          setError("");
          if (view === "all-tokens") setTokensTurn((turn) => turn + 1); else void load();
        }}>{error}</ListNotice>
      ) : null}
      {adding ? (
        <MCPServiceAddDialog session={session} again={adding.again} onChanged={mine.reload} onClose={closeAdding} />
      ) : null}
      {managing ? <MCPServiceToolsDialog session={session} service={managing} onChanged={mine.reload} onClose={closeManaging} /> : null}
    </div>
  );
}

/** The "+ 연결 추가하기" menu, closed by Escape or a press outside it. */
function AddMenu({ onClose, children }: { onClose(): void; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  usePopoverAppear(true, ref);
  useOverlayLayer(true, { kind: "menu", restoreFocus: false, ref, onEscape: onClose, onOutsidePress: onClose });
  return <div ref={ref} className={menu.popover} role="menu">{children}</div>;
}

const TOKEN_STATUS_KEYS: Record<PersonalAccessToken["status"], string> = {
  active: "personalAccessTokenStatusActive",
  expired: "personalAccessTokenStatusExpired",
  revoked: "personalAccessTokenStatusRevoked",
};
