import { useEffect, useMemo, useRef, useState } from "react";
import type { MakeOnce } from "@/lib/uncertainWrites";
import { useTranslation } from "react-i18next";
import { useNoticeFor } from "@/lib/useNoticeFor";
import { LeafenApiError } from "@/apiTransport";
import { userFailureText } from "@/lib/settingsErrors";
import { createNativeTeamspaceBrowser } from "@/lib/nativeTeamspaceBrowser";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";
import { canCreateNativeTeamspace, canManageNativeWorkspace } from "@/lib/nativeWorkspaceRolePolicy";
import type { Session, Workspace } from "@/types";
import type {
  TeamspaceAccess,
  TeamspaceJoinRequest,
  TeamspaceManagementResponse,
  TeamspaceMember,
  TeamspaceMutationInput,
  TeamspaceMutationResponse,
  TeamspacePageRole,
  TeamspacePrincipalOption,
  TeamspaceSettings,
  TeamspaceSnapshotResponse,
} from "@/teamspaceWire.generated";
import { teamspaceErrorCodes } from "@/teamspaceWire.generated";
import { teamspaceDialogCommands } from "./teamspaceDialogCommands";
import {
  type DialogTeamspace,
  type DialogView,
  isDefaultTeamspace,
  mergeById,
  optionLabel,
  ownedTeamspace,
  principalKey,
  projectTeamspaceSnapshot,
} from "./teamspaceDialogModel";

type UseTeamspaceDialogOptions = {
  session: Session;
  workspace: Workspace;
  initialTeamspaceId?: string;
  initialView?: DialogView;
  /** 설정 › 팀스페이스 and the window its rows open: every active teamspace for the workspace's owner. */
  everyTeamspace?: boolean;
};

// The server's own words are for logs; the reader sees what failed in theirs,
// a refusal's reason, and the reference only of a failure no one foresaw.
function messageFor(cause: unknown, fallback: string) {
  return userFailureText(cause, fallback);
}

export function useTeamspaceDialog({
  session,
  workspace,
  initialTeamspaceId,
  initialView = "browse", everyTeamspace = false,
}: UseTeamspaceDialogOptions) {
  const { t } = useTranslation(["teamspaceDialog", "common"]);
  const identity = `${session.user.id}\u0000${workspace.id}\u0000${everyTeamspace}`;
  const contextRef = useRef({ generation: 1, identity, session, workspaceId: workspace.id, everyTeamspace });
  if (contextRef.current.identity !== identity) {
    contextRef.current = {
      generation: contextRef.current.generation + 1,
      identity,
      session,
      workspaceId: workspace.id,
      everyTeamspace,
    };
  } else {
    contextRef.current.session = session;
  }
  const [browser] = useState(() => createNativeTeamspaceBrowser({
    getContext: () => contextRef.current,
  }));
  const browserLifecycleGeneration = useRef(0);
  const actionGeneration = useRef(0);
  const actionPending = useRef(false);
  // The dialog's writes go out one after another in the order pressed, each
  // on the revision its predecessor's answer left (rebased below).
  const writes = useRef<{ tail: Promise<unknown>; waiting: number }>({ tail: Promise.resolve(), waiting: 0 });
  const written = useRef({ teamspaces: new Map<string, Partial<DialogTeamspace> & { revision: number }>(), settings: 0 });
  const [view, setView] = useState<DialogView>(initialView);
  const [teamspaces, setTeamspaces] = useState<DialogTeamspace[]>([]);
  const [settings, setSettings] = useState<TeamspaceSettings | null>(null);
  const [snapshotCursor, setSnapshotCursor] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState(initialTeamspaceId ?? "");
  const [members, setMembers] = useState<TeamspaceMember[]>([]);
  const [requests, setRequests] = useState<TeamspaceJoinRequest[]>([]);
  const [principalOptions, setPrincipalOptions] = useState<TeamspacePrincipalOption[]>([]);
  const [memberCursor, setMemberCursor] = useState<string | null>(null);
  const [requestCursor, setRequestCursor] = useState<string | null>(null);
  const [principalCursor, setPrincipalCursor] = useState<string | null>(null);
  const [archived, setArchived] = useState<DialogTeamspace[]>([]);
  const [archivedCursor, setArchivedCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true), [actionBusy, setActionBusy] = useState(false);
  const [managedId, setManagedId] = useState(""), [manageTurn, setManageTurn] = useState(0); // whose members were read; a turn reads them again
  const [error, setError] = useState<string | null>(null), [readFailed, setReadFailed] = useState<string | null>(null); // a list read that failed is both
  // What a result line is about — the teamspace its action changed, or ""
  // for the workspace's teamspace rules; none for a line about nothing that
  // changes.
  const [noticeTarget, setNoticeTarget] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("");
  const [description, setDescription] = useState("");
  const [access, setAccess] = useState<TeamspaceAccess>("open");
  // A new teamspace's members start with full access, as in Notion.
  const [memberPageRole, setMemberPageRole] = useState<TeamspacePageRole>("full_access");
  const [openPageRole, setOpenPageRole] = useState<TeamspacePageRole>("full_access");
  const [membersCanInvite, setMembersCanInvite] = useState(true);
  const [membersCanEditSidebar, setMembersCanEditSidebar] = useState(true);

  const selected = useMemo(() => teamspaces.find((teamspace) => teamspace.id === selectedId), [selectedId, teamspaces]);
  const workspaceAdmin = canManageNativeWorkspace(workspace);
  const canManageSelected = selected?.membershipRole === "owner" || workspaceAdmin && !!selected && !selected.summary.answeringOwnerCount; // a workspace owner only where no owner answers (managesTeamspace)
  // Members may invite and remove members where the teamspace lets them.
  const canInviteSelected = !selected?.membersLocked && (canManageSelected || selected?.membershipRole === "member" && selected.membersCanInvite); // a restricted member adds no one
  const canCreate = canCreateNativeTeamspace(workspace, settings?.ownersOnlyCreate);
  const busy = loading || actionBusy;

  // The result line's target as it stands now: the line goes once that is
  // otherwise (a request approved or declined, a teamspace left, restored,
  // made default or archived elsewhere), as every result line keeps to its
  // own state (lib/useNoticeFor).
  const noticeItem = noticeTarget ? teamspaces.find((teamspace) => teamspace.id === noticeTarget) : undefined;
  const noticeAbout = noticeTarget === null ? ""
    : noticeTarget ? JSON.stringify(noticeItem
      ? [noticeItem.archivedAt, noticeItem.isDefault, noticeItem.access, noticeItem.joined, noticeItem.requestPending, noticeItem.role] : null)
      : JSON.stringify([settings?.ownersOnlyCreate, settings?.defaultTeamspaceIds]);
  const [notice, setNotice] = useNoticeFor<string>(noticeAbout);

  function publishSnapshot(result: TeamspaceSnapshotResponse, append = false) {
    const projected = projectTeamspaceSnapshot(result);
    setTeamspaces((current) => append ? mergeById(current, projected) : projected);
    setSettings(result.settings);
    setSnapshotCursor(result.hasMore ? result.nextCursor ?? null : null);
    setSelectedId((current) => current || initialTeamspaceId || projected.find((item) => item.joined)?.id || "");
  }

  function publishManagement(result: TeamspaceManagementResponse) {
    setTeamspaces((current) => current.map((item) => item.id === result.teamspace.id ? {
      ...item,
      ...result.teamspace,
      ...result.decision,
      role: result.decision.membershipRole,
    } : item));
    setMembers(result.members);
    setRequests(result.requests);
    setPrincipalOptions(result.principalOptions);
    setMemberCursor(result.membersHaveMore ? result.nextMemberCursor ?? null : null);
    setRequestCursor(result.requestsHaveMore ? result.nextRequestCursor ?? null : null);
    setPrincipalCursor(result.principalOptionsHaveMore ? result.nextPrincipalCursor ?? null : null);
  }

  useEffect(() => {
    const generation = ++browserLifecycleGeneration.current;
    return () => {
      // Development StrictMode replays effects without unmounting state. Defer
      // disposal so the replay can retain this browser while real unmounts
      // still retire every request lane.
      queueMicrotask(() => {
        if (browserLifecycleGeneration.current === generation) browser.dispose();
      });
    };
  }, [browser]);

  useEffect(() => {
    let current = true;
    actionGeneration.current += 1;
    actionPending.current = false;
    writes.current = { tail: Promise.resolve(), waiting: 0 };
    written.current = { teamspaces: new Map(), settings: 0 };
    setActionBusy(false);
    setLoading(true);
    setError(null); setReadFailed(null);
    setNotice(null);
    void browser.readSnapshot().then((result) => {
      if (current && result) publishSnapshot(result);
    }).catch((cause) => {
      if (current) { const said = messageFor(cause, t("teamspaceDialog:error.load")); setError(said); setReadFailed(said); }
    }).finally(() => {
      if (current) setLoading(false);
    });
    return () => {
      current = false;
      browser.cancelSnapshot();
    };
  }, [browser, identity, t]);

  // Who belongs changes while the window is open (the workspace's change
  // check says so): the teamspaces and their members are read again, what the
  // window is doing left as it is.
  const accessRevision = useAccessRevision();
  const seenAccessRevision = useRef(accessRevision);
  useEffect(() => {
    if (seenAccessRevision.current === accessRevision) return;
    seenAccessRevision.current = accessRevision;
    let current = true;
    void browser.readSnapshot().then((result) => {
      if (!current || !result) return;
      publishSnapshot(result);
    }).catch(() => undefined);
    return () => { current = false; };
  }, [accessRevision, browser]);

  useEffect(() => {
    if (teamspaces.length === 0 || teamspaces.some((teamspace) => teamspace.id === selectedId)) return;
    setSelectedId(teamspaces.find((teamspace) => teamspace.joined)?.id ?? "");
  }, [selectedId, teamspaces]);

  useEffect(() => {
    if (!selected || view === "create") return;
    setName(selected.name);
    setIcon(selected.icon);
    setDescription(selected.description || "");
    setAccess(selected.access);
    setMemberPageRole(selected.memberPageRole);
    setOpenPageRole(selected.openPageRole);
    setMembersCanInvite(selected.membersCanInvite);
    setMembersCanEditSidebar(selected.membersCanEditSidebar);
  }, [selected, teamspaces, view]);

  useEffect(() => {
    if (view !== "manage" || !selected || !canManageSelected && !selected.joined && !selected.visible && !workspaceAdmin) return; // any one's members for the workspace's owner (readTeamspaceManagementState)
    let current = true;
    setLoading(true); setReadFailed(null);
    void browser.readManagement(selected.id).then((result) => {
      if (current && result) { publishManagement(result); setManagedId(selected.id); }
    }).catch((cause) => {
      if (current) { const said = messageFor(cause, t("teamspaceDialog:error.load")); setError(said); setReadFailed(said); }
    }).finally(() => {
      if (current) setLoading(false);
    });
    return () => {
      current = false;
      browser.cancelManagement();
    };
  }, [browser, canManageSelected, identity, manageTurn, selected?.id, selected?.joined, selected?.visible, t, view, workspaceAdmin]);

  useEffect(() => {
    if (view !== "archived" || !workspaceAdmin) return;
    let current = true;
    setLoading(true); setReadFailed(null);
    void browser.readArchived().then((result) => {
      if (current && result) {
        setArchived(projectTeamspaceSnapshot(result));
        setArchivedCursor(result.hasMore ? result.nextCursor ?? null : null);
      }
    }).catch((cause) => {
      if (current) { const said = messageFor(cause, t("teamspaceDialog:error.load")); setError(said); setReadFailed(said); }
    }).finally(() => {
      if (current) setLoading(false);
    });
    return () => {
      current = false;
      browser.cancelArchived();
    };
  }, [browser, identity, t, view, workspaceAdmin]);

  function applyMutation(input: Omit<TeamspaceMutationInput, "requestId">, result: TeamspaceMutationResponse) {
    if (result.settings) setSettings(result.settings);
    if (input.teamspaceId) {
      setTeamspaces((current) => current.map((item) => item.id === input.teamspaceId ? {
        ...item,
        ...(result.teamspace ?? {}),
        revision: result.revision,
        isDefault: result.settings ? isDefaultTeamspace(result.settings, item.id) : item.isDefault,
      } : result.settings ? { ...item, isDefault: isDefaultTeamspace(result.settings, item.id) } : item));
    } else if (result.settings) {
      // The workspace's settings, as its default teamspaces chosen at once.
      const settings = result.settings;
      setTeamspaces((current) => current.map((item) => ({ ...item, isDefault: isDefaultTeamspace(settings, item.id) })));
    }
    if (result.member) setMembers((current) => mergeById(current, [result.member!]));
    if (result.joinRequest) setRequests((current) => mergeById(current, [result.joinRequest!]));
    if (input.action === "create" && result.teamspace) {
      const ownerLabel = session.user.name || session.user.email || session.user.id;
      const created = ownedTeamspace(result.teamspace, isDefaultTeamspace(result.settings, result.teamspace.id), {
        teamspaceId: result.teamspace.id, ownerLabels: [ownerLabel], ownerUserIds: [session.user.id], ownerCount: 1, memberCount: 1,
      });
      setTeamspaces((current) => mergeById(current, [created]));
      setSelectedId(created.id);
      setView("manage");
    } else if (input.action === "join" && input.teamspaceId) {
      setTeamspaces((current) => current.map((item) => item.id === input.teamspaceId ? {
        ...item, joined: true, canJoin: false, canRequest: false, requestPending: false,
        membershipRole: "member", membershipSource: "explicit", role: "member",
      } : item));
    } else if (input.action === "request" && input.teamspaceId) {
      setTeamspaces((current) => current.map((item) => item.id === input.teamspaceId ? {
        ...item, canRequest: false, requestPending: true,
      } : item));
    } else if (input.action === "leave" && input.teamspaceId) {
      setTeamspaces((current) => current.map((item) => item.id === input.teamspaceId ? {
        ...item, joined: false, visible: item.access !== "private", canJoin: item.access === "open",
        canRequest: item.access === "closed", membershipRole: undefined, membershipSource: undefined, role: undefined,
        pageRole: item.access !== "private" && item.openPageRole !== "none" ? item.openPageRole : undefined,
      } : item));
    } else if (input.action === "remove_member") {
      setMembers((current) => current.filter((member) => (
        member.principalType !== input.principalType || member.principalId !== input.principalId
      )));
    } else if (input.action === "respond" && input.joinRequestId) {
      setRequests((current) => current.filter((request) => request.id !== input.joinRequestId));
    } else if (input.action === "archive" && input.teamspaceId) {
      const existing = teamspaces.find((item) => item.id === input.teamspaceId);
      const value = existing && result.teamspace ? { ...existing, ...result.teamspace } : existing;
      setTeamspaces((current) => current.filter((item) => item.id !== input.teamspaceId));
      if (value) setArchived((current) => mergeById(current, [value]));
      setSelectedId("");
      setView("browse");
    } else if (input.action === "restore" && result.teamspace) {
      const archivedValue = archived.find((item) => item.id === result.teamspace!.id);
      // Back as the default one when none was in use (restoredDefaultTeamspaces).
      const restored = ownedTeamspace(result.teamspace, isDefaultTeamspace(result.settings, result.teamspace.id), archivedValue?.summary ?? {
        teamspaceId: result.teamspace.id, ownerLabels: [], ownerUserIds: [], ownerCount: 0, memberCount: 0,
      });
      setArchived((current) => current.filter((item) => item.id !== result.teamspace!.id));
      setTeamspaces((current) => mergeById(current, [restored]));
    }
  }

  type WriteInput = Omit<TeamspaceMutationInput, "requestId">;
  // A write pressed while earlier ones are out goes on the revision their
  // answers left — a teamspace's for its own writes, the settings' for the
  // workspace's — not the older one it was pressed on.
  function rebased(input: WriteInput): WriteInput {
    if (!input.expectedRevision) return input;
    const known = input.teamspaceId ? written.current.teamspaces.get(input.teamspaceId)?.revision : written.current.settings;
    return known && known > input.expectedRevision ? { ...input, expectedRevision: known } : input;
  }

  function remember(input: WriteInput, result: TeamspaceMutationResponse) {
    if (result.settings) written.current.settings = result.settings.revision;
    if (input.teamspaceId) {
      const before = written.current.teamspaces.get(input.teamspaceId);
      written.current.teamspaces.set(input.teamspaceId, { ...before, ...(result.teamspace ?? {}), revision: result.revision });
    }
  }

  /**
   * Sends a write after those pressed before it — none is dropped while one is
   * out (2026-11-13) — and shows its answer; `input` may be read as it goes
   * out, from the teamspace as the earlier answers left it (`latest`).
   */
  function execute(input: WriteInput | ((latest: (teamspace: DialogTeamspace) => DialogTeamspace) => WriteInput),
    success: string, make?: MakeOnce): Promise<TeamspaceMutationResponse | false> {
    const generation = actionGeneration.current;
    const lane = writes.current;
    lane.waiting += 1;
    actionPending.current = true;
    setActionBusy(true);
    setError(null);
    setNotice(null);
    const latest = (teamspace: DialogTeamspace) => ({ ...teamspace, ...written.current.teamspaces.get(teamspace.id) });
    const sent = lane.tail.then(() => send(rebased(typeof input === "function" ? input(latest) : input), success, make, generation));
    lane.tail = sent.then(() => undefined, () => undefined);
    void lane.tail.then(() => {
      lane.waiting -= 1;
      if (lane.waiting === 0 && writes.current === lane) {
        actionPending.current = false;
        setActionBusy(false);
      }
    });
    return sent;
  }

  async function send(input: WriteInput, success: string, make: MakeOnce | undefined, generation: number): Promise<TeamspaceMutationResponse | false> {
    if (generation !== actionGeneration.current) return false;
    try {
      const result = await (make ? make(() => browser.mutate(input)) : browser.mutate(input));
      if (!result || generation !== actionGeneration.current) return false;
      remember(input, result);
      applyMutation(input, result);
      setNoticeTarget(input.teamspaceId ?? "");
      setNotice(success);
      return result;
    } catch (cause) {
      if (generation === actionGeneration.current) {
        // The teamspace's rules are said in the reader's words.
        const code = cause instanceof LeafenApiError ? cause.code : "";
        setError(code === teamspaceErrorCodes.privateDefault ? t("teamspaceDialog:error.privateDefault")
          : code === teamspaceErrorCodes.defaultMember ? t("teamspaceDialog:error.defaultMember")
            : code === teamspaceErrorCodes.lastOwner ? t("teamspaceDialog:error.lastOwner")
              : code === teamspaceErrorCodes.defaultRequired ? t("teamspaceDialog:error.defaultRequired")
                : code === teamspaceErrorCodes.conflict ? t("teamspaceDialog:error.conflict")
                  : messageFor(cause, t("teamspaceDialog:error.action")));
      }
      return false;
    }
  }

  function openView(nextView: DialogView) {
    setView(nextView);
    setError(null); setReadFailed(null);
    setNotice(null);
    if (nextView !== "create") return;
    setName(""); setIcon(""); setDescription(""); setAccess("open");
    setMemberPageRole("full_access"); setOpenPageRole("full_access");
    setMembersCanInvite(true); setMembersCanEditSidebar(true);
  }

  async function loadMore(kind: "browse" | "archived" | "members" | "requests" | "principals") {
    if (actionPending.current) return;
    const generation = ++actionGeneration.current;
    actionPending.current = true;
    setActionBusy(true);
    setError(null); setReadFailed(null);
    try {
      if (kind === "browse" && snapshotCursor) {
        const result = await browser.readSnapshot(snapshotCursor);
        if (result && generation === actionGeneration.current) publishSnapshot(result, true);
      } else if (kind === "archived" && archivedCursor) {
        const result = await browser.readArchived(archivedCursor);
        if (result && generation === actionGeneration.current) {
          setArchived((current) => mergeById(current, projectTeamspaceSnapshot(result)));
          setArchivedCursor(result.hasMore ? result.nextCursor ?? null : null);
        }
      } else if (selected && kind === "members" && memberCursor) {
        const result = await browser.readManagement(selected.id, { memberCursor });
        if (result && generation === actionGeneration.current) {
          setMembers((current) => mergeById(current, result.members));
          setMemberCursor(result.membersHaveMore ? result.nextMemberCursor ?? null : null);
        }
      } else if (selected && kind === "requests" && requestCursor) {
        const result = await browser.readManagement(selected.id, { requestCursor });
        if (result && generation === actionGeneration.current) {
          setRequests((current) => mergeById(current, result.requests));
          setRequestCursor(result.requestsHaveMore ? result.nextRequestCursor ?? null : null);
        }
      } else if (selected && kind === "principals" && principalCursor) {
        const result = await browser.readManagement(selected.id, { principalCursor });
        if (result && generation === actionGeneration.current) {
          setPrincipalOptions((current) => mergeById(
            current.map((item) => ({ ...item, id: principalKey(item.principalType, item.principalId) })),
            result.principalOptions.map((item) => ({ ...item, id: principalKey(item.principalType, item.principalId) })),
          ).map(({ id: _, ...item }) => item));
          setPrincipalCursor(result.principalOptionsHaveMore ? result.nextPrincipalCursor ?? null : null);
        }
      }
    } catch (cause) {
      if (generation === actionGeneration.current) {
        const said = messageFor(cause, t("teamspaceDialog:error.load")); setError(said); setReadFailed(said);
      }
    } finally {
      if (generation === actionGeneration.current && writes.current.waiting === 0) {
        actionPending.current = false;
        setActionBusy(false);
      }
    }
  }

  async function refreshSnapshot() {
    if (actionPending.current) return false;
    const generation = ++actionGeneration.current;
    actionPending.current = true;
    setActionBusy(true);
    setError(null); setReadFailed(null);
    try {
      const result = await browser.readSnapshot();
      if (!result || generation !== actionGeneration.current) return false;
      publishSnapshot(result);
      return true;
    } catch (cause) {
      if (generation === actionGeneration.current) {
        const said = messageFor(cause, t("teamspaceDialog:error.load")); setError(said); setReadFailed(said);
      }
      return false;
    } finally {
      if (generation === actionGeneration.current && writes.current.waiting === 0) {
        actionPending.current = false;
        setActionBusy(false);
      }
    }
  }

  return {
    access, actionBusy, archived, archivedCursor, busy, canCreate, canInviteSelected, canManageSelected, description,
    error, readFailed, icon, memberCursor, memberPageRole, managementRead: Boolean(selected) && managedId === selected?.id,
    loading, members, membersCanEditSidebar, membersCanInvite, name, notice, openPageRole,
    principalCursor, principalOptions, requestCursor,
    requests, selected, settings, snapshotCursor, teamspaces, view, workspaceAdmin,
    memberCount: workspace.memberCount,
    setAccess, setDescription, setIcon, setMemberPageRole, setMembersCanEditSidebar,
    setMembersCanInvite, setName, setOpenPageRole, setSelectedId,
    currentUserId: session.user.id,
    session,
    workspaceId: workspace.id,
    openManage(teamspace: DialogTeamspace) { setSelectedId(teamspace.id); setView("manage"); setError(null); },
    /** A create whose answer was lost but whose teamspace is there ends as answered (lib/uncertainWrites.ts). */
    landedWithoutAnswer(success: string) { setError(null); setNoticeTarget(null); setNotice(success); },
    openView, readManagementAgain() { setError(null); setReadFailed(null); setManageTurn((turn) => turn + 1); },
    loadMore,
    refreshSnapshot,
    memberLabel(member: TeamspaceMember) {
      return optionLabel(principalOptions, member.principalType, member.principalId);
    },
    requestLabel(request: TeamspaceJoinRequest) {
      return optionLabel(principalOptions, "user", request.userId);
    },
    ...teamspaceDialogCommands({
      execute, selected, settings, teamspaces,
      fields: { name, icon, description, access, memberPageRole, openPageRole, membersCanInvite, membersCanEditSidebar },
    }),
  };
}

export type TeamspaceDialogController = ReturnType<typeof useTeamspaceDialog>;
