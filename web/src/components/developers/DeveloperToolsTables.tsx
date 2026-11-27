import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { appRegistrationOperations, type AppRegistration, type AppRegistrationListResponse } from "@/appRegistrationWire.generated";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { WorkspaceIconGlyph } from "@/components/page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PersonalAccessTokenDialog } from "@/components/settings/PersonalAccessTokenDialog";
import { ArrowDown, CheckIcon, FilterIcon, GlobeIcon, Plus, Search } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import { activeDateLocale } from "@/lib/i18n";
import { listInternalConnections, type InternalConnection } from "@/lib/internalConnections";
import { listAllMCPConnections } from "@/lib/mcpConnectionListTransport";
import { onMcpConnectionsChanged } from "@/lib/mcpTokens";
import { listPersonalAccessTokens, revokePersonalAccessToken, type PersonalAccessToken } from "@/lib/personalAccessTokens";
import { developerConnectionHref, developerMcpTokenHref, pushRoute } from "@/lib/router";
import { useMadeWithoutAnswer } from "@/lib/uncertainWrites";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { MCPConnectionListItem } from "@/mcpConnectionListWire.generated";
import type { Session, Workspace } from "@/types";
import { ConnectionMark } from "./ConnectionIcon";
import { DeveloperConnectionDialog } from "./DeveloperConnectionDialog";
import { DeveloperMcpTokenDialog } from "./DeveloperMcpTokenDialog";
import styles from "./DeveloperTools.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

const date = (value: number) => new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(value));

/** A workspace's icon in a small grey square, as Notion's beside its name. */
export function WorkspaceMark({ workspace }: { workspace: Pick<Workspace, "name" | "icon"> }) {
  return <span className={styles.workspaceMark} aria-hidden="true">
    <WorkspaceIconGlyph icon={workspace.icon} size={12} fallback={workspace.name.trim().slice(0, 1).toUpperCase() || "W"} /></span>;
}

/** The bar over a list, as Notion's: its filter, search, then the tab's blue "+" button. */
function ListBar({ query, onQuery, action, filter }: { query: string; onQuery(value: string): void; action: ReactNode; filter?: ReactNode }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [searching, setSearching] = useState(query !== "");
  const inputRef = useRef<HTMLInputElement>(null);
  // Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: Developer connections' 🔍 pressed, "z" went into
  // the search (Leafen by code, not seen).
  useEffect(() => { if (searching) inputRef.current?.focus(); }, [searching]);
  return (
    <div className={styles.bar}>
      {filter}
      {searching ? (
        <input ref={inputRef} className={styles.search} value={query} placeholder={t("developerTools.search")}
          aria-label={t("developerTools.search")} onChange={(event) => onQuery(event.target.value)}
          onBlur={() => { if (!query) setSearching(false); }}
          onKeyDown={(event) => { if (event.key === "Escape") { onQuery(""); setSearching(false); } }} />
      ) : (
        <button type="button" className={styles.icon} aria-label={t("developerTools.search")} onClick={() => setSearching(true)}>
          <Search size="regular" aria-hidden="true" />
        </button>
      )}
      {action}
    </div>
  );
}

function Me({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <span className={styles.person}>
      <PersonAvatar userId={session.user.id} size={20} />
      <span>{session.user.name}</span>
      <span className={styles.muted}>{t("developerTools.me")}</span>
    </span>
  );
}

function Table({ columns, empty, children, rows, sortedBy }: {
  columns: string[]; empty: string; children: ReactNode; rows: number;
  /** The column the rows are sorted by, newest first, marked "↓" as Notion's MCP tab's "생성 시점" (seen 2026-11-09). */
  sortedBy?: string;
}) {
  return (
    <div className={styles.tableScroll}>
      <table className={styles.table}>
        <thead><tr>{columns.map((column) => <th key={column} scope="col" aria-sort={column === sortedBy ? "descending" : undefined}>
          {column}{column === sortedBy ? <ArrowDown size="tiny" aria-hidden="true" className={styles.sortMark} /> : null}</th>)}</tr></thead>
        <tbody>
          {rows ? children : <tr><td className={styles.empty} colSpan={columns.length}>{empty}</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

type ConnectionFilter = { authType: "all" | "oauth" | "token"; workspace: "all" | "every"; owner: "all" | "me" };
const NO_FILTER: ConnectionFilter = { authType: "all", workspace: "all", owner: "all" };

/**
 * The connections' filter, as Notion's (seen 2026-10-30, a 180px menu under
 * the button): "인증 유형" 전체 · OAuth · API 토큰, "설치 가능 워크스페이스:"
 * 전체 · 모든 워크스페이스, "소유자" 모든 사용자 · 나.
 */
function ConnectionFilterMenu({ filter, onFilter }: { filter: ConnectionFilter; onFilter(filter: ConnectionFilter): void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const groups: FilterGroup[] = [
    { key: "authType", label: t("developerTools.columns.authType"), choices: [
      { value: "all", label: t("developerTools.filter.all") }, { value: "oauth", label: t("developerTools.oauth") },
      { value: "token", label: t("developerTools.filter.apiToken") }] },
    { key: "workspace", label: t("developerTools.columns.workspaces"), choices: [
      { value: "all", label: t("developerTools.filter.all") },
      { value: "every", label: t("developerTools.allWorkspaces"), icon: <GlobeIcon size="small" aria-hidden="true" /> }] },
    { key: "owner", label: t("developerTools.columns.owner"), choices: [
      { value: "all", label: t("developerTools.filter.everyone") }, { value: "me", label: t("developerTools.filter.me") }] },
  ];
  const active = filter.authType !== "all" || filter.workspace !== "all" || filter.owner !== "all";
  return <ListFilterMenu groups={groups} values={filter} active={active}
    onChoose={(key, value) => onFilter({ ...filter, [key]: value })} />;
}

type FilterGroup = { key: string; label: string; choices: Array<{ value: string; label: string; icon?: ReactNode }> };

/** A list's filter button and its menu: groups of choices, one chosen in each. */
function ListFilterMenu({ groups, values, active, onChoose }: {
  groups: readonly FilterGroup[]; values: Readonly<Record<string, string>>; active: boolean; onChoose(key: string, value: string): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menu, open && button.current
    ? (size, viewport) => placeAround(popoverRectOf(button.current)!, size, viewport, { gap: 4 }) : null, [open], undefined, false);
  const { zIndex } = useOverlayLayer(open, { kind: "menu", ref: menu, inside: () => [button.current], onEscape: () => setOpen(false),
    onOutsidePress: () => setOpen(false), returnFocusRef: button, initialFocus: "first" });
  return <>
    <button ref={button} type="button" className={styles.icon} aria-label={t("developerTools.filter.label")} title={t("developerTools.filter.label")}
      aria-haspopup="menu" aria-expanded={open} data-active={active ? "true" : undefined} data-developer-filter onClick={() => setOpen((value) => !value)}>
      <FilterIcon size="regular" aria-hidden="true" />
    </button>
    {open ? <PopoverLayer>
      <div ref={menu} role="menu" className={styles.filterMenu} style={{ ...popoverPlacementStyle(place), zIndex }} data-developer-filter-menu>
        {groups.map((group) => (
          <div key={group.key} role="group" aria-label={group.label}>
            <div className={styles.filterGroup}>{group.label}</div>
            {group.choices.map((choice) => (
              <button key={choice.value} type="button" role="menuitemradio" aria-checked={values[group.key] === choice.value}
                className={styles.filterChoice} onClick={() => onChoose(group.key, choice.value)}>
                {choice.icon}<span>{choice.label}</span>
                {values[group.key] === choice.value ? <CheckIcon size="small" aria-hidden="true" /> : null}
              </button>
            ))}
          </div>
        ))}
      </div>
    </PopoverLayer> : null}
  </>;
}

const matches = (query: string, ...texts: string[]) => !query.trim() || texts.join(" ").toLowerCase().includes(query.trim().toLowerCase());

/** "연결": the apps the person registered, which people connect to their workspaces. */
export function DeveloperConnectionsTab({ session, workspace, creating, onCreatingChange }: {
  session: Session; workspace: Workspace; creating: boolean; onCreatingChange(creating: boolean): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [apps, setApps] = useState<AppRegistration[] | null>(null);
  const [internal, setInternal] = useState<InternalConnection[]>([]);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  async function load() {
    try {
      const listed = await request<AppRegistrationListResponse>(appRegistrationOperations.list.path,
        { method: appRegistrationOperations.list.method }, session);
      setApps(listed.apps);
      setInternal(await listInternalConnections(session, workspace.id));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appLoadFailed")));
    }
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { void load(); }, [session, workspace.id]);
  // A connection made without the server's answer and found in the list read
  // again goes on to its own page as the answer would have (lib/uncertainWrites.ts).
  const makeInternal = useMadeWithoutAnswer({
    items: internal,
    idOf: (connection) => connection.id,
    readAgain: () => void load(),
    landed: (made) => { onCreatingChange(false); pushRoute(developerConnectionHref(made.id)); },
  });
  const [filter, setFilter] = useState<ConnectionFilter>(NO_FILTER);
  // An internal connection is this workspace's, with its token ("API 토큰");
  // an app is one a person authorizes (OAuth), open to every workspace and
  // listed for the person who made it.
  const shown = (apps ?? []).filter((app) => matches(query, app.name) && filter.authType !== "token");
  const shownInternal = internal.filter((connection) => matches(query, connection.name) && filter.authType !== "oauth"
    && filter.workspace !== "every" && (filter.owner !== "me" || connection.creator.id === session.user.id));
  const capabilitySummary = (connection: InternalConnection) => [
    connection.capabilities.readContent && t("developerTools.internal.capability.readContent"),
    connection.capabilities.updateContent && t("developerTools.internal.capability.updateContent"),
    connection.capabilities.insertContent && t("developerTools.internal.capability.insertContent"),
    connection.capabilities.readComments && t("developerTools.internal.capability.readComments"),
    connection.capabilities.insertComments && t("developerTools.internal.capability.insertComments"),
    connection.capabilities.userInfo !== "none" && t("developerTools.internal.capability.userInfo"),
  ].filter(Boolean).join(", ");
  return (
    <section role="tabpanel" data-developer-connections>
      <ListBar query={query} onQuery={setQuery} filter={<ConnectionFilterMenu filter={filter} onFilter={setFilter} />} action={
        <button type="button" className={styles.primary} data-developer-new-connection onClick={() => onCreatingChange(true)}>
          <Plus size="small" aria-hidden="true" />{t("developerTools.newConnection")}
        </button>} />
      {apps ? (
        <Table rows={shown.length + shownInternal.length} empty={t("developerTools.connectionsEmpty")} columns={[t("developerTools.columns.connection"),
          t("developerTools.columns.workspaces"), t("developerTools.columns.authType"), t("developerTools.columns.owner"),
          t("developerTools.columns.created")]}>
          {shownInternal.map((connection) => (
            <tr key={connection.id} data-internal-connection-row={connection.id} className={styles.openRow} tabIndex={0}
              onClick={() => pushRoute(developerConnectionHref(connection.id))}
              onKeyDown={(event) => { if (fieldEnter(event)) pushRoute(developerConnectionHref(connection.id)); }}>
              <td>
                <span className={styles.connection}>
                  <ConnectionMark name={connection.name} icon={connection.icon} />
                  <span>
                    <strong>{connection.name}</strong>
                    <small>{capabilitySummary(connection)}</small>
                  </span>
                </span>
              </td>
              <td><span className={styles.person}><WorkspaceMark workspace={workspace} />{workspace.name}</span></td>
              <td>{t("developerTools.authMethods.token")}</td>
              <td>{connection.creator.id === session.user.id ? <Me session={session} />
                : <span className={styles.person}><PersonAvatar userId={connection.creator.id} size={20} /><span>{connection.creator.name}</span></span>}</td>
              <td>{date(connection.createdAt)}</td>
            </tr>
          ))}
          {shown.map((app) => (
            // A row opens its connection's page, as Notion's.
            <tr key={app.id} data-app-registration={app.id} className={styles.openRow} tabIndex={0}
              onClick={() => pushRoute(developerConnectionHref(app.id))}
              onKeyDown={(event) => { if (fieldEnter(event)) pushRoute(developerConnectionHref(app.id)); }}>
              <td>
                <span className={styles.connection}>
                  <ConnectionMark name={app.name} />
                  <span>
                    <strong>{app.name}</strong>
                    <small>{app.actsAs === "integration" ? t("appActsAsIntegration") : t("appActsAsPerson")} · {app.redirectUris.join(", ")}</small>
                  </span>
                </span>
              </td>
              <td><span className={styles.person}><GlobeIcon size="small" aria-hidden="true" />{t("developerTools.allWorkspaces")}</span></td>
              <td>{t("developerTools.oauth")}</td>
              <td><Me session={session} /></td>
              <td>{date(app.createdAt)}</td>
            </tr>
          ))}
        </Table>
      ) : null}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {creating ? <DeveloperConnectionDialog session={session} makeInternal={makeInternal} onClose={() => onCreatingChange(false)}
        onCreated={() => { onCreatingChange(false); void load(); }} /> : null}
    </section>
  );
}

const STATUS_KEYS: Record<PersonalAccessToken["status"], string> = {
  active: "personalAccessTokenStatusActive",
  expired: "personalAccessTokenStatusExpired",
  revoked: "personalAccessTokenStatusRevoked",
};

/** "개인 액세스 토큰": the person's tokens for this workspace, each revocable. */
export function DeveloperTokensTab({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [tokens, setTokens] = useState<PersonalAccessToken[] | null>(null);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    listPersonalAccessTokens(session, workspace.id, controller.signal)
      .then((result) => setTokens(result.items))
      .catch((failure) => { if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("personalAccessTokenLoadFailed"))); });
    return () => controller.abort();
  }, [session, workspace.id, t]);
  async function revoke(token: PersonalAccessToken) {
    if (!await confirmAction({ message: t("personalAccessTokenRevokeNamed", { name: token.name }), detail: t("personalAccessTokenRevokeHint"),
      confirmLabel: t("personalAccessTokenRevokeConfirm"), danger: true })) return;
    try {
      const revoked = await revokePersonalAccessToken(session, workspace.id, token.id);
      setTokens((current) => current?.map((item) => (item.id === revoked.id ? revoked : item)) ?? current);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("personalAccessTokenRevokeFailed")));
    }
  }
  // Notion's list starts on the active ones, its "상태" 전체 · 활성 · 만료됨 · 철회됨 (seen 2026-11-04).
  const [status, setStatus] = useState("active");
  const shown = (tokens ?? []).filter((token) => matches(query, token.name) && (status === "all" || token.status === status));
  return (
    <section role="tabpanel" data-developer-tokens>
      <ListBar query={query} onQuery={setQuery} filter={<ListFilterMenu active={status !== "all"} values={{ status }}
        onChoose={(_key, value) => setStatus(value)} groups={[{ key: "status", label: t("developerTools.columns.status"), choices: [
          { value: "all", label: t("developerTools.filter.all") },
          ...(Object.keys(STATUS_KEYS) as Array<PersonalAccessToken["status"]>).map((value) => ({ value, label: t(STATUS_KEYS[value]) })),
        ] }]} />} action={
        <button type="button" className={styles.primary} data-developer-new-token onClick={() => setCreating(true)}>
          <Plus size="small" aria-hidden="true" />{t("personalAccessTokenNew")}
        </button>} />
      {tokens ? (
        <Table rows={shown.length} empty={t("developerTools.tokensEmpty")} columns={[t("developerTools.columns.name"),
          t("developerTools.columns.workspace"), t("developerTools.columns.capabilities"), t("developerTools.columns.status"),
          t("developerTools.columns.createdDate"), t("developerTools.columns.lastUsed"), ""]}>
          {shown.map((token) => (
            <tr key={token.id} data-personal-access-token={token.id} data-status={token.status}>
              <td><strong>{token.name}</strong></td>
              <td>{workspace.name}</td>
              <td>{t("developerTools.capabilitiesPerson")}</td>
              <td>{t(STATUS_KEYS[token.status])}</td>
              <td>{date(token.createdAt)}</td>
              <td>{token.lastUsedAt ? date(token.lastUsedAt) : t("personalAccessTokenNeverUsed")}</td>
              <td className={styles.rowAction}>
                {token.status === "active" ? <button type="button" className={styles.secondary}
                  aria-label={t("personalAccessTokenRevokeNamed", { name: token.name })} onClick={() => void revoke(token)}>
                  {t("personalAccessTokenRevoke")}</button> : null}
              </td>
            </tr>
          ))}
        </Table>
      ) : null}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {creating ? <PersonalAccessTokenDialog session={session} workspaceId={workspace.id}
        onCreated={(token) => setTokens((current) => [token, ...(current ?? [])])} onClose={() => setCreating(false)} /> : null}
    </section>
  );
}

/** "MCP 토큰": the person's agent connections, each opening its own page; "+ 새 MCP 토큰" makes one there. */
export function DeveloperMcpTokensTab({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [connections, setConnections] = useState<MCPConnectionListItem[] | null>(null);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [generation, setGeneration] = useState(0);
  const identity = `${session.user.id}:${session.token}`;
  useEffect(() => {
    const controller = new AbortController();
    const current = () => ({ sessionIdentity: identity, userId: session.user.id, requestGeneration: 1 });
    listAllMCPConnections({ session, sessionIdentity: identity, requestGeneration: 1, signal: controller.signal, current })
      .then((snapshot) => setConnections(snapshot.connections.filter((connection) => connection.status === "active")))
      .catch((failure) => { if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("developerTools.mcpLoadFailed"))); });
    return () => controller.abort();
  }, [generation, identity, session, t]);
  // A token disconnected elsewhere (settings › 에이전트) leaves this list at once.
  useEffect(() => onMcpConnectionsChanged(() => setGeneration((value) => value + 1)), []);
  // As Notion's (seen 2026-11-05): where it was made, and who made it; these
  // are the person's own, none of them for every workspace.
  const [filter, setFilter] = useState({ workspace: "all", owner: "all" });
  const shown = useMemo(() => (connections ?? []).filter((connection) => matches(query, connection.clientName) && filter.workspace === "all"),
    [connections, filter.workspace, query]);
  const filterGroups: FilterGroup[] = [
    { key: "workspace", label: t("developerTools.columns.createdIn"), choices: [
      { value: "all", label: t("developerTools.filter.all") },
      { value: "every", label: t("developerTools.allWorkspaces"), icon: <GlobeIcon size="small" aria-hidden="true" /> }] },
    { key: "owner", label: t("developerTools.columns.createdBy"), choices: [
      { value: "all", label: t("developerTools.filter.everyone") }, { value: "me", label: t("developerTools.filter.me") }] },
  ];
  return (
    <section role="tabpanel" data-developer-mcp-tokens>
      <ListBar query={query} onQuery={setQuery} filter={<ListFilterMenu groups={filterGroups} values={filter}
        active={filter.workspace !== "all" || filter.owner !== "all"} onChoose={(key, value) => setFilter({ ...filter, [key]: value })} />} action={
        <button type="button" className={styles.primary} data-developer-new-mcp-token onClick={() => setCreating(true)}>
          <Plus size="small" aria-hidden="true" />{t("developerTools.newMcpToken")}
        </button>} />
      {connections ? (
        <Table rows={shown.length} empty={t("developerTools.mcpEmpty")} columns={[t("developerTools.columns.mcpClient"),
          t("developerTools.columns.createdIn"), t("developerTools.columns.authType"), t("developerTools.columns.createdBy"),
          t("developerTools.columns.created")]} sortedBy={t("developerTools.columns.created")}>
          {shown.map((connection) => (
            // A row opens its token's page, as Notion's: its square (its
            // icon, or its first letter), its name and description, where it was made beside its
            // workspace's mark, and "MCP 토큰" (seen 2026-11-09).
            <tr key={connection.id} data-mcp-connection={connection.id} className={styles.openRow} tabIndex={0}
              onClick={() => pushRoute(developerMcpTokenHref(connection.id))}
              onKeyDown={(event) => { if (fieldEnter(event)) pushRoute(developerMcpTokenHref(connection.id)); }}>
              <td>
                <span className={styles.connection}>
                  <ConnectionMark name={connection.clientName} icon={connection.icon} />
                  <span>
                    <strong>{connection.clientName}</strong>
                    {connection.description ? <small>{connection.description}</small> : null}
                  </span>
                </span>
              </td>
              <td><span className={styles.person}><WorkspaceMark workspace={workspace} />{connection.workspaces.map((place) => place.name).join(", ") || workspace.name}</span></td>
              {/* A key made by hand lists its own id as its client: an MCP token; an OAuth client has one of its own. */}
              <td>{connection.clientId && connection.clientId !== connection.id ? t("developerTools.oauth") : t("developerTools.mcpTokenKind")}</td>
              <td><Me session={session} /></td>
              <td>{date(connection.createdAt)}</td>
            </tr>
          ))}
        </Table>
      ) : null}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {creating ? <DeveloperMcpTokenDialog session={session} workspace={workspace}
        onClose={(made) => { setCreating(false); if (made) setGeneration((value) => value + 1); }}
        onMade={(id) => { setCreating(false); pushRoute(developerMcpTokenHref(id)); }} /> : null}
    </section>
  );
}
