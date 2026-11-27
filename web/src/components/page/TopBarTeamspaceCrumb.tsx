import { useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, DotsHorizontal, TeamspaceRestoreIcon } from "@/icons/leafen";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { orderTeamspaces, teamspaceLeaveLock } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { rootTreePages } from "@/components/sidebar/pageTreeModel";
import { useReadSidebarLayout } from "@/components/sidebar/useSidebarLayout";
import { pageHref } from "@/lib/navigation";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { pathParent } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { useRouter } from "@/lib/router";
import { useKeptTeamspaces } from "@/lib/workspaceTeamspaces";
import type { Page, Session } from "@/types";
import { PageIconGlyph } from "./PageIcon";
import { CrumbMenu, useCrumbHover } from "./TopBarCrumbMenu";
import { useArchivedTeamspaceRestore } from "./useArchivedTeamspaceRestore";
import { TeamspaceJoinButton } from "@/components/teamspace/TeamspaceJoinButton";
import { useTeamspaceCrumbJoin } from "./useTeamspaceCrumbJoin";
import teamspaceStyles from "./TopBarTeamspace.module.css";
import { styles } from "./topBarStyles";
import { pageMapOf } from "@/lib/pageIndex";

// Above a database page open over the page, as the path's own menu.
const MENU_LAYER = 130;
const NO_PAGES: readonly Page[] = [];

/** The teamspace a page sits in: its own, else its top page's, up through a database's pieces (pagePathPieces.ts). */
export function teamspaceIdOf(page: Page, pages: readonly Page[]) {
  const byId = pageMapOf(pages);
  return teamspaceIdAt(page, (id) => byId.get(id));
}

/** teamspaceIdOf with the pages looked up as the caller keeps them, for many pages at once. */
export function teamspaceIdAt(page: Page, local: (id: string) => Page | undefined) {
  const seen = new Set([page.id]);
  let top = page;
  for (let next = pathParent(top, local); next && !seen.has(next.id); next = pathParent(top, local)) {
    seen.add(next.id);
    top = next;
  }
  return page.teamspaceId || top.teamspaceId || "";
}

/**
 * A teamspace page's path starts with its teamspace, as Notion's (seen
 * 2026-10-28): its icon in a small gray square and its name, which opens a
 * window under it with the teamspace's name, "멤버 N명" (seen 2026-11-14)
 * and the reader's place in it — "✓ 참여 중", which leaves it, "참여하기"
 * or "요청" (TeamspaceJoinButton, the settings window's own; Notion's code,
 * read 2026-11-17: one button for both) —
 * its settings, and its top pages. The teamspaces are those the sidebar
 * last read; a page in none of them shows no piece.
 *
 * An archived teamspace whose pages the reader still opens has its piece
 * too, as Notion's (measured 2026-11-14): its window lists no pages and its
 * "…" holds only "팀스페이스 복원", dim with its reason for all but its
 * owners as the page's band; resting on the piece lists it first, above the
 * reader's teamspaces, with no pages beside it.
 */
export function TopBarTeamspaceCrumb({ page, pages, session, onOpen }: {
  page: Page;
  pages: readonly Page[];
  session: Session;
  /** Closes the bar's other menus as the window opens. */
  onOpen?: () => void;
}) {
  const { t } = useTranslation(["topBar", "teamspaceDialog", "sidebar", "common"]);
  const router = useRouter();
  const userId = session.user.id;
  const kept = useKeptTeamspaces(userId, page.workspaceId);
  // Drawn again as the server names the pieces above a database's page.
  usePagePathsAbove(NO_PAGES);
  const teamspaceId = teamspaceIdOf(page, pages);
  const active = teamspaceId ? kept?.teamspaces.find((item) => item.id === teamspaceId) : undefined;
  const archived = teamspaceId && !active ? kept?.archived?.find((item) => item.id === teamspaceId) : undefined;
  const teamspace = active ?? (archived ? { id: archived.id, name: archived.name, icon: archived.icon,
    memberCount: archived.memberCount ?? 0, joined: archived.joined === true } : undefined);
  const restoring = useArchivedTeamspaceRestore(session, page.workspaceId, archived, kept);
  const joining = useTeamspaceCrumbJoin(session, page.workspaceId, kept);
  const [open, setOpen] = useState(false);
  const [restoreMenu, setRestoreMenu] = useState(false);
  const [asking, setAsking] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const windowRef = useRef<HTMLDivElement>(null);
  const moreRef = useRef<HTMLButtonElement>(null);
  const topPages = useMemo(() => open && teamspaceId && !archived
    ? rootTreePages(pages).filter((item) => item.teamspaceId === teamspaceId) : [], [archived, open, pages, teamspaceId]);
  const place = usePopoverPlacement(windowRef, open ? (size, viewport) => placeAround(
    popoverRectOf(triggerRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4 },
  ) : null, [open], undefined, {
    kind: "menu", onEscape: () => { if (!restoreMenu && !asking) setOpen(false); },
    onOutsidePress: () => { if (!restoreMenu && !asking) setOpen(false); },
    inside: () => [triggerRef.current], initialFocus: "first", restoreFocus: false,
  });
  // The pointer resting on it lists the reader's teamspaces, each opening its top pages.
  const hover = useCrumbHover();
  const layout = useReadSidebarLayout(userId, page.workspaceId);
  const onPath = useMemo(() => new Set([teamspaceId]), [teamspaceId]);
  if (!teamspace) return null;

  function go(href: string) {
    setOpen(false);
    router.push(href);
  }

  return (
    <span className={styles.crumbWrap} data-topbar-teamspace-crumb>
      <button ref={triggerRef} type="button" className={teamspaceStyles.crumb} aria-haspopup="dialog" aria-expanded={open}
        onClick={() => { if (!open) { onOpen?.(); setAsking(false); } setOpen(!open); }} {...(open ? {} : hover.bind(teamspace.id))}>
        <span className={teamspaceStyles.crumbGlyph} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
        <span className={styles.crumbLabel}>{teamspace.name}</span>
      </button>
      <span className={styles.sep} aria-hidden="true">/</span>
      {hover.open && !open ? (
        <CrumbMenu anchor={hover.open.anchor} pages={pages} currentId={page.id} onPath={onPath}
          list={{ head: t("sidebar:teamspaces"), rows: [
            ...(archived ? [{ id: archived.id, teamspace: archived, archived: true }] : []),
            ...orderTeamspaces((kept?.teamspaces ?? []).filter((item) => item.joined && !item.archivedAt), layout?.teamspaceOrder)
              .map((item) => ({ id: item.id, teamspace: item })),
          ] }}
          onStay={hover.stay} onLeave={hover.leave} onClose={hover.close} />
      ) : null}
      {open && typeof document !== "undefined" ? createPortal((
        <div ref={windowRef} role="dialog" aria-label={teamspace.name} className={teamspaceStyles.window} data-topbar-teamspace-window
          style={{ position: "fixed", zIndex: MENU_LAYER, ...popoverPlacementStyle(place) } as CSSProperties}>
          <div className={teamspaceStyles.head}>
            <span className={teamspaceStyles.headGlyph} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
            <span className={teamspaceStyles.headText}>
              <span className={teamspaceStyles.headName}>
                <span className={teamspaceStyles.name}>{teamspace.name}</span>
                {archived ? (
                  <button ref={moreRef} type="button" className={teamspaceStyles.settings} aria-label={t("topBar:teamspaceSettingsAndMembers")}
                    data-icon-tip={t("topBar:teamspaceSettingsAndMembers")} data-tip-on-focus aria-haspopup="menu" aria-expanded={restoreMenu} onClick={() => setRestoreMenu((current) => !current)}>
                    <DotsHorizontal size="small" aria-hidden="true" />
                  </button>
                ) : (
                  <button type="button" className={teamspaceStyles.settings} aria-label={t("topBar:teamspaceSettingsAndMembers")}
                    data-icon-tip={t("topBar:teamspaceSettingsAndMembers")} data-tip-on-focus
                    onClick={() => {
                      const url = new URL(window.location.href);
                      url.searchParams.set("teamspace-settings", teamspace.id);
                      go(`${url.pathname}${url.search}${url.hash}`);
                    }}>
                    <DotsHorizontal size="small" aria-hidden="true" />
                  </button>
                )}
              </span>
              <span className={teamspaceStyles.members}>
                {t("topBar:teamspaceMemberCount", { count: "summary" in teamspace ? teamspace.summary.memberCount : teamspace.memberCount })}
              </span>
            </span>
            {active ? (
              <TeamspaceJoinButton teamspace={active} leaveLock={teamspaceLeaveLock(active, active.summary.ownerCount)} busy={joining.busy}
                onJoin={() => void joining.join(active)} onRequest={(message) => joining.request(active, message)}
                onLeave={() => void joining.leave(active)} onAskingChange={setAsking} />
            ) : teamspace.joined ? (
              <span className={teamspaceStyles.joined} aria-disabled="true">
                <CheckIcon size="small" aria-hidden="true" />
                {t("teamspaceDialog:head.joined")}
              </span>
            ) : null}
          </div>
          {topPages.length > 0 ? (
            <div role="menu" aria-label={teamspace.name} className={teamspaceStyles.pages}>
              {topPages.map((item) => (
                <a key={item.id} role="menuitem" className={teamspaceStyles.page} href={pageHref(item.id)}
                  onClick={(event) => {
                    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                    event.preventDefault();
                    go(pageHref(item.id));
                  }}>
                  <span className={teamspaceStyles.pageIcon} aria-hidden="true"><PageIconGlyph page={item} size={18} /></span>
                  <span className={teamspaceStyles.pageTitle}><PageTitleText page={item} /></span>
                </a>
              ))}
            </div>
          ) : null}
        </div>
      ), document.body) : null}
      {open && restoreMenu && archived && typeof document !== "undefined" ? createPortal(
        <SidebarActionMenu button={moreRef} label={archived.name} backdropLabel={t("common:actions.close")}
          close={() => setRestoreMenu(false)} contentWidth pastWindow rows={[{
            key: "restore", label: t("teamspaceDialog:restoreTeamspace"), disabled: Boolean(restoring.lock) || restoring.busy,
            title: restoring.lockReason || undefined, icon: <TeamspaceRestoreIcon size="regular" aria-hidden="true" />,
            data: { "data-topbar-teamspace-restore": "" },
            run: () => { setRestoreMenu(false); setOpen(false); void restoring.restore(); },
          }]} />, document.body) : null}
    </span>
  );
}
