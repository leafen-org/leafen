import { GlobeIcon, LockIcon, SharePeopleIcon, TeamspaceIcon, type LeafenIconSize } from "@/icons/leafen";
import type { TeamspaceSecurity } from "./teamspaceDialogModel";

// Each kind's mark, an open teamspace's the teamspace mark, wherever a kind
// shows — the library's teamspaces, "팀스페이스 만들기", a teamspace's
// "설정" box and its menu — as Notion's (seen 2026-11-05 and 2026-11-11).
const ICONS = { default: GlobeIcon, open: TeamspaceIcon, closed: SharePeopleIcon, private: LockIcon } as const;

/** A kind of teamspace's mark. */
export function TeamspaceKindIcon({ security, size }: { security: TeamspaceSecurity; size: LeafenIconSize }) {
  const Icon = ICONS[security];
  return <Icon size={size} aria-hidden="true" />;
}
