import { useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { ImportedPeopleMapping } from "@/components/ImportedPeopleMapping";
import { OpenInNew } from "@/icons/leafen";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";
import { organizationModule } from "@/lib/organizationModule";
import { PEOPLE_DIRECTORY_HREF, usePeopleProfileSettings } from "@/lib/peopleProfiles";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";
import { useNoticeFor } from "@/lib/useNoticeFor";
import type { Session, Workspace } from "@/types";
import { workspaceMembershipBounds, type WorkspaceGuestDirectoryItem } from "@/workspaceMembershipWire.generated";
import { styles } from "./workspaceSettingsOverviewStyles";
import { EmptyLineReadFailed } from "./SettingsRowNotice";
import { useWorkspacePeople } from "./useWorkspacePeople";
import { WorkspaceGuestList } from "./WorkspaceGuestList";
import { WorkspaceInviteLinkControls } from "./WorkspaceInviteLinkControls";
import { WorkspaceMemberTable } from "./WorkspaceMemberTable";
import { WorkspaceAddMembersDialog, type WorkspaceAddMembersResult } from "./WorkspaceAddMembersDialog";
import { addWorkspaceMembers } from "./workspaceMemberAdds";
import addMembersStyles from "./WorkspaceAddMembersDialog.module.css";
import { workspaceMemberRoleName } from "./WorkspaceMemberRoleMenu";
import { WorkspaceGroupsTab } from "./WorkspaceGroupsTab";
import { WorkspaceRequestsSection } from "./WorkspaceRequestsSection";
import { readEveryWindow } from "@/lib/listWindows";
import { WorkspacePeopleCsvIconButton } from "./WorkspacePeopleCsvExport";
import { SettingsSearchToggle } from "./SettingsSearchToggle";
import { useSidebarLayout } from "@/components/shell/AppShell";
import analyticsStyles from "./WorkspaceAnalytics.module.css";
import groupStyles from "./WorkspaceGroups.module.css";
import phoneCss from "./WorkspacePeoplePhone.module.css";

type PeopleTab = "members" | "guests" | "groups" | "requests";

// Notion's 사람 tabs on a paid plan (Plus seen 2026-11-09, Business
// 2026-11-11): 멤버 n · 게스트 n · 그룹 n · 연락처, the members first and
// open, right under the link row. Only a free Notion workspace puts 게스트
// first (its showGuestTabFirst); Leafen has no plans and works as a workspace
// with no plan limit, so it keeps the paid row. Contacts come from accounts
// Notion connects to elsewhere; Leafen connects to none, so it has no
// contacts tab.
const PEOPLE_TABS: readonly PeopleTab[] = ["members", "guests", "groups"];
// A wider touch screen's are Notion's iPad's (seen 2026-11-16 in iPad Safari
// at 834 and 1210, a paid workspace): 멤버 n · 그룹 n · 게스트 n · 요청 n, a 0
// shown too, the tabs at the page's top and the invite link under them in
// 멤버's own; its 요청 is this tab, not an item of the settings' list.
const TOUCH_PEOPLE_TABS: readonly PeopleTab[] = ["members", "groups", "guests", "requests"];

export function WorkspacePeopleSection({
  initialAddOpen = false,
  initialTab,
  onNavigate,
  onOpenApps,
  onOpenSecurity,
  onWorkspaceRefresh,
  session,
  workspace,
}: {
  initialAddOpen?: boolean;
  /** The tab it opens at: a wider touch screen's "요청" asked for by its address. */
  initialTab?: "requests";
  /** Opens the security settings, from the touch "요청" tab's note. */
  onOpenSecurity?: () => void;
  /** Closes the settings window on another of the app's addresses. */
  onNavigate?(path: string): void;
  /** Opens 서버 → 앱, for a server administrator. */
  onOpenApps?: () => void;
  onWorkspaceRefresh: () => Promise<void>;
  session: Session;
  workspace: Workspace;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { phone, touch } = useSidebarLayout(true);
  const touchTabs = touch && !phone;
  const people = useWorkspacePeople({ onWorkspaceRefresh, session, workspace });
  const [memberQuery, setMemberQuery] = useState("");
  const [memberAddError, setMemberAddError] = useState("");
  const [memberAddOpen, setMemberAddOpen] = useState(initialAddOpen);
  // The addresses the add window opens with: guests being made members.
  const [addInitial, setAddInitial] = useState<string[]>([]);
  const [guestReload, setGuestReload] = useState(0);
  const members = people.snapshot?.members ?? [];
  const invitations = people.snapshot?.invitations ?? [];
  // The people the last add named, and the line saying what it did, held to
  // how they stand — a member, an invitation waiting — so it goes once that is
  // otherwise: an invitation accepted or cancelled, a request let in, a
  // member removed (lib/useNoticeFor).
  const [addedPeople, setAddedPeople] = useState<readonly string[]>([]);
  const addedStanding = JSON.stringify(addedPeople.map((key) => {
    const userId = key.startsWith("user:") ? key.slice("user:".length) : "";
    const email = key.toLowerCase();
    return [
      members.some((member) => userId ? member.userId === userId : member.email.toLowerCase() === email),
      !userId && invitations.some((invitation) => invitation.email.toLowerCase() === email),
    ];
  }));
  const [memberAddNotice, setMemberAddNotice] = useNoticeFor<string>(addedStanding);
  // Imported people map to the owner and the members, not to apps.
  const mappingMembers = useMemo(() => members
    .filter((member) => !member.app && (member.role === "owner" || member.role === "member"))
    .map((member) => ({ userId: member.userId, displayName: member.displayName, email: member.email, role: member.role })), [members]);
  const memberAddPolicy = people.snapshot?.memberAddPolicy ?? "enabled";
  const normalizedMemberQuery = memberQuery.trim().toLowerCase();
  const filteredMembers = useMemo(() => normalizedMemberQuery
    ? members.filter((member) => [
        member.displayName,
        member.email,
        member.userId,
        workspaceMemberRoleName(t, member.role === "owner" ? "owner" : member.restricted ? "restricted" : "member"),
      ].join(" ").toLowerCase().includes(normalizedMemberQuery))
    : members, [members, normalizedMemberQuery, t]);
  const filteredInvitations = normalizedMemberQuery
    ? invitations.filter((invitation) => invitation.email.includes(normalizedMemberQuery))
    : invitations;
  // A search that finds no one keeps the table's head and says so in it, as Notion's (2026-11-19);
  // a phone's list has no head and keeps its own empty line.
  const searchMissedInTable = !phone && normalizedMemberQuery !== "" && !people.loading && people.snapshot !== null
    && filteredMembers.length === 0 && filteredInvitations.length === 0;
  const canManage = people.snapshot?.currentMember.role === "owner";
  // A restricted member sees, as in Notion (seen 2026-10-12), no counts and
  // nothing about adding members: only the people they share a place with
  // are listed to them.
  const restricted = people.snapshot?.currentMember.restricted === true;
  // "사람 디렉토리 보기 ↗" over the link row while the workspace keeps its people list.
  const profiles = usePeopleProfileSettings();
  const directoryShown = Boolean(onNavigate) && Boolean(people.snapshot) && !restricted && profiles?.workspaceId === workspace.id && profiles.settings.directory;
  const memberAddsDisabled = memberAddPolicy === "disabled" || (!canManage && people.snapshot?.memberRequests !== true);
  const memberAddsEnabled = !memberAddsDisabled;
  const memberAddsReason = t(canManage ? "memberAddDisabledNotice" : "memberRequestNotAllowed");
  const memberAddsReasonId = useId();
  // An app's own workspace belongs to the app's account, which signs in
  // nowhere: its members are the people allowed to use the app, chosen at
  // 설정 → 서버 → 앱, so nobody adds or asks to add anyone here.
  const appManaged = Boolean(workspace.appInstallId);
  const reasonShown = !restricted && (appManaged || Boolean(people.snapshot && memberAddsDisabled));
  const currentError = memberAddError || people.error;
  const [chosenTab, setTab] = useState<PeopleTab | null>(touchTabs ? initialTab ?? null : null);
  const tab = chosenTab ?? "members";
  const [guestCount, setGuestCount] = useState<number | null>(null);
  const [groupCount, setGroupCount] = useState<number | null>(null);
  const [requestCount, setRequestCount] = useState<number | null>(null);
  const [groupCreateOpen, setGroupCreateOpen] = useState(false);
  const tabListRef = useRef<HTMLDivElement>(null);
  // The guests' and the groups' counts show on their tabs before they open,
  // as Notion's "게스트 n" and "그룹 n", and follow who joins and what is
  // made while one is open.
  const accessRevision = useAccessRevision();
  useEffect(() => {
    const controller = new AbortController();
    api.listWorkspaceGuests(session, workspace.id, "", controller.signal)
      .then((directory) => setGuestCount(directory.total), () => undefined);
    api.listWorkspaceGroups(session, workspace.id, controller.signal)
      .then((list) => setGroupCount(list.groups.length), () => undefined);
    if (touchTabs) void readEveryWindow(async (cursor) => {
      const page = await api.listAccessRequests(session, workspace.id, controller.signal, cursor);
      return { items: page.requests, nextCursor: page.nextCursor };
    }, controller.signal).then((list) => setRequestCount(list.length), () => undefined);
    return () => controller.abort();
  }, [accessRevision, session, touchTabs, workspace.id]);
  // A tab's count, as Notion's: none at 0, and none at all to a restricted
  // member (seen 2026-11-05).
  const tabCount = (item: PeopleTab) => restricted ? 0
    : item === "members" ? members.length : (item === "guests" ? guestCount : item === "groups" ? groupCount : requestCount) ?? 0;
  const countShown = (item: PeopleTab) => !restricted && (touchTabs || tabCount(item) > 0);
  const tabName = (item: PeopleTab) => t(item === "members" ? "peopleTabMembers" : item === "guests" ? "peopleTabGuests"
    : item === "groups" ? "peopleTabGroups" : "navRequests");
  const tabLabel = (item: PeopleTab) => countShown(item) ? `${tabName(item)} ${tabCount(item)}` : tabName(item);
  // A member has no 요청, as Notion's member's settings (seen 2026-11-21).
  const peopleTabs = touchTabs ? TOUCH_PEOPLE_TABS.filter((item) => item !== "requests" || canManage) : PEOPLE_TABS;
  const tabList = useNativeTabs<PeopleTab>({
    tabs: peopleTabs.map((item) => ({ key: item, text: tabLabel(item) })),
    selected: tab, onSelect: setTab, label: t("navWorkspaceMembers"), listRef: tabListRef,
  });

  // Making guests members is Leafen's own member add, with no plan to
  // upgrade to: accounts join at once; a guest with no account opens the add
  // window with their addresses.
  async function upgradeGuests(guests: WorkspaceGuestDirectoryItem[]) {
    setMemberAddError("");
    setMemberAddNotice("");
    if (guests.some((guest) => !guest.userId)) {
      setAddInitial(guests.map((guest) => guest.email));
      setMemberAddOpen(true);
      return;
    }
    for (const guest of guests) {
      const result = await people.addMemberById(guest.userId);
      if ("failed" in result && result.failed) {
        setMemberAddError(result.failed);
        break;
      }
    }
    setGuestReload((value) => value + 1);
  }

  async function addMembers(emails: string[], message: string, picked: readonly { userId: string }[] = [], restricted = false): Promise<WorkspaceAddMembersResult> {
    setMemberAddError("");
    setMemberAddNotice("");
    const result = await addWorkspaceMembers(people.addMember, t, emails, message, picked, people.addMemberById, restricted);
    setAddedPeople(result.added);
    if (result.notice) setMemberAddNotice(result.notice);
    return { added: result.added, error: result.error };
  }

  // The settings window's place at the end of the title's line.
  const sectionRef = useRef<HTMLElement>(null);
  const [titleAction, setTitleAction] = useState<HTMLElement | null>(null);
  useEffect(() => {
    setTitleAction(sectionRef.current?.closest("[data-settings-content]")?.querySelector<HTMLElement>("[data-settings-title-action]") ?? null);
  }, []);

  // What it says and whether it adds or asks are the member read's: none
  // until that read is in (2026-11-20: an owner saw "멤버 추가 요청" after it failed).
  const addButton = !people.snapshot || restricted || appManaged ? null : <button
    type="button"
    className={addMembersStyles.trigger}
    aria-haspopup="dialog"
    aria-expanded={memberAddsEnabled && memberAddOpen}
    disabled={!memberAddsEnabled}
    // An add that can't be used says why at the button too (2026-10-16).
    title={memberAddsDisabled ? memberAddsReason : undefined}
    aria-describedby={reasonShown ? memberAddsReasonId : undefined}
    data-add-members-open
    // A phone's leads the screen across its width, as Notion's phone "멤버 추가" over its invite link (seen 2026-11-12).
    data-phone-wide={phone ? "true" : undefined}
    onClick={() => { setAddInitial([]); setMemberAddOpen(true); }}
  >
    {/* A member's opens their request, its button "멤버 추가" as Notion's member's (seen 2026-11-21). */}
    {t(phone ? "memberPhoneAdd" : canManage ? "addMembers" : "memberRequestOpen")}
  </button>;

  const directoryLink = (
    <a className={styles.peopleDirectoryLink} href={PEOPLE_DIRECTORY_HREF} data-people-directory-link
      onClick={(event) => {
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        onNavigate?.(PEOPLE_DIRECTORY_HREF);
      }}>
      {t("peopleDirectoryOpen")}
      <OpenInNew size="small" aria-hidden="true" />
    </a>
  );

  const tabActions = tab === "groups" ? (canManage ? (
    <button type="button" className={addMembersStyles.trigger} data-group-create onClick={() => setGroupCreateOpen(true)}>
      {t("groupCreate")}
    </button>
  ) : null) : <>
    <SettingsSearchToggle query={memberQuery} onQueryChange={setMemberQuery} placeholder={t("searchPeoplePlaceholder")}
      data={{ "data-people-search": "" }} />
    {canManage ? <WorkspacePeopleCsvIconButton session={session} workspace={workspace} className={groupStyles.iconButton} /> : null}
    {phone ? null : addButton}
  </>;

  return (
    <section
      ref={sectionRef}
      id="people"
      className={styles.section}
      aria-label={t("navWorkspaceMembers")}
      aria-busy={people.loading || people.busyActions.size > 0}
    >

      {currentError ? <div className={styles.notice} role="alert">{currentError}</div> : null}

      {memberAddNotice && !currentError ? (
        <div className={styles.notice} data-tone="neutral" role="status">{memberAddNotice}</div>
      ) : null}

      {reasonShown ? (
        <div id={memberAddsReasonId} className={styles.notice} data-tone="neutral" data-app-workspace-members={appManaged ? "" : undefined}>
          {appManaged ? t(onOpenApps ? "appWorkspaceMembersAdmin" : "appWorkspaceMembers") : memberAddsReason}
          {appManaged && onOpenApps ? <>
            {" "}
            <button type="button" className={styles.noticeLink} data-app-workspace-members-open onClick={onOpenApps}>
              {t("appWorkspaceMembersOpen")}
            </button>
          </> : null}
        </div>
      ) : null}

      {/* In the settings window the link sits at the end of the title's line, as Notion's (seen 2026-11-07); elsewhere on a line of its own.
          A phone's members screen has none, as Notion's phone (seen 2026-11-16 at 402): the directory opens from the library and the sidebar there. */}
      {directoryShown && !phone ? (titleAction ? createPortal(directoryLink, titleAction)
        : <div className={styles.peopleDirectoryRow}>{directoryLink}</div>) : null}

      {phone && addButton ? <div className={phoneCss.addBlock}>{addButton}</div> : null}

      {canManage && !touchTabs ? <WorkspaceInviteLinkControls session={session} workspace={workspace} phone={phone} /> : null}
      {phone && canManage ? <div className={phoneCss.parting} aria-hidden="true" /> : null}

      <div className={groupStyles.tabRow}>
        <div {...tabList.tabListProps} ref={tabListRef} className={analyticsStyles.tabs}>
          {peopleTabs.map((item) => (
            <NativeTab key={item} state={tabList.state} tabKey={item} data-people-tab={item} className={analyticsStyles.tab}>
              {tabName(item)}
              {countShown(item) ? <span className={analyticsStyles.tabCount}>{tabCount(item)}</span> : null}
            </NativeTab>
          ))}
        </div>
        {touchTabs ? null : <div className={groupStyles.tabActions}>{tabActions}</div>}
      </div>

      {touchTabs && tab === "members" && canManage ? <WorkspaceInviteLinkControls session={session} workspace={workspace} phone={phone} /> : null}
      {/* A wider touch screen's search and add stand in their own line over the list, as Notion's iPad's over its table. */}
      {touchTabs && tab !== "requests" ? <div className={phoneCss.touchActions}>{tabActions}</div> : null}
      {tab === "requests" ? (
        <WorkspaceRequestsSection session={session} workspace={workspace} onOpenApps={onOpenApps} touchTab onOpenSecurity={onOpenSecurity} />
      ) : tab === "groups" ? (
        <WorkspaceGroupsTab session={session} workspace={workspace} members={members} canManage={canManage}
          createOpen={groupCreateOpen} onCreateOpenChange={setGroupCreateOpen} onCount={setGroupCount} />
      ) : <>
      {tab === "guests" ? (
        <WorkspaceGuestList key={guestReload} session={session} workspace={workspace} query={memberQuery}
          canManage={canManage && !appManaged} onCount={setGuestCount}
          onUpgrade={canManage && memberAddsEnabled && !appManaged ? (guests) => void upgradeGuests(guests) : undefined} />
      ) : <>

      {/* Invitations are mailed, so a workspace keeps a hundred waiting at
          a time; nearing that, the list says so and how to make room. */}
      {canManage && invitations.length >= workspaceMembershipBounds.memberInvitations - 10 ? (
        <p className={styles.organizationMeta} data-member-invitations-capacity={
          invitations.length >= workspaceMembershipBounds.memberInvitations ? "full" : "near"}>
          {t(invitations.length >= workspaceMembershipBounds.memberInvitations ? "invitationsFull" : "invitationsNear",
            { count: invitations.length, limit: workspaceMembershipBounds.memberInvitations })}
        </p>
      ) : null}
      {/* A failed read is said where the people show, with a way to read again — never as no members. */}
      {people.readFailed ? <EmptyLineReadFailed onRetry={people.readAgain} data-people-read-failed>{people.readFailed}</EmptyLineReadFailed> : null}
      {filteredMembers.length > 0 || filteredInvitations.length > 0 || searchMissedInTable ? (
        <WorkspaceMemberTable members={filteredMembers} invitations={filteredInvitations} session={session} workspace={workspace}
          emptyText={t("noSearchResults")}
          canManage={canManage} restrictable={organizationModule()?.restrictMember !== undefined} busyActions={people.busyActions}
          onMakeOwner={(userId) => void people.transferOwner(userId)}
          onRestrict={(userId, restricted) => void people.restrictMember(userId, restricted)}
          onRemove={(userId) => void people.removeMember(userId)}
          onCancelInvitation={(invitationId) => {
            setMemberAddNotice("");
            void people.cancelInvitation(invitationId);
          }} />
      ) : null}
      {people.loading && members.length === 0 && !people.readFailed ? (
        <div className={styles.emptyState}>{t("loadingMembers")}</div>
      ) : null}

      {!people.loading && people.snapshot && filteredMembers.length === 0 && filteredInvitations.length === 0 && !searchMissedInTable ? (
        <div className={styles.emptyState}>
          {normalizedMemberQuery ? t("noSearchResults") : t("noMembers")}
        </div>
      ) : null}
      </>}

      {people.snapshot && memberAddsEnabled && memberAddOpen && !appManaged ? (
        <WorkspaceAddMembersDialog session={session} workspace={workspace} request={!canManage}
          members={members.filter((member) => !member.app)} invitations={invitations}
          busy={people.busyActions.has("addMember")} onAdd={addMembers} initialEmails={addInitial}
          onClose={() => { setMemberAddOpen(false); setAddInitial([]); setGuestReload((value) => value + 1); }} />
      ) : null}

      {/* A member maps the imported people of the pages they may change; the owner, the workspace's. */}
      {(canManage || (people.snapshot?.currentMember.role === "member" && !restricted)) && tab === "members" ? <ImportedPeopleMapping
        bounds={people.importedPeopleBounds}
        members={mappingMembers}
        ownPagesOnly={!canManage}
        service={people.importedPeopleService}
        workspaceId={workspace.id}
      /> : null}
      </>}
    </section>
  );
}
