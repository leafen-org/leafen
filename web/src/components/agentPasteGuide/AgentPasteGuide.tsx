import { useTranslation } from "react-i18next";
import { GuidePlayer } from "@/components/guide/GuidePlayer";
import { GUIDE_AGENT, agentPasteGuideScreens } from "./agentPasteGuideScreens";

// How long each scene plays: the agent's two take the longer, as there is
// more to read in them.
const SCENE_MS = [3000, 4200, 5200, 3000];

/**
 * The moving guide under "Copy text" (the first-run guides decision of
 * 2026-11-08, 2): copy the text in Leafen, paste it into Claude Code and send
 * it, let it run what adds Leafen, and Leafen says it connected. `prompt` is
 * the text the button copies — or one made the same way while none is — so
 * the lines the agent shows are the real text's; `place` draws Leafen's
 * screens as the place the guide stands in.
 */
export function AgentPasteGuide({ prompt, place, offerStart }: { prompt: string; place: "firstRun" | "settings"; offerStart: boolean }) {
  const { t } = useTranslation(["agentPasteGuide", "firstInstall", "workspaceSettingsDialog", "common"]);
  const scenes = t("agentPasteGuide:scenes", { returnObjects: true }) as { title: string; caption: string }[];
  const screens = agentPasteGuideScreens({
    place,
    prompt,
    leafenWindow: t("agentPasteGuide:mock.leafenWindow"),
    stepTitle: t("firstInstall:agentsTitle"),
    stepBody: t("firstInstall:agentsBody"),
    copy: t("workspaceSettingsDialog:agents.copyText"),
    more: t("workspaceSettingsDialog:agents.more"),
    later: t("firstInstall:later"),
    copied: t("common:copiedToClipboard"),
    connected: t("firstInstall:agentsConnected", { names: GUIDE_AGENT }),
    connectedBody: t("firstInstall:agentsConnectedBody"),
    continueLabel: t("firstInstall:agentsContinue"),
    navAgents: t("workspaceSettingsDialog:navAiConnections"),
    settingsLead: t("workspaceSettingsDialog:agents.meta"),
    pasteHint: t("workspaceSettingsDialog:agents.pasteHint"),
    connectedListTitle: t("workspaceSettingsDialog:agents.connectedTitle"),
    agentStart: t("agentPasteGuide:mock.agentStart"),
    // The agent asks how to start only where the text does (AgentConnect's offerStart).
    agentDone: t(offerStart ? "agentPasteGuide:mock.agentDone" : "agentPasteGuide:mock.agentDoneConnected"),
  });
  return (
    <GuidePlayer
      label={t("agentPasteGuide:region")}
      shows={t("agentPasteGuide:shows")}
      scenes={scenes.map((scene, index) => ({ ...scene, picture: screens[index], durationMs: SCENE_MS[index] }))}
    />
  );
}
