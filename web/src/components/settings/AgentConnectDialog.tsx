import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import dialogs from "@/components/settings/DeveloperSettings.module.css";
import { useConnectedAgents } from "@/lib/useConnectedAgents";
import type { Session } from "@/types";
import { AgentConnect } from "./AgentConnect";
import { SettingsFormDialog } from "./SettingsFormDialog";

/**
 * "AI 에이전트 연결" from the workspace menu (2026-11-12: the text lay three
 * levels down, Settings → 기능 → 에이전트): the same connect as there —
 * "문구 복사" first, the moving guide, the other ways folded — in a window
 * of its own, which says at once which agent connected with the text made
 * here. The settings keep the list of connected agents.
 */
export function AgentConnectDialog({ session, workspaceId, onClose }: {
  session: Session;
  workspaceId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation(["sidebar", "workspaceSettingsDialog", "common"]);
  const connected = useConnectedAgents(session, workspaceId);
  // The agents there were when a text was made: one that connects since is new.
  const [before, setBefore] = useState<Set<string> | null>(null);
  const known = useRef(connected);
  known.current = connected;
  const connectedNow = before && connected ? connected.filter((agent) => !before.has(agent.id)).map((agent) => agent.name) : [];
  // The window opens on its "문구 복사", what is first to do.
  const body = useRef<HTMLDivElement>(null);
  const first = useRef<HTMLElement | null>(null);
  useEffect(() => {
    first.current = body.current?.querySelector<HTMLElement>("[data-agent-copy='prompt']") ?? null;
  }, []);
  return (
    <SettingsFormDialog title={t("sidebar:connectAgent")} busy={false} wide initialFocus={first} onClose={onClose}>
      <div ref={body} className={dialogs.dialogBody} data-agent-connect-window>
        <p className={dialogs.dialogNote}>{t("workspaceSettingsDialog:agents.meta")}</p>
        <AgentConnect session={session} workspaceId={workspaceId} connectedNow={connectedNow}
          onCodeMade={() => setBefore(new Set((known.current ?? []).map((agent) => agent.id)))} />
        <div className={dialogs.dialogActions}>
          <button type="button" className={dialogs.primary} onClick={onClose}>{t("common:actions.done")}</button>
        </div>
      </div>
    </SettingsFormDialog>
  );
}
