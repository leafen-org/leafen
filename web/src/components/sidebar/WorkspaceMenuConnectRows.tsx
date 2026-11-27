import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { McpTokenIcon } from "@/icons/leafen";
import { DeviceMobileIcon } from "@/icons/leafen/deviceIcons";
import { DeviceSignInDialog } from "@/components/account/DeviceSignInDialog";
import { AgentConnectDialog } from "@/components/settings/AgentConnectDialog";
import { NativeActionMenuItem } from "@/lib/nativeActionMenu";
import type { Session, Workspace } from "@/types";
import { styles } from "./sidebarStyles";

type ConnectWindow = "device" | "agent";

/**
 * The workspace menu's ways in for people and agents (2026-11-12, the
 * user's own findings on the NAS: the code a new device signs in with and
 * the text an agent connects with lay three levels down in the settings):
 * "새 기기 로그인" shows the account's QR and one-time code at once, "AI
 * 에이전트 연결" the one text an agent connects with, each in a window of
 * its own. The sign-in screen's hints name the first. The same rows stand
 * in the desktop menu, under "계정 추가하기", and in a phone's "계정과
 * 워크스페이스" sheet, over its foot; the settings keep theirs. A guest and
 * a restricted member have no agents' settings, and no agents' row
 * (settingsWindowModel).
 */
export function useWorkspaceMenuConnect({ session, workspace, mayConnectAgents }: {
  session: Session;
  workspace: Workspace | null;
  mayConnectAgents: boolean;
}): { rows: (closeMenu: () => void) => ReactNode; windows: ReactNode } {
  const { t } = useTranslation(["sidebar", "workspaceSettingsDialog"]);
  const [open, setOpen] = useState<ConnectWindow | null>(null);
  const rows = (closeMenu: () => void) => (
    <>
      <NativeActionMenuItem type="button" className={styles.workspaceMenuItem} data-workspace-menu-item data-workspace-menu-device
        role="menuitem" onClick={() => { closeMenu(); setOpen("device"); }}>
        <DeviceMobileIcon size="regular" aria-hidden="true" />
        <span>{t("workspaceSettingsDialog:addDevice")}</span>
      </NativeActionMenuItem>
      {workspace && mayConnectAgents ? (
        <NativeActionMenuItem type="button" className={styles.workspaceMenuItem} data-workspace-menu-item data-workspace-menu-agent
          role="menuitem" onClick={() => { closeMenu(); setOpen("agent"); }}>
          <McpTokenIcon size="regular" aria-hidden="true" />
          <span>{t("sidebar:connectAgent")}</span>
        </NativeActionMenuItem>
      ) : null}
    </>
  );
  const windows = open === "device" ? <DeviceSignInDialog session={session} onClose={() => setOpen(null)} />
    : open === "agent" && workspace ? <AgentConnectDialog session={session} workspaceId={workspace.id} onClose={() => setOpen(null)} />
    : null;
  return { rows, windows };
}
