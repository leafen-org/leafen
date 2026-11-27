import { libraryOpeningView } from "@/components/library/libraryViewOrder";
import type { DatabaseFavoriteProjection } from "@/workspaceMembershipWire.generated";
import type { AddRootPageDetails } from "@/lib/databaseStarterCreation";
import type { DatabaseStarterSourceLoader } from "@/components/database/DatabaseStarterGallery";
import type { ProductStatusSink } from "@/lib/productStatus";
import { Suspense, useCallback, useEffect, useRef, useState, useSyncExternalStore, type PointerEvent as ReactPointerEvent } from "react";
import { signedInLinkNoticeShown, subscribeSignedInLinkNotice } from "@/lib/signedInLinkNoticeShown";
import { usePhoneInboxGoesWithDrawer } from "./usePhoneInboxGoesWithDrawer";
import { settingsWindowStore, trashPanelStore } from "@/lib/productWindows";
import { lazyScreen } from "@/lib/lazyScreen";
import { useTranslation } from "react-i18next";
import type { PageDuplicationOptions } from "@/lib/pageDuplicationService";
import type { NativePageMetadataChange } from "@/lib/nativePageMetadata";
import type { PendingSignOutPrompt } from "@/lib/productSignOut";
import { developerToolsHref, HOME_PATH, pushRoute, replaceRoute, usePathname, useSearchParams, type LibraryViewId } from "@/lib/router";
import { useAppPlatformAvailable, useAppPlatformMissing } from "@/lib/appPlatformAvailability";
import type { Page, Session, Workspace } from "@/types";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import type { SettingsSection } from "@/components/settings/settingsWindowModel";
import { orderTeamspaces } from "@/components/teamspace/teamspaceDialogModel";
import type { TeamspaceDialogView } from "@/components/TeamspaceDialog";
import type { TeamspaceSettingsTab } from "@/components/teamspace/TeamspaceSettingsPanel";
import type { WorkspaceCreateChoice } from "@/components/WorkspaceCreateDialog";
import { listJoinableWorkspaces, type NoWorkspaceOffer } from "@/lib/workspaceAllowedEmailDomains";
import { AddPageWindowHost } from "./AddPageWindow";
import { openAddPageWindow } from "./addPageWindowStore";
import { SidebarFooter } from "./SidebarFooter";
import { SidebarPhoneFoot, SidebarPhoneNavigation, SidebarPhoneTabBar } from "./SidebarPhoneNavigation";
import { useSidebarLayout as useShellLayout } from "@/components/shell/AppShell";
import { SidebarHeader } from "./SidebarHeader";
import { SidebarPageSections } from "./SidebarPageSections";
import { SidebarTopActions, type SidebarNewChoices } from "./SidebarTopActions";
import { useSidebarHomeRow } from "./useSidebarHomeRow";
import { PageTreeProvider } from "./pageTreeContext";
import { SIDEBAR_HOME_TAB, addSidebarPreset } from "./sidebarLayoutModel";
import { useSidebarPresetChoices } from "./useSidebarPresetChoices";
import { openSidebarShortcut, type SidebarShortcutActions } from "./sidebarShortcutOpen";
import { styles } from "./sidebarStyles";
import { useSidebarInbox } from "./useSidebarInbox";
import { useSidebarLayout } from "./useSidebarLayout";
import { useSidebarTeamspaces } from "./useSidebarTeamspaces";
import { useSidebarTeamspaceActions } from "./useSidebarTeamspaceActions";
import { useServerProfiles } from "./useServerProfiles";
import { useWorkspaceNumberKeys } from "./useWorkspaceNumberKeys";
import { useWorkspaceMembershipAsk } from "./WorkspaceMembershipRequest";
import { dropHeldWorkspaceFile, onWorkspaceCreateRequest } from "@/lib/openWorkspace";
import { takeWorkspaceCreateRequest } from "@/lib/profileRegistry";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";


export type SidebarProps = {
  session: Session;
  workspace: Workspace | null;
  workspaces: Workspace[];
  /** The list has come: until then no workspace is not yet "none". */
  workspacesShown: boolean;
  /** The list has been read: an empty one means none. */
  noWorkspaceOffer: NoWorkspaceOffer | null;
  pages: Page[];
  /** Pages in the trash with the page above them, for the favorites they keep. */
  trashedPages?: readonly Page[];
  /** The pages of the reader's Shared section and the pages under them. */
  sharedPageIds: readonly string[];
  workspaceWidePageIds: readonly string[];
  favoritePageIds: readonly string[];
  favoriteDatabases?: readonly DatabaseFavoriteProjection[];
  selectedPageId: string | null;
  trashActive: boolean;
  creatingPage: boolean;
  collapsed: boolean;
  mobile: boolean;
  open: boolean;
  resizing: boolean;
  width: number;
  minWidth: number;
  maxWidth: number;
  /** Makes a page and opens it; "새로" asks for it in a new tab in the Mac and Windows apps. */
  onCreatePage: (teamspaceId?: string, options?: { inNewTab?: boolean }) => void;
  /** Makes a page at the top of the private pages (or a teamspace's) without opening it; a database starts as one. */
  loadDatabaseSources?: DatabaseStarterSourceLoader;
  onCreateRootPage: (database: boolean, place?: AddRootPageDetails) => Promise<Page | null>;
  onCreateSubpage: (parentId: string, workspaceId?: string) => Promise<Page | null>;
  onDuplicatePage: (page: Page, signal: AbortSignal, options?: PageDuplicationOptions) => Promise<Page>;
  onFavoriteChange: (pageId: string, favorite: boolean) => Promise<void>;
  onPageChange: (page: Page) => void;
  onPageMetadataChange: (
    page: Page,
    change: NativePageMetadataChange,
    signal: AbortSignal,
  ) => Promise<boolean | Page | void>;
  onTransferBlock: (input: {
    blockId: string;
    copy: boolean;
    destinationPageId: string;
    signal: AbortSignal;
    sourcePageId: string;
  }) => Promise<void>;
  onStatus: ProductStatusSink;
  onCreateWorkspace: (input: {
    workspaceId: string;
    name: string;
    choice: WorkspaceCreateChoice;
  }) => Promise<Workspace>;
  /** Joins a workspace open to the person's email domain and opens it. */
  onJoinWorkspace: (workspaceId: string) => Promise<void>;
  onWorkspaceCreateOpenChange: (open: boolean) => void;
  onTeamspaceDialogOpenChange: (open: boolean) => void;
  /** Opens the settings window, at its people with their invitation for "멤버 초대". */
  onOpenSettings: (section?: SettingsSection, options?: { invite?: boolean; part?: string }) => void;
  onOpenHome: () => void;
  /** Opens a page here or in a new tab; here, `replace` puts it in the place of the page shown in the history. */
  onOpenPage: (page: Page, newTab: boolean, replace?: boolean) => void;
  onOpenSearch: () => void;
  onOpenTrash: () => void;
  /** Opens the library at one of its views. */
  onOpenLibrary: (view: LibraryViewId) => void;
  onResizeNudge: (delta: number) => void;
  onResizeReset: () => void;
  onResizeStart: (event: ReactPointerEvent<HTMLDivElement>) => void;
  onSelectWorkspace: (workspace: Workspace) => void;
  /** ask: asks even with nothing waiting, as an account with no way back does. */
  onSignOut: (after?: () => void, ask?: boolean) => void;
  onToggle: () => void;
  pendingSignOut: PendingSignOutPrompt | null;
};

const WorkspaceCreateDialog = lazyScreen(() =>
  import("@/components/WorkspaceCreateDialog").then(({ WorkspaceCreateDialog }) => ({
    default: WorkspaceCreateDialog,
  })), { typedOnOpen: true }
);


const TeamspaceDialog = lazyScreen(() =>
  import("@/components/TeamspaceDialog").then(({ TeamspaceDialog }) => ({
    default: TeamspaceDialog,
  })),
);

const WorkspaceInviteDialog = lazyScreen(() =>
  import("@/components/settings/WorkspaceInviteDialog").then(({ WorkspaceInviteDialog }) => ({
    default: WorkspaceInviteDialog,
  })),
);

const UpdatesPanel = lazyScreen(() =>
  import("@/components/UpdatesPanel").then(({ UpdatesPanel }) => ({
    default: UpdatesPanel,
  })),
);

export function Sidebar({
  session,
  workspace,
  workspaces,
  workspacesShown,
  noWorkspaceOffer: offeredNoWorkspace,
  pages,
  trashedPages,
  sharedPageIds,
  workspaceWidePageIds,
  favoritePageIds,
  favoriteDatabases,
  selectedPageId,
  trashActive: trashAddressed,
  creatingPage,
  collapsed,
  mobile,
  open,
  resizing,
  width,
  minWidth,
  maxWidth,
  onCreatePage,
  onCreateRootPage,
  loadDatabaseSources,
  onCreateSubpage,
  onDuplicatePage,
  onFavoriteChange,
  onPageChange,
  onPageMetadataChange,
  onTransferBlock,
  onStatus,
  onCreateWorkspace,
  onJoinWorkspace,
  onWorkspaceCreateOpenChange,
  onTeamspaceDialogOpenChange,
  onOpenSettings,
  onOpenHome,
  onOpenPage,
  onOpenSearch,
  onOpenTrash,
  onOpenLibrary,
  onResizeNudge,
  onResizeReset,
  onResizeStart,
  onSelectWorkspace,
  onSignOut,
  onToggle,
  pendingSignOut,
}: SidebarProps) {
  const { t } = useTranslation("sidebar");
  const interactive = open && !collapsed;
  // The settings and trash windows open in lib/productWindows.ts: the sidebar
  // follows them here, drawn again only where they change it — an offer to
  // make a workspace waits out the settings, the trash takes the page's mark.
  const noWorkspaceOffer = useSyncExternalStore(settingsWindowStore.subscribe, () => settingsWindowStore.get() ? null : offeredNoWorkspace);
  const trashActive = useSyncExternalStore(trashPanelStore.subscribe, () => trashAddressed || trashPanelStore.get() !== null);
  const [workspaceCreateOpen, setWorkspaceCreateOpen] = useState(false);
  const [teamspaceDialogOpen, setTeamspaceDialogOpen] = useState(false);
  const [inviteOpen, setInviteOpen] = useState(false);
  const openPeopleFromInvite = useCallback(() => {
    setInviteOpen(false);
    onOpenSettings("people");
  }, [onOpenSettings]);
  const [teamspaceDialogView, setTeamspaceDialogView] = useState<TeamspaceDialogView>("manage");
  const [teamspaceDialogTargetId, setTeamspaceDialogTargetId] = useState<string | undefined>();
  const [teamspaceDialogInvite, setTeamspaceDialogInvite] = useState(false);
  const [teamspaceDialogTab, setTeamspaceDialogTab] = useState<TeamspaceSettingsTab | undefined>();
  const inbox = useSidebarInbox({ session, workspaceId: workspace?.id, mobile, collapsed });
  const setUpdatesOpen = inbox.setOpen;
  const sidebarTeamspaces = useSidebarTeamspaces({ session, workspace });
  const teamspaceActions = useSidebarTeamspaceActions({ session, workspace, teamspaces: sidebarTeamspaces.joinedTeamspaces,
    onChanged: sidebarTeamspaces.refresh, onOpenSettings: (teamspaceId) => openTeamspaceDialog("manage", teamspaceId) });
  const sidebarLayout = useSidebarLayout(session, workspace?.id ?? null);
  const [customizing, setCustomizing] = useState(false);
  const presetChoices = useSidebarPresetChoices(session, workspace?.id ?? null, customizing, sidebarLayout.layout);
  // A guest's sidebar holds only the pages shared with them, as in Notion.
  const guest = workspace?.guest === true;
  const membershipAsk = useWorkspaceMembershipAsk(session, guest && workspace ? workspace.id : null);

  useEffect(() => {
    if (sidebarTeamspaces.error) onStatus(sidebarTeamspaces.error, "error");
  }, [onStatus, sidebarTeamspaces.error]);

  useEffect(() => {
    if (workspace) return;
    setTeamspaceDialogOpen(false);
    onTeamspaceDialogOpenChange(false);
  }, [onTeamspaceDialogOpenChange, workspace]);

  function changeWorkspaceCreateOpen(next: boolean) {
    if (!next) openedByOffer.current = false;
    setWorkspaceCreateOpen(next);
    onWorkspaceCreateOpenChange(next);
  }

  const loadJoinable = useCallback((signal: AbortSignal) => listJoinableWorkspaces(session, signal), [session]);
  // Someone without a workspace, as after leaving their last one, is asked
  // to make or join one, as Notion asks at sign-up. Where they may not make
  // one the page says so, and only a workspace open to them asks.
  // Each offer asks once; while the settings window shows there is none yet.
  const askedOffer = useRef<NoWorkspaceOffer | null>(null);
  // The window this offer opened, not one the person opened: it closes once
  // a workspace comes (2026-11-06: a guest made a member found it still open).
  const openedByOffer = useRef(false);
  // Not over the notice of a link this browser didn't use: it asks after.
  const linkNotice = useSyncExternalStore(subscribeSignedInLinkNotice, signedInLinkNoticeShown);
  useEffect(() => {
    if (linkNotice || !noWorkspaceOffer || askedOffer.current === noWorkspaceOffer || window.location.pathname !== "/") return;
    askedOffer.current = noWorkspaceOffer;
    if (!noWorkspaceOffer.creationRestricted || noWorkspaceOffer.joinable) {
      changeWorkspaceCreateOpen(true);
      openedByOffer.current = true;
    }
  }, [linkNotice, noWorkspaceOffer]);
  useEffect(() => {
    if (noWorkspaceOffer || !openedByOffer.current) return;
    changeWorkspaceCreateOpen(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only as the offer ends
  }, [noWorkspaceOffer]);
  // What a request asked the new workspace to start from, chosen as the window opens.
  const [createStart, setCreateStart] = useState<WorkspaceCreateChoice | undefined>(undefined);
  useEffect(() => onWorkspaceCreateRequest((start) => {
    setCreateStart(start);
    changeWorkspaceCreateOpen(true);
  }));
  // Another account's "+ New workspace" in the workspace menu came here to start one.
  useEffect(() => {
    if (takeWorkspaceCreateRequest()) changeWorkspaceCreateOpen(true);
    // Once, as the page starts.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function changeTeamspaceDialogOpen(next: boolean) {
    setTeamspaceDialogOpen(next);
    onTeamspaceDialogOpenChange(next);
    if (!next) sidebarTeamspaces.refresh();
  }

  function openTeamspaceDialog(view: TeamspaceDialogView, teamspaceId?: string, invite = false, tab?: TeamspaceSettingsTab) {
    setTeamspaceDialogView(view);
    setTeamspaceDialogTargetId(teamspaceId);
    setTeamspaceDialogInvite(invite);
    setTeamspaceDialogTab(tab);
    changeTeamspaceDialogOpen(true);
  }

  // A notice about a teamspace (a join request, its approval) opens its
  // settings, at the tab the notice names; the address then lets go of it.
  const searchParams = useSearchParams();
  const askedTeamspaceSettings = searchParams.get("teamspace-settings");
  useEffect(() => {
    if (!askedTeamspaceSettings || !workspace) return;
    const tab = searchParams.get("teamspace-tab");
    openTeamspaceDialog("manage", askedTeamspaceSettings, false, tab === "members" || tab === "security" ? tab : undefined);
    const url = new URL(window.location.href);
    url.searchParams.delete("teamspace-settings");
    url.searchParams.delete("teamspace-tab");
    replaceRoute(`${url.pathname}${url.search}${url.hash}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [askedTeamspaceSettings, workspace?.id]);

  // A workspace's optional preset, as "섹션 추가" offers it: its sections and
  // shortcuts on the list, and a note that it was added, as Notion's "탭이 추가됨" (seen 2026-11-09).
  function addPreset(id: string) {
    const preset = presetChoices.find((candidate) => candidate.id === id);
    if (!preset) return;
    sidebarLayout.change(addSidebarPreset(sidebarLayout.layout, preset));
    onStatus(t("presetAdded", { name: preset.name }), "default");
  }

  function openMyTasks() {
    setUpdatesOpen(false);
    pushRoute("/my-tasks");
  }

  const shortcutActions: SidebarShortcutActions = {
    session,
    workspaceId: workspace?.id,
    pages,
    openSettings: () => onOpenSettings(),
    openLibrary: () => { if (workspace) onOpenLibrary(libraryOpeningView(session.user.id, workspace)); },
    openDocuments: () => onOpenLibrary("documents"),
    openMyTasks,
    openPage: (page) => onOpenPage(page, false),
    createRootPage: onCreateRootPage,
    createSubpage: onCreateSubpage,
  };

  // The sidebar takes no Escape of its own, a phone's drawer or a narrow window's beside the page: Notion's
  // stays open under it with nothing open over it (seen 2026-10-29 and 2026-11-16 in iPhone Safari; and
  // 2026-11-17 in a 606px Chrome window, the narrowest Chrome draws, two tabs: the focus on its toggle, a
  // section's head or the workspace switcher); a menu or sheet opened from it closes alone (escapeTopSheet).

  const profiles = useServerProfiles({ session, workspaces, workspacesShown });
  const { phone, touch } = useShellLayout();
  const homeRow = useSidebarHomeRow(session.user.id, workspace, workspacesShown, phone);
  const pathname = usePathname();
  const appPlatformHere = useAppPlatformAvailable();
  const appPlatformMissing = useAppPlatformMissing();
  const appPlatform = appPlatformHere || appPlatformMissing !== "";
  // On a phone what opens a place closes the drawer over it (a page's foot has none open).
  const thenClose = (run: () => void) => () => {
    setUpdatesOpen(false);
    run();
    if (phone && open) onToggle();
  };
  // A section's last "+" row opens the add window; on a phone it makes the
  // page at once and closes the drawer — Notion's (its sidebar code, read 2026-10-31).
  const addFromEndRow = (teamspaceId?: string) => {
    if (phone) thenClose(() => onCreatePage(teamspaceId))();
    else openAddPageWindow({ place: teamspaceId ? { kind: "teamspace", teamspaceId } : { kind: "private" }, mode: "page" });
  };
  // A guest makes nothing here: "새로" asks to become a member where the
  // workspace takes such requests, as Notion's Plus workspace's guest (seen
  // 2026-11-12), and otherwise starts a workspace of their own, as Notion's
  // free workspace's guest (seen 2026-11-05).
  const newChoices: SidebarNewChoices = guest && membershipAsk.asks ? { creating: false, onAsk: thenClose(membershipAsk.open) } : guest ? {
    creating: false,
    onPage: thenClose(() => changeWorkspaceCreateOpen(true)),
    onDatabase: thenClose(() => changeWorkspaceCreateOpen(true)),
  } : {
    creating: creatingPage,
    onPage: thenClose(() => onCreatePage(undefined, { inNewTab: true })),
    onDatabase: () => openAddPageWindow({ place: { kind: "private" }, mode: "database" }),
  };
  // A phone drawer's "알림과 설정", "멤버" and "휴지통" open over the drawer,
  // there again at the same scroll as they close — Notion's (seen 2026-11-14
  // and 2026-11-16 in iPhone Safari: "‹", "완료" and Escape come back to the
  // drawer open).
  const overDrawer = (run: () => void) => () => {
    setUpdatesOpen(false);
    run();
  };
  // Its search, from its tab bar or its inbox's foot, opens over them and goes back
  // to them as it is cancelled; a result opened takes them along (usePhoneInboxGoesWithDrawer.ts).
  const openSearch = phone ? onOpenSearch : thenClose(onOpenSearch);
  usePhoneInboxGoesWithDrawer(phone, pathname, () => setUpdatesOpen(false));
  const footPlaces = {
    onOpenLibrary: workspace ? thenClose(() => onOpenLibrary(libraryOpeningView(session.user.id, workspace))) : undefined,
    onOpenApps: appPlatform ? thenClose(() => pushRoute("/apps")) : undefined,
    onOpenTrash: guest ? undefined : phone ? overDrawer(onOpenTrash) : thenClose(onOpenTrash),
    onOpenSettings: phone ? overDrawer(() => onOpenSettings("notifications")) : thenClose(() => onOpenSettings()),
  };
  useWorkspaceNumberKeys({
    enabled: profiles.packaged, workspaces, others: profiles.others, onSelectWorkspace, openProfile: profiles.open,
  });

  const switcher = (
      <SidebarHeader
        session={session}
        workspace={workspace}
        workspaces={workspaces}
        workspacesShown={workspacesShown}
        profiles={profiles}
        guest={guest}
        canInviteMembers={workspace !== null && !guest}
        onCreateWorkspace={() => changeWorkspaceCreateOpen(true)}
        onOpenSettings={() => onOpenSettings()}
        onInviteMembers={() => setInviteOpen(true)}
        onOpenPeople={() => onOpenSettings("people")}
        onSelectWorkspace={onSelectWorkspace}
        onSignOut={onSignOut}
        pendingSignOut={pendingSignOut}
      />
  );

  return (
    <>
      <aside
      className={styles.sidebar}
      data-app-sidebar="true"
      data-mobile={mobile}
      data-phone={phone ? "true" : undefined}
      data-open={open}
      data-collapsed={collapsed ? "true" : undefined}
      data-resizing={resizing ? "true" : undefined}
      data-sections-ready={sidebarLayout.loaded && sidebarTeamspaces.loaded ? "true" : undefined}
      aria-label={t("sidebar")}
      aria-hidden={interactive ? undefined : true}
      inert={interactive ? undefined : true}
    >
      <SidebarTopActions
        switcher={switcher}
        phone={phone}
        homeActive={(pathname === "/" || pathname === HOME_PATH) && !inbox.active}
        inboxActive={inbox.active}
        inboxDisabled={workspace === null}
        inboxUnread={inbox.unread}
        newChoices={newChoices}
        onOpenHome={homeRow ? thenClose(onOpenHome) : undefined}
        onOpenInbox={() => setUpdatesOpen((current) => !current)}
        onOpenSearch={openSearch}
        onToggle={onToggle}
      />
      {inbox.rendered && workspace ? (
        <Suspense fallback={mobile ? <div className={styles.inboxLoading} aria-hidden="true" /> : null}>
          <ErrorBoundary scope="sidebar-inbox">
            <UpdatesPanel
              exiting={inbox.exiting}
              left={width}
              onClose={() => setUpdatesOpen(false)}
              phoneFoot={phone ? <SidebarPhoneFoot newChoices={newChoices} onOpenSearch={openSearch} /> : undefined}
              pinned={mobile || touch ? null : inbox.pinned}
              onPinnedChange={inbox.setPinned}
              width={inbox.width}
              onWidthChange={inbox.setWidth}
              pages={pages}
              placement={phone ? "phone" : mobile ? "sidebar-inline" : "sidebar"} touch={touch}
              session={session}
              workspaceId={workspace.id} workspaceName={workspace.name}
              workspaceIcon={workspace.icon}
            />
          </ErrorBoundary>
        </Suspense>
      ) : null}
      {inbox.rendered && workspace && mobile && !phone ? null : (
        <>
          <PageTreeProvider value={{
            pages,
            trashedPages,
            canCreateRoot: !guest,
            favoritePageIds,
            selectedPageId: trashActive ? null : selectedPageId,
            session,
            // A page opened or made from the sidebar closes the inbox, pinned
            // or not, as Notion's (seen 2026-11-06); one opened in a new tab leaves it.
            onCreateSubpage: (parentId, workspaceId) => { setUpdatesOpen(false); return onCreateSubpage(parentId, workspaceId); },
            onDuplicatePage,
            onFavoriteChange,
            onOpenPage: (page, newTab) => { if (!newTab) setUpdatesOpen(false); onOpenPage(page, newTab); },
            onPageChange,
            onPageMetadataChange,
            onStatus,
            onTransferBlock,
          }}>
            <SidebarPageSections
              phone={phone}
              footer={phone ? <SidebarPhoneNavigation {...footPlaces}
                onOpenMembers={workspace && !guest ? overDrawer(() => onOpenSettings("people")) : undefined} /> : undefined}
              session={session}
              guest={guest}
              guestPrivate={workspace?.pendingPrivatePages === true} privateAdds={workspace?.noPrivatePages !== true}
              pages={pages}
              trashedPages={trashedPages}
              sharedPageIds={sharedPageIds}
              workspaceWidePageIds={workspaceWidePageIds}
              favoritePageIds={favoritePageIds}
              favoriteDatabases={favoriteDatabases}
              layout={sidebarLayout.layout}
              tab={SIDEBAR_HOME_TAB}
              onLayoutChange={sidebarLayout.change}
              onOpenShortcut={(shortcut) => void openSidebarShortcut(shortcut, shortcutActions)}
              onOpenLibrary={onOpenLibrary}
              onOpenDeveloperTools={(tab) => thenClose(() => pushRoute(developerToolsHref(tab)))()}
              workspaceId={workspace?.id ?? null}
              presets={presetChoices}
              onAddPreset={addPreset}
              customizing={customizing}
              onCustomize={() => setCustomizing(true)}
              onCustomizeDone={() => setCustomizing(false)}
              creatingPage={creatingPage}
              joinedTeamspaces={orderTeamspaces(sidebarTeamspaces.joinedTeamspaces, sidebarLayout.layout.teamspaceOrder)}
              teamspaceBrowseCount={sidebarTeamspaces.browseCount}
              canCreateTeamspace={sidebarTeamspaces.canCreateTeamspace}
              onCreatePage={() => addFromEndRow()}
              onAddPrivatePage={() => onCreatePage()}
              onCreateTeamspacePage={(teamspaceId) => openAddPageWindow({ place: { kind: "teamspace", teamspaceId }, mode: "page" })}
              onAddTeamspaceRowPage={addFromEndRow}
              onOpenTeamspaceCreate={() => openTeamspaceDialog("create")}
              onOpenTeamspaceManage={(teamspaceId, invite) => openTeamspaceDialog("manage", teamspaceId, invite)}
              onTeamspaceAction={(teamspace, action) => void teamspaceActions.act(teamspace, action)}
            />
          </PageTreeProvider>
        </>
      )}
      {phone ? <SidebarPhoneTabBar inboxUnread={inbox.unread} inboxActive={inbox.active} newChoices={newChoices}
        onOpenHome={homeRow ? thenClose(onOpenHome) : undefined}
        onOpenInbox={() => setUpdatesOpen((current) => !current)} onOpenSearch={openSearch} pageFoot={!open && !inbox.active} />
        : <SidebarFooter switcher={switcher} guest={guest} workspace={workspace} session={session} onInviteMembers={() => setInviteOpen(true)} onOpenSettings={onOpenSettings}
          places={{ ...footPlaces, onOpenMyTasks: workspace ? thenClose(openMyTasks) : undefined }} />}
      {!mobile && (
        <div
          className={styles.resizeHandle}
          role="separator"
          aria-orientation="vertical"
          aria-label={t("resizeSidebar")}
          tabIndex={0}
          aria-valuemin={minWidth}
          aria-valuemax={maxWidth}
          aria-valuenow={width}
          onPointerDown={onResizeStart}
          onDoubleClick={onResizeReset}
          // 10px a press, Shift or not, as Notion's (seen 2026-11-01).
          onKeyDown={(event) => {
            // Toward the page is narrower, whichever side the sidebar is on.
            if (event.key === inlineStartKey(event.currentTarget)) {
              event.preventDefault();
              onResizeNudge(-10);
            } else if (event.key === inlineEndKey(event.currentTarget)) {
              event.preventDefault();
              onResizeNudge(10);
            } else if (event.key === "Home") {
              event.preventDefault();
              onResizeReset();
            }
          }}
        />
      )}
      </aside>
      {workspaceCreateOpen && (
        <Suspense fallback={null}>
          <WorkspaceCreateDialog
            initialChoice={createStart}
            onClose={() => { setCreateStart(undefined); dropHeldWorkspaceFile(); changeWorkspaceCreateOpen(false); }}
            onCreate={async (input) => {
              const workspace = await onCreateWorkspace(input);
              changeWorkspaceCreateOpen(false);
              return workspace;
            }}
            loadJoinable={loadJoinable}
            onJoin={async (workspaceId) => {
              await onJoinWorkspace(workspaceId);
              changeWorkspaceCreateOpen(false);
            }}
          />
        </Suspense>
      )}
      {inviteOpen && workspace && (
        <Suspense fallback={null}>
          <WorkspaceInviteDialog session={session} workspace={workspace}
            onClose={() => setInviteOpen(false)} onOpenPeople={openPeopleFromInvite} />
        </Suspense>
      )}
      {teamspaceActions.archiveConfirm}
      {membershipAsk.dialog}
      <AddPageWindowHost teamspaces={orderTeamspaces(sidebarTeamspaces.joinedTeamspaces, sidebarLayout.layout.teamspaceOrder)}
        previewAuthor={session.user}
        loadDatabaseSources={loadDatabaseSources} canAddPrivate={!guest} onCreateRootPage={onCreateRootPage} onOpenPage={(page, replace) => onOpenPage(page, false, replace)} />
      {teamspaceDialogOpen && workspace && (
        <Suspense fallback={null}>
          <ErrorBoundary scope="teamspace-dialog">
            <TeamspaceDialog
              session={session}
              workspace={workspace}
              initialTeamspaceId={teamspaceDialogTargetId}
              initialView={teamspaceDialogView}
              initialInvite={teamspaceDialogInvite}
              initialTab={teamspaceDialogTab}
              onClose={() => changeTeamspaceDialogOpen(false)}
            />
          </ErrorBoundary>
        </Suspense>
      )}
    </>
  );
}
