import { useCallback, useEffect, useState } from "react";
import { isPackagedClient } from "@/lib/packagedClient";
import {
  listProfiles,
  LOCAL_PROFILE_ID,
  noteServerName,
  reachedAt,
  rememberProfile,
  subscribeProfiles,
  type ProfileApp,
  type ProfileListState,
  type ProfileWorkspace,
  type ServerProfile,
} from "@/lib/profileRegistry";
import { openProfile, signOutOfProfile } from "@/lib/profileSwitch";
import { startRemoteSignOut } from "@/lib/remoteSignOut";
import {
  accountOfProfileId,
  accountProfileId,
  listServerAccounts,
  openOtherAccount,
  rememberServerAccount,
  signOutOfOtherAccount,
  subscribeServerAccounts,
  type ServerAccount,
} from "@/lib/serverAccounts";
import { serverDisplayName, useServerNaming } from "@/lib/serverName";
import { serverIdentity, serverOrigin } from "@/lib/serverOrigin";
import { subscribeServerLists } from "@/lib/switcherLists";
import type { Session, Workspace } from "@/types";

/** A profile as the workspace menu shows it. */
export type SwitcherProfile = {
  id: string;
  /** The packaged client's own server, which has no sign-in. */
  local: boolean;
  /**
   * The account as the menu names it: its email, or its name where it has
   * none; "" for this device and for a server not signed in on yet.
   */
  email: string;
  /**
   * The account as the menu shows it (2026-10-20): its server — the person's
   * own name for it, else the server's name or alias — and the person's
   * name, the email only on hover; "" parts it doesn't know.
   */
  server: string;
  person: string;
  accountEmail: string;
  /** The server's host, for another server's profile. */
  host: string;
  workspaces: ProfileWorkspace[];
  /** Another server's apps, its workspaces with unread notifications and how its list was last read. */
  apps: ProfileApp[];
  unread: string[];
  listState: ProfileListState;
  /** Whether its list can be read: a list key in a page a server serves, a sign-in in the packaged client. */
  listed: boolean;
};

function hostOf(id: string): string {
  try {
    return new URL(id).host;
  } catch {
    return id;
  }
}

function switcherProfile(profile: ServerProfile, packaged: boolean): SwitcherProfile {
  const local = profile.id === LOCAL_PROFILE_ID;
  return {
    id: profile.id,
    local,
    email: local ? "" : profile.email || profile.name,
    server: profile.label || (local ? "" : serverDisplayName({ name: profile.serverName, alias: profile.serverAlias })),
    person: local ? "" : profile.name || profile.email,
    accountEmail: local ? "" : profile.email,
    host: local ? "" : hostOf(reachedAt(profile)),
    workspaces: profile.workspaces,
    apps: profile.apps,
    unread: profile.unread,
    listState: profile.listState,
    listed: local || (packaged ? Boolean(profile.email || profile.name) : Boolean(profile.listKey)),
  };
}

/** Another account on the active server, a group of its own in the menu. */
function otherAccountProfile(account: ServerAccount, server: ServerProfile | undefined): SwitcherProfile {
  return {
    id: accountProfileId(account.userId),
    local: false,
    email: account.email || account.name,
    server: server?.label || (server ? serverDisplayName({ name: server.serverName, alias: server.serverAlias }) : ""),
    person: account.name || account.email,
    accountEmail: account.email,
    host: hostOf(server?.address || serverOrigin()),
    workspaces: account.workspaces,
    apps: account.apps,
    unread: account.unread,
    listState: "",
    listed: true,
  };
}

/**
 * The server profiles the workspace menu groups its workspaces by: the
 * active one, kept as it stands, and the others this client knows, all in
 * the order they were added — the active one where it was added (`activeAt`
 * others before it), as Notion's accounts keep their place (seen 2026-10-30).
 * More than one account on the active server shows as a group each.
 */
export function useServerProfiles({ session, workspaces, workspacesShown }: {
  session: Session;
  workspaces: Workspace[];
  /** Whether `workspaces` is a list read or kept, not the empty one before it. */
  workspacesShown: boolean;
}) {
  const activeId = serverIdentity();
  // The app has a server of its own on this computer; a browser is always on one's.
  const packaged = isPackagedClient();
  const local = packaged && activeId === LOCAL_PROFILE_ID;
  const [others, setOthers] = useState<SwitcherProfile[]>([]);
  const [activeAt, setActiveAt] = useState(0);
  // The person's own name for this server, read with the list as it changes,
  // not from storage at each drawing of the sidebar (some 20 ms of a route's
  // press at CPU ×2, 2026-11-14).
  const [activeLabel, setActiveLabel] = useState(() => listProfiles().find((profile) => profile.id === activeId)?.label ?? "");
  const userId = session.user.id;
  const refresh = useCallback(() => {
    const listed = listProfiles();
    setActiveLabel(listed.find((profile) => profile.id === activeId)?.label ?? "");
    const shown = listed.filter((profile) => packaged || profile.id !== LOCAL_PROFILE_ID);
    const server = shown.find((profile) => profile.id === activeId);
    const here = listServerAccounts();
    // A row with no profile is where the active account goes.
    const rows: Array<{ addedAt: number; profile: SwitcherProfile | null }> = shown
      .filter((profile) => profile.id !== activeId)
      .map((profile) => ({ addedAt: profile.addedAt, profile: switcherProfile(profile, packaged) }));
    if (here.length > 1) {
      for (const account of here) {
        rows.push({ addedAt: account.addedAt, profile: account.userId === userId ? null : otherAccountProfile(account, server) });
      }
    } else if (server) rows.push({ addedAt: server.addedAt, profile: null });
    rows.sort((left, right) => left.addedAt - right.addedAt);
    const at = rows.findIndex((row) => !row.profile);
    setActiveAt(at < 0 ? rows.length : at);
    setOthers(rows.flatMap((row) => row.profile ?? []));
  }, [activeId, packaged, userId]);
  // Other servers' lists come back while the menu shows them, and the
  // accounts themselves change from settings or another tab.
  useEffect(() => subscribeServerLists(refresh), [refresh]);
  useEffect(() => subscribeProfiles(refresh), [refresh]);
  useEffect(() => subscribeServerAccounts(refresh), [refresh]);

  useEffect(() => {
    // Before the list is drawn there is none to keep: what was kept stands, as
    // another tab or window shows it (2026-11-15: a reload kept this server's
    // and this account's workspaces as none for some 50 ms, and another
    // account's tab showed this one's group empty meanwhile).
    if (!workspacesShown) {
      refresh();
      return;
    }
    const kept = workspaces.map((item) => ({
      id: item.id,
      name: item.name,
      ...(item.icon ? { icon: item.icon } : {}),
      ...(item.guest ? { guest: true } : {}),
    }));
    rememberProfile({
      id: activeId,
      email: local ? "" : session.user.email,
      name: local ? "" : session.user.name,
      workspaces: kept,
    });
    if (!local) {
      rememberServerAccount(session, kept, listProfiles().find((profile) => profile.id === activeId)?.addedAt ?? Date.now());
    }
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `session` changes with each renewed token
  }, [activeId, local, refresh, session.user.email, session.user.name, workspaces, workspacesShown]);

  // The active server's name, as its runtime settings say it.
  const naming = useServerNaming();
  useEffect(() => {
    if (naming) noteServerName(activeId, naming);
  }, [activeId, naming]);

  const active: SwitcherProfile = {
    id: activeId,
    local,
    email: local ? "" : session.user.email || session.user.name,
    server: activeLabel || (local || !naming ? "" : serverDisplayName(naming)),
    person: local ? "" : session.user.name || session.user.email,
    accountEmail: local ? "" : session.user.email,
    host: local ? "" : hostOf(serverOrigin()),
    workspaces: [],
    apps: [],
    unread: [],
    listState: "",
    listed: true,
  };

  const open = useCallback((profileId: string, workspaceId = "", at = "", create = false) => {
    const account = accountOfProfileId(profileId);
    if (account) void openOtherAccount(account, workspaceId, at, create);
    else openProfile(profileId, workspaceId, at, create);
  }, []);

  const signOut = useCallback((profileId: string) => {
    const account = accountOfProfileId(profileId);
    if (account) {
      signOutOfOtherAccount(account);
      return;
    }
    void (packaged ? signOutOfProfile(profileId) : startRemoteSignOut([profileId], false)).then(refresh);
  }, [packaged, refresh]);

  // Every other server account this app is signed in to, ended one by one.
  const signOutOthers = useCallback(async () => {
    const signedIn = listProfiles().filter((profile) => (
      profile.id !== activeId && profile.id !== LOCAL_PROFILE_ID && (profile.email || profile.name)
    ));
    for (const profile of signedIn) await signOutOfProfile(profile.id);
    refresh();
  }, [activeId, refresh]);

  // A page a server serves: each other account's server in turn, then here
  // (remoteSignOut.ts), as Notion's "모든 계정에서 로그아웃".
  const signOutEverywhere = useCallback(() => {
    const others = listProfiles().filter((profile) => profile.id !== activeId && profile.id !== LOCAL_PROFILE_ID);
    void startRemoteSignOut(others.map((profile) => profile.id), true).then(refresh);
  }, [activeId, refresh]);

  return { active, activeAt, others, packaged, open, refresh, signOut, signOutOthers, signOutEverywhere };
}
