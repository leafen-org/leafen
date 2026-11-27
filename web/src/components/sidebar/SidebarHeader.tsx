import {
  AddAccountIcon as AddAccount,
  ChevronDown as CaretDown,
  Settings as Gear,
  SharePeopleIcon as Invite,
} from "@/icons/leafen";
import { useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import type { PendingSignOutPrompt } from "@/lib/productSignOut";
import type { Session, Workspace } from "@/types";
import { AddServerAccountDialog } from "./AddServerAccountDialog";
import { beginAddingAccount } from "@/lib/serverAccounts";
import { signOutHere } from "@/lib/remoteSignOut";
import { SidebarPendingSignOut } from "./SidebarPendingSignOut";
import { SidebarProfileGroups } from "./SidebarProfileGroups";
import { useWorkspaceUnread } from "./useWorkspaceUnread";
import { useUsableApps } from "@/components/apps/useUsableApps";
import { styles } from "./sidebarStyles";
import type { SwitcherProfile } from "./useServerProfiles";
import { NativeActionMenu, NativeActionMenuItem, NativeMenuSeparator } from "@/lib/nativeActionMenu";
import { useOpenedFromKeyboard } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { useGitHubLineShown, WorkspaceMenuGitHubLine } from "./WorkspaceMenuGitHubLine";
import { WorkspaceIconGlyph, workspaceIconKind } from "@/components/page/PageIcon";
import { useEnteredFromSwitcher } from "./switcherEntry";
import { useWorkspaceMenuConnect } from "./WorkspaceMenuConnectRows";
import { SidebarOfflineMark } from "./SidebarOfflineMark";

/**
 * The workspace switcher and its menu, at the foot of the sidebar as Notion's
 * since its 2026-09 redesign (seen 2026-10-01); the menu opens above it and
 * groups the workspaces by account, here by server profile (seen 2026-10-02).
 * Its head counts the workspace's members; under it is the GitHub line,
 * where Notion's asks to try a plan, and "설정" opens the settings
 * window that holds the account, the workspace and the server, as Notion's
 * holds the account and the workspace.
 */
export function SidebarHeader({
  session,
  workspace,
  workspaces,
  workspacesShown,
  profiles,
  guest,
  canInviteMembers,
  onCreateWorkspace,
  onOpenSettings,
  onInviteMembers,
  onOpenPeople,
  onSelectWorkspace,
  onSignOut,
  pendingSignOut,
}: {
  session: Session;
  workspace: Workspace | null;
  workspaces: Workspace[];
  workspacesShown: boolean;
  profiles: {
    active: SwitcherProfile;
    /** How many of `others` come before the active one. */
    activeAt: number;
    others: SwitcherProfile[];
    /** The packaged app, which keeps sign-ins on other servers. */
    packaged: boolean;
    open: (profileId: string, workspaceId?: string, at?: string, create?: boolean) => void;
    signOut: (profileId: string) => void;
    signOutOthers: () => Promise<void>;
    /** A page a server serves: the other accounts' servers in turn, then here. */
    signOutEverywhere: () => void;
  };
  /** A guest has no members to count in the menu's head. */
  guest: boolean;
  /** Those who add members to the workspace invite them from the menu. */
  canInviteMembers: boolean;
  onCreateWorkspace: () => void;
  onOpenSettings: () => void;
  onInviteMembers: () => void;
  /** Opens Settings › People. */
  onOpenPeople: () => void;
  onSelectWorkspace: (workspace: Workspace) => void;
  onSignOut: (after?: () => void, ask?: boolean) => void;
  pendingSignOut: PendingSignOutPrompt | null;
}) {
  const { t } = useTranslation("sidebar");
  // Its first row starts highlighted only when opened from the keyboard, as Notion's.
  const fromKeyboard = useOpenedFromKeyboard();
  const [workspaceMenuOpen, setWorkspaceMenuOpen] = useState(false);
  const [addAccountOpen, setAddAccountOpen] = useState(false);
  const workspaceButtonRef = useRef<HTMLButtonElement>(null);
  const workspaceMenuRef = useRef<HTMLDivElement>(null);

  const restoreMenuFocus = useRef(true);
  // Another account on this server, or, from this device's own server, which has no sign-in, on another.
  function addAccount() {
    if (profiles.active.local) setAddAccountOpen(true);
    else beginAddingAccount();
  }
  function closeWorkspaceMenu(restoreFocus: boolean) {
    restoreMenuFocus.current = restoreFocus;
    setWorkspaceMenuOpen(false);
  }
  // A touch screen's switcher is Notion's "계정과 워크스페이스" sheet, which
  // keeps its own layer: a phone's a whole screen, a wider one's (an iPad's)
  // a half sheet in its middle (seen 2026-11-13 in iPad Safari 834 and 1210).
  const { phone, touch } = useSidebarLayout(true);
  const sheet = phone || touch;
  usePopoverAppear(workspaceMenuOpen && !sheet, workspaceMenuRef);
  const { zIndex } = useOverlayLayer(workspaceMenuOpen && !sheet, { kind: "menu", floats: false, ref: workspaceMenuRef,
    onEscape: () => closeWorkspaceMenu(true), onOutsidePress: () => closeWorkspaceMenu(true),
    shouldRestoreFocus: () => restoreMenuFocus.current });
  // The menu is wider than the sidebar, as Notion's, so it lays out where
  // the switcher puts it and then stays there fixed, over the page beside
  // the sidebar that would clip it.
  const [menuPlace, setMenuPlace] = useState<CSSProperties | null>(null);
  useLayoutEffect(() => {
    if (!workspaceMenuOpen) { setMenuPlace(null); return; }
    const place = () => {
      const menu = workspaceMenuRef.current;
      if (!menu) return;
      menu.style.position = ""; menu.style.top = ""; menu.style.left = ""; menu.style.right = ""; menu.style.bottom = "";
      const rect = menu.getBoundingClientRect();
      // Opened upward from the switcher at the sidebar's foot, it keeps its foot as it grows.
      const above = (workspaceButtonRef.current?.getBoundingClientRect().top ?? 0) >= rect.bottom - 1;
      setMenuPlace(above
        ? { position: "fixed", top: "auto", bottom: window.innerHeight - rect.bottom, left: rect.left, right: "auto" }
        : { position: "fixed", top: rect.top, bottom: "auto", left: rect.left, right: "auto" });
    };
    place();
    window.addEventListener("resize", place);
    return () => window.removeEventListener("resize", place);
  }, [workspaceMenuOpen]);

  // A first sign-in's list is still on its way: saying "none" then is false (2026-11-05).
  const workspaceName = workspace?.name ?? (workspacesShown ? t("noWorkspace") : "");
  // The workspace's icon at the switcher and its menu's head, as Notion's
  // (seen 2026-11-11): an emoji, a built-in icon or a picture, or without
  // one its name's letter on a tint.
  const iconKind = workspaceIconKind(workspace?.icon);
  const otherUnread = useWorkspaceUnread(session, workspaces.map(({ id }) => id), workspace?.id ?? null);
  // The menu lists the apps with a page to open; one that only opens files
  // has nothing to show from here (2026-11-05).
  const apps = useUsableApps(session, workspaceMenuOpen).filter((app) => app.fullPage);
  const githubLine = useGitHubLineShown();
  // A restricted member is not told how many members there are, as Notion's
  // menu shows them none, and opens the people they work with instead of
  // inviting (seen 2026-10-12).
  const restricted = workspace?.restricted === true;
  const memberCount = workspace && !guest && !restricted ? workspace.memberCount : undefined;
  const enteredFromSwitcher = useEnteredFromSwitcher(workspace?.id);
  // "새 기기 로그인" and "AI 에이전트 연결" one press from the menu (WorkspaceMenuConnectRows).
  const connect = useWorkspaceMenuConnect({ session, workspace, mayConnectAgents: !guest && !restricted });
  // With other accounts signed in, a phone names the account under the
  // workspace, as Notion's phone sidebar does (seen 2026-10-27).
  const accountLine = profiles.others.some((profile) => !profile.local && profile.email) ? profiles.active.email : "";

  // With more than one account signed in, as Notion's, the foot logs out of
  // them all: the app comes back to this device (its own profile has no
  // sign-in to end), a page a server serves visits each other server and
  // then signs out here.
  const signOutRows = (
    <>
      {profiles.others.some((profile) => !profile.local && profile.email) ? (
        <>
          <NativeMenuSeparator className={styles.workspaceMenuDivider} />
          <NativeActionMenuItem
            type="button"
            className={styles.workspaceMenuItem}
            data-workspace-menu-item
            data-workspace-menu-sign-out
            role="menuitem"
            onClick={() => {
              closeWorkspaceMenu(false);
              if (!profiles.packaged) {
                profiles.signOutEverywhere();
                return;
              }
              void profiles.signOutOthers().then(() => {
                if (!profiles.active.local) signOutHere();
              });
            }}
          >
            <span>{t("logOutAll")}</span>
          </NativeActionMenuItem>
        </>
      ) : profiles.active.local ? null : (
        <>
          <NativeMenuSeparator className={styles.workspaceMenuDivider} />
          <NativeActionMenuItem
            type="button"
            className={styles.workspaceMenuItem}
            data-workspace-menu-item
            data-workspace-menu-sign-out
            role="menuitem"
            onClick={() => {
              closeWorkspaceMenu(false);
              // An account with no email has no way back from a browser alone:
              // it says so, in the one question that also names what is unsent.
              onSignOut(undefined, !profiles.active.accountEmail);
            }}
          >
            <span>{t("logOut")}</span>
          </NativeActionMenuItem>
        </>
      )}
    </>
  );

  return (
    <div className={styles.workspaceSwitcher} data-sidebar-workspace-switcher>
      <button
        ref={workspaceButtonRef}
        type="button"
        className={styles.workspaceBtn}
        data-sidebar-workspace-button
        data-account-line={accountLine ? "true" : undefined}
        onClick={() => { restoreMenuFocus.current = true; setWorkspaceMenuOpen((current) => !current); }}
        aria-label={t("openWorkspaceMenu")}
        aria-haspopup="menu"
        aria-expanded={workspaceMenuOpen}
      >
        <span className={styles.wsIcon} aria-hidden="true" data-unread={otherUnread.size > 0 ? "true" : undefined}
          data-workspace-icon={iconKind}>
          {iconKind === "letter" ? workspaceName.slice(0, 1) : <WorkspaceIconGlyph icon={workspace?.icon} size={21} fallback="" />}
          {otherUnread.size > 0 ? <span className={styles.workspaceUnreadDot} /> : null}
        </span>
        <span className={styles.wsNameStack}>
          <span className={styles.wsName}>{workspaceName}</span>
          {accountLine ? <span className={styles.wsAccount} data-sidebar-account-line>{accountLine}</span> : null}
        </span>
        <SidebarOfflineMark />
        {/* A guest's switcher says so after the name once chosen from its menu, until the page loads again, as Notion's (switcherEntry.ts). */}
        {guest && !phone && enteredFromSwitcher ? <span className={`${styles.profileGuestBadge} ${styles.wsGuestBadge}`} data-sidebar-guest-badge>
          {t("profiles.guest")}</span> : null}
        <CaretDown
          className={styles.wsChevron}
          data-sidebar-workspace-chevron
          size="small"
          aria-hidden="true"
        />
      </button>
      {workspaceMenuOpen && sheet ? (
        // Notion's phone sheet (seen 2026-10-27, a whole screen 2026-10-28):
        // each account's workspaces, then "다른 계정 추가" and the log-out; its
        // settings and people are the sidebar's own rows on a phone. Its title
        // runs past its third rather than being cut, as Notion's whole title
        // shows at 390px (Leafen's font draws it 136px to Notion's 129). A
        // wider touch screen's sidebar is a desktop's, which has no settings
        // row, so its sheet starts its foot with the menu's "설정" and invite.
        <PhoneSheet size={phone ? "screen" : "half"} title={t("accountsAndWorkspaces")} titleFit="spills" onClose={() => closeWorkspaceMenu(false)}
          action={{ label: t("common:actions.done"), onPress: () => closeWorkspaceMenu(false) }} data={{ "data-workspace-phone-sheet": "" }}>
          <NativeActionMenu aria-label={t("workspaceMenu")} className={styles.workspacePhoneMenu}>
            {githubLine ? <WorkspaceMenuGitHubLine session={session} phone onOpen={() => closeWorkspaceMenu(false)} /> : null}
            <SidebarProfileGroups
              active={profiles.active}
              activeAt={profiles.activeAt}
              others={profiles.others}
              workspace={workspace}
              workspaces={workspaces}
              apps={apps}
              unread={otherUnread}
              onCloseMenu={() => closeWorkspaceMenu(false)}
              onCreateWorkspace={onCreateWorkspace}
              onOpenProfile={profiles.open}
              onSelectWorkspace={onSelectWorkspace}
              onSignOut={onSignOut}
              onSignOutProfile={profiles.signOut}
            />
            <NativeMenuSeparator className={styles.workspaceMenuDivider} />
            {phone ? null : <>
              <NativeActionMenuItem type="button" className={styles.workspaceMenuItem} data-workspace-menu-item data-workspace-menu-settings
                role="menuitem" data-workspace-phone-foot onClick={() => { closeWorkspaceMenu(false); onOpenSettings(); }}>
                <Gear size="regular" aria-hidden="true" />
                <span>{t("settings")}</span>
              </NativeActionMenuItem>
              {workspace && canInviteMembers && !workspace.appInstallId ? (
                <NativeActionMenuItem type="button" className={styles.workspaceMenuItem} data-workspace-menu-item data-workspace-menu-invite
                  role="menuitem" onClick={() => { closeWorkspaceMenu(false); if (restricted) onOpenPeople(); else onInviteMembers(); }}>
                  <Invite size="regular" aria-hidden="true" />
                  <span>{t(restricted ? "people" : "inviteMembers")}</span>
                </NativeActionMenuItem>
              ) : null}
            </>}
            <NativeActionMenuItem type="button" className={styles.workspaceMenuItem} data-workspace-menu-item role="menuitem"
              data-workspace-phone-foot={phone ? "" : undefined} onClick={() => { closeWorkspaceMenu(false); addAccount(); }}>
              <AddAccount size="regular" aria-hidden="true" />
              <span>{t("phoneAddAccount")}</span>
            </NativeActionMenuItem>
            {connect.rows(() => closeWorkspaceMenu(false))}
            {signOutRows}
          </NativeActionMenu>
        </PhoneSheet>
      ) : null}
      {workspaceMenuOpen && !sheet && (
        <>
          <button
            type="button"
            className={styles.workspaceMenuBackdrop}
            style={{ zIndex: zIndex === undefined ? undefined : zIndex - 1 }}
            onClick={() => closeWorkspaceMenu(true)}
            tabIndex={-1}
            aria-label={t("closeWorkspaceMenu")}
          />
          <div
            ref={workspaceMenuRef}
            className={styles.workspaceMenu}
            style={{ zIndex, ...menuPlace }}
            aria-label={t("workspaceMenu")}
          >
            <div className={styles.workspaceMenuAccount}>
              <span className={styles.workspaceMenuIcon} aria-hidden="true" data-workspace-icon={iconKind}>
                {iconKind === "letter" ? workspaceName.slice(0, 1) : <WorkspaceIconGlyph icon={workspace?.icon} size={36} fallback="" />}
              </span>
              <span>
                <strong>{workspaceName}</strong>
                {/* A guest's head says "게스트" under the name, as Notion's (seen 2026-11-12). */}
                <span data-workspace-member-count={memberCount}>
                  {memberCount ? t("memberCount", { count: memberCount }) : t(guest ? "profiles.guest" : "workspaces")}
                </span>
              </span>
            </div>
            <NativeActionMenu aria-label={t("workspaceMenu")} autoFocus={fromKeyboard ? "first" : true}>
            <NativeMenuSeparator className={styles.workspaceMenuDivider} />
            {/* The GitHub line under the head, where Notion's menu asks to try a plan (user decision 2026-11-25). */}
            {githubLine ? <WorkspaceMenuGitHubLine session={session} onOpen={() => closeWorkspaceMenu(false)} /> : null}
            <NativeActionMenuItem
              type="button"
              className={styles.workspaceMenuItem}
              data-workspace-menu-item
              data-workspace-menu-settings
              role="menuitem"
              onClick={() => {
                closeWorkspaceMenu(false);
                onOpenSettings();
              }}
            >
              <Gear size="regular" aria-hidden="true" />
              <span>{t("settings")}</span>
            </NativeActionMenuItem>
            {/* An app's workspace takes its people from who may use the app (2026-10-16). */}
            {workspace && canInviteMembers && !workspace.appInstallId && (
              <NativeActionMenuItem
                type="button"
                className={styles.workspaceMenuItem}
                data-workspace-menu-item
                data-workspace-menu-invite
                role="menuitem"
                onClick={() => {
                  closeWorkspaceMenu(false);
                  if (restricted) onOpenPeople();
                  else onInviteMembers();
                }}
              >
                <Invite size="regular" aria-hidden="true" />
                <span>{t(restricted ? "people" : "inviteMembers")}</span>
              </NativeActionMenuItem>
            )}
            <NativeActionMenuItem
              type="button"
              className={styles.workspaceMenuItem}
              data-workspace-menu-item
              role="menuitem"
              onClick={() => {
                // Its window opens over the menu, which is there again if it is left, as Notion's.
                if (profiles.active.local) closeWorkspaceMenu(false);
                addAccount();
              }}
            >
              <AddAccount size="regular" aria-hidden="true" />
              <span>{t("profiles.addAccount")}</span>
            </NativeActionMenuItem>
            {connect.rows(() => closeWorkspaceMenu(true))}
            <NativeMenuSeparator className={styles.workspaceMenuDivider} />
            <SidebarProfileGroups
                active={profiles.active}
                activeAt={profiles.activeAt}
                others={profiles.others}
                workspace={workspace}
                workspaces={workspaces}
                apps={apps}
                unread={otherUnread}
                onCloseMenu={() => closeWorkspaceMenu(false)}
                onCreateWorkspace={onCreateWorkspace}
                onOpenProfile={profiles.open}
                onSelectWorkspace={onSelectWorkspace}
                onSignOut={onSignOut}
                onSignOutProfile={profiles.signOut}
              />
            {signOutRows}
            </NativeActionMenu>
          </div>
        </>
      )}
      {pendingSignOut && (
        <SidebarPendingSignOut pending={pendingSignOut} noEmail={!profiles.active.accountEmail} returnFocusRef={workspaceButtonRef} />
      )}
      {connect.windows}
      {addAccountOpen && (
        <AddServerAccountDialog returnFocusRef={workspaceButtonRef} onClose={() => setAddAccountOpen(false)} />
      )}
    </div>
  );
}
