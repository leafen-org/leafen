import { useEffect, useState } from "react";
import { api } from "@/api";
import type { WorkspaceInviteLink } from "@/accessRequestWire.generated";
import { workspaceInviteAddress } from "@/components/settings/WorkspaceInviteLinkControls";
import type { Session } from "@/types";

/**
 * A teamspace's invite link, as Notion's teamspace security "초대 링크" (on
 * for a new teamspace, seen 2026-10-12): read for whoever may invite its
 * members, switched by whoever may change its settings. A member of the
 * workspace who opens it joins the teamspace; it opens at the same address
 * as the workspace's link, with a key of its own.
 */
export function useTeamspaceInviteLink(session: Session, workspaceId: string, teamspaceId: string | null, allowed: boolean) {
  const [link, setLink] = useState<WorkspaceInviteLink | null>(null);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    setLink(null);
    setFailed(false);
    if (!allowed || !teamspaceId) return;
    const controller = new AbortController();
    api.readTeamspaceInviteLink(session, workspaceId, teamspaceId, controller.signal).then(setLink, () => {
      if (!controller.signal.aborted) setFailed(true);
    });
    return () => controller.abort();
  }, [allowed, attempt, session, teamspaceId, workspaceId]);
  return {
    link,
    /** The link couldn't be read; `retry` reads it again. */
    failed,
    retry: () => setAttempt((current) => current + 1),
    address: link?.enabled && link.token ? workspaceInviteAddress(link.token) : null,
    async set(enabled: boolean) {
      if (!teamspaceId) return;
      setLink(await api.setTeamspaceInviteLink(session, workspaceId, teamspaceId, enabled));
    },
  };
}
