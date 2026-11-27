import type { Workspace } from "@/types";

// A guest manages nothing of a workspace, as in Notion.
export function canManageNativeWorkspace(workspace: Pick<Workspace, "role" | "guest">): boolean {
  return workspace.role !== "member" && workspace.guest !== true;
}

// A restricted member makes no teamspace, as Notion's "제한된 멤버"; the
// others may unless the workspace keeps it to its owner.
export function canCreateNativeTeamspace(workspace: Pick<Workspace, "role" | "guest" | "restricted">, ownersOnlyCreate: boolean | undefined): boolean {
  return workspace.restricted !== true && (!ownersOnlyCreate || canManageNativeWorkspace(workspace));
}
