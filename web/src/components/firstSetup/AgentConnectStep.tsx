import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { AgentConnect } from "@/components/settings/AgentConnect";
import { useConnectedAgents } from "@/lib/useConnectedAgents";
import type { Session } from "@/types";
import auth from "../AuthGate.module.css";
import { FirstRunScreen } from "../firstInstall/FirstRunScreen";
import css from "./FirstSetup.module.css";

/**
 * The first run's agents step (the agents-outside decision of 2026-10-29, 8):
 * Leafen runs no agents of its own, so it shows how an outside one connects
 * — one text to copy and paste into it, whichever it is, with a moving guide
 * of doing so (the first-run guides decision of 2026-11-08, 2) — as
 * Settings' agents do. Once one has connected the step goes on; "나중에" is a
 * small one that says where it waits. An agent that connected itself before
 * the step came — one going through the first run from the terminal (user
 * decision 2026-11-09) — passes it.
 */
export function AgentConnectStep({ session, workspaceId, onDone }: {
  session: Session;
  workspaceId: string;
  /** The step is over: an agent connected, or it was left for later. */
  onDone(connected: boolean): void;
}) {
  const { t } = useTranslation("firstInstall");
  // An agent that connected before the step came passes it.
  const connected = (useConnectedAgents(session, workspaceId, (agents) => { if (agents.length > 0) onDone(true); }) ?? [])
    .map((agent) => agent.name);
  // On a short window the way on comes up below the text: it is what's next.
  const onward = useRef<HTMLButtonElement>(null);
  const anyConnected = connected.length > 0;
  useEffect(() => { if (anyConnected) onward.current?.scrollIntoView({ block: "nearest" }); }, [anyConnected]);
  return (
    // Once one connects the way on is a new view: the focus, left on the gone
    // connect text, enters it.
    <FirstRunScreen step="agents" name={t("agentsTitle")} view={anyConnected ? "connected" : "connect"}>
      <div className={auth.form} data-first-run-agents={connected.length > 0 ? "connected" : ""}>
        <div className={auth.notice}>
          <strong>{connected.length > 0 ? t("agentsConnected", { names: connected.join(", ") }) : t("agentsTitle")}</strong>
          <p role={connected.length > 0 ? "status" : undefined} data-first-run-agents-connected={connected.length > 0 || undefined}>
            {t(connected.length > 0 ? "agentsConnectedBody" : "agentsBody")}
          </p>
        </div>
        {connected.length > 0 ? (
          // Once it has connected the way to connect is done with: what is left is to go on.
          <div className={css.connectedActions}>
            <button ref={onward} className={auth.primary} type="button" onClick={() => onDone(true)} data-first-run-agents-done>
              {t("agentsContinue")}
            </button>
          </div>
        ) : (
          <>
            <AgentConnect session={session} workspaceId={workspaceId} firstRun />
            <div className={css.screenLater}>
              <button type="button" className={css.laterLink} onClick={() => onDone(false)} data-first-run-agents-later>
                {t("later")}
              </button>
              {/* Later is one press from the workspace menu (WorkspaceMenuConnectRows), not three in the settings. */}
              <p className={css.skipCost}>{t("agentsSkipCost", { connectAgent: t("sidebar:connectAgent") })}</p>
            </div>
          </>
        )}
      </div>
    </FirstRunScreen>
  );
}
