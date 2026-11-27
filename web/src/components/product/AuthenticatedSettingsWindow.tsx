import { Suspense } from "react";
import type { AdministratorSettings } from "@/administratorWire.generated";
import { lazyScreen } from "@/lib/lazyScreen";
import { settingsWindowStore, useProductWindow } from "@/lib/productWindows";
import type { BrowserLocalLockSettings } from "@/lib/localLockBrowserSettings";
import type { useSettingsWindow } from "@/components/settings/useSettingsWindow";
import type { Session, Workspace } from "@/types";

// Its code is fetched ahead once the page's start has settled (lazyScreen's
// `drawnOnPress`), so the press that opens it — the sidebar's settings, the
// account's, an app's link to a section (settingsWindowRequest.ts) — draws
// the window on its own frame. Fetched at the press, React showed it no
// sooner than 300 ms after: 390–460 ms on a NAS's first opening with a
// device half as fast, 70–130 ms from the second (2026-11-14).
const SettingsWindow = lazyScreen(() => import("@/components/settings/SettingsWindow").then((module) => ({ default: module.SettingsWindow })), { drawnOnPress: true });

/** The settings window over the product, while its address or a menu opens it: drawn as it opens and closes, the product not (lib/productWindows.ts). */
export function AuthenticatedSettingsWindow({
  administrator,
  localLockSettings,
  onSession,
  onWorkspaceChange,
  onWorkspaceDeleted,
  refresh,
  session,
  settingsWindow,
  workspace,
  workspaces,
}: {
  administrator: { settings: AdministratorSettings | null; setSettings: (settings: AdministratorSettings) => void };
  localLockSettings: BrowserLocalLockSettings;
  onSession: (session: Session | null) => void;
  onWorkspaceChange: (workspace: Workspace) => void;
  onWorkspaceDeleted: (workspaceId: string) => Promise<void>;
  refresh: () => Promise<unknown>;
  session: Session;
  settingsWindow: ReturnType<typeof useSettingsWindow>;
  workspace: Workspace | null;
  workspaces: Workspace[];
}) {
  const state = useProductWindow(settingsWindowStore);
  if (!state) return null;
  return (
    <Suspense fallback={null}>
      <SettingsWindow
        state={state}
        onSelect={settingsWindow.select}
        onClose={settingsWindow.close}
        opener={settingsWindow.opener}
        session={session}
        onSession={onSession}
        localLockSettings={localLockSettings}
        workspace={workspace}
        workspaces={workspaces}
        onImported={() => void refresh()}
        onWorkspaceChange={onWorkspaceChange}
        onWorkspaceDeleted={onWorkspaceDeleted}
        onWorkspaceRefresh={async () => { await refresh(); }}
        administratorSettings={administrator.settings}
        onAdministratorSettingsChange={administrator.setSettings}
      />
    </Suspense>
  );
}
