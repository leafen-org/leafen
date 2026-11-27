"use client";

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { makeAgentConnectionText } from "@/lib/agentConnectionText";
import type { Session } from "@/types";
import { remoteAddressPrompt, tailscaleReason, type ServerReach } from "./remoteAddressPrompt";
import styles from "./RemoteAddressChoice.module.css";
import { copyText } from "@/lib/copyText";

const DO_IT_YOURSELF = "https://github.com/leafen-org/leafen/blob/main/docs/self-hosting.md#do-it-yourself";

/**
 * The address choice when Tailscale can't be used (the built-in tunnel
 * decision's changes of 2026-10-19): the one large button hands it to an
 * agent — a text with what the server measured, to work out with the person;
 * the other ways are folded under "다른 방법" as the choice's are (the
 * one-thing-per-screen decision of 2026-11-09): the do-it-yourself guide and
 * Tailscale itself, which only says why it can't be used. A closed network
 * hides Tailscale. A company's server, which could use it, has it there with
 * its own settings. "나중에" is small, with what waiting costs. No retrying
 * and no list of errors: once the agent sets the address, the screen notices.
 */
export function RemoteAddressOwnWay({ session, reach, workspaceId, screen = false, onLater, onTailscale, onDomain }: {
  session: Session;
  /** The first run's step (its skip cost is marked as the step's). */
  screen?: boolean;
  reach: ServerReach;
  /** The workspace an agent's connection code is for; without one the text asks the agent to connect first. */
  workspaceId?: string;
  onLater: () => void;
  /** For a company's server, where Tailscale works: starting it anyway. */
  onTailscale?: () => void;
  /** For a company's server or a closed network's, both "사내 주소 사용": its own address, given in the same screen (RemoteAddressChoice). */
  onDomain?: () => void;
}) {
  const { t } = useTranslation(["serverConsole", "workspaceSettingsDialog"]);
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const closed = reach.situation === "closed";
  // A company's server that could use Tailscale is offered its own address first.
  const company = reach.situation === "ok" && reach.company === true;
  // The words are copied for the person's outside agent.
  async function handToAgent() {
    setBusy(true);
    try {
      const task = remoteAddressPrompt(t, reach);
      const connection = workspaceId ? await makeAgentConnectionText(session, {
        workspaceId, clientName: t("workspaceSettingsDialog:agents.otherClientName"), publicAddress: "", startWith: task,
      }).catch(() => "") : "";
      const text = connection || `${t("serverConsole:remoteChoice.prompt.connectFirst")}\n\n${task}`;
      // Where it isn't copied, a card shows it to copy by hand (copyText.ts).
      setCopied(await copyText(text));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.choices} data-remote-own-way={reach.situation}>
      <button type="button" className={styles.primaryChoice} disabled={busy} data-remote-choice="agent" onClick={() => void handToAgent()}>
        <strong>{t("serverConsole:remoteChoice.handToAgent")}<span className={styles.badge}>{t("serverConsole:remoteChoice.recommended")}</span></strong>
        <span>{t(closed || company ? "serverConsole:remoteChoice.handToAgentClosed" : "serverConsole:remoteChoice.handToAgentBody")}</span>
        {copied ? <span className={styles.fact} role="status">{t("serverConsole:remoteChoice.handToAgentCopied")}</span> : null}
      </button>
      <details className={styles.otherWays} data-remote-other-ways>
        <summary>{t("serverConsole:remoteChoice.otherWays")}</summary>
        <div className={styles.smallChoices}>
          {/* A closed network's screen is titled "사내 주소 사용" as a company's: its own
              address is given here too (2026-11-06: only an agent or the guide were offered). */}
          {(company || closed) && onDomain ? (
            <button type="button" className={styles.smallChoice} data-remote-choice="domain" onClick={onDomain}>
              <strong>{t("serverConsole:remoteChoice.domain")}</strong>
              <span>{t("serverConsole:remoteChoice.domainBody")}</span>
            </button>
          ) : null}
          <a className={styles.smallChoice} href={DO_IT_YOURSELF} target="_blank" rel="noopener noreferrer" data-remote-choice="guide">
            <strong>{t("serverConsole:remoteChoice.doItYourself")}</strong>
            <span>{t(closed ? "serverConsole:remoteChoice.doItYourselfClosed" : "serverConsole:remoteChoice.doItYourselfBody")}</span>
          </a>
          {company && onTailscale ? (
            <button type="button" className={styles.smallChoice} data-remote-choice="tailscale" onClick={onTailscale}>
              <strong>{t("serverConsole:tunnel.tailscale")}</strong>
              <span>{t("serverConsole:remoteChoice.tailscaleAtCompany")}</span>
            </button>
          ) : null}
          {/* Tailscale says why it can't be used here from the start, not the easy
              steps the screen's first line has just ruled out; nothing to press, and
              no card changing height under the pointer (2026-11-06). */}
          {!closed && !company ? (
            <div className={styles.smallChoice} data-remote-choice="tailscale-unusable">
              <strong>{t("serverConsole:tunnel.tailscale")}</strong>
              <span>{t("serverConsole:remoteChoice.tailscaleUnusable", { reason: tailscaleReason(t, reach) })}</span>
            </div>
          ) : null}
        </div>
      </details>
      <div className={styles.screenLater}>
        <button type="button" className={styles.laterLink} data-remote-choice="later" onClick={onLater}>
          {t("serverConsole:remoteChoice.later")}
        </button>
        <p className={styles.fact} data-first-run-skip-cost={screen || undefined}>{t("serverConsole:remoteChoice.laterBody")}</p>
      </div>
    </div>
  );
}
