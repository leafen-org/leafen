import type { Workspace } from "@/types";
import { canManageNativeWorkspace } from "@/lib/nativeWorkspaceRolePolicy";

export function canManageWorkspace(workspace: Workspace): boolean {
  return canManageNativeWorkspace(workspace);
}
