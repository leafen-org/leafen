import { rememberTeamspaceNames } from "@/lib/teamspaceNames";
import { i18next } from "@/i18n";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { withArchiveLocks, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { createNativeTeamspaceBrowser } from "@/lib/nativeTeamspaceBrowser";
import { currentKeptTeamspaces, keepTeamspaces, onTeamspacesReadRequest, readKeptTeamspaces, readTeamspacesShared, readWorkspaceTeamspaces } from "@/lib/workspaceTeamspaces";
import { canCreateNativeTeamspace } from "@/lib/nativeWorkspaceRolePolicy";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import type { Session, Workspace } from "@/types";

type SidebarTeamspaceState = {
  ownersOnlyCreate: boolean | null;
  error: string | null;
  identity: string;
  teamspaces: DialogTeamspace[];
};

const EMPTY_STATE: SidebarTeamspaceState = {
  ownersOnlyCreate: null,
  error: null,
  identity: "",
  teamspaces: [],
};

export function useSidebarTeamspaces({
  session,
  workspace,
}: {
  session: Session;
  workspace: Workspace | null;
}) {
  const workspaceId = workspace?.id ?? "";
  const identity = workspace
    ? `${session.user.id}\u0000${workspace.id}`
    : "";
  const [reloadGeneration, setReloadGeneration] = useState(0);
  const [state, setState] = useState<SidebarTeamspaceState>(EMPTY_STATE);
  const refresh = useCallback(() => setReloadGeneration((value) => value + 1), []);
  // Read again when a page's band brought an archived teamspace back.
  useEffect(() => onTeamspacesReadRequest((asked) => { if (asked === workspaceId) refresh(); }), [refresh, workspaceId]);

  // Read when the workspace's teamspaces changed — its bootstrap's teamspace
  // revision moved — or when asked, not with every bootstrap: what this
  // device read at that revision is the list (currentKeptTeamspaces).
  const guest = !workspace || workspace.guest === true;
  const listRevision = workspace?.teamspaceRevision;
  const readGeneration = useRef(-1);
  useEffect(() => {
    // A guest sees the pages shared with them, not the workspace's teamspaces.
    if (guest) return;
    const kept = currentKeptTeamspaces(session.user.id, workspaceId, listRevision);
    if (kept && (reloadGeneration === 0 || readGeneration.current === reloadGeneration)) {
      rememberTeamspaceNames(kept.teamspaces);
      setState((existing) => existing.identity === identity && existing.teamspaces === kept.teamspaces ? existing
        : { ownersOnlyCreate: kept.ownersOnlyCreate, error: null, identity, teamspaces: kept.teamspaces });
      return;
    }
    readGeneration.current = reloadGeneration;
    // Read anew, the list this device last read here stands meanwhile, so
    // the sections keep their places as the answer comes (2026-11-12: on a
    // slow network the teamspace section came nine seconds after the
    // private pages and pushed them down under the pointer).
    const last = reloadGeneration === 0 ? readKeptTeamspaces(session.user.id, workspaceId) : null;
    if (last) {
      rememberTeamspaceNames(last.teamspaces);
      setState((existing) => existing.identity === identity ? existing
        : { ownersOnlyCreate: last.ownersOnlyCreate, error: null, identity, teamspaces: last.teamspaces });
    }
    const context = {
      generation: reloadGeneration + 1,
      session,
      workspaceId,
    };
    const browser = createNativeTeamspaceBrowser({ getContext: () => context });
    let current = true;
    let stopRetry: (() => void) | undefined;
    void (async () => {
      // Asked again, after a change here, it reads for itself.
      const read = (reloadGeneration === 0 && await readTeamspacesShared(`${identity}\u0000${listRevision}`, () => readWorkspaceTeamspaces(browser)))
        || (current ? await readWorkspaceTeamspaces(browser) : null);
      if (!read || !current) return;
      keepTeamspaces(session.user.id, workspaceId, read);
      rememberTeamspaceNames(read.teamspaces);
      setState({ ownersOnlyCreate: read.ownersOnlyCreate, error: null, identity, teamspaces: read.teamspaces });
    })().catch((error: unknown) => {
      if (!current) return;
      if (isServerUnreachable(error)) {
        // Without its server the sidebar shows the teamspaces this device
        // last read, with no failure, and reads them when the server answers.
        const kept = readKeptTeamspaces(session.user.id, workspaceId);
        if (kept) rememberTeamspaceNames(kept.teamspaces);
        setState((existing) => existing.identity === identity ? existing : kept ? {
          ownersOnlyCreate: kept.ownersOnlyCreate, error: null, identity, teamspaces: kept.teamspaces,
        } : { ...EMPTY_STATE, identity });
        stopRetry = retryWhenServerMayAnswer(refresh);
        return;
      }
      const message = i18next.t("sidebar:couldntLoadTeamspaces");
      setState((existing) => existing.identity === identity
        ? { ...existing, error: message }
        : { ...EMPTY_STATE, error: message, identity });
    });
    return () => {
      current = false;
      stopRetry?.();
      browser.dispose();
    };
  }, [guest, identity, listRevision, refresh, reloadGeneration, session, workspaceId]);
  const visible = state.identity === identity ? state : EMPTY_STATE;
  // Each with why it may not be archived, from the whole list read.
  const memberCount = workspace?.memberCount;
  const joinedTeamspaces = useMemo(() => withArchiveLocks(visible.teamspaces, memberCount)
    .filter((teamspace) => teamspace.joined), [memberCount, visible.teamspaces]);
  // Those the reader may see but isn't in, which the library offers to join.
  const browseTeamspaces = useMemo(() => visible.teamspaces.filter(
    (teamspace) => teamspace.visible && !teamspace.joined,
  ), [visible.teamspaces]);

  return {
    // A guest has no teamspaces to wait for.
    loaded: !workspace || workspace.guest || state.identity === identity,
    browseCount: browseTeamspaces.length,
    browseTeamspaces,
    canCreateTeamspace: workspace !== null && visible.ownersOnlyCreate !== null && canCreateNativeTeamspace(workspace, visible.ownersOnlyCreate),
    error: visible.error,
    joinedTeamspaces,
    refresh,
  };
}
