import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { setDocumentChrome } from "@/lib/documentChrome";
import { useStoredIconFaviconHref } from "@/components/page/pageFavicon";
import { requestWorkspaceCreate } from "@/lib/openWorkspace";
import { TopBar } from "@/components/page/TopBar";
import { TopBarTitleCrumb } from "@/components/page/TopBarTitle";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { Session, Workspace } from "@/types";
import { HomeMyTasks, HomeUpcoming, useHomeMyTasks } from "./HomeMyTasks";
import { HomeMenu, useHiddenHomeWidgets } from "./HomeMenu";
import { HomeDatabaseWidget } from "./HomeDatabaseWidget";
import { HomeRecentEdits, HomeRecentVisits } from "./HomeRecents";
import { HomeWidgetHideProvider } from "./HomeWidgetMenu";
import styles from "./HomeView.module.css";

/**
 * Home as Notion's full-page home (the decision of 2026-10-31, replacing the
 * workspace head of 2026-10-17): its greeting in the middle, then sections
 * under a small grey head — the pages visited as cards ending in "+ 새
 * 페이지", then Leafen's own edited pages, coming dates and tasks. The
 * workspace's icon changes in settings and the switcher.
 */
export function HomeView({
  workspaceName,
  workspace,
  canCreatePage,
  guest = false,
  onCreatePage,
  session,
  workspaceId,
  creationRestricted = false,
  joinable = false,
  noWorkspace,
}: {
  workspaceName?: string;
  /** The workspace whose icon the tab shows. */
  workspace?: Workspace;
  canCreatePage: boolean;
  /** A guest's home holds their tasks, not a new page of the workspace. */
  guest?: boolean;
  onCreatePage: () => void;
  session?: Session;
  workspaceId?: string;
  creationRestricted?: boolean;
  /** With no workspace, one here takes this person by their email's domain. */
  joinable?: boolean;
  /** No workspace to show: none is left to this person, or none could be read. */
  noWorkspace?: "none" | "unread";
}) {
  const { t } = useTranslation(["homeView", "workspaceCreateDialog", "sidebar"]);
  const title = workspaceName?.trim() || t("workspace");
  const workspaceIcon = workspace?.icon;
  const { hidden, toggle } = useHiddenHomeWidgets(session);
  const favicon = useStoredIconFaviconHref(workspaceIcon, workspace?.id);
  // Named "홈" in the window and its tab, as Notion's ("홈 | Notion", seen 2026-11-13).
  const homeName = t("sidebar:home");
  useEffect(() => {
    setDocumentChrome({ title: homeName, iconHref: favicon });
  }, [favicon, homeName]);
  const { phone } = useSidebarLayout();

  return (
    <>
      {/* Notion's home bar names nothing (seen 2026-10-31), but on a phone, "☰ 홈 ⋯" (seen 2026-11-13). */}
      <TopBar title={title} crumbs={phone ? <TopBarTitleCrumb title={homeName} /> : <></>} screenActions={session && workspaceId && !noWorkspace
        ? <HomeMenu session={session} workspaceId={workspaceId} hidden={hidden} onToggle={toggle} /> : undefined} />
      <div className={styles.wrap} role="region" aria-label={title}>
        <section className={styles.home} data-testid="empty-workspace-surface">
          {noWorkspace ? <div className={styles.noWorkspace} data-no-workspace={noWorkspace}>
            {noWorkspace === "none" && joinable ? <>
              {/* Someone a workspace takes by their email's domain, who closed
                  the window offering it (2026-11-06: this said there was none). */}
              <h2>{t("homeView:noWorkspaceJoinableTitle")}</h2>
              <p>{t(creationRestricted ? "homeView:noWorkspaceJoinableBodyRestricted" : "homeView:noWorkspaceJoinableBody")}</p>
              <button type="button" onClick={requestWorkspaceCreate}>
                {t(creationRestricted ? "homeView:noWorkspaceJoin" : "sidebar:profiles.createOrJoinWorkspace")}</button>
            </> : <>
              <h2>{t(noWorkspace === "none" ? "homeView:noWorkspaceTitle" : "homeView:noWorkspaceUnreadTitle")}</h2>
              <p>{t(noWorkspace === "none" ? creationRestricted ? "homeView:noWorkspaceBodyRestricted" : "homeView:noWorkspaceBody" : "homeView:noWorkspaceUnreadBody")}</p>
              {noWorkspace === "none" && !creationRestricted ? <button type="button" onClick={requestWorkspaceCreate}>
                {t("sidebar:newWorkspace")}</button> : null}
            </>}
            {noWorkspace === "none" && creationRestricted ? <p>{t("workspaceCreateDialog:restricted")}</p> : null}
          </div> : hidden.has("greeting") ? null : <HomeGreeting userId={session?.user.id} />}
          {session && workspaceId ? <HomeWidgetHideProvider value={(widget) => { if (!hidden.has(widget)) toggle(widget); }}>
            <HomeLists session={session} workspaceId={workspaceId} workspaceName={title} hidden={hidden}
              onCreatePage={guest ? undefined : onCreatePage} canCreatePage={canCreatePage} />
          </HomeWidgetHideProvider> : null}
        </section>
      </div>
    </>
  );
}

// Notion greets by the hour — evening from 19 to 2, morning to 12, afternoon
// after — and names the reader only by a nickname, which Leafen has none of
// (seen 2026-11-01, personalHomeHeader). Until they have seen it, "홈에 오신 것을
// 환영합니다" stands first and gives way 2.75s on, rising 12px as it fades
// over 0.5s; Notion keeps that it was seen with the account, Leafen with the
// browser.
const WELCOMED = "leafen.homeWelcomed.";

function greetingKey(hour: number) {
  return hour >= 19 || hour < 2 ? "greetingEvening" : hour < 12 ? "greetingMorning" : "greetingAfternoon";
}

function HomeGreeting({ userId }: { userId?: string }) {
  const { t } = useTranslation("homeView");
  const [welcome, setWelcome] = useState(() => {
    if (!userId) return false;
    try { return window.localStorage.getItem(WELCOMED + userId) === null; } catch { return false; }
  });
  useEffect(() => {
    if (!welcome || !userId) return;
    try { window.localStorage.setItem(WELCOMED + userId, "1"); } catch { /* shown again next time */ }
    const timer = window.setTimeout(() => setWelcome(false), 2750);
    return () => window.clearTimeout(timer);
  }, [welcome, userId]);
  const greeting = t(greetingKey(new Date().getHours()));
  return (
    <h1 className={styles.homeTitle} data-testid="empty-workspace-title" data-welcome={welcome ? "true" : undefined}>
      <span className={styles.homeGreeting} aria-hidden={welcome ? "true" : undefined}>{greeting}</span>
      <span className={styles.homeWelcome} aria-hidden={welcome ? undefined : "true"}>{t("welcome")}</span>
    </h1>
  );
}

/**
 * Home's lists, as Notion's home holds documents (the one-sidebar decision
 * of 2026-10-29, 2): the pages visited and edited last, what is coming up,
 * and the reader's tasks.
 */
function HomeLists({ session, workspaceId, workspaceName, hidden, onCreatePage, canCreatePage }: {
  session: Session; workspaceId: string; workspaceName: string; hidden: ReadonlySet<string>; onCreatePage?: () => void; canCreatePage: boolean;
}) {
  const tasks = useHomeMyTasks(session, workspaceId);
  return <>
    <HomeRecentVisits session={session} workspaceId={workspaceId} onCreatePage={onCreatePage} canCreatePage={canCreatePage} />
    {hidden.has("recentEdits") ? null : <HomeRecentEdits session={session} workspaceId={workspaceId} />}
    {hidden.has("upcoming") ? null : <HomeUpcoming load={tasks.load} />}
    {hidden.has("myTasks") ? null : <HomeMyTasks session={session} workspaceId={workspaceId}
      workspaceName={workspaceName} onSourcesSaved={tasks.reload} />}
    {hidden.has("database") ? null : <HomeDatabaseWidget session={session} workspaceId={workspaceId} />}
  </>;
}
