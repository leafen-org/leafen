import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { GuidePlayer, type GuideScene } from "../guide/GuidePlayer";
import { chatgptScreens, type ChatGPTWords } from "./chatgptConnectorScreens";
import { claudeScreens, type ClaudeWords } from "./claudeConnectorScreens";
import { leafenScreens, type LeafenWords } from "./leafenConnectorScreens";
import { claudeConnectorBlockingPort } from "@/lib/claudeConnector";
import css from "./AgentConnectorGuide.module.css";

type App = "claude" | "chatgpt";
const APPS: readonly App[] = ["claude", "chatgpt"];

type StepWords = { title: string; caption: string };

/**
 * The moving guide under the address a chat app's connector takes: adding
 * Leafen as a custom connector in Claude (web and desktop) or, as the other
 * choice, as a custom MCP server in ChatGPT, then Leafen's own sign-in and
 * allow screens as they are. It is shown only for an address that opens from
 * the internet over HTTPS (the one passed in), and shows only: nothing here
 * changes the person's apps. At an address whose port keeps Claude's
 * connector out (lib/claudeConnector), Claude's steps would end at Claude's
 * check: Claude's choice says in a line, in their place, that they show at
 * a 443 address (the connector's row above says the rest), and the guide
 * opens on ChatGPT's.
 */
export function AgentConnectorGuide({ mcpUrl }: { mcpUrl: string }) {
  const { t } = useTranslation(["agentConnectorGuide", "workspaceSettingsDialog", "authGate", "common"]);
  const claudePort = claudeConnectorBlockingPort(mcpUrl);
  const [app, setApp] = useState<App>(claudePort ? "chatgpt" : "claude");
  const appName = t(`agentConnectorGuide:apps.${app}`);
  const leafen = useMemo<LeafenWords>(() => ({
    host: hostOf(mcpUrl),
    mcpUrl,
    appRow: t("workspaceSettingsDialog:agents.kind.app"),
    appHint: t("workspaceSettingsDialog:agents.connectorHint"),
    copyAddress: t("workspaceSettingsDialog:agents.copyAddress"),
    copied: t("common:copiedToClipboard"),
    codeTitle: t("authGate:deviceCodeTitle"),
    codeHint: t("authGate:deviceCodeHint"),
    codeLabel: t("authGate:deviceCodeLabel"),
    continueLabel: t("authGate:continue"),
    scanCode: t("authGate:scanCode"),
    consentTitle: t("authGate:mcpConsent.title"),
    consentAccess: t("authGate:mcpConsent.access"),
    consentLegend: t("authGate:mcpConsent.workspacesLegend"),
    deny: t("authGate:mcpConsent.deny"),
    allow: t("authGate:mcpConsent.allow"),
    workspace: t("agentConnectorGuide:workspace"),
    client: appName,
  }), [appName, mcpUrl, t]);
  const scenes = useMemo<GuideScene[]>(() => {
    const step = (key: string, values?: Record<string, string>): StepWords => ({
      title: t(`agentConnectorGuide:${key}.title`, values),
      caption: t(`agentConnectorGuide:${key}.caption`, values),
    });
    const shared = leafenScreens(leafen);
    const name = t("agentConnectorGuide:connectorName");
    const own = { app: appName };
    if (app === "claude") {
      const screens = claudeScreens({
        ...(t("agentConnectorGuide:claude.mock", { returnObjects: true }) as ClaudeWords),
        connected: t("agentConnectorGuide:claude.mock.connected", { name }),
        connectorName: name, mcpUrl,
      });
      return [
        { ...step("steps.copy", own), picture: shared.copy },
        { ...step("claude.steps.open"), picture: screens.open, durationMs: 4200 },
        { ...step("claude.steps.address"), picture: screens.address, durationMs: 3400 },
        { ...step("claude.steps.auth"), picture: screens.auth, durationMs: 3600 },
        { ...step("claude.steps.connect"), picture: screens.connect },
        { ...step("steps.signIn"), picture: shared.signIn, durationMs: 3400 },
        { ...step("steps.allow", own), picture: shared.allow },
        { ...step("claude.steps.done"), picture: screens.done },
      ];
    }
    const screens = chatgptScreens({
      ...(t("agentConnectorGuide:chatgpt.mock", { returnObjects: true }) as ChatGPTWords),
      connectorName: name, mcpUrl,
    });
    return [
      { ...step("steps.copy", own), picture: shared.copy },
      { ...step("chatgpt.steps.open"), picture: screens.open, durationMs: 3800 },
      { ...step("chatgpt.steps.address"), picture: screens.address, durationMs: 4000 },
      { ...step("steps.signIn"), picture: shared.signIn, durationMs: 3400 },
      { ...step("steps.allow", own), picture: shared.allow },
      { ...step("chatgpt.steps.done"), picture: screens.done },
    ];
  }, [app, appName, leafen, t]);

  return (
    <div className={css.guide} data-agent-connector-guide={app}>
      <div className={css.apps} role="group" aria-label={t("agentConnectorGuide:appsLabel")}>
        {APPS.map((each) => (
          <button key={each} type="button" className={css.app} aria-pressed={each === app}
            onClick={() => setApp(each)} data-agent-connector-guide-app={each}>
            {t(`agentConnectorGuide:apps.${each}`)}
          </button>
        ))}
      </div>
      {app === "claude" && claudePort ? (
        <p className={css.note} data-agent-connector-guide-port={claudePort}>
          {t("agentConnectorGuide:claudePort", { port: claudePort })}
        </p>
      ) : (
        // Another app is another guide: it starts from its first scene.
        <GuidePlayer key={app} label={t("agentConnectorGuide:region", { app: appName })}
          shows={t("agentConnectorGuide:shows", { app: appName })} scenes={scenes} />
      )}
    </div>
  );
}

function hostOf(address: string): string {
  try {
    return new URL(address).host;
  } catch {
    return address;
  }
}
