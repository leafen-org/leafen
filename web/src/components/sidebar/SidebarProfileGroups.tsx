import { WorkspaceIconGlyph, workspaceIconKind, workspaceIconLetter } from "@/components/page/PageIcon";
import { CheckIcon as Check, DotsHorizontal as More, DragHandleIcon, LogOutIcon, Plus, ServerAppsIcon } from "@/icons/leafen";
import { useEffect, useRef, useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { UsableApp } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { useAppPlatformAvailable, useAppPlatformMissing } from "@/lib/appPlatformAvailability";
import { loadSession } from "@/lib/authSessionStore";
import { isPackagedClient } from "@/lib/packagedClient";
import { refreshServerLists, startServerConnect } from "@/lib/switcherLists";
import { pushRoute } from "@/lib/router";
import type { ProfileWorkspace } from "@/lib/profileRegistry";
import type { Workspace } from "@/types";
import { styles } from "./sidebarStyles";
import type { SwitcherProfile } from "./useServerProfiles";
import { NativeActionMenuItem, NativeMenuHeader, NativeMenuSection } from "@/lib/nativeActionMenu";
import { SidebarProfileOptions, type ProfileMenuOptions, type ProfileAction } from "./SidebarProfileOptions";
import { useWorkspaceOrderDrag } from "./useWorkspaceOrderDrag";
import { openAppInTenancy } from "@/components/apps/appTenancyChoice";
import { markSwitcherEntry } from "./switcherEntry";

/**
 * The workspace menu's account groups, as Notion's: one per signed-in
 * account, here one per server profile, in the order they were
 * added, each with its "…", its workspaces and "New workspace" (seen
 * 2026-10-30: the current account keeps its place, every account ends with
 * it); the active one's then lists the server's apps the person may use
 *, whose own workspaces open from the app.
 * Another server's group shows the list it last answered at once and is
 * asked again while the menu is open (lib/switcherLists.ts); one whose list
 * can't be read offers to connect it.
 */
export function SidebarProfileGroups({
  active,
  activeAt = 0,
  others,
  workspace,
  workspaces,
  apps = [],
  unread,
  onCloseMenu,
  onCreateWorkspace,
  onOpenProfile,
  onSelectWorkspace,
  onSignOut,
  onSignOutProfile,
}: {
  active: SwitcherProfile;
  /** How many of `others` come before the active one. */
  activeAt?: number;
  others: SwitcherProfile[];
  workspace: Workspace | null;
  workspaces: Workspace[];
  /** The apps this account may use on this server. */
  apps?: UsableApp[];
  /** This account's other workspaces with unread notifications, each marked with a red dot as Notion's. */
  unread: ReadonlySet<string>;
  onCloseMenu: () => void;
  onCreateWorkspace: () => void;
  onOpenProfile: (profileId: string, workspaceId?: string, at?: string, create?: boolean) => void;
  onSelectWorkspace: (workspace: Workspace) => void;
  onSignOut: () => void;
  onSignOutProfile: (profileId: string) => void;
}) {
  const { t, i18n } = useTranslation("sidebar");
  // A phone's own server or the App Store edition browses to why it has no apps.
  const appPlatformHere = useAppPlatformAvailable();
  const appPlatformMissing = useAppPlatformMissing();
  const appPlatform = appPlatformHere || appPlatformMissing !== "";
  // The open "…" and where its menu goes: beside it, past the sidebar's edge
  // that would cut it off, as Notion's.
  const [options, setOptions] = useState<ProfileMenuOptions | null>(null);
  // The app keeps a sign-in on each server in the system's keychain, so
  // leaving one signs out of it; a browser only lists them.
  const packaged = isPackagedClient();
  useEffect(() => { void refreshServerLists(); }, []);

  function connect(profileId: string) {
    const signedIn = loadSession();
    onCloseMenu();
    if (signedIn) void startServerConnect(signedIn, profileId);
  }

  const ownWorkspaces = useMemo(() => workspaces.filter((item) => !item.appInstallId), [workspaces]);
  const orderedWorkspaces = useWorkspaceOrderDrag(active.id, ownWorkspaces);
  // A workspace named as one in another group shows its server after it (2026-10-20).
  const names = [
    ...workspaces.filter((item) => !item.appInstallId).map((item) => item.name),
    ...others.flatMap((profile) => profile.workspaces.map((item) => item.name)),
  ];
  const repeated = new Set(names.filter((name, index) => names.indexOf(name) !== index));
  // An app the person uses in more than one organization stands once for
  // each, its organization after its name, as a workspace named as one on
  // another server shows its server (2026-11-15: three "팀 퀴즈" rows).
  const installs = apps.map((app) => app.installId);
  const repeatedApps = new Set(installs.filter((id, index) => installs.indexOf(id) !== index));
  // Two groups with one head (two accounts of one name on one server) show
  // their emails after it, which tell them apart (the switcher's "server ·
  // name" head is for telling the accounts apart; 2026-11-16).
  const thisDevice = t("profiles.thisDevice");
  const heads = [profileHead(active, others.length > 0, thisDevice), ...others.map((profile) => profileHead(profile, true, thisDevice))];
  const sameHeads = new Set(heads.filter((head, index) => heads.indexOf(head) !== index));

  // The workspace one is in is a row like the others, as Notion's (seen 2026-11-21 in two tabs): it takes the
  // pointer and the hover ground, and choosing it only closes the menu.
  function workspaceRow(item: ProfileWorkspace, current: boolean, open: () => void, marked: boolean, server: string) {
    return (
      <NativeActionMenuItem
        key={item.id}
        type="button"
        className={styles.workspaceMenuItem}
        data-workspace-menu-item
        data-workspace-order-row={item.id}
        role="menuitemradio"
        aria-checked={current}
        onClick={current ? () => onCloseMenu() : open}
      >
        <span className={styles.profileWorkspaceIcon} aria-hidden="true" data-workspace-icon={workspaceIconKind(item.icon)}
          data-unread={marked && !current ? "true" : undefined}>
          <WorkspaceIconGlyph icon={item.icon} size={20} fallback={workspaceIconLetter(item.name)} />
          {marked && !current ? <span className={styles.workspaceUnreadDot} data-workspace-unread={item.id} /> : null}
          <DragHandleIcon className={styles.profileWorkspaceHandle} aria-hidden="true" />
        </span>
        <span>{item.name}</span>
        {repeated.has(item.name) && server ? <span className={styles.profileWorkspaceServer} data-workspace-server>{server}</span> : null}
        {item.guest ? <span className={styles.profileGuestBadge}>{t("profiles.guest")}</span> : null}
        {current ? <Check size="regular" className={styles.profileCurrentCheck} aria-hidden="true" /> : null}
      </NativeActionMenuItem>
    );
  }

  const plus = <Plus size="regular" aria-hidden="true" />, out = <LogOutIcon size="regular" aria-hidden="true" />;
  const activeOptions = [
    { label: t("profiles.createOrJoinWorkspace"), icon: plus, run: () => { onCloseMenu(); onCreateWorkspace(); } },
    ...(active.local ? [] : [{ label: t("logOut"), icon: out, run: () => { onCloseMenu(); onSignOut(); } }]),
  ];

  function newWorkspaceRow(create: () => void) {
    return (
      <NativeActionMenuItem
        type="button"
        className={`${styles.workspaceMenuItem} ${styles.profileNewWorkspace}`}
        data-workspace-menu-item
        data-switcher-new-workspace
        role="menuitem"
        onClick={() => { onCloseMenu(); create(); }}
      >
        <Plus size="regular" aria-hidden="true" />
        <span>{t("newWorkspace")}</span>
      </NativeActionMenuItem>
    );
  }

  // Another account's "New workspace" opens that account with the new workspace window.
  const createIn = (profile: SwitcherProfile) => () => onOpenProfile(profile.id, "", "/", true);

  const activeGroup = (
      <NativeMenuSection key={active.id} className={styles.profileGroup} aria-label={active.local ? t("profiles.thisDevice") : active.email}>
        <ProfileHeader profile={active} actions={activeOptions} showHost={others.length > 0} sameHeads={sameHeads} options={options} onOptions={setOptions} />
        {orderedWorkspaces.map((item) => workspaceRow(
          { id: item.id, name: item.name, ...(item.icon ? { icon: item.icon } : {}), ...(item.guest ? { guest: true } : {}) },
          item.id === workspace?.id,
          () => { onCloseMenu(); markSwitcherEntry(item.id); onSelectWorkspace(item); },
          unread.has(item.id),
          profileServer(active, t("profiles.thisDevice")),
        ))}
        {newWorkspaceRow(onCreateWorkspace)}
        {apps.map((app) => {
          const name = appText(app.name, i18n.language);
          return (
            <NativeActionMenuItem
              key={`${app.installId}-${app.tenancyKey}`}
              type="button"
              className={styles.workspaceMenuItem}
              data-workspace-menu-item
              data-switcher-app={app.slug}
              role="menuitem"
              onClick={() => { onCloseMenu(); openAppInTenancy(app.slug, app.tenancyKey); }}
            >
              <span className={styles.profileWorkspaceIcon} aria-hidden="true">{name.slice(0, 1)}</span>
              <span>{name}</span>
              {repeatedApps.has(app.installId) && app.tenancyName
                ? <span className={styles.profileWorkspaceServer} data-switcher-app-organization>{app.tenancyName}</span> : null}
              <span className={styles.profileAppBadge}>{t("apps.badge")}</span>
            </NativeActionMenuItem>
          );
        })}
        {appPlatform ? (
          <NativeActionMenuItem
            type="button"
            className={`${styles.workspaceMenuItem} ${styles.profileNewWorkspace}`}
            data-workspace-menu-item
            data-switcher-browse-apps
            role="menuitem"
            onClick={() => { onCloseMenu(); pushRoute("/apps"); }}
          >
            <ServerAppsIcon size="regular" aria-hidden="true" />
            <span>{t("apps.browse")}</span>
          </NativeActionMenuItem>
        ) : null}
      </NativeMenuSection>
  );

  const otherGroup = (profile: SwitcherProfile) => (
        <NativeMenuSection key={profile.id} className={styles.profileGroup} aria-label={profile.local ? t("profiles.thisDevice") : profile.email || profile.host}>
          <ProfileHeader profile={profile} actions={[
            { label: t("profiles.createOrJoinWorkspace"), icon: plus, run: () => { onCloseMenu(); createIn(profile)(); } },
            ...(profile.local ? [] : [{
              // A page a server serves signs out there by going to that
              // server's page and back (remoteSignOut.ts), as the app ends the
              // sign-in it keeps. A server whose list this page wasn't allowed
              // to read gave it no key to ask with: its row only leaves the
              // menu, and says so.
              label: packaged || profile.listed ? t("logOut") : t("profiles.removeFromList"), icon: out,
              run: () => onSignOutProfile(profile.id),
            }]),
          ]} showHost sameHeads={sameHeads} options={options} onOptions={setOptions} />
          {profile.workspaces.map((item) => workspaceRow(
            item, false, () => { onCloseMenu(); onOpenProfile(profile.id, item.id); }, profile.unread.includes(item.id),
            profileServer(profile, t("profiles.thisDevice")),
          ))}
          {/* Opened there, an app goes by the workspace open there: one row each. */}
          {profile.apps.filter((app, index, all) => all.findIndex((other) => other.slug === app.slug) === index).map((app) => {
            const name = appText(app.name, i18n.language);
            return (
              <NativeActionMenuItem key={app.slug} type="button" className={styles.workspaceMenuItem} data-workspace-menu-item
                data-switcher-app={app.slug} role="menuitem" onClick={() => { onCloseMenu(); onOpenProfile(profile.id, "", `/apps/${app.slug}`); }}>
                <span className={styles.profileWorkspaceIcon} aria-hidden="true">{name.slice(0, 1)}</span>
                <span>{name}</span>
                <span className={styles.profileAppBadge}>{t("apps.badge")}</span>
              </NativeActionMenuItem>
            );
          })}
          {!profile.listed && !packaged && profile.listState !== "off" ? (
            // A page a server serves reads another server's list once the person allows it there.
            <NativeActionMenuItem type="button" className={styles.workspaceMenuItem} data-workspace-menu-item
              data-switcher-connect={profile.id} data-switcher-list-state={profile.listState || undefined} role="menuitem"
              onClick={() => connect(profile.id)}>
              <span>{profile.workspaces.length ? t("profiles.reconnect") : t("profiles.connect")}</span>
            </NativeActionMenuItem>
          ) : null}
          {profile.workspaces.length === 0 && (profile.listed || packaged || profile.listState === "off") ? (
            <NativeActionMenuItem
              type="button"
              className={styles.workspaceMenuItem}
              data-workspace-menu-item
              role="menuitem"
              onClick={() => { onCloseMenu(); onOpenProfile(profile.id); }}
            >
              {/* A server not signed in on yet signs in; this device and a known account open. */}
              <span>{profile.local || profile.email ? t("profiles.open") : t("profiles.signIn")}</span>
            </NativeActionMenuItem>
          ) : null}
          {profile.listState === "off" || profile.listState === "failed" ? (
            // Quietly, on the server's own group: what it shows is what it last answered.
            <NativeActionMenuItem type="button" className={`${styles.workspaceMenuItem} ${styles.profileListState}`} data-workspace-menu-item
              data-switcher-list-state={profile.listState} role="menuitem" disabled>
              <span>{t(`profiles.listState.${profile.listState}`)}</span>
            </NativeActionMenuItem>
          ) : null}
          {profile.listed && (profile.local || profile.email) ? newWorkspaceRow(createIn(profile)) : null}
        </NativeMenuSection>
  );

  return (
    <>
      {others.slice(0, activeAt).map(otherGroup)}
      {activeGroup}
      {others.slice(activeAt).map(otherGroup)}
    </>
  );
}

/** The server a group is, as the menu names it: the person's name for it, the server's, or this device or its host. */
function profileServer(profile: SwitcherProfile, thisDevice: string): string {
  return profile.server || (profile.local ? thisDevice : profile.host);
}

/**
 * A group's head: "server · name"; a group alone needs only the person
 * (2026-11-05: a first-time "벚꽃 · 〈이름〉" read as an unexplained word); a
 * server not signed in on yet shows its server.
 */
function profileHead(profile: SwitcherProfile, showHost: boolean, thisDevice: string) {
  const server = profileServer(profile, thisDevice);
  return profile.person ? (showHost ? `${server} · ${profile.person}` : profile.person) : server;
}

/**
 * A group's account, as "server · name" (2026-10-20, an approved difference
 * from Notion's email: Leafen's accounts are each a server's), its email
 * and address on hover; a head another group has too shows the email after
 * it (`sameHeads`).
 */
function ProfileHeader({ profile, actions, showHost, sameHeads, options, onOptions }: {
  profile: SwitcherProfile; actions: ProfileAction[]; showHost: boolean; sameHeads: ReadonlySet<string>;
  options: ProfileMenuOptions | null; onOptions(next: ProfileMenuOptions | null): void;
}) {
  const { t } = useTranslation("sidebar");
  const trigger = useRef<HTMLDivElement | HTMLAnchorElement>(null);
  const server = profileServer(profile, t("profiles.thisDevice"));
  const head = profileHead(profile, showHost, t("profiles.thisDevice"));
  const label = sameHeads.has(head) && profile.accountEmail && !head.includes(profile.accountEmail) ? `${head} · ${profile.accountEmail}` : head;
  const detail = [showHost ? "" : server, profile.accountEmail, showHost ? profile.host : ""].filter((part) => part && !label.includes(part)).join(" · ");
  const open = options?.id === profile.id;
  return <>
    <NativeMenuHeader className={styles.profileHeader}>
      <span className={styles.profileLabel} title={detail || undefined} data-profile-label>{label}</span>

      {open ? <SidebarProfileOptions options={options} actions={actions} onClose={() => onOptions(null)} /> : null}
    </NativeMenuHeader>
    {actions.length ? <NativeActionMenuItem actionRef={trigger} className={styles.profileOptionsButton} data-workspace-menu-item
      aria-label={t("profiles.accountOptions")} aria-expanded={open} onClick={() => {
        if (trigger.current) onOptions(open ? null : { id: profile.id, anchor: trigger.current });
      }}><More size="small" aria-hidden="true" /></NativeActionMenuItem> : null}
  </>;
}
