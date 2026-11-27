import { useEffect, useState } from "react";
import { readSidebarPresets, type SidebarPreset } from "@/lib/sidebarPresets";
import type { Session } from "@/types";
import type { SidebarLayout } from "@/workspaceMembershipWire.generated";
import { SIDEBAR_HOME_TAB, sidebarSectionKey, sidebarShownSections } from "./sidebarLayoutModel";

/**
 * The workspace's optional presets whose sections or shortcuts the reader's
 * list doesn't all hold yet, read while the sidebar is being customized, for "섹션 추가"
 * (sidebar_presets.go).
 */
export function useSidebarPresetChoices(session: Session, workspaceId: string | null, active: boolean, layout: SidebarLayout): SidebarPreset[] {
  const [presets, setPresets] = useState<SidebarPreset[]>([]);
  useEffect(() => {
    if (!active || !workspaceId) return undefined;
    const controller = new AbortController();
    readSidebarPresets(session, workspaceId, controller.signal).then(
      (read) => { if (!controller.signal.aborted) setPresets(read.presets); },
      () => { if (!controller.signal.aborted) setPresets([]); },
    );
    return () => controller.abort();
  }, [active, session, workspaceId]);
  const shown = new Set(sidebarShownSections(layout, SIDEBAR_HOME_TAB));
  const held = new Set((layout.shortcuts ?? []).map((shortcut) => `${shortcut.kind}:${shortcut.pageId ?? ""}`));
  return presets.filter((preset) => preset.optional && (preset.sections.some((section) => !shown.has(sidebarSectionKey(section)))
    || (preset.shortcuts ?? []).some((shortcut) => !held.has(`${shortcut.kind}:${shortcut.pageId ?? ""}`))));
}
