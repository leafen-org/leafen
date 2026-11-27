import type { Page } from "@/types";
import { pageEditTime } from "@/lib/pageEditedHere";
import {
  workspaceMembershipBounds,
  type SidebarLayout,
  type SidebarLayoutSection,
  type SidebarLayoutShortcut,
  type SidebarLayoutView,
  type SidebarOpenMode,
  type SidebarSectionId,
  type SidebarSectionSort,
} from "@/workspaceMembershipWire.generated";

/**
 * The sidebar is one list without tabs (the one-sidebar decision of
 * 2026-10-29): Notion's sections as its 2026-09 redesign has them (seen 2026-09-29 and
 * 2026-10-01): each sorts its pages by hand or by when they were last edited
 * and shows 5 to 100 of them, with "모두 보기" past that; "위로 옮기기" and
 * "아래로 옮기기" change their order, "섹션 제거" takes one off the sidebar
 * and "섹션 추가" puts it back at the end. The recent pages and the developer
 * links start off it. The
 * list may also show a database view's rows as a section ("데이터베이스
 * 보기") and a teamspace's pages as one ("팀스페이스 ›", seen 2026-11-11),
 * each leaving the sidebar when taken off it. The server keeps a
 * person's layout per workspace (sidebar_layout.go), the tabs it had before
 * folded into the list; the functions here still name the one tab, "home".
 */

/** How many pages a section may show, as Notion's "표시" offers. */
export const SIDEBAR_SECTION_LIMITS = [5, 10, 15, 20, 50, 100] as const;

/** The list's one tab, as the layout still names it. */
export const SIDEBAR_HOME_TAB = "home";

/** A section the sidebar knows by name; a database view and one teamspace are ones a person adds. */
export type KnownSidebarSectionId = Exclude<SidebarSectionId, "view" | "teamspace">;

const SIDEBAR_SECTION_IDS: readonly KnownSidebarSectionId[] = ["favorites", "teamspaces", "private", "shared", "recents", "developer"];

/** How many rows a database view section shows at first, as Notion's "표시". */
export const SIDEBAR_VIEW_LIMIT = 5;

/**
 * A section's key on its tab: a known section's id, a database view's
 * "view:<key>", or one teamspace's "teamspace:<id>".
 */
export function sidebarSectionKey(section: SidebarLayoutSection): string {
  if (section.id === "teamspace") return teamspaceSectionKey(section.teamspaceId ?? "");
  return section.id === "view" && section.view ? `view:${section.view.key}` : section.id;
}

/** The key of one teamspace's own section. */
export function teamspaceSectionKey(teamspaceId: string): `teamspace:${string}` {
  return `teamspace:${teamspaceId}`;
}

/** A list whose "모두 보기" opens a tab of the library: a teamspace's, the teamspaces tab. */
export type SidebarShowAllKey = "favorites" | "private" | "shared" | `teamspace:${string}`;

/** The teamspace a section of its own shows, from its key; null for any other section. */
export function sidebarSectionTeamspace(key: string): string | null {
  return key.startsWith("teamspace:") ? key.slice("teamspace:".length) || null : null;
}

/** A known section's id from its key, or null for a database view. */
export function knownSidebarSection(key: string): KnownSidebarSectionId | null {
  return (SIDEBAR_SECTION_IDS as readonly string[]).includes(key) ? key as KnownSidebarSectionId : null;
}

/** The layout a workspace starts with, as the server's. */
export const DEFAULT_SIDEBAR_LAYOUT: SidebarLayout = {
  sections: [
    { id: "favorites", sort: "manual", limit: 5, hidden: false },
    { id: "teamspaces", sort: "manual", limit: 5, hidden: false },
    { id: "private", sort: "edited", limit: 5, hidden: false },
    { id: "shared", sort: "edited", limit: 5, hidden: false },
    { id: "recents", sort: "edited", limit: 5, hidden: true },
    { id: "developer", sort: "manual", limit: 5, hidden: true },
  ],
  homeName: "",
  homeNameHidden: false,
  tabs: [],
};

/** The sections a tab names, in its order; a tab that's gone has none. */
function sidebarTabSections(layout: SidebarLayout, tab: string): readonly SidebarLayoutSection[] {
  if (tab === SIDEBAR_HOME_TAB) return layout.sections;
  return layout.tabs.find((candidate) => candidate.id === tab)?.sections ?? [];
}

// A tab's sections changed; a change that keeps them keeps the layout.
function withTabSections(
  layout: SidebarLayout,
  tab: string,
  change: (sections: readonly SidebarLayoutSection[]) => readonly SidebarLayoutSection[],
): SidebarLayout {
  const current = sidebarTabSections(layout, tab);
  const next = change(current);
  if (next === current) return layout;
  if (tab === SIDEBAR_HOME_TAB) return { ...layout, sections: [...next] };
  return {
    ...layout,
    tabs: layout.tabs.map((candidate) => (candidate.id === tab ? { ...candidate, sections: [...next] } : candidate)),
  };
}

function defaultSection(key: string): SidebarLayoutSection | undefined {
  return DEFAULT_SIDEBAR_LAYOUT.sections.find((section) => section.id === key);
}

export function sidebarSection(layout: SidebarLayout, tab: string, key: string): SidebarLayoutSection {
  const teamspaceId = sidebarSectionTeamspace(key);
  return sidebarTabSections(layout, tab).find((section) => sidebarSectionKey(section) === key)
    ?? defaultSection(key)
    ?? (teamspaceId ? { id: "teamspace", teamspaceId, sort: "manual", limit: SIDEBAR_VIEW_LIMIT, hidden: true }
      : { id: "view", sort: "manual", limit: SIDEBAR_VIEW_LIMIT, hidden: true });
}

/** The sections a tab shows, in its order, by key. */
export function sidebarShownSections(layout: SidebarLayout, tab: string): string[] {
  return sidebarTabSections(layout, tab).filter((section) => !section.hidden).map(sidebarSectionKey);
}

/**
 * The known sections "섹션 추가" can put on a tab: those it doesn't show, and
 * the teamspaces while one of them can still be a section of its own
 * ("팀스페이스 ›").
 */
export function sidebarAddableSections(layout: SidebarLayout, tab: string, teamspaceLeft = false): KnownSidebarSectionId[] {
  const shown = new Set(sidebarShownSections(layout, tab));
  return SIDEBAR_ADD_ORDER.filter((id) => !shown.has(id) || (teamspaceLeft && id === "teamspaces"));
}

// "섹션 추가" lists the sections in Notion's order, whatever the tab's own
// (seen 2026-11-09: 즐겨찾기, 최근, 개인, 공유, 팀스페이스, then 데이터베이스 보기).
const SIDEBAR_ADD_ORDER: readonly KnownSidebarSectionId[] = ["favorites", "recents", "private", "shared", "teamspaces", "developer"];

/**
 * The layout with a teamspace as a section of its own, after the sections
 * the tab shows ("섹션 추가 › 팀스페이스 ›", Notion's, seen 2026-11-11): its
 * pages leave the teamspaces section for it.
 */
export function addSidebarTeamspace(layout: SidebarLayout, tab: string, teamspaceId: string): SidebarLayout {
  const key = teamspaceSectionKey(teamspaceId);
  if (sidebarShownSections(layout, tab).includes(key)) return layout;
  const section: SidebarLayoutSection = { id: "teamspace", teamspaceId, sort: "manual", limit: SIDEBAR_VIEW_LIMIT, hidden: false };
  return withTabSections(layout, tab, (sections) => {
    const others = sections.filter((candidate) => sidebarSectionKey(candidate) !== key);
    const after = others.reduce((last, candidate, index) => (candidate.hidden ? last : index + 1), 0);
    return [...others.slice(0, after), section, ...others.slice(after)];
  });
}

/** The layout with a teamspace's own section showing another teamspace in its place (its "팀스페이스 ›"). */
export function changeSidebarTeamspace(layout: SidebarLayout, tab: string, key: string, teamspaceId: string): SidebarLayout {
  if (sidebarTabSections(layout, tab).some((section) => sidebarSectionKey(section) === teamspaceSectionKey(teamspaceId))) return layout;
  return withTabSections(layout, tab, (sections) => sections.map((section) => (
    sidebarSectionKey(section) === key ? { ...section, teamspaceId } : section
  )));
}

/** Whether a tab can take another database view; the server keeps ten. */
export function canAddSidebarView(layout: SidebarLayout, tab: string): boolean {
  return sidebarTabSections(layout, tab).filter((section) => section.id === "view").length
    < workspaceMembershipBounds.sidebarViewsPerTab;
}

/** The layout with a database view's rows as a section, after the sections the tab shows. */
export function addSidebarView(layout: SidebarLayout, tab: string, view: Omit<SidebarLayoutView, "name" | "showIcons">): SidebarLayout {
  if (!canAddSidebarView(layout, tab)) return layout;
  const section: SidebarLayoutSection = {
    id: "view", sort: "manual", limit: SIDEBAR_VIEW_LIMIT, hidden: false, view: { ...view, name: "", showIcons: true },
  };
  return withTabSections(layout, tab, (sections) => {
    const after = sections.reduce((last, candidate, index) => (candidate.hidden ? last : index + 1), 0);
    return [...sections.slice(0, after), section, ...sections.slice(after)];
  });
}

/**
 * A database view section with a setting changed: its name, icons, where its
 * rows open, the template a new row starts from, or its source and view.
 */
export function changeSidebarView(
  layout: SidebarLayout,
  tab: string,
  key: string,
  change: Partial<Omit<SidebarLayoutView, "key">>,
): SidebarLayout {
  return withTabSections(layout, tab, (sections) => sections.map((section) => (
    sidebarSectionKey(section) === key && section.view ? { ...section, view: { ...section.view, ...change } } : section
  )));
}

/** A section's pages in its order: as placed, or the last edited first, by their edit times as last known (lib/pageEditedHere.ts). */
export function sortSidebarPages(pages: readonly Page[], sort: SidebarSectionSort): Page[] {
  if (sort !== "edited") return [...pages];
  // Each page's time read once, not at each comparison (thousands of pages).
  const rows = pages.map((page) => ({ page, edited: Date.parse(pageEditTime(page).updatedAt ?? "") || 0 }));
  return rows.sort((left, right) => right.edited - left.edited).map((row) => row.page);
}

/** A section's layout with one setting changed. */
export function changeSidebarSection(
  layout: SidebarLayout,
  tab: string,
  key: string,
  change: Partial<Pick<SidebarLayoutSection, "sort" | "limit">>,
): SidebarLayout {
  return withTabSections(layout, tab, (sections) => sections.map((section) => (
    sidebarSectionKey(section) === key ? { ...section, ...change } : section
  )));
}

/** The layout with a section moved one place up or down among the sections shown. */
export function moveSidebarSection(
  layout: SidebarLayout,
  tab: string,
  key: string,
  step: -1 | 1,
  shown: readonly string[],
): SidebarLayout {
  return withTabSections(layout, tab, (current) => {
    const order = shown.filter((section) => current.some((candidate) => sidebarSectionKey(candidate) === section));
    const at = order.indexOf(key);
    const neighbor = order[at + step];
    if (at < 0 || !neighbor) return current;
    const sections = [...current];
    const from = sections.findIndex((section) => sidebarSectionKey(section) === key);
    const to = sections.findIndex((section) => sidebarSectionKey(section) === neighbor);
    [sections[from], sections[to]] = [sections[to], sections[from]];
    return sections;
  });
}

/**
 * The layout with a section taken off a tab, or put on it after the
 * sections it shows: the home tab keeps naming a known section, another tab
 * lets it go, and a database view or a teamspace's own section leaves the
 * sidebar.
 */
export function setSidebarSectionHidden(layout: SidebarLayout, tab: string, key: string, hidden: boolean): SidebarLayout {
  return withTabSections(layout, tab, (sections) => {
    const fallback = defaultSection(key);
    const section = sections.find((candidate) => sidebarSectionKey(candidate) === key) ?? (fallback ? { ...fallback, hidden: true } : undefined);
    if (!section || section.hidden === hidden) return sections;
    const others = sections.filter((candidate) => sidebarSectionKey(candidate) !== key);
    if (hidden) {
      return tab === SIDEBAR_HOME_TAB && section.id !== "view" && section.id !== "teamspace"
        ? sections.map((candidate) => (sidebarSectionKey(candidate) === key ? { ...candidate, hidden } : candidate))
        : others;
    }
    const after = others.reduce((last, candidate, index) => (candidate.hidden ? last : index + 1), 0);
    return [...others.slice(0, after), { ...section, hidden }, ...others.slice(after)];
  });
}

/** The layout with a section dragged before another one, or to the end. */
export function moveSidebarSectionBefore(
  layout: SidebarLayout,
  tab: string,
  key: string,
  before: string | null,
): SidebarLayout {
  return withTabSections(layout, tab, (sections) => {
    const section = sections.find((candidate) => sidebarSectionKey(candidate) === key);
    if (!section || key === before) return sections;
    const others = sections.filter((candidate) => sidebarSectionKey(candidate) !== key);
    const at = before ? others.findIndex((candidate) => sidebarSectionKey(candidate) === before) : others.length;
    if (at < 0) return sections;
    return [...others.slice(0, at), section, ...others.slice(at)];
  });
}

/** An id for a new shortcut. */
export function newSidebarTabId(): string {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/**
 * The list with a workspace's optional preset added, as a new member's starts
 * with its default ones (sidebar_presets.go): its sections after the list's,
 * one it had taken off back on, and its shortcuts after the list's.
 */
export function addSidebarPreset(
  layout: SidebarLayout,
  preset: { sections: readonly SidebarLayoutSection[]; shortcuts?: readonly SidebarLayoutShortcut[] },
): SidebarLayout {
  let next = layout;
  for (const section of preset.sections) {
    const key = sidebarSectionKey(section);
    const held = layout.sections.some((candidate) => sidebarSectionKey(candidate) === key);
    if (held) next = setSidebarSectionHidden(next, SIDEBAR_HOME_TAB, key, false);
    else if (section.id !== "view" || canAddSidebarView(next, SIDEBAR_HOME_TAB)) next = { ...next, sections: [...next.sections, { ...section, hidden: false }] };
  }
  for (const shortcut of preset.shortcuts ?? []) {
    if (canAddSidebarShortcut(next, SIDEBAR_HOME_TAB)) next = { ...next, shortcuts: [...(next.shortcuts ?? []), { ...shortcut, key: newSidebarTabId() }] };
  }
  return next;
}

/** A tab's shortcuts, above its sections ("단축키 추가", Notion seen 2026-10-01). */
export function sidebarTabShortcuts(layout: SidebarLayout, tab: string): readonly SidebarLayoutShortcut[] {
  if (tab === SIDEBAR_HOME_TAB) return layout.shortcuts ?? [];
  return layout.tabs.find((candidate) => candidate.id === tab)?.shortcuts ?? [];
}

function withTabShortcuts(
  layout: SidebarLayout,
  tab: string,
  change: (shortcuts: readonly SidebarLayoutShortcut[]) => SidebarLayoutShortcut[],
): SidebarLayout {
  if (tab === SIDEBAR_HOME_TAB) return { ...layout, shortcuts: change(layout.shortcuts ?? []) };
  return {
    ...layout,
    tabs: layout.tabs.map((candidate) => (candidate.id === tab ? { ...candidate, shortcuts: change(candidate.shortcuts ?? []) } : candidate)),
  };
}

/** Whether a tab can take another shortcut; the server keeps ten. */
export function canAddSidebarShortcut(layout: SidebarLayout, tab: string): boolean {
  return sidebarTabShortcuts(layout, tab).length < workspaceMembershipBounds.sidebarShortcutsPerTab;
}

/** The layout with a shortcut after the tab's others. */
export function addSidebarShortcut(layout: SidebarLayout, tab: string, shortcut: Omit<SidebarLayoutShortcut, "name"> & { name?: string }): SidebarLayout {
  if (!canAddSidebarShortcut(layout, tab)) return layout;
  return withTabShortcuts(layout, tab, (shortcuts) => [...shortcuts, { ...shortcut, name: shortcut.name ?? "" }]);
}

/** The layout with a shortcut moved one place up or down among the tab's, as its "위로 옮기기" and "아래로 옮기기" do. */
export function moveSidebarShortcut(layout: SidebarLayout, tab: string, key: string, step: -1 | 1): SidebarLayout {
  return withTabShortcuts(layout, tab, (shortcuts) => {
    const at = shortcuts.findIndex((shortcut) => shortcut.key === key), next = [...shortcuts];
    if (at < 0 || !next[at + step]) return next;
    [next[at], next[at + step]] = [next[at + step], next[at]];
    return next;
  });
}

/** The layout with a shortcut dragged before another one, or to the end. */
export function moveSidebarShortcutBefore(layout: SidebarLayout, tab: string, key: string, before: string | null): SidebarLayout {
  return withTabShortcuts(layout, tab, (shortcuts) => {
    const shortcut = shortcuts.find((candidate) => candidate.key === key);
    if (!shortcut || key === before) return [...shortcuts];
    const others = shortcuts.filter((candidate) => candidate.key !== key);
    const at = before ? others.findIndex((candidate) => candidate.key === before) : others.length;
    return at < 0 ? [...shortcuts] : [...others.slice(0, at), shortcut, ...others.slice(at)];
  });
}

/** The layout without a shortcut, as "사이드바에서 제거" leaves it. */
export function removeSidebarShortcut(layout: SidebarLayout, tab: string, key: string): SidebarLayout {
  return withTabShortcuts(layout, tab, (shortcuts) => shortcuts.filter((shortcut) => shortcut.key !== key));
}

/** A shortcut's or a view section's name cut to what the server keeps. */
export function sidebarItemName(name: string): string {
  return Array.from(name.trim()).slice(0, workspaceMembershipBounds.sidebarViewNameRunes).join("");
}

/**
 * Where a shortcut's page opens, a full page until another is chosen, and
 * where a view section's rows open, beside the open page until then (Notion,
 * seen 2026-10-02).
 */
export function sidebarShortcutOpenMode(shortcut: SidebarLayoutShortcut): SidebarOpenMode {
  return shortcut.openIn || "full";
}

export function sidebarViewOpenMode(view: SidebarLayoutView): SidebarOpenMode {
  return view.openIn || "side";
}

/** The layout with a shortcut's name, where it opens, or a database shortcut's view or template changed. */
export function changeSidebarShortcut(
  layout: SidebarLayout,
  tab: string,
  key: string,
  change: Partial<Pick<SidebarLayoutShortcut, "name" | "openIn" | "viewId" | "templateId">>,
): SidebarLayout {
  return withTabShortcuts(layout, tab, (shortcuts) => shortcuts.map((shortcut) => (
    shortcut.key === key ? { ...shortcut, ...change } : shortcut
  )));
}

/** A favorite's key in the reader's order: a page's id, or "database:" and an inline database's block. */
export function favoriteOrderKey(kind: "page" | "database" | "form", id: string) {
  return kind === "page" ? id : `${kind}:${id}`;
}

/**
 * Favorites in the reader's order (Notion's, seen 2026-11-06): those it
 * names, as it names them, then any it doesn't — kept before it had one —
 * as they come.
 */
export function orderFavorites<T>(items: readonly T[], keyOf: (item: T) => string, order: readonly string[] = []): T[] {
  const place = new Map(order.map((key, index) => [key, index]));
  return items.map((item, index) => ({ item, index, at: place.get(keyOf(item)) }))
    .sort((left, right) => (left.at ?? order.length + left.index) - (right.at ?? order.length + right.index))
    .map(({ item }) => item);
}

/** The reader's order once a favorite moved before or after another in the shown list. */
export function movedFavoriteOrder(shown: readonly string[], moved: string, target: string, edge: "before" | "after"): string[] {
  const keys = shown.filter((key) => key !== moved);
  const at = keys.indexOf(target);
  if (at < 0) return [...shown];
  keys.splice(edge === "before" ? at : at + 1, 0, moved);
  return keys;
}
