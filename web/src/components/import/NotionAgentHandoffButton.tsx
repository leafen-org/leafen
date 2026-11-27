import { useState } from "react";
import { useTranslation } from "react-i18next";
import { outsideAddress } from "@/lib/addressReach";
import { makeAgentConnectionText } from "@/lib/agentConnectionText";
import { copyText } from "@/lib/copyText";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { serverOrigin } from "@/lib/serverOrigin";
import { useConnectedAgents } from "@/lib/useConnectedAgents";
import type { Session } from "@/types";
import styles from "./ImportDialog.module.css";
import own from "./NotionImportAgentFirst.module.css";

/**
 * "에이전트에게 맡기기" of the Notion window, at its start and at the end of
 * an import made by hand (decision of 2026-11-12, "6의 보탬"): a card with
 * one short line and one button. With an agent connected the button copies
 * the move's first words. Without one, two steps show — ① connect the
 * agent, ② start the move — and the button is the step's own: ① copies the
 * connection text (which ends with the move), and once an agent connects ②
 * copies the move, beside the line that says to paste it into the same
 * conversation: an agent that doesn't see Leafen's tools there yet goes on as
 * its connection text says (after /reload-plugins in Claude Code's terminal,
 * by calling Leafen's MCP over HTTP elsewhere — 2026-11-19).
 */
export function NotionAgentHandoff({ session, workspaceId, step, body, move, copiedMove, onHanded }: {
  session: Session;
  workspaceId: string;
  /** Which hand-off it is, for the page's own marks. */
  step: "start" | "finish";
  body: string;
  /** The move's first words, with the address the agent reaches Leafen at. */
  move(origin: string): string;
  /** What the card says once the move's words are copied. */
  copiedMove: string;
  /** The move's words were copied: the agent has it from here. */
  onHanded?(): void;
}) {
  const { t } = useTranslation(["importDialog", "agentPrompt", "workspaceSettingsDialog"]);
  const [copied, setCopied] = useState<"" | "connect" | "move">("");
  const [copying, setCopying] = useState(false);
  // Whether an agent was connected as the card came: without one, the two steps show.
  const [staged, setStaged] = useState<boolean | null>(null);
  // The agents there were when a connection text was copied: one since is new.
  const [before, setBefore] = useState<Set<string> | null>(null);
  const agents = useConnectedAgents(session, workspaceId, (first) => setStaged(first.length === 0));
  const connected = (agents?.length ?? 0) > 0;
  const joined = before ? (agents ?? []).filter((agent) => !before.has(agent.id)).map((agent) => agent.name) : [];

  async function hand(kind: "connect" | "move") {
    setCopying(true);
    try {
      const { config } = await fetchRuntimeConfig();
      // The address the connection text gives agents: the outside one where the
      // server has it, else this page's (2026-11-22: an http address of this
      // computer's in LEAFEN_PUBLIC_URL was named as if it were the outside one).
      const written = config.publicAddress ?? "";
      const publicAddress = outsideAddress(written) ? written : "";
      const words = move(publicAddress || serverOrigin());
      if (kind === "move") {
        // The move's first words alone say which language to talk in, as the connection text does.
        if (await copyText(`${words} ${t("agentPrompt:talk")}`)) {
          setCopied("move");
          onHanded?.();
        }
        return;
      }
      // An agent connects first, and the text it connects with ends with the move.
      const text = await makeAgentConnectionText(session, {
        workspaceId, clientName: t("workspaceSettingsDialog:agents.otherClientName"), publicAddress, startWith: words,
      }).catch(() => "");
      setBefore(new Set((agents ?? []).map((agent) => agent.id)));
      if (text && await copyText(text)) setCopied("connect");
    } finally {
      setCopying(false);
    }
  }

  const kind = connected ? "move" : "connect";
  const status = copied === "move" ? copiedMove
    : joined.length > 0 ? t("importDialog:handoff.connectedNow", { names: joined.join(", ") })
    : copied === "connect" ? t("importDialog:handoff.copiedConnect") : "";
  return (
    <section className={own.handoff} data-notion-hand-to-agent={step} data-notion-handoff-stage={staged ? (connected ? 2 : 1) : undefined}>
      <strong>{t("importDialog:handoff.title")}<span className={own.badge}>{t("importDialog:handoff.recommended")}</span></strong>
      <p>{body}</p>
      {/* What to press waits for the first read of the agents, so its words don't change under the pointer. */}
      {agents === null ? <span className={own.actionPlace} aria-hidden="true" /> : (
        <>
          {staged ? (
            <ol className={own.stages}>
              <li data-current={!connected || undefined} data-done={connected || undefined}>{t("importDialog:handoff.stages.connect")}</li>
              <li data-current={connected || undefined}>
                {t("importDialog:handoff.stages.move")}
                {connected ? <small>{t("importDialog:handoff.sameConversation")}</small> : null}
              </li>
            </ol>
          ) : connected ? <small className={own.agents} data-notion-agents>{t("importDialog:handoff.agents", { names: agents.map((agent) => agent.name).join(", ") })}</small> : null}
          <span className={own.action}>
            {/* At the end the main thing is the imported page: this one is the second. */}
            <button type="button" className={step === "finish" ? own.quietButton : styles.primary} disabled={copying} data-notion-handoff-copy={kind}
              onClick={() => void hand(kind)}>
              {copying ? t("importDialog:handoff.copying") : t(kind === "move" ? "importDialog:handoff.copyMove" : "importDialog:handoff.copyConnect")}
            </button>
            {status ? <span className={own.status} role="status" data-notion-handoff-status>{status}</span> : null}
          </span>
        </>
      )}
    </section>
  );
}
