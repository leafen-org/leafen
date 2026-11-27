import { api } from "@/api";
import type { Session } from "@/types";
import type { DeletingWorkspace } from "@/workspaceMembershipWire.generated";
import { createWorkspaceDeletionDriver } from "./workspaceDeletionDriver";

/**
 * The one driver of this tab's workspace deletions: the settings' deletion
 * and the one carried on here when the list names it as deleting share a
 * flight, so the two never push the same deletion at once (2026-10-16).
 */
export const workspaceDeletionDriver = createWorkspaceDeletionDriver({ deleteWorkspace: api.deleteWorkspace });
const running = new Set<string>();

/**
 * Carries on with the person's workspace deletions that didn't finish — the
 * tab closed or a request failed — when the app opens, each with its own
 * request so the server takes it as the same deletion. One that still can't
 * go on waits for the next opening; the workspace is out of the person's
 * list meanwhile.
 */
export function resumeWorkspaceDeletions(session: Session, deleting: readonly DeletingWorkspace[]) {
  for (const item of deleting) {
    const key = `${session.user.id}\u0000${item.workspaceId}`;
    if (running.has(key)) continue;
    running.add(key);
    void workspaceDeletionDriver.run({
      session,
      sessionIdentity: session.user.id,
      workspaceId: item.workspaceId,
      confirmationName: item.name,
      requestId: item.requestId,
      isCurrent: () => true,
      onProgress: () => undefined,
    }).catch(() => undefined).finally(() => running.delete(key));
  }
}
