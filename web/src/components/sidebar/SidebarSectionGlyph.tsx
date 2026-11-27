import { ClockIcon, DatabaseCylinderIcon, DeveloperIcon, LockIcon, SharePeopleIcon, Star, TeamspaceIcon, type LeafenIconSize } from "@/icons/leafen";
import type { Session } from "@/types";
import type { SidebarLayoutView } from "@/workspaceMembershipWire.generated";
import { SidebarViewSectionIcon } from "./SidebarDatabaseViewSection";
import { knownSidebarSection, sidebarSectionTeamspace } from "./sidebarLayoutModel";

/**
 * A section's mark in a customize list (the sidebar's and a preset's) and in
 * its "섹션 추가", as Notion's rows have one (seen 2026-10-08), at the size
 * the row draws it (16px unless said): a known section's glyph, a
 * teamspace's, and a database view's source — its own icon, else the
 * cylinder (DataSourceMark, Notion's seen 2026-11-15) — once it is known.
 */
export function SidebarSectionGlyph({ id, view, session, size = "small" }: { id: string; view?: SidebarLayoutView; session?: Session | null; size?: LeafenIconSize }) {
  const known = knownSidebarSection(id);
  if (!known && view && session) return <SidebarViewSectionIcon view={view} session={session} size={size === "small" ? "small" : "regular"} />;
  const Icon = known === "favorites" ? Star : known === "teamspaces" || sidebarSectionTeamspace(id) ? TeamspaceIcon : known === "private" ? LockIcon
    : known === "shared" ? SharePeopleIcon : known === "recents" ? ClockIcon : known === "developer" ? DeveloperIcon : DatabaseCylinderIcon;
  return <Icon size={size} aria-hidden="true" />;
}
