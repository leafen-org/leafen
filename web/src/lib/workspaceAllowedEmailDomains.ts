import { useEffect, useState } from "react";
import { request } from "@/apiTransport";
import type { Session } from "@/types";
import { readEveryWindow } from "@/lib/listWindows";

/**
 * Notion's "허용된 이메일 도메인" (설정 → 일반, seen 2026-10-03): the email
 * domains whose people join the workspace as members without an invitation,
 * from "워크스페이스 생성 또는 참여". Its owners add only domains its members
 * use. Joins wait for a server whose new accounts confirm their email.
 */
export type WorkspaceAllowedEmailDomains = {
  domains: Array<{ domain: string; createdAt: number }>;
  joinsAvailable: boolean;
};

/** A workspace open to the person's email domain that isn't theirs yet. */
export type JoinableWorkspace = { id: string; name: string; icon: string; members: number };

function domainsPath(workspaceId: string, domain?: string) {
  const path = `/workspaces/${encodeURIComponent(workspaceId)}/allowed-email-domains`;
  return domain === undefined ? path : `${path}/${encodeURIComponent(domain)}`;
}

export function readAllowedEmailDomains(session: Session, workspaceId: string, signal?: AbortSignal) {
  return request<WorkspaceAllowedEmailDomains>(domainsPath(workspaceId), { signal }, session);
}

export function addAllowedEmailDomain(session: Session, workspaceId: string, domain: string) {
  return request<WorkspaceAllowedEmailDomains>(domainsPath(workspaceId, domain), { method: "PUT" }, session);
}

export function removeAllowedEmailDomain(session: Session, workspaceId: string, domain: string) {
  return request<WorkspaceAllowedEmailDomains>(domainsPath(workspaceId, domain), { method: "DELETE" }, session);
}

/** Every workspace the person may join by their email's domain, a window at a time. */
export async function listJoinableWorkspaces(session: Session, signal?: AbortSignal) {
  return readEveryWindow(async (cursor) => {
    const response = await request<{ workspaces: JoinableWorkspace[]; nextCursor?: string }>(
      cursor ? `/account/joinable-workspaces?cursor=${encodeURIComponent(cursor)}` : "/account/joinable-workspaces", { signal }, session);
    return { items: response.workspaces, nextCursor: response.nextCursor };
  }, signal);
}

/** What someone with no workspace is offered: whether they may make one (the server's policy), and any to join. */
export type NoWorkspaceOffer = { creationRestricted: boolean; joinable: boolean };

/** Read while the person has no workspace; null until then and until it is known. */
export function useNoWorkspaceOffer(session: Session, noWorkspace: boolean): NoWorkspaceOffer | null {
  const [offer, setOffer] = useState<NoWorkspaceOffer | null>(null);
  useEffect(() => {
    setOffer(null);
    if (!noWorkspace) return undefined;
    const controller = new AbortController();
    void request<{ workspaces: JoinableWorkspace[]; creationRestricted?: boolean }>(
      "/account/joinable-workspaces", { signal: controller.signal }, session,
    ).then(
      (read) => ({ creationRestricted: read.creationRestricted === true, joinable: read.workspaces.length > 0 }),
      // Unread, its window opens and creating says if it is refused.
      () => ({ creationRestricted: false, joinable: false }),
    ).then((next) => { if (!controller.signal.aborted) setOffer(next); });
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `session` changes with each renewed token
  }, [noWorkspace, session.user.id]);
  return offer;
}

export function joinWorkspaceByEmailDomain(session: Session, workspaceId: string) {
  return request<{ workspaceId: string; role: string }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/join`, { method: "POST" }, session,
  );
}
