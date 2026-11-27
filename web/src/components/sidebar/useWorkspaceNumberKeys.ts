import { useEffect } from "react";
import { workspaceNumberKey } from "@/lib/keyboardShortcuts";
import type { Workspace } from "@/types";
import type { SwitcherProfile } from "./useServerProfiles";

/**
 * The packaged app's workspace key (⌃⇧1…9 on a Mac, Alt+Shift+1…9 elsewhere,
 * keyboardShortcuts.ts workspaceNumberKey) opens the workspace at that place
 * in the workspace menu, as Notion's desktop app does: this account's
 * workspaces in their order, then each other account's.
 */
export function useWorkspaceNumberKeys({ enabled, workspaces, others, onSelectWorkspace, openProfile }: {
  enabled: boolean;
  workspaces: Workspace[];
  others: SwitcherProfile[];
  onSelectWorkspace: (workspace: Workspace) => void;
  openProfile: (profileId: string, workspaceId?: string) => void;
}) {
  useEffect(() => {
    if (!enabled) return undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      const number = workspaceNumberKey(event);
      if (!number) return;
      const places = [
        ...workspaces.map((workspace) => () => onSelectWorkspace(workspace)),
        ...others.flatMap((profile) => profile.workspaces.map((item) => () => openProfile(profile.id, item.id))),
      ];
      const open = places[number - 1];
      if (!open) return;
      event.preventDefault();
      open();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled, others, onSelectWorkspace, openProfile, workspaces]);
}
