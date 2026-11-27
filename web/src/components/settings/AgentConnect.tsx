import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { request as apiRequest } from "@/apiTransport";
import { AgentPasteGuide } from "@/components/agentPasteGuide/AgentPasteGuide";
import { AgentConnectorGuide } from "@/components/agentConnectorGuide/AgentConnectorGuide";
import { AgentAllowByCode } from "./AgentAllowByCode";
import { RemoteAddressChoice } from "@/components/remoteAccess/RemoteAddressChoice";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { serverOrigin } from "@/lib/serverOrigin";
import {
  agentConnectionPrompt, agentEntryNames, agentInstallLinks, agentInstructionLines, agentServerAddress,
} from "@/lib/agentConnection";
import { CODE_EXCHANGE_PATH, makeAgentConnection } from "@/lib/agentConnectionText";
import { agentServerLatinName } from "@/lib/agentEntryNames";
import { agentConnectThisComputerText } from "@/lib/agentConnectThisComputer";
import { claudeConnectorBlockingPort } from "@/lib/claudeConnector";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import type { Session } from "@/types";
import { styles } from "./workspaceSettingsOverviewStyles";
import agentStyles from "./AgentConnect.module.css";
import { useCopyNotice } from "@/lib/copyNotice";
import { AppSelect } from "@/components/AppSelect";
import { PERSONAL_ACCESS_TOKEN_LIFETIMES, type PersonalAccessTokenLifetime } from "@/lib/personalAccessTokens";
import { KEY_LIFETIME_KEYS, keyLifetimeEnd } from "./KeyLifetimeRow";

// How long a one-time code works, as the server makes it.
const CODE_LIFETIME_MS = 10 * 60_000;

/**
 * "Connect an agent" (the agents-outside decision's addition of 2026-11-04;
 * the first-run guides decision of 2026-11-08, 2): one thing shows first —
 * "Copy text", the one text to paste into any agent, and a moving guide of
 * pasting it into one under it. An agent that adds servers itself connects
 * by it, and one that can't tells the person where to put what. Everything
 * else stands folded below in one place: the text itself and a new code, the
 * command an agent on Leafen's own computer connects by (asking its person
 * in the conversation first), the address a chat app's connector takes,
 * the editors' links, the instruction lines, and — without an outside address
 * — what to turn on for agents elsewhere. Settings' agents and the first
 * run's agents step show the same.
 */
export function AgentConnect({
  session,
  workspaceId,
  firstRun = false,
  onCodeMade,
  connectedNow = [],
}: {
  session: Session;
  workspaceId: string;
  /**
   * The first run's agents step: the text is made as it opens, and the step's
   * own head says what to do, so the copy button stands alone.
   */
  firstRun?: boolean;
  /** A text with a new one-time code is out: an agent may connect with it now. */
  onCodeMade?: () => void;
  /** Agents that connected since a text was made here, said at once. */
  connectedNow?: string[];
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  // In the screen's language: read again as it renders, so a language chosen meanwhile shows.
  const thisComputerText = agentConnectThisComputerText();
  // The server's public address — written, or a running tunnel's — comes
  // before the address this page was opened at; its name in plain letters
  // goes into the text as makeAgentConnectionText reads it.
  const [publicAddress, setPublicAddress] = useState("");
  const [serverName, setServerName] = useState("");
  const [serverCode, setServerCode] = useState("");
  useEffect(() => {
    let current = true;
    void fetchRuntimeConfig(true).then(({ config }) => {
      if (!current) return;
      setPublicAddress(config.publicAddress ?? "");
      setServerName(agentServerLatinName({ name: config.serverName ?? "", alias: config.serverAlias ?? "" }));
      setServerCode(config.serverCode ?? "");
    });
    return () => { current = false; };
  }, []);
  const address = useMemo(() => agentServerAddress(publicAddress || serverOrigin()), [publicAddress]);
  const quickTunnel = /\.trycloudflare\.com$/u.test(address.origin);
  // A port that keeps Claude's connector from the public address, said only
  // at the connector's row (lib/claudeConnector).
  const connectorPort = address.outside ? claudeConnectorBlockingPort(address.origin) : "";
  const [prompt, setPrompt] = useState("");
  // Whether the agent asks how to start, as the server judged it with the
  // last code (agent_start_offer.go); until one is made, the first run's yes.
  const [offerStart, setOfferStart] = useState(firstRun);
  // How long the key an agent trades the text's code for lives, said beside
  // what it may do (agent key decision of 2026-11-16); another choice makes
  // a new text.
  const [lifetime, setLifetime] = useState<PersonalAccessTokenLifetime>("1y");
  // The agent connection a text's code makes works as its person, with
  // everything they may, in all their workspaces
  // (internal/app/connection_person_rights.go): nothing to choose here.
  // A text made for one address is made again when the address changes.
  const madeFor = useRef(address.origin);
  useEffect(() => {
    if (madeFor.current === address.origin) return;
    madeFor.current = address.origin;
    const remake = firstRun || Boolean(prompt);
    setPrompt("");
    if (remake) void makePrompt();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only an address change remakes the text
  }, [address.origin]);
  const [making, setMaking] = useState(false);
  const [error, setError] = useState("");
  const copyNotice = useCopyNotice();
  const [choosingAddress, setChoosingAddress] = useState(false);
  // The outside address is the server administrator's to turn on.
  const [mayChooseAddress, setMayChooseAddress] = useState(false);
  const request = useRef<AbortController | null>(null);
  // The text being made, which a press of "Copy text" meanwhile waits for
  // rather than making a second code.
  const pending = useRef<Promise<string> | null>(null);

  useEffect(() => () => request.current?.abort(), []);
  // A text whose code is spent — an agent connected since it was made, or its
  // ten minutes ran out — is let go: the box no longer shows a code the server
  // refuses, and "Copy text" makes a new one (2026-11-07, OP-06).
  const connectedSince = connectedNow.length;
  useEffect(() => { if (connectedSince) setPrompt(""); }, [connectedSince]);
  useEffect(() => {
    if (!prompt) return;
    const timer = window.setTimeout(() => setPrompt(""), CODE_LIFETIME_MS);
    return () => window.clearTimeout(timer);
  }, [prompt]);
  useEffect(() => {
    setPrompt("");
    setError("");
    setOfferStart(firstRun);
    if (firstRun) void makePrompt();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- a new workspace takes a new text
  }, [workspaceId]);
  // The text an agent is given says the same: who turns the address on (2026-11-07:
  // a member's text sent them to a button only the administrator has).
  const mayChoose = useMemo(() => address.outside ? Promise.resolve(false)
    : apiRequest("/admin/tunnel", { method: "GET" }, session).then(() => true, () => false), [address.outside, session]);
  useEffect(() => {
    let current = true;
    void mayChoose.then((may) => { if (current) setMayChooseAddress(may); });
    return () => { current = false; };
  }, [mayChoose]);
  // What the guide shows the agent getting before a text is made here: one
  // made the same way, with a stand-in for the code.
  const shownPrompt = useMemo(() => prompt || agentConnectionPrompt({
    localOrigin: serverOrigin(), publicOrigin: address.outside ? address.origin : "", linesOrigin: address.origin, code: "lcc_…",
    exchangePath: CODE_EXCHANGE_PATH, expiresAt: Date.now() + CODE_LIFETIME_MS,
    canTurnOnPublic: address.outside ? undefined : mayChooseAddress, serverName, serverCode, person: session.user,
    keyEnds: keyLifetimeEnd(lifetime), offerStart,
  }), [prompt, address, mayChooseAddress, serverName, serverCode, session.user, lifetime, offerStart]);

  // A copy button keeps its label and a toast says it copied (lib/copyNotice;
  // 2026-10-16: "명령 복사" copied and said nothing).
  async function copy(value: string) {
    setError("");
    await copyNotice(value);
  }

  function makePrompt(): Promise<string> {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setMaking(true);
    setError("");
    setPrompt("");
    const made = (async () => {
      try {
        const { text, offerStart: offered } = await makeAgentConnection(session, {
          workspaceId, clientName: t("agents.otherClientName"), expiresIn: lifetime,
          publicAddress: address.outside ? address.origin : "", signal: controller.signal,
          canTurnOnPublic: address.outside ? undefined : await mayChoose,
        });
        if (controller.signal.aborted) return "";
        setOfferStart(offered);
        setPrompt(text);
        onCodeMade?.();
        return text;
      } catch (makeError) {
        if (!controller.signal.aborted) setError(settingsErrorMessage(makeError, t("agents.codeFailed")));
        return "";
      } finally {
        if (request.current === controller) setMaking(false);
      }
    })();
    pending.current = made;
    void made.finally(() => { if (pending.current === made) pending.current = null; });
    return made;
  }

  // "Copy text" copies the text made, or makes it first. Safari lets a click
  // copy only what it starts copying during the click, so a text still being
  // made is handed to the clipboard as a promise (as an invitation link is).
  async function copyPrompt() {
    if (prompt) {
      await copy(prompt);
      return;
    }
    const making = pending.current ?? makePrompt();
    let writing: Promise<void> | undefined;
    if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
      writing = navigator.clipboard.write([new ClipboardItem({
        "text/plain": making.then((text) => {
          if (!text) throw new Error("no text");
          return new Blob([text], { type: "text/plain" });
        }),
      })]);
      writing.catch(() => undefined);
    }
    const text = await making;
    if (text) await copyNotice(text, undefined, undefined, writing);
  }

  const chooseAddress = choosingAddress ? (
    <RemoteAddressChoice session={session} workspaceId={workspaceId} onClose={(chosen) => {
      setChoosingAddress(false);
      if (chosen) setPublicAddress(chosen);
    }} />
  ) : null;
  if (address.limit === "https-required") {
    return (
      <div className={styles.plainGroup} data-agent-connect="https-required">
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("agents.connect")}</strong>
            <span>{t("agents.httpsRequired", { origin: address.origin })}</span>
            <span>{t(mayChooseAddress ? "agents.httpsRequiredChoose" : "agents.httpsRequiredAsk")}</span>
          </div>
          {mayChooseAddress ? (
            <button type="button" className={styles.secondaryButton} onClick={() => setChoosingAddress(true)} data-agent-choose-address>
              {t("agents.outsideChoose")}
            </button>
          ) : null}
        </div>
        {chooseAddress}
      </div>
    );
  }
  const links = agentInstallLinks(address.mcpUrl, agentEntryNames(serverName, serverCode)[0]);
  // Without an outside address the text connects an agent on this computer
  // or network (Claude Code, Codex), first; the chat apps need the address,
  // said right under it with where it is turned on or who turns it on, not
  // folded away (2026-11-22: the sidebar's agent card leads here). The first
  // run asked for the address a step before, so there it stays folded.
  const needsOutside = !address.outside ? (
    <div className={styles.securityRow} data-agent-needs-outside>
      <div className={styles.securityInstruction}>
        <span>{t(mayChooseAddress ? "agents.outsideNote" : "agents.outsideNoteAsk")}</span>
      </div>
      {mayChooseAddress ? (
        <button type="button" className={styles.secondaryButton} onClick={() => setChoosingAddress(true)} data-agent-choose-address>
          {t("agents.outsideChoose")}
        </button>
      ) : null}
    </div>
  ) : null;
  const copyButton = (
    // Never `disabled`: the focus a first run's step gives it stays, and a
    // press while the text is made copies it once it is.
    <button type="button" className={styles.primaryButton} onClick={() => void copyPrompt()} data-agent-copy="prompt">
      {t("agents.copyText")}
    </button>
  );
  return (
    <div className={styles.plainGroup} data-agent-connect>
      {firstRun ? (
        <div className={agentStyles.copyAlone} data-agent-prompt>{copyButton}</div>
      ) : (
        <div className={styles.securityRow} data-agent-prompt>
          <div className={styles.securityInstruction}>
            <span>{t("agents.pasteHint")}</span>
          </div>
          {copyButton}
        </div>
      )}
      {firstRun ? null : needsOutside}
      {/* What the key may do and until when, in one line, its end chosen at its end. */}
      <div className={styles.securityRow} data-agent-key-line={lifetime}>
        <div className={styles.securityInstruction}>
          <span>{t("agents.keyLine", { date: keyLifetimeEnd(lifetime) })}</span>
        </div>
        <AppSelect look="value" plain="ticked" menuWidth={180} menuGap={8} menuAlign="end" aria-label={t("personalAccessTokenExpiryLabel")}
          value={lifetime} onChange={(event) => { setLifetime(event.target.value as PersonalAccessTokenLifetime); setPrompt(""); }}>
          {PERSONAL_ACCESS_TOKEN_LIFETIMES.map((value) => <option key={value} value={value}>{t(KEY_LIFETIME_KEYS[value])}</option>)}
        </AppSelect>
      </div>
      {connectedNow.length ? (
        <div className={styles.securityInstruction} role="status" data-agent-connected-now>
          <strong>{t("firstInstall:agentsConnected", { names: connectedNow.join(", ") })}</strong>
          <span>{t("firstInstall:agentsConnectedBody")}</span>
        </div>
      ) : null}
      {error ? <div className={styles.errorText} role="alert">{error}</div> : null}
      <AgentPasteGuide prompt={shownPrompt} place={firstRun ? "firstRun" : "settings"} offerStart={offerStart} />
      <details className={styles.securityDetails} data-agent-other-ways>
        <summary>{t("agents.more")}</summary>
        <div className={styles.securityRow} data-agent-prompt-text>
          <div className={styles.securityInstruction}>
            <strong>{t("agents.promptTitle")}</strong>
            <span>{making ? t("agents.makingCode") : t("agents.promptHint")}</span>
            {quickTunnel ? <span data-agent-quick-tunnel>{t("agents.tunnelQuickNote")}</span> : null}
            {/* It is for the agent to read: copying is the way, reading it a choice. */}
            {prompt ? <code className={`${styles.securityCode} ${agentStyles.wrapped}`}>{prompt}</code> : null}
          </div>
          <button type="button" className={styles.secondaryButton} disabled={making} onClick={() => void makePrompt()}>
            {t("agents.newCode")}
          </button>
        </div>
        {firstRun ? needsOutside : null}
        {/* An agent on Leafen's own computer connects by its command, asking its person in the conversation first. */}
        <div className={styles.securityRow} data-agent-command="this-computer">
          <div className={styles.securityInstruction}>
            <strong>{t("agents.thisComputer")}</strong>
            <code className={`${styles.securityCode} ${agentStyles.wrapped}`}>{thisComputerText}</code>
            <span>{t("agents.thisComputerHint")}</span>
          </div>
          <button type="button" className={styles.secondaryButton}
            onClick={() => void copy(thisComputerText)} data-agent-copy="this-computer">
            {t("agents.copyPrompt")}
          </button>
        </div>
        <AgentAllowByCode session={session} />
        {address.outside ? (
          <div className={styles.securityRow} data-agent-connector>
            <div className={styles.securityInstruction}>
              <strong>{t("agents.kind.app")}</strong>
              <code className={`${styles.securityCode} ${agentStyles.wrapped}`}>{address.mcpUrl}</code>
              <span>{t("agents.connectorHint")}</span>
              {connectorPort ? <span data-agent-connector-port={connectorPort}>{t("agents.connectorPort", { port: connectorPort })}</span> : null}
            </div>
            <button type="button" className={styles.secondaryButton}
              onClick={() => void copy(address.mcpUrl)} data-agent-copy="connector">
              {t("agents.copyAddress")}
            </button>
          </div>
        ) : null}
        {address.outside ? <AgentConnectorGuide mcpUrl={address.mcpUrl} /> : null}
        <div className={agentStyles.installLinks}>
          <a className={styles.secondaryButton} href={links.cursor}>{t("agents.cursor")}</a>
          <a className={styles.secondaryButton} href={links.vscode}>{t("agents.vscode")}</a>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{t("agents.instructionTitle")}</strong>
            <span>{t("agents.instructionHint")}</span>
            <code className={`${styles.securityCode} ${agentStyles.wrapped}`}>{agentInstructionLines(address.origin)}</code>
          </div>
          <button type="button" className={styles.secondaryButton}
            onClick={() => void copy(agentInstructionLines(address.origin))} data-agent-copy="instructions">
            {t("agents.copyPrompt")}
          </button>
        </div>
      </details>
      {chooseAddress}
    </div>
  );
}
