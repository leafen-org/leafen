import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SearchClearButton } from "@/components/PageActionSearch";
import { InSettingsContext } from "./settingsSurface";
import { Fragment, useCallback, useDeferredValue, useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { AccountSettingsContent, type AccountSettingsSection } from "@/components/account/AccountSettingsPage";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { AdministratorSettingsContent, type AdministratorSettingsSection } from "@/components/admin/AdministratorSettingsPage";
import { ImportSettingsSection } from "@/components/import/ImportSettingsSection";
import type { AdministratorSettings } from "@/administratorWire.generated";
import { api } from "@/api";
import { ArrowUpRightIcon, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { clearSessionIfCurrent, loadSession } from "@/lib/authSessionStore";
import { forgetDevicePageCopies } from "@/lib/deviceSignOutData";
import { localFirstProductCache as recordCache } from "@/lib/localFirstProductCache";
import type { BrowserLocalLockSettings } from "@/lib/localLockBrowserSettings";
import { pageHref } from "@/lib/navigation";
import { developerToolsHref, pushRoute, pushRouteClosingWindow, replaceRoute } from "@/lib/router";
import { requestWorkspaceCreateFrom } from "@/lib/openWorkspace";
import { scrollStartBeforePaint } from "@/lib/scrollStartBeforePaint";
import type { Session, Workspace } from "@/types";
import { SettingsSearchMark, SettingsSectionIcon, useSettingsLabels } from "./settingsWindowLabels";
import { searchSettings } from "./settingsWindowSearch";
import { SettingsSearchResults } from "./SettingsSearchResults";
import { useFoundSettingsRow, type FoundSettingsRow } from "./settingsFoundRow";
import { useSettingsRowWrap } from "./settingsRowWrap";
import {
  LEAFEN_OWN_SETTINGS_SECTIONS,
  settingsGroups,
  visibleSettingsSection,
  type SettingsAccess,
  type SettingsSection,
  type SettingsWindowState,
} from "./settingsWindowModel";
import {
  useWorkspaceSettingsAccess,
  WorkspaceSettingsContent,
  type WorkspaceSettingsAccess,
} from "./WorkspaceSettingsPage";
import { mayLeaveSettings } from "./settingsUnsaved";
import { useSettingsReadAgain } from "./useSettingsReadAgain";
import styles from "./SettingsWindow.module.css";
import { SettingsPageTitle } from "./SettingsSectionTitle";
import { serverDisplayName } from "@/lib/serverName";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PHONE_SETTINGS_SECTIONS, SettingsPhoneDangerZone, SettingsPhoneHead, SettingsPhoneMore, SettingsPhoneSection, useSettingsPhoneScreenFocus } from "./SettingsPhoneScreen";
import { AccountSecurityShareProvider } from "@/components/account/AccountSecurityShare";
import touchHead from "./SettingsTouchHead.module.css";
import touchWindow from "./SettingsTouchWindow.module.css";

type SettingsWindowProps = {
  /** What had the focus when the window was asked for, given it back on closing (useSettingsWindow). */
  opener?: RefObject<HTMLElement | null>;
  state: SettingsWindowState;
  onSelect: (section: SettingsSection) => void;
  onClose: () => void;
  session: Session;
  onSession: (session: Session | null) => void;
  localLockSettings: BrowserLocalLockSettings;
  workspace: Workspace | null;
  workspaces: Workspace[];
  onImported: () => void;
  onWorkspaceChange: (workspace: Workspace) => void;
  onWorkspaceDeleted: (workspaceId: string) => void | Promise<void>;
  onWorkspaceRefresh: () => Promise<void>;
  /** The server's settings, for its administrators only. */
  administratorSettings: AdministratorSettings | null;
  onAdministratorSettingsChange: (settings: AdministratorSettings) => void;
};

/**
 * Settings, as Notion's: a window over the current screen with the account,
 * the workspace and its administration listed at its side (seen
 * 2026-10-02); Escape, its close button or a click beside it closes it.
 */
export function SettingsWindow(windowProps: SettingsWindowProps) {
  // Changes a section hasn't saved are asked about before the window closes
  // or moves to a section that drops them (settingsUnsaved.ts).
  const current = windowProps.state.section;
  const props: SettingsWindowProps = {
    ...windowProps,
    onClose: () => void mayLeaveSettings().then((leave) => { if (leave) windowProps.onClose(); }),
    onSelect: (section) => {
      if (section === current) windowProps.onSelect(section);
      else void mayLeaveSettings(section).then((leave) => { if (leave) windowProps.onSelect(section); });
    },
  };
  const { onClose } = props;
  const labels = useSettingsLabels();
  const dialogRef = useRef<HTMLElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  // A layer over the page: Escape closes it once whatever opened over it
  // has closed, and the page stays still under it. Focus enters the window
  // on "검색 설정", as Notion's does (seen 2026-10-28) — unless a section took
  // it first (the invite field) — and returns to its opener. On a phone,
  // which has no search there, the window itself takes it (overlayLayer.ts).
  const { phone, touch } = useSidebarLayout(true);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
    initialFocus: phone ? dialogRef : searchRef, returnFocusRef: windowProps.opener });

  const workspace = props.workspace && !props.workspace.guest ? props.workspace : null;
  // One tree whether or not the workspace is known yet: settings opened by an
  // address before it was read kept the layout above, and switched to another
  // once it came, dropping what its sections held (2026-10-16).
  const workspaceAccess = useWorkspaceSettingsAccess({
    section: workspace ? props.state.section : "account", session: props.session,
    workspace: workspace ?? NO_WORKSPACE, workspaces: props.workspaces,
  });
  return (
    <div className={styles.overlay} style={{ zIndex }} data-settings-window>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={labels.close} onClick={onClose} />
      <section
        ref={dialogRef}
        className={`${styles.dialog} ${touch && !phone ? touchWindow.window : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={labels.title}
        tabIndex={-1}
      >
        <InSettingsContext.Provider value>
          <SettingsLayout {...props} workspace={workspace} workspaceAccess={workspace ? workspaceAccess : null} searchRef={searchRef} phone={phone}
            touchList={touch && !phone} />
        </InSettingsContext.Provider>
        {phone ? null : <button type="button" className={styles.close} aria-label={labels.close} onClick={onClose}>
          <X size="regular" aria-hidden="true" />
        </button>}
      </section>
    </div>
  );
}

// Stands in while no workspace is known: its settings are read only for one.
const NO_WORKSPACE = { id: "", organizationId: "" } as Workspace;
// How long the list waits for the reads that add items before it shows what it has.
const SETTINGS_LIST_WAIT_MS = 3_000;

function SettingsLayout({
  workspace,
  workspaceAccess,
  searchRef,
  phone,
  touchList,
  ...props
}: SettingsWindowProps & { workspace: Workspace | null; workspaceAccess: WorkspaceSettingsAccess | null; searchRef: RefObject<HTMLInputElement | null>; phone: boolean; touchList: boolean }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const labels = useSettingsLabels(touchList);
  const titleId = useId();
  const navRef = useRef<HTMLElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState("");
  const [foundRow, setFoundRow] = useState<FoundSettingsRow | null>(null);
  const [emailManagementOpen, setEmailManagementOpen] = useState(false);
  const { onSession } = props;
  // The server says whether it has the app platform; a certified build has
  // none, and a phone's own server or the App Store edition says why there.
  const [appPlatform, setAppPlatform] = useState<boolean | null>(null);
  useEffect(() => {
    let live = true;
    void fetchRuntimeConfig()
      .then(({ config }) => { if (live) setAppPlatform(config.appPlatform || config.appPlatformMissing !== ""); })
      .catch(() => { if (live) setAppPlatform(false); });
    return () => { live = false; };
  }, []);
  const access: SettingsAccess = {
    workspace: workspaceAccess !== null,
    restricted: workspace?.restricted === true,
    canManage: workspaceAccess?.canManage ?? false,
    canUseImports: workspaceAccess !== null,
    canViewEnterprise: workspaceAccess ? workspaceAccess.canViewEnterprise : false,
    enterpriseItems: workspaceAccess?.enterpriseItems ?? { security: false, identity: false },
    canReadOrganization: workspaceAccess ? workspaceAccess.canReadOrganization : false,
    administrator: props.administratorSettings !== null,
    appPlatform: appPlatform === true,
    touchList,
  };
  // The list waits for the reads that add items to it, as Notion's comes
  // whole: an item put in above the pointer moved the one under it as it was
  // pressed, and the press went nowhere (2026-11-06). A read that failed
  // settles it too, without its items, and so does one that hasn't answered
  // in a few seconds (2026-11-07: on a network that stopped the list stayed
  // empty for over a minute).
  const [listWaitOver, setListWaitOver] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setListWaitOver(true), SETTINGS_LIST_WAIT_MS);
    return () => window.clearTimeout(timer);
  }, []);
  const listPending = !listWaitOver && ((props.administratorSettings !== null && appPlatform === null)
    || (workspaceAccess !== null && (
      (workspaceAccess.canViewEnterprise === null && workspaceAccess.enterprise.loading)
      || (workspaceAccess.canReadOrganization === null && workspaceAccess.organizationSettings.loading))));
  const section = visibleSettingsSection(props.state.section, access);
  useEffect(() => { setEmailManagementOpen(false); }, [section, props.session.user.id]);
  // A phone lays the person's own sections on one long screen (SettingsPhoneScreen.tsx),
  // opening at the one asked for; another section is a screen of its own,
  // whose "‹" goes back to the long screen's rows it was opened from.
  const phoneRoot = phone && (PHONE_SETTINGS_SECTIONS as readonly SettingsSection[]).includes(section);
  const [phoneOpenedFrom, setPhoneOpenedFrom] = useState<SettingsSection | null>(null);
  const [phoneReveal, setPhoneReveal] = useState<{ section: SettingsSection; part: string; at: number } | null>(null);
  // The press that opens the window draws its frame — the list, the search,
  // the head and the section's title — on its own frame, and the section's
  // content in a draw after it (React's deferred value, which yields to the
  // frame): drawn in the press, the content's first drawing put the window's
  // first paint at 135–167 ms, past the 150 a press has, on a NAS's condition
  // with a device half as fast (2026-11-14; 77–103 ms so, the content 50–60
  // ms later).
  const contentDrawn = useDeferredValue(true, false);
  // The long screen draws the sections down to the one it opens at first and
  // the rest a frame after them, so the press shows the screen at once.
  const [phoneRestShown, setPhoneRestShown] = useState(false);
  useEffect(() => {
    if (!phoneRoot || phoneRestShown || !contentDrawn) return undefined;
    const frame = window.requestAnimationFrame(() => setPhoneRestShown(true));
    return () => window.cancelAnimationFrame(frame);
  }, [phoneRoot, phoneRestShown, contentDrawn]);
  // Another section, or the email page, opens at its title (2026-10-27: it
  // opened at the scroll the section before had, its title out of sight),
  // put there before the frame drawing it paints: set as the draw ended, the
  // scroll made the browser work out the new section's styles and layout
  // there and then, even at the top already (2026-11-14). A part or found
  // row brought into sight in that frame puts it back first, measured from
  // the top.
  const scrollStart = useRef<((now?: boolean) => void) | null>(null);
  useLayoutEffect(() => {
    const stop = scrollStartBeforePaint([panelRef.current]);
    scrollStart.current = stop;
    return () => stop();
  }, [section, emailManagementOpen]);
  const scrollStartNow = useCallback(() => scrollStart.current?.(true), []);
  // A link to a part of the section (an organization's SSO, say) opens at that
  // part, its title in sight while the parts above it load, until they
  // settle or the person scrolls.
  const revealed = phoneRoot && !emailManagementOpen
    ? phoneReveal?.section === section ? phoneReveal : { part: `section-${section}`, at: 0 } : undefined;
  const part = props.state.section === section ? props.state.part ?? revealed?.part : undefined;
  useEffect(() => {
    const panel = panelRef.current;
    if (!panel || !part) return undefined;
    const reveal = () => {
      const target = panel.querySelector(`[data-settings-part="${part}"]`);
      if (!target) return;
      scrollStartNow();
      target.scrollIntoView({ block: "start" });
    };
    const observer = new MutationObserver(reveal);
    const stop = () => observer.disconnect();
    reveal();
    observer.observe(panel, { childList: true, subtree: true });
    const timer = window.setTimeout(stop, 3000);
    const events = ["wheel", "touchstart", "keydown", "pointerdown"] as const;
    for (const name of events) panel.addEventListener(name, stop, { passive: true });
    return () => {
      stop();
      window.clearTimeout(timer);
      for (const name of events) panel.removeEventListener(name, stop);
    };
  }, [part, section, revealed?.at, scrollStartNow]);
  // A screen that asks for a row lit opens at it lit, as a found row is.
  const lit = props.state.lit;
  useEffect(() => {
    if (lit?.length) setFoundRow({ section: props.state.section, titles: lit, picked: Date.now() });
  }, [lit, props.state.section]);
  useFoundSettingsRow(panelRef, foundRow, section, scrollStartNow);
  const readAgain = useSettingsReadAgain(panelRef, workspaceAccess, props.onWorkspaceRefresh);
  // A found row reads as its page does, with the values the page puts in
  // (2026-11-06: "서버 이름" showed "비우면 {{alias}}입니다").
  const searchValues = {
    alias: serverDisplayName({ name: "", alias: props.administratorSettings?.serverAlias ?? "" }),
    workspace: workspace?.name ?? "",
  };
  // The section shown stays in sight in the list, which scrolls sideways on a
  // phone, also once the sections the person's access adds have come in.
  const sectionList = settingsGroups(access).flatMap((group) => group.sections).join();
  useEffect(() => {
    const nav = navRef.current;
    if (!nav) return;
    const revealSelection = () => nav.querySelector("[aria-current='page']")?.scrollIntoView({ block: "nearest", inline: "nearest" });
    // The observer's first notice, once the frame drawing the section has
    // laid it out and before it paints, brings the item in sight: brought
    // there as the draw ended, it made the browser work out the new section's
    // styles and layout there and then (2026-11-14). Crossing the phone
    // boundary changes the scroll axis without selecting another section:
    // later notices keep the current item reachable in that new layout too.
    const observer = new ResizeObserver(revealSelection);
    observer.observe(nav);
    return () => observer.disconnect();
  }, [section, sectionList]);
  useSettingsRowWrap(panelRef, section);
  const displayName = props.session.user.name || props.session.user.email.split("@")[0] || "?";
  // The account's section is named as the list names it, by the person.
  const searchNames = { ...labels, section: (item: SettingsSection) => item === "account" ? displayName : labels.section(item) };
  const searchResults = searchSettings(query, settingsGroups(access), searchNames, (key) => t(key, searchValues));
  // The account's item shows its picture, as Notion's shows the person's.
  const [avatar, setAvatar] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    api.readAccountProfile(props.session, controller.signal)
      .then((profile) => setAvatar(profile.avatar))
      .catch(() => undefined);
    return () => controller.abort();
  }, [props.session]);

  // A deleted account's server sessions are already gone; this device
  // forgets it as signing out does.
  const accountDeleted = useCallback(async () => {
    void recordCache.clear(props.session.user.id).catch(() => undefined);
    void forgetDevicePageCopies(props.session.user.id).catch(() => undefined);
    if (!await clearSessionIfCurrent(props.session)) return;
    onSession(loadSession());
    replaceRoute("/");
  }, [onSession, props.session]);

  const accountContent = (item: AccountSettingsSection, emailOpen: boolean) => (
    <AccountSettingsContent
      section={item}
      session={props.session}
      workspaceId={workspace?.id ?? null}
      onSession={onSession}
      onAccountDeleted={accountDeleted}
      localLockSettings={props.localLockSettings}
      emailManagementOpen={emailOpen}
      onEmailManagementChange={setEmailManagementOpen}
    />
  );
  let content: ReactNode = null;
  if (section === "account" || section === "preferences" || section === "notifications" || section === "servers") {
    content = accountContent(section, emailManagementOpen);
  } else if (section.startsWith("server-") && props.administratorSettings) {
    content = (
      <AdministratorSettingsContent
        key="server"
        section={section.slice("server-".length) as AdministratorSettingsSection}
        session={props.session}
        initialSettings={props.administratorSettings}
        onSettingsChange={props.onAdministratorSettingsChange}
        appInstall={section === "server-apps" ? props.state.appInstall : undefined}
        appRow={section === "server-apps" ? props.state.appRow : undefined}
        currentTenancy={workspace?.organizationId ? `org:${workspace.organizationId}` : undefined}
        onSelectSection={(next) => props.onSelect(`server-${next}`)}
      />
    );
  } else if (section === "import" && workspace) {
    content = <ImportSettingsSection session={props.session} workspaceId={workspace.id} workspaceName={workspace.name} destination={props.state.importPlace} initialSource={props.state.importSource}
      arrival={props.state.importArrival}
      onNewWorkspaceFromLeafen={(file) => { props.onClose(); requestWorkspaceCreateFrom("leafen", file); }}
      onNavigate={(path) => { props.onClose(); pushRouteClosingWindow(path); }} />;
  } else if (workspace && workspaceAccess) {
    content = (
      <WorkspaceSettingsContent
        section={section}
        access={workspaceAccess}
        invite={props.state.invite === true && section === "people"}
        requestsTab={touchList && props.state.section === "requests"}
        session={props.session}
        workspace={workspace}
        workspaces={props.workspaces}
        onWorkspaceChange={props.onWorkspaceChange}
        onWorkspaceDeleted={props.onWorkspaceDeleted}
        onWorkspaceRefresh={props.onWorkspaceRefresh}
        onSelectSection={props.onSelect}
        serverAdministrator={props.administratorSettings !== null}
        onOpenPage={(pageId) => {
          props.onClose();
          pushRoute(pageHref(pageId));
        }}
        onNavigate={(path) => { props.onClose(); pushRouteClosingWindow(path); }}
      />
    );
  }

  const subtitle = labels.subtitle(section) ? <p className={styles.subtitle} data-settings-subtitle>
    {labels.subtitle(section)}
    {/* "개발자 도구 열기 ↗" at the subtitle's end, as Notion's (seen 2026-10-30). */}
    {section === "developer" ? <a href={developerToolsHref("start")} className={styles.subtitleLink} data-developer-tools-open
      onClick={(event) => { event.preventDefault(); props.onClose(); pushRouteClosingWindow(developerToolsHref("start")); }}>
      {t("developerTools.open")}<ArrowUpRightIcon size="small" aria-hidden="true" /></a> : null}
  </p> : null;

  const titleAction = <div className={styles.titleAction} data-settings-title-action />;
  useSettingsPhoneScreenFocus(panelRef, phone ? phoneRoot ? emailManagementOpen ? "email" : "list" : section : null);
  if (phone) {
    // The account's email page is a screen of its own here too, its "‹" back to the account's place.
    const emailShown = phoneRoot && emailManagementOpen;
    const listShown = phoneRoot && !emailShown;
    const back = emailShown ? () => { setEmailManagementOpen(false); setPhoneReveal({ section, part: "section-account", at: Date.now() }); }
      : phoneRoot || !phoneOpenedFrom ? props.onClose
      : () => { setPhoneReveal({ section: phoneOpenedFrom, part: "phone-more", at: Date.now() }); props.onSelect(phoneOpenedFrom); };
    // 사람 opened on its own (the drawer's "멤버") is Notion's phone "멤버": that title, "완료" in place of "‹".
    const membersScreen = section === "people" && !phoneOpenedFrom;
    return (
      <>
        <SettingsPhoneHead titleId={titleId} onBack={membersScreen ? undefined : back} onDone={membersScreen ? props.onClose : undefined}
          title={emailShown ? t("emailManagementTitle") : listShown ? labels.title : membersScreen ? t("peopleTabMembers") : labels.section(section)} />
        <div ref={panelRef} className={styles.panel} role="region" aria-labelledby={titleId} data-settings-phone={listShown ? "list" : "screen"}>
          <div className={styles.content} data-settings-content={emailShown ? "account" : section}>
            {listShown ? <>
              {/* The sections share the account's security: the password and passkeys stand in the preferences here. */}
              <AccountSecurityShareProvider key={readAgain} session={props.session} onSession={onSession} phonePlaces>
                {PHONE_SETTINGS_SECTIONS.slice(0, phoneRestShown ? undefined : PHONE_SETTINGS_SECTIONS.indexOf(section as AccountSettingsSection) + 1).map((item) => (
                  <SettingsPhoneSection key={item} part={`section-${item}`} title={labels.section(item)}>
                    {contentDrawn ? <SettingsPageTitle.Provider value={labels.section(item)}><Fragment key={readAgain}>{accountContent(item, false)}</Fragment></SettingsPageTitle.Provider> : null}
                  </SettingsPhoneSection>
                ))}
              </AccountSecurityShareProvider>
              {phoneRestShown ? <SettingsPhoneDangerZone session={props.session} onDeleted={accountDeleted} /> : null}
              {phoneRestShown ? <SettingsPhoneMore groups={settingsGroups(access).filter((group) => group.id !== "account")}
                newVersion={props.administratorSettings?.updates.newer === true}
                onOpen={(item) => { setPhoneOpenedFrom(section); setFoundRow(null); props.onSelect(item); }} /> : null}
            </> : emailShown ? contentDrawn ? <Fragment key={readAgain}>{accountContent("account", true)}</Fragment> : null : <>
              {/* The head names the screen; a section's link at its title's end and the line under it stay (사람's not: Notion's phone "멤버" has none). */}
              <div className={styles.titleAction} data-settings-title-action />
              {section === "people" ? null : subtitle}
              {contentDrawn ? <SettingsPageTitle.Provider value={labels.section(section)}><Fragment key={readAgain}>{content}</Fragment></SettingsPageTitle.Provider> : null}
            </>}
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      <nav ref={navRef} className={styles.nav} aria-label={t("ariaSettingsNavigation", { title: labels.title })}>
        <div className={styles.search}>
          <SettingsSearchMark wide={touchList} />
          <input
            ref={searchRef}
            type="text"
            value={query}
            placeholder={labels.searchPlaceholder}
            aria-label={labels.searchPlaceholder}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              // Escape clears a search before it closes the window.
              if (event.key !== "Escape" || !query) return;
              event.preventDefault();
              setQuery("");
            }}
          />
          <SearchClearButton value={query} onClear={() => setQuery("")} />
        </div>
        {query.trim() ? (
          <SettingsSearchResults
            results={searchResults}
            noResults={labels.noResults(query)}
            accountIcon={<PersonAvatar className={styles.navAvatar} avatar={avatar} name={displayName} size={20} />}
            onOpen={(section, rows) => {
              setQuery("");
              setFoundRow(rows === undefined ? null : { section, titles: rows, picked: Date.now() });
              props.onSelect(section);
            }}
          />
        ) : <div className={styles.navGroups} data-pending={listPending ? "true" : undefined}>{settingsGroups(access).map((group) => (
          <div key={group.id} className={styles.navGroup} data-settings-group={group.id}>
            <div className={styles.navGroupLabel}>{labels.group(group.id)}</div>
            {group.sections.map((item) => (
              <button
                key={item}
                type="button"
                className={styles.navItem}
                data-settings-section={item}
                data-active={item === section ? "true" : undefined}
                aria-current={item === section ? "page" : undefined}
                onClick={() => { setFoundRow(null); props.onSelect(item); }}
                data-leafen-own={LEAFEN_OWN_SETTINGS_SECTIONS.has(item) ? "" : undefined}
              >
                {item === "account" ? (
                  <PersonAvatar className={styles.navAvatar} avatar={avatar} name={displayName} size={touchList ? 22 : 20} />
                ) : <SettingsSectionIcon section={item} size={touchList ? "wide" : "regular"} />}
                <span className={styles.navLabel} data-settings-nav-label>{item === "account" ? displayName : labels.section(item)}</span>
                {item === "server-updates" && props.administratorSettings?.updates.newer ? (
                  <span className={styles.updateMarker} data-update-available aria-label={labels.newVersion} />
                ) : null}
              </button>
            ))}
          </div>
        ))}</div>}
      </nav>
      <div ref={panelRef} className={styles.panel} role="region" aria-labelledby={titleId}>
        <div className={`${styles.content} ${section === "account" && emailManagementOpen ? styles.emailContent : ""} ${touchList ? touchHead.head : ""}`} data-settings-content={section} data-settings-touch={touchList || undefined}>
          {section === "account" && emailManagementOpen ? <div id={titleId} className={styles.breadcrumb}>
            <button type="button" onClick={() => setEmailManagementOpen(false)}>{labels.section("account")}</button>
            <span aria-hidden="true">/</span><span>{t("emailManagementTitle")}</span>
          </div> : <>
            <h2 id={titleId} className={styles.title}>{labels.section(section)}</h2>
            {/* What a section shows at the end of the title's line (사람's directory link); a wider touch screen's after the line under it (SettingsTouchHead). */}
            {touchList && subtitle ? <div className={touchHead.subline}>{subtitle}{titleAction}</div> : <>{titleAction}{subtitle}</>}
          </>}
          {contentDrawn ? <SettingsPageTitle.Provider value={labels.section(section)}><Fragment key={readAgain}>{content}</Fragment></SettingsPageTitle.Provider> : null}
        </div>
      </div>
    </>
  );
}
