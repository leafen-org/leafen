import { request } from "@/apiTransport";
import { mergeSidebarLayouts } from "@/components/sidebar/sidebarLayoutMerge";
import type { Session } from "@/types";
import type { SidebarLayout, SidebarLayoutSection, SidebarLayoutShortcut } from "@/workspaceMembershipWire.generated";

/**
 * A workspace's sidebar tab presets, as Notion's settings "Sidebar" holds
 * them (seen 2026-10-11 and 2026-11-09): tabs its owners set up, each with an
 * icon, a name, and the sections and the shortcuts a tab holds; a "Default"
 * one is a tab a new member's sidebar starts with, an "Optional" one a
 * member may add. Its shortcuts and sections may name pages and database
 * views, as Notion's do; the editor says when one is not open to every member.
 */
export type SidebarPreset = {
  id: string;
  name: string;
  /** A built-in icon, "name:color"; none shows the preset's own. */
  icon?: string;
  optional: boolean;
  sections: SidebarLayoutSection[];
  shortcuts?: SidebarLayoutShortcut[];
  /** Who made it; the server keeps it. */
  createdBy: string;
};

/**
 * A workspace's presets as read or saved, with the revision they are at — 0
 * while it keeps none. A save names the revision its change was made on and
 * is refused (409) if another owner, settings window or agent saved since.
 */
export type SidebarPresets = { presets: SidebarPreset[]; canEdit: boolean; revision: number };

/** How many presets a workspace keeps, as the server takes. */
export const SIDEBAR_PRESETS_LIMIT = 10;

/** The one tab a preset is, for the sidebar's layout functions to change. */
export const SIDEBAR_PRESET_TAB = "preset";

/** A preset as a layout holding it as its one tab, for the sidebar's customize lists. */
export function sidebarPresetLayout(preset: Pick<SidebarPreset, "name" | "sections" | "shortcuts">): SidebarLayout {
  return {
    sections: [], homeName: "", homeNameHidden: false, shortcuts: [],
    tabs: [{ id: SIDEBAR_PRESET_TAB, name: preset.name, nameHidden: false, sections: preset.sections, shortcuts: preset.shortcuts ?? [] }],
  };
}

/** The preset with what its tab holds in a changed layout. */
export function sidebarPresetFromLayout(preset: SidebarPreset, layout: SidebarLayout): SidebarPreset {
  const tab = layout.tabs.find((candidate) => candidate.id === SIDEBAR_PRESET_TAB);
  return tab ? { ...preset, sections: tab.sections, shortcuts: tab.shortcuts ?? [] } : preset;
}

/** The pages a preset's shortcuts and database views name, by the key of the item that names it. */
export function sidebarPresetPages(preset: Pick<SidebarPreset, "sections" | "shortcuts">): { key: string; pageId: string }[] {
  return [
    ...(preset.shortcuts ?? []).flatMap((shortcut) => (shortcut.pageId && shortcut.kind !== "app" ? [{ key: shortcut.key, pageId: shortcut.pageId }] : [])),
    ...preset.sections.flatMap((section) => (section.view ? [{ key: `view:${section.view.key}`, pageId: section.view.pageId }] : [])),
  ];
}

// A value as the server keeps it: keys in order, and no empty text, list or missing value, which it leaves out.
function kept(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(kept);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined && item !== "" && !(Array.isArray(item) && item.length === 0))
    .sort(([left], [right]) => left.localeCompare(right)).map(([key, item]) => [key, kept(item)]));
}

/** What a preset or a list of them holds, the same for two that save alike. */
export function sidebarPresetsKey(value: SidebarPreset | readonly SidebarPreset[] | undefined): string {
  return JSON.stringify(kept(value)) ?? "";
}

/**
 * A change made here on presets another owner, settings window or agent has
 * since saved, put on what they are now, as the sidebar layout's tabs merge
 * (sidebarLayoutMerge.ts): what this window deleted goes, what it added or
 * moved goes where it put it, a preset it changed is its own, and the rest is
 * theirs, in their order. `base` is the presets as this window last read or
 * saved them, `mine` with its changes, `theirs` the server's now.
 */
export function mergeSidebarPresets(base: readonly SidebarPreset[], mine: readonly SidebarPreset[], theirs: readonly SidebarPreset[]): SidebarPreset[] {
  const asLayout = (presets: readonly SidebarPreset[]) => ({ tabs: presets }) as unknown as SidebarLayout;
  return (mergeSidebarLayouts(asLayout(base), asLayout(mine), asLayout(theirs)).tabs ?? []) as unknown as SidebarPreset[];
}

function presetsPath(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId)}/sidebar-presets`;
}

export function readSidebarPresets(session: Session, workspaceId: string, signal?: AbortSignal) {
  return request<SidebarPresets>(presetsPath(workspaceId), { signal }, session);
}

export function saveSidebarPresets(session: Session, workspaceId: string, presets: readonly SidebarPreset[], revision: number, signal?: AbortSignal) {
  return request<SidebarPresets>(presetsPath(workspaceId), { method: "PUT", body: JSON.stringify({ presets, revision }), signal }, session);
}
