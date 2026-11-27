import {
  type ComponentProps,
  type MouseEvent as ReactMouseEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { ArchiveIcon } from "@/icons/leafen";
import { isSyntheticNotionImportRootPage } from "@/lib/importedNotionUi";
import { databasePageHref, databasePageViewUnknown, readDatabasePageViews, useDatabasePageViewsRead } from "@/lib/databasePageLink";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useNamedPage, usePageNames } from "@/lib/pageNames";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { knownPathHref, knownPathPage, pathLeavesOwnPages, pathParent } from "@/lib/pagePathPieces";
import { usePagePathsAbove } from "@/lib/usePagePathsAbove";
import { useRouter } from "@/lib/router";
import type { Page } from "@/types";
import { hasExplicitPageIcon, PageIconGlyph } from "./PageIcon";
import { TitleWithCustomEmoji } from "./TitleWithCustomEmoji";
import {
  partitionTopBarBreadcrumbs,
  projectTopBarBreadcrumbs,
  revealedBreadcrumbLabel,
  type TopBarBreadcrumb,
} from "./topBarBreadcrumbModel";
import { styles } from "./topBarStyles";
import { renamePopoverUnder } from "./PageRenamePopover";
import { PATH_SHEET_MARK, TopBarPathSheet } from "./TopBarPathSheet";
import { useProductPages } from "@/components/product/productPagesContext";
import { CrumbMenu, pagesBeside, useCrumbHover } from "./TopBarCrumbMenu";
import { DEFAULT_SIDEBAR_LAYOUT, SIDEBAR_HOME_TAB, sidebarSection } from "@/components/sidebar/sidebarLayoutModel";
import { useReadSidebarLayout } from "@/components/sidebar/useSidebarLayout";
import { pageMapOf } from "@/lib/pageIndex";

function includeBreadcrumb(page: Page) {
  return !isSyntheticNotionImportRootPage(page);
}

function BreadcrumbContent({ breadcrumb, label: cutLabel }: { breadcrumb: TopBarBreadcrumb<Page>; label?: string }) {
  const pageNames = usePageNames();
  const { t } = useTranslation("topBar");
  // The page as it is named now, a title being typed in its field as it is typed (lib/pageNames.ts).
  const page = useNamedPage(breadcrumb.page);
  const label = cutLabel ?? (page === breadcrumb.page ? breadcrumb.label : pageNames.title(page));
  const showIcon = hasExplicitPageIcon(breadcrumb.page);
  return (
    <>
      {showIcon ? (
        <span className={styles.crumbIcon} data-topbar-crumb-icon aria-hidden="true">
          <PageIconGlyph page={breadcrumb.page} size={18} fallback="none" />
        </span>
      ) : null}
      <span className={styles.crumbLabel} data-topbar-crumb-label>
        <TitleWithCustomEmoji text={label} named={breadcrumb.page} workspaceId={breadcrumb.page.workspaceId} />
      </span>
      {breadcrumb.page.archivedAt ? (
        <span className={styles.crumbIcon} data-topbar-crumb-archived title={t("archivedPage")}>
          <ArchiveIcon size="small" aria-label={t("archivedPage")} />
        </span>
      ) : null}
    </>
  );
}

/** A last piece after the page's own, as an inline database opened alone shows its name. */
export type TopBarTrailingCrumb = {
  label: string;
  icon: ComponentProps<typeof PageIconGlyph>["page"];
  href: string;
};

export function TopBarBreadcrumbs({
  onOpenPage,
  page,
  pages,
  trailing,
  hrefOf,
  currentOnly = false,
  onRenameCurrent,
  known = false,
}: {
  /** Opens a piece of the path its own way; false leaves it to the page's address. */
  onOpenPage?: (pageId: string) => boolean;
  page: Page;
  pages: readonly Page[];
  trailing?: TopBarTrailingCrumb;
  /** A piece's own address where it isn't a page's, as an inline database's. */
  hrefOf?: (pageId: string) => string | undefined;
  currentOnly?: boolean;
  /** Opens the rename popover at the current piece, as Notion's; none where the page can't be renamed. */
  onRenameCurrent?: (at: { top: number; left: number }) => void;
  /** Draws the path as this window knows it and asks the server nothing: a page's place while the page is read (RowPageReading). */
  known?: boolean;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["topBar", "sidebar"]);
  const router = useRouter();
  const productPages = useProductPages();
  // A full-page database's piece names the view it opens at, as Notion's path
  // does (databasePageHref; seen twice 2026-11-25: a row page's database piece
  // /p/<database>?v=<the view last chosen there>), its views read as the
  // pointer comes to it where none is chosen yet.
  const userId = productPages?.session?.user.id;
  const pageOf = (pageId: string) => pages.find(({ id }) => id === pageId) ?? (pageId === page.id ? page : knownPathPage(pageId) ?? { id: pageId });
  const hrefFor = (pageId: string) => hrefOf?.(pageId) ?? knownPathHref(pageId) ?? databasePageHref(pageOf(pageId), userId);
  useDatabasePageViewsRead(pages.some(({ kind }) => kind === "database"));
  const readView = (pageId: string) => databasePageViewUnknown(pageOf(pageId), userId)
    ? () => { void readDatabasePageViews(productPages?.session, pageOf(pageId)); } : undefined;
  // Where the path goes past the reader's pages — under a database's page —
  // the server names the rest, as Notion's goes on up through the database
  // page and its database (seen 2026-11-10).
  const leaves = useMemo(() => {
    const byId = pageMapOf(pages);
    return pathLeavesOwnPages(page, (id) => byId.get(id));
  }, [page, pages]);
  const pathsRead = usePagePathsAbove(leaves && !known ? [page] : [], { fresh: true });
  const breadcrumbs = useMemo(
    () => projectTopBarBreadcrumbs(page, pages, includeBreadcrumb, pageNames.title, pathParent),
    // The paths read so far draw it again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [page, pages, pathsRead, pageNames],
  );
  const { hidden, visible } = useMemo(
    // A phone shows only the last piece: the page's, or the database's opened alone.
    () => currentOnly ? { hidden: [], visible: trailing ? [] : breadcrumbs.slice(-1) } : partitionTopBarBreadcrumbs(breadcrumbs),
    [breadcrumbs, currentOnly, trailing],
  );

  // The "…" spreads the pieces it hides out in its place, each cut to its
  // first letters, as Notion's (seen 2026-11-08); they stay out — through a
  // click elsewhere, Escape and a rename — until another page opens, back
  // to the same page too. Pressed or keyed, the focus goes with the "…" to
  // the page, and the next Tab goes on to the first piece shown, as Notion's.
  // Measured after the press only (no script in the page before it), Notion
  // in Chrome 1440, a page four levels deep: the "…" clicked, "z" typed went
  // nowhere, the focus on <body>; Tab to the "…" and Enter, no ring anywhere
  // and the next Tab ringing the first piece shown (2026-11-15, worker w1139,
  // and w1249 in another tab at another time).
  const [revealedFor, setRevealedFor] = useState<string | null>(null);
  if (revealedFor !== null && revealedFor !== page.id) setRevealedFor(null);
  const revealed = revealedFor === page.id && hidden.length > 0;
  const firstRevealed = useRef<HTMLAnchorElement>(null);
  const focusRevealed = useRef(false);
  useLayoutEffect(() => {
    if (!revealed || !focusRevealed.current) return;
    focusRevealed.current = false;
    // Tab goes on from the piece before the first shown, which keeps no focus.
    const before = firstRevealed.current?.parentElement?.previousElementSibling?.querySelector<HTMLElement>("a");
    before?.focus({ preventScroll: true });
    before?.blur();
  }, [revealed]);

  // A piece the pointer rests on lists the pages beside its page, as
  // Notion's: only a page of the workspace's own list, not a database's
  // piece or a database page standing in for one.
  const workspacePages = productPages?.pages;
  const hover = useCrumbHover();
  const hoverable = (pageId: string) => !currentOnly && Boolean(workspacePages?.some(({ id }) => id === pageId));
  const hoveredPage = hover.open ? workspacePages?.find(({ id }) => id === hover.open?.key) : undefined;
  // The private and shared pages in their sidebar sections' order.
  const layout = useReadSidebarLayout(productPages?.session?.user.id ?? "", page.workspaceId) ?? DEFAULT_SIDEBAR_LAYOUT;
  const onPath = useMemo(() => new Set([...breadcrumbs.map(({ id }) => id), page.teamspaceId ?? ""]), [breadcrumbs, page.teamspaceId]);

  function openBreadcrumb(
    event: ReactMouseEvent<HTMLAnchorElement | HTMLButtonElement>,
    pageId: string,
  ) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (onOpenPage?.(pageId)) return;
    router.push(hrefFor(pageId));
  }

  // The last piece, the page's own or an inline database's opened alone, as
  // Notion's (seen 2026-10-27): on a phone it opens the path as a sheet,
  // elsewhere the rename popover under it.
  const [pathOpen, setPathOpen] = useState(false);
  function openCurrent(event: ReactMouseEvent<HTMLAnchorElement>) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (currentOnly) setPathOpen(true);
    else onRenameCurrent?.(renamePopoverUnder(event.currentTarget));
  }

  function link(breadcrumb: TopBarBreadcrumb<Page>, cut = false) {
    const current = breadcrumb.id === page.id && !trailing;
    return (
      <a
        ref={cut && breadcrumb === hidden[0] ? firstRevealed : undefined}
        className={styles.crumb}
        href={hrefFor(breadcrumb.id)}
        aria-current={current ? "page" : undefined}
        // The cut name keeps the whole one for readers.
        aria-label={cut ? breadcrumb.label : undefined}
        onClick={(event) => current ? openCurrent(event) : openBreadcrumb(event, breadcrumb.id)}
        onPointerOver={current ? undefined : readView(breadcrumb.id)}
        {...(hoverable(breadcrumb.id) ? hover.bind(breadcrumb.id) : {})}
      >
        <BreadcrumbContent breadcrumb={breadcrumb}
          label={cut ? revealedBreadcrumbLabel(titleWithClosedMentions(breadcrumb.page.title, breadcrumb.page.titleSpans), untitledPageDisplayTitle(breadcrumb.page)) : undefined} />
      </a>
    );
  }

  return (
    <>
      {hidden.length > 0 && !revealed ? (
        <>
          <span className={styles.crumbWrap}>{link(visible[0])}</span>
          <span className={styles.crumbWrap} data-hidden-crumbs>
            <span className={styles.sep} aria-hidden="true">/</span>
            {/* The pointer on it names how many it hides in the dark tip
                (its title), and lists no pages, as Notion's (seen 2026-11-08). */}
            <button
              type="button"
              className={styles.crumb}
              aria-label={t("hiddenBreadcrumbPages")}
              aria-expanded={false}
              title={t("hiddenBreadcrumbCount", { count: hidden.length })}
              onClick={(event) => {
                // A press from the keyboard (no pointer detail) leaves Tab's place before the pieces.
                focusRevealed.current = event.detail === 0;
                setRevealedFor(page.id);
              }}
            >
              {/* Three full stops in the path's own words, as Notion's (seen 2026-11-07: 24px, dots on the line). */}
              <span aria-hidden="true">...</span>
            </button>
          </span>
          {visible.slice(1).map((breadcrumb) => (
            <span key={breadcrumb.id} className={styles.crumbWrap}>
              <span className={styles.sep} aria-hidden="true">/</span>
              {link(breadcrumb)}
            </span>
          ))}
        </>
      ) : (revealed ? breadcrumbs : visible).map((breadcrumb, index) => {
        const cut = revealed && hidden.includes(breadcrumb);
        return (
          <span key={breadcrumb.id} className={styles.crumbWrap} data-revealed={cut ? "true" : undefined}>
            {index > 0 ? (
              <span className={styles.sep} aria-hidden="true">/</span>
            ) : null}
            {link(breadcrumb, cut)}
          </span>
        );
      })}
      {trailing ? (
        <span className={styles.crumbWrap} data-topbar-trailing-crumb>
          {visible.length > 0 ? <span className={styles.sep} aria-hidden="true">/</span> : null}
          <a className={styles.crumb} href={trailing.href} aria-current="page" onClick={openCurrent}>
            {hasExplicitPageIcon(trailing.icon) ? (
              <span className={styles.crumbIcon} data-topbar-crumb-icon aria-hidden="true">
                <PageIconGlyph page={trailing.icon} size={18} fallback="none" />
              </span>
            ) : null}
            <span className={styles.crumbLabel} data-topbar-crumb-label>{trailing.label}</span>
          </a>
        </span>
      ) : null}
      {hover.open && hoveredPage && workspacePages ? (
        <CrumbMenu key={hover.open.key} anchor={hover.open.anchor} pages={workspacePages} currentId={trailing ? "" : page.id} onPath={onPath}
          list={pagesBeside(hoveredPage, workspacePages, { shared: productPages?.sharedPageIds ?? [], workspaceWide: productPages?.workspaceWidePageIds ?? [] },
            { shared: t("sidebar:sharedPages"), private: t("sidebar:privatePages") },
            { shared: sidebarSection(layout, SIDEBAR_HOME_TAB, "shared").sort, private: sidebarSection(layout, SIDEBAR_HOME_TAB, "private").sort })}
          onStay={hover.stay} onLeave={hover.leave} onClose={hover.close} />
      ) : null}
      {pathOpen ? (
        <TopBarPathSheet page={page} pages={pages} userId={productPages?.session?.user.id ?? ""} onClose={() => setPathOpen(false)}
          rows={[
            ...breadcrumbs.map((breadcrumb) => ({
              key: breadcrumb.id, icon: <PageIconGlyph page={breadcrumb.page} size={PATH_SHEET_MARK} />, label: breadcrumb.label,
              onPress: () => {
                setPathOpen(false);
                if (breadcrumb.id === page.id && !trailing) return;
                if (!onOpenPage?.(breadcrumb.id)) router.push(hrefFor(breadcrumb.id));
              },
            })),
            ...(trailing ? [{
              key: "trailing", icon: <PageIconGlyph page={trailing.icon} size={PATH_SHEET_MARK} fallback="database" />, label: trailing.label,
              onPress: () => setPathOpen(false),
            }] : []),
          ]} />
      ) : null}
    </>
  );
}
