import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { HoverTip } from "@/components/IconTipButton";
import { TopBar } from "@/components/page/TopBar";
import { styles as topBarStyles } from "@/components/page/topBarStyles";
import { currentMCPServerUrl } from "@/components/settings/WorkspaceAIConnections";
import { Copy, DeveloperIcon, EyeIcon, EyeSlashIcon, McpTokenIcon } from "@/icons/leafen";
import { RefreshInCircleIcon } from "@/icons/leafen/notionMarkIcons";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { useCopyNotice } from "@/lib/copyNotice";
import { setDocumentChrome } from "@/lib/documentChrome";
import { activeDateLocale } from "@/lib/i18n";
import {
  changeMcpToken, deleteMcpToken, readMcpToken, refreshMcpToken, revealMcpToken, takeMcpTokenKey, type McpTokenSettings,
} from "@/lib/mcpTokens";
import { developerToolsHref, pushRoute } from "@/lib/router";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { McpTokenAccess, McpTokenTools } from "./DeveloperMcpTokenParts";
import { McpTokenAsks } from "./DeveloperMcpTokenAsks";
import { McpTokenNotionOnlyTools } from "./DeveloperMcpTokenNotionOnly";
import { McpTokenWorkspaces } from "./DeveloperMcpTokenWorkspaces";
import { ConnectionIconEdit } from "./ConnectionIcon";
import { askDeveloperDelete, askTokenRefresh, dateAndTime } from "./DeveloperQuestions";
import { DeveloperDangerZone } from "./DeveloperDangerZone";
import styles from "./DeveloperTools.module.css";
import own from "./InternalConnection.module.css";
import token from "./McpToken.module.css";

type Tab = "config" | "access";

const longDate = (value: number) => new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(value);

/**
 * An MCP token's own page, as Notion's /developers/mcp-tokens/〈id〉 (the
 * user's walk of 2026-11-09): the path "개발자 도구 / MCP 토큰 / 〈name〉",
 * its name as the title, then 구성 — 정보 표시 (its icon, name and description),
 * MCP 토큰 (its workspace and key), 도구 and 위험 구역 — and 콘텐츠 사용
 * 권한. Its key shows masked with an eye that shows it again, a refresh and a
 * copy, as Notion's integration token field (seen 2026-11-13): the server
 * keeps it sealed for its person alone (decided 2026-11-13). One made before
 * keys were kept says that refreshing it makes one that shows again. The
 * server's address and the key's expiry sit under it, as Leafen's own.
 */
export function DeveloperMcpTokenPage({ session, connectionId }: { session: Session; connectionId: string }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [settings, setSettings] = useState<McpTokenSettings | null | undefined>(undefined);
  const [key, setKey] = useState(() => takeMcpTokenKey(connectionId));
  const [shown, setShown] = useState(false);
  // Its key isn't kept to show again: made before keys were kept, or sealed
  // with a settings key this server no longer has.
  const [notKept, setNotKept] = useState(false);
  const [tab, setTab] = useState<Tab>("config");
  const [error, setError] = useState("");
  const copy = useCopyNotice();
  useEffect(() => {
    const controller = new AbortController();
    void readMcpToken(session, connectionId, controller.signal).then(setSettings)
      .catch((failure) => { if (!controller.signal.aborted) { setSettings(null); setError(settingsErrorMessage(failure, t("developerTools.mcpLoadFailed"))); } });
    return () => controller.abort();
  }, [connectionId, session, t]);
  useEffect(() => { if (settings) setDocumentChrome({ title: settings.clientName }); }, [settings]);

  // A switch shows its new place at the press (shown), the server's answer after it.
  async function save(change: Parameters<typeof changeMcpToken>[2], shown?: Partial<McpTokenSettings>) {
    if (!settings) return;
    const before = settings;
    setError("");
    if (shown) setSettings({ ...before, ...shown });
    try {
      setSettings(await changeMcpToken(session, settings.id, change));
    } catch (failure) {
      setSettings(before);
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }
  async function refresh() {
    if (!settings) return;
    const answer = await askTokenRefresh("mcpToken");
    if (!answer) return;
    setError("");
    try {
      const made = await refreshMcpToken(session, settings.id, answer);
      setKey(made.key ?? "");
      setNotKept(false);
      setSettings({ ...made, key: undefined });
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }
  // revealed is its key, asked of the server the first time it is shown or
  // copied.
  async function revealed() {
    if (key || !settings) return key;
    try {
      const value = await revealMcpToken(session, settings.id);
      setKey(value);
      return value;
    } catch (failure) {
      if (failure instanceof LeafenApiError && failure.status === 404) setNotKept(true);
      else setError(settingsErrorMessage(failure, t("appFailed")));
      return "";
    }
  }
  // The eye turns at the press; the key fills in once it is read.
  async function toggleShown() {
    if (shown) return setShown(false);
    setShown(true);
    if (!await revealed()) setShown(false);
  }
  async function remove() {
    if (!settings || !await askDeveloperDelete({ message: t("developerTools.mcpToken.removeConfirm"),
      detail: t("developerTools.mcpToken.removeDetail"), action: t("developerTools.mcpToken.removeAction") })) return;
    try {
      await deleteMcpToken(session, settings.id);
      pushRoute(developerToolsHref("mcp-tokens"));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appFailed")));
    }
  }

  const name = settings?.clientName ?? "";
  const kept = Boolean(key) || Boolean(settings?.keyKept) && !notKept;
  return (
    <>
      <TopBar title={name || t("developerTools.title")} crumbs={<McpTokenCrumbs name={name} />} />
      <main className={styles.page} data-mcp-token={connectionId}>
        {settings === null ? <p className={styles.description}>{error || t("developerTools.mcpToken.missing")}</p> : settings ? <>
          <h1 className={styles.title}>{settings.clientName}</h1>
          <nav className={styles.tabs} role="tablist" aria-label={settings.clientName}>
            {(["config", "access"] as const).map((candidate) => (
              <button key={candidate} type="button" role="tab" aria-selected={candidate === tab} className={styles.tab}
                data-mcp-token-tab={candidate} onClick={() => setTab(candidate)}>{t(`developerTools.mcpToken.tabs.${candidate}`)}</button>
            ))}
          </nav>
          {tab === "config" ? <>
            <section className={styles.card} data-mcp-token-card="display">
              <h2 className={styles.cardTitle}>{t("developerTools.connection.display")}</h2>
              <div className={styles.fieldGrid}>
                <div className={styles.displayRow}>
                  <ConnectionIconEdit kind="mcpToken" name={settings.clientName} icon={settings.icon} onChange={(icon) => void save({ icon })} />
                  <label className={styles.cardField}>
                    <span className={token.label}>{t("developerTools.mcpTokenName")}
                      <HoverTip label={t("developerTools.mcpTokenNameHint")} placement="top" width={240} className={own.help}>
                        <QuestionCircleIcon size="small" aria-label={t("developerTools.mcpTokenNameHint")} /></HoverTip></span>
                    <input key={`name-${settings.clientName}`} defaultValue={settings.clientName} maxLength={255} data-mcp-token-name
                      onBlur={(event) => { const value = event.currentTarget.value.trim(); if (value && value !== settings.clientName) void save({ clientName: value }); }} />
                  </label>
                </div>
                <label className={styles.cardField}>
                  <span className={token.label}>{t("developerTools.mcpToken.description")}</span>
                  <input key={`description-${settings.description}`} defaultValue={settings.description} maxLength={500} data-mcp-token-description
                    onBlur={(event) => { const value = event.currentTarget.value.trim(); if (value !== settings.description) void save({ description: value }); }} />
                </label>
              </div>
            </section>
            <section className={styles.card} data-mcp-token-card="token">
              <h2 className={styles.cardTitle}>{t("developerTools.mcpToken.token")}</h2>
              {/* Notion's words for its token of one workspace; one reaching several says how many (the chips under it name them). */}
              <p className={`${styles.description} ${own.description}`} data-mcp-token-description>{settings.workspaces.length > 1
                ? t("developerTools.mcpToken.tokenDescriptionSeveral", { number: settings.workspaces.length })
                : t("developerTools.mcpToken.tokenDescription")}</p>
              <div className={styles.fieldGrid}>
                <McpTokenWorkspaces settings={settings} />
                <div className={styles.cardField}>
                  <span>{t("developerTools.mcpToken.tokenLabel")}</span>
                  {settings.oauth ? <span className={styles.secretRow} data-mcp-token-value>{t("developerTools.oauth")}</span> : (
                    <span className={styles.secretRow} data-mcp-token-value={shown && key ? "shown" : "masked"}>
                      <code className={own.token}>{shown && key ? key : "•".repeat(32)}</code>
                      <span className={own.tokenActions}>
                        <button type="button" className={styles.icon} aria-label={t("developerTools.mcpToken.showOrHide")}
                          aria-pressed={shown} disabled={!kept} data-mcp-token-show onClick={() => void toggleShown()}>
                          {shown ? <EyeSlashIcon size="small" aria-hidden="true" /> : <EyeIcon size="small" aria-hidden="true" />}</button>
                        <button type="button" className={styles.icon} aria-label={t("developerTools.mcpToken.refresh")}
                          title={t("developerTools.internal.renewedAt", { date: longDate(settings.tokenMadeAt) })}
                          data-mcp-token-refresh onClick={() => void refresh()}><RefreshInCircleIcon size="small" aria-hidden="true" /></button>
                        <button type="button" className={styles.icon} aria-label={t("developerTools.connection.copy")}
                          title={t("developerTools.connection.copy")} disabled={!kept} data-mcp-token-copy
                          onClick={() => void revealed().then((value) => { if (value) void copy(value, t("developerTools.mcpToken.copied")); })}>
                          <Copy size="small" aria-hidden="true" /></button>
                      </span>
                    </span>
                  )}
                  {settings.oauth || kept ? null : <small className={own.note} data-mcp-token-not-kept>{t("developerTools.mcpToken.notKept")}</small>}
                </div>
              </div>
              {settings.oauth ? null : <div className={styles.fieldGrid}>
                <div className={styles.cardField}>
                  <span>{t("developerTools.mcpTokenAddress")}</span>
                  <span className={styles.secretRow} data-mcp-token-address>
                    <code className={own.token}>{currentMCPServerUrl()}</code>
                    <button type="button" className={styles.icon} aria-label={t("developerTools.connection.copy")} title={t("developerTools.connection.copy")}
                      onClick={() => void copy(currentMCPServerUrl())}><Copy size="small" aria-hidden="true" /></button>
                  </span>
                </div>
                {/* A key ends on its day (agent key decision of 2026-11-16); an OAuth connection renews itself and says none. */}
                {settings.expiresAt ? <div className={styles.cardField}>
                  <span>{t("developerTools.mcpToken.expires")}</span>
                  <span className={`${styles.secretRow} ${token.oneLine}`} data-mcp-token-expiry>
                    {t("developerTools.mcpToken.expiresOn", { date: longDate(settings.expiresAt) })}</span>
                </div> : null}
              </div>}
              {settings.previousUntil ? <p className={own.note} data-mcp-token-previous>
                {t("developerTools.mcpToken.previousUntil", { date: dateAndTime(settings.previousUntil) })}</p> : null}
            </section>
            <McpTokenTools settings={settings} onChange={(disabledTools) => void save({ disabledTools })} />
            <McpTokenNotionOnlyTools settings={settings} onChange={(notionOnlyTools) => void save({ notionOnlyTools }, { notionOnlyTools })} />
            <McpTokenAsks settings={settings} onChange={(handsAllOver) => void save({ handsAllOver }, { handsAllOver })} />
            <DeveloperDangerZone kind="mcpToken" description={t("developerTools.mcpToken.dangerDescription")} onRemove={() => void remove()} />
          </> : (
            <McpTokenAccess session={session} settings={settings} onSettings={setSettings} onError={setError} />
          )}
          {error ? <p className={styles.error} role="alert">{error}</p> : null}
        </> : null}
      </main>
    </>
  );
}

/** "개발자 도구 / MCP 토큰 / 〈name〉" over a token's page. */
function McpTokenCrumbs({ name }: { name: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <>
      <span className={topBarStyles.crumbWrap}>
        <button type="button" className={topBarStyles.crumb} onClick={() => pushRoute(developerToolsHref("start"))}>
          <span className={topBarStyles.crumbIcon}><DeveloperIcon size="small" aria-hidden="true" /></span>
          <span className={topBarStyles.crumbLabel}>{t("developerTools.title")}</span>
        </button>
      </span>
      <span className={styles.crumbSep} aria-hidden="true">/</span>
      <span className={topBarStyles.crumbWrap}>
        <button type="button" className={topBarStyles.crumb} onClick={() => pushRoute(developerToolsHref("mcp-tokens"))}>
          <span className={topBarStyles.crumbIcon}><McpTokenIcon size="small" aria-hidden="true" /></span>
          <span className={topBarStyles.crumbLabel}>{t("developerTools.tabs.mcp-tokens")}</span>
        </button>
      </span>
      <span className={styles.crumbSep} aria-hidden="true">/</span>
      <span className={topBarStyles.crumbWrap}>
        <span className={topBarStyles.crumb} data-topbar-current-page><span className={topBarStyles.crumbLabel}>{name}</span></span>
      </span>
    </>
  );
}
