import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { HoverTip } from "@/components/IconTipButton";
import { listedPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { TopBar } from "@/components/page/TopBar";
import { useProductPages } from "@/components/product/productPagesContext";
import { ChevronDown, ChevronRight, Copy, EyeIcon, EyeSlashIcon, InfoIcon, Search } from "@/icons/leafen";
import { RefreshInCircleIcon } from "@/icons/leafen/notionMarkIcons";
import { copyText } from "@/lib/copyText";
import { setDocumentChrome } from "@/lib/documentChrome";
import {
  deleteInternalConnection, listInternalConnectionPages, listInternalConnections, renewInternalConnectionToken,
  revealInternalConnectionToken, takeInternalConnectionSecret, updateInternalConnection,
  type InternalConnection, type InternalConnectionCapabilities, type InternalConnectionPage as ConnectionPage,
} from "@/lib/internalConnections";
import { activeDateLocale } from "@/lib/i18n";
import { listedPageNameKind, untitledPageDisplayTitle } from "@/lib/pageTitle";
import { pagesByIdOf } from "@/lib/pageIndex";
import { developerToolsHref, pushRoute } from "@/lib/router";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { DeveloperConnectionCrumbs } from "./DeveloperConnectionPage";
import { InternalConnectionShare } from "./InternalConnectionShare";
import { InternalConnectionPagesPicker } from "./InternalConnectionPagesPicker";
import { InternalConnectionWebhooks } from "./InternalConnectionWebhooks";
import { ConnectionIconEdit } from "./ConnectionIcon";
import { WorkspaceMark } from "./DeveloperToolsTables";
import { askDeveloperDelete, askTokenRefresh } from "./DeveloperQuestions";
import { DeveloperDangerZone } from "./DeveloperDangerZone";
import styles from "./DeveloperTools.module.css";
import own from "./InternalConnection.module.css";
import { ReadFailed } from "@/components/ReadFailed";

type Tab = "config" | "access" | "webhooks";
const TABS: readonly Tab[] = ["config", "access", "webhooks"];

/**
 * An internal connection's own page, as Notion's /developers/connections/〈id〉
 * for an "API 토큰" connection (seen 2026-10-30): its name, then 구성 —
 * 정보 표시, API 통합 토큰 (its workspace and token), 기능 and 위험 구역 —
 * 콘텐츠 사용 권한 (the pages it reaches, "편집 권한" to choose them) and
 * 웹훅 (InternalConnectionWebhooks); who manages it is under the bar's
 * "공유" (InternalConnectionShare), as Notion's since 2026-11-21. Its token
 * shows only as it is made (notion_internal_connections.go). Notion's 에이전트
 * 사용 권한 tab (the Notion agents the connection may act for) and the 기능
 * card's "에이전트 기능" group aren't here: Leafen holds no agent (decided
 * 2026-10-29).
 */
export function InternalConnectionPage({ session, connectionId }: { session: Session; connectionId: string }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const pages = useProductPages();
  const workspace = useMemo(() => pages?.workspaces?.find((candidate) => candidate.id === pages.workspaceId), [pages?.workspaces, pages?.workspaceId]);
  const [connection, setConnection] = useState<InternalConnection | null | undefined>(undefined);
  const [secret, setSecret] = useState(() => takeInternalConnectionSecret(connectionId));
  const [shown, setShown] = useState(Boolean(secret));
  const [unsealed, setUnsealed] = useState(false);
  const [tab, setTab] = useState<Tab>("config");
  const [error, setError] = useState("");
  useEffect(() => {
    if (!workspace) return;
    const controller = new AbortController();
    void listInternalConnections(session, workspace.id, controller.signal)
      .then((items) => setConnection(items.find((candidate) => candidate.id === connectionId) ?? null))
      .catch((failure) => { if (!controller.signal.aborted) { setConnection(null); setError(settingsErrorMessage(failure, t("appLoadFailed"))); } });
    return () => controller.abort();
  }, [connectionId, session, t, workspace]);
  useEffect(() => { if (connection) setDocumentChrome({ title: connection.name }); }, [connection]);

  async function save(change: { name?: string; icon?: string; capabilities?: InternalConnectionCapabilities }) {
    if (!connection || !workspace) return;
    const before = connection;
    setConnection({ ...connection, ...change });
    setError("");
    try {
      setConnection(await updateInternalConnection(session, workspace.id, connection.id, change));
    } catch (failure) {
      setConnection(before);
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }
  // Asked when the token it replaces stops: in seven days, or at once.
  async function renew() {
    if (!connection || !workspace) return;
    const revokeOld = await askTokenRefresh("internal");
    if (!revokeOld) return;
    try {
      const renewed = await renewInternalConnectionToken(session, workspace.id, connection.id, revokeOld);
      setConnection(renewed);
      setSecret(renewed.secret ?? "");
      setShown(true);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }
  // revealed is the token, asked of the server the first time it is shown or
  // copied; one made before tokens were kept can't be shown again.
  async function revealed() {
    if (secret || !connection || !workspace) return secret;
    try {
      const value = await revealInternalConnectionToken(session, workspace.id, connection.id);
      setSecret(value);
      return value;
    } catch (failure) {
      if (failure instanceof LeafenApiError && failure.status === 404) setUnsealed(true);
      else setError(settingsErrorMessage(failure, t("appFailed")));
      return "";
    }
  }
  async function toggleShown() {
    if (shown) setShown(false);
    else if (await revealed()) setShown(true);
  }
  async function remove() {
    if (!connection || !workspace || !await askDeveloperDelete()) return;
    try {
      await deleteInternalConnection(session, workspace.id, connection.id);
      pushRoute(developerToolsHref("connections"));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }

  const name = connection?.name ?? "";
  const capabilities = connection?.capabilities;
  // A capability is one button, as Notion's: its box, its name and the line
  // under it. Tab stops at every row, each radio too, and Enter or Space
  // picks it.
  const choice = (role: "checkbox" | "radio", checked: boolean, label: string, note: string,
    data: Record<`data-${string}`, string>, pick: () => void) => (
    <button key={label} type="button" role={role} aria-checked={checked} className={own.capability} {...data} onClick={pick}>
      <span className={own.box} aria-hidden="true">
        {!checked ? null : role === "radio" ? <span className={own.dot} /> : (
          <svg viewBox="0 0 12 12" width="12" height="12" fill="none">
            <path d="M2 6l2.5 2.5L10 3" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>)}
      </span>
      <span className={own.capabilityText}><strong>{label}</strong><small>{note}</small></span>
    </button>
  );
  const check = (key: Exclude<keyof InternalConnectionCapabilities, "userInfo">) => choice("checkbox", Boolean(capabilities?.[key]),
    t(`developerTools.internal.capability.${key}`), t(`developerTools.internal.capability.${key}Note`), { "data-capability": key },
    () => capabilities && void save({ capabilities: { ...capabilities, [key]: !capabilities[key] } }));
  const group = (label: string, role: "group" | "radiogroup", rows: ReactNode) => (
    <div className={own.group} role={role} aria-label={label}>
      <h3 className={own.groupName}>{label}</h3>
      <div className={own.capabilities}>{rows}</div>
    </div>
  );
  return (
    <>
      <TopBar title={name || t("developerTools.title")} crumbs={<DeveloperConnectionCrumbs name={name} />}
        screenActions={connection && workspace ? <InternalConnectionShare session={session} workspace={workspace} connectionId={connection.id} /> : undefined} />
      <main className={styles.page} data-internal-connection={connectionId}>
        {connection === null ? <p className={styles.description}>{error || t("developerTools.connection.missing")}</p> : connection && workspace ? <>
          <h1 className={styles.title}>{connection.name}</h1>
          <nav className={styles.tabs} role="tablist" aria-label={connection.name}>
            {TABS.map((candidate) => (
              <button key={candidate} type="button" role="tab" aria-selected={candidate === tab} className={styles.tab}
                data-internal-connection-tab={candidate} onClick={() => setTab(candidate)}>{t(`developerTools.internal.tabs.${candidate}`)}</button>
            ))}
          </nav>
          {tab === "config" ? <>
            <section className={styles.card} data-connection-card="display">
              <h2 className={styles.cardTitle}>{t("developerTools.connection.display")}</h2>
              <div className={styles.fieldGrid}>
                <div className={styles.displayRow}>
                  <ConnectionIconEdit kind="internal" name={connection.name} icon={connection.icon} onChange={(icon) => void save({ icon })} />
                  <label className={styles.cardField}>
                    <span>{t("developerTools.connection.name")}</span>
                    <input defaultValue={connection.name} maxLength={100} data-connection-name
                      onBlur={(event) => { const value = event.currentTarget.value.trim(); if (value && value !== connection.name) void save({ name: value }); }} />
                  </label>
                </div>
              </div>
            </section>
            <section className={styles.card} data-connection-card="token">
              <h2 className={styles.cardTitle}>{t("developerTools.internal.token")}</h2>
              <p className={styles.description}>{t("developerTools.internal.tokenDescription")}</p>
              <div className={styles.fieldGrid}>
                <div className={styles.cardField}>
                  <span>{t("developerTools.internal.workspace")}</span>
                  <span className={own.chipField}><span className={own.chip}><WorkspaceMark workspace={workspace} />{workspace.name}</span></span>
                </div>
                <div className={styles.cardField}>
                  <span>{t("developerTools.internal.tokenLabel")}</span>
                  <span className={styles.secretRow} data-connection-token>
                    <code className={own.token}>{secret && shown ? secret : unsealed ? connection.tokenHint : "•".repeat(32)}</code>
                    <span className={own.tokenActions}>
                      {unsealed ? null : <button type="button" className={styles.icon} aria-label={t(shown ? "developerTools.internal.hide" : "developerTools.internal.show")}
                        title={t(shown ? "developerTools.internal.hide" : "developerTools.internal.show")} data-connection-token-show onClick={() => void toggleShown()}>
                        {shown ? <EyeSlashIcon size="small" aria-hidden="true" /> : <EyeIcon size="small" aria-hidden="true" />}</button>}
                      <button type="button" className={styles.icon} aria-label={t("developerTools.internal.renew")}
                        title={t("developerTools.internal.renewedAt", { date: new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(connection.tokenMadeAt) })}
                        data-connection-token-renew onClick={() => void renew()}><RefreshInCircleIcon size="small" aria-hidden="true" /></button>
                      <button type="button" className={styles.icon} aria-label={t("developerTools.connection.copy")} title={t("developerTools.connection.copy")}
                        disabled={unsealed} onClick={() => void revealed().then((value) => { if (value) void copyText(value); })}><Copy size="small" aria-hidden="true" /></button>
                    </span>
                  </span>
                  {unsealed ? <small className={own.note}>{t("developerTools.internal.tokenOnce")}</small> : null}
                </div>
              </div>
            </section>
            <section className={styles.card} data-connection-card="capabilities">
              <h2 className={styles.cardTitle}>{t("developerTools.internal.capabilities")}</h2>
              <p className={styles.description}>{t("developerTools.internal.capabilitiesDescription")}</p>
              <div className={own.capabilityGrid}>
                {group(t("developerTools.internal.contentGroup"), "group", <>{check("readContent")}{check("updateContent")}{check("insertContent")}</>)}
                {group(t("developerTools.internal.commentGroup"), "group", <>{check("readComments")}{check("insertComments")}</>)}
              </div>
              <div className={own.capabilityGrid}>
                {group(t("developerTools.internal.userGroup"), "radiogroup", (["none", "no_email", "email"] as const).map((level) => (
                  choice("radio", capabilities?.userInfo === level, t(`developerTools.internal.userInfo.${level}`),
                    t(`developerTools.internal.userInfo.${level}Note`), { "data-user-info": level },
                    () => capabilities && capabilities.userInfo !== level && void save({ capabilities: { ...capabilities, userInfo: level } }))
                )))}
              </div>
            </section>
            <DeveloperDangerZone kind="connection" description={t("developerTools.internal.dangerDescription")} onRemove={() => void remove()} />
          </> : tab === "access" ? (
            <ContentAccess session={session} workspaceId={workspace.id} connection={connection}
              onPages={(count) => setConnection((current) => current && { ...current, pages: count })} />
          ) : (
            <InternalConnectionWebhooks session={session} workspaceId={workspace.id} connection={connection} />
          )}
          {error ? <p className={styles.error} role="alert">{error}</p> : null}
        </> : null}
      </main>
    </>
  );
}

/**
 * "콘텐츠 사용 권한": the pages the connection reaches under "공유된 페이지
 * (N개 페이지)", opened like Notion's, and "편집 권한" to choose them.
 */
function ContentAccess({ session, workspaceId, connection, onPages }: {
  session: Session; workspaceId: string; connection: InternalConnection; onPages(count: number): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const pagesById = pagesByIdOf(useProductPages()?.pages ?? []);
  const [items, setItems] = useState<ConnectionPage[] | null>(null);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [generation, setGeneration] = useState(0);
  // A read that failed says so where the pages show, with a way to read again — not "0 pages".
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    setFailed(false);
    void listInternalConnectionPages(session, workspaceId, connection.id, controller.signal)
      .then((listed) => { setItems(listed); onPages(listed.length); })
      .catch(() => { if (!controller.signal.aborted) setFailed(true); });
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connection.id, generation, session, workspaceId]);
  const needle = query.trim().toLocaleLowerCase();
  const shown = (items ?? []).filter((page) => !needle || page.title.toLocaleLowerCase().includes(needle));
  return (
    <section className={styles.card} data-connection-card="access">
      <div className={own.cardHead}>
        <div>
          <h2 className={styles.cardTitle}>{t("developerTools.internal.access")}
            <HoverTip label={t("developerTools.internal.accessHelp")} placement="top" className={own.help}>
              <InfoIcon size="small" aria-label={t("developerTools.internal.accessHelp")} /></HoverTip></h2>
          <p className={`${styles.description} ${own.description}`}>{t("developerTools.internal.accessDescription")}</p>
        </div>
        <button type="button" className={own.link} data-connection-access-edit onClick={() => setEditing(true)}>{t("developerTools.internal.editAccess")}</button>
      </div>
      <label className={own.search}>
        <Search size="small" aria-hidden="true" />
        <input value={query} placeholder={t("developerTools.internal.accessSearch")} aria-label={t("developerTools.internal.accessSearch")}
          onChange={(event) => setQuery(event.currentTarget.value)} />
      </label>
      {failed ? <ReadFailed message={t("developerTools.internal.accessLoadFailed")} onRetry={() => setGeneration((value) => value + 1)} /> : null}
      {items ? <button type="button" className={own.groupRow} aria-expanded={open} data-connection-access-group onClick={() => setOpen(!open)}>
        <span>{t("developerTools.internal.sharedPages", { count: items?.length ?? 0 })}</span>
        {open ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}
      </button> : null}
      {open && items ? <div className={own.pages}>
        {shown.length ? <p className={own.subgroup}>{t("developerTools.internal.chosen")}</p> : null}
        {shown.map((page) => (
          <div key={page.id} className={own.pageRow} data-connection-page={page.id}>
            <PageIconGlyph page={listedPageIcon(pagesById[page.id], page)} size={18} />
            <span>{page.title || untitledPageDisplayTitle(listedPageNameKind(page))}</span>
          </div>
        ))}
      </div> : null}
      {editing ? <InternalConnectionPagesPicker session={session} grantId={connection.grantId} chosen={new Set((items ?? []).map((page) => page.id))}
        onChanged={() => setGeneration((value) => value + 1)} onClose={() => setEditing(false)} /> : null}
    </section>
  );
}
