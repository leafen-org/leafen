import { type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { usePagesWithEditTimes } from "@/lib/pageEditedHere";
import { ChevronLeft, ChevronRight, ClockIcon, Pencil, Plus } from "@/icons/leafen";
import { HoverTip } from "@/components/IconTipButton";
import type { HomeWidget } from "./HomeMenu";
import { HomeWidgetHead } from "./HomeWidgetMenu";
import { listedPageIcon, PageIconGlyph } from "@/components/page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useProductPages } from "@/components/product/productPagesContext";
import { useLibraryPages, usePageVisitsReadNow } from "@/components/library/useLibraryData";
import { libraryRowHref, libraryRowPage } from "@/components/library/libraryModel";
import { pageHref } from "@/lib/navigation";
import { pagePlace, placeText } from "@/lib/pagePath";
import { pageTrashRoot } from "@/lib/pageTrash";
import { editorName } from "@/lib/peopleDirectory";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { PageTitleText } from "@/components/page/TitleWithCustomEmoji";
import { relativeAgoLabel } from "@/lib/relativeTime";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import type { Page, Session } from "@/types";
import type { RecentEdit } from "@/workspaceMembershipWire.generated";
import styles from "./HomeView.module.css";
import { listedPageNameKind } from "../../lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import { inlineSign } from "@/lib/readingDirection";
import { usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { pagesByIdOf } from "@/lib/pageIndex";

/** As many pages as each of Home's lists shows. */
const HOME_LIST_LENGTH = 8;
// Notion's row of pages visited runs on to about 19 cards (seen 2026-11-03).
const HOME_VISITS_LENGTH = 20;

/**
 * A Home section's head: its mark and name, small and grey, as Notion's
 * (HomeView.module.css); what the section is for in a tip above it from its
 * start while the pointer is on it, where Notion's says (seen 2026-11-11).
 */
export function HomeSectionHead({ id, icon, tip, widget, children }: { id: string; icon: ReactNode; tip?: string;
  /** A widget Home may hide, with its "⋯" (HomeWidgetMenu). */
  widget?: HomeWidget; children: ReactNode }) {
  const name = <>{icon}<span>{children}</span></>;
  return (
    <HomeWidgetHead widget={widget}>
      <h2 id={id} className={styles.sectionHead}>
        {tip ? <HoverTip label={tip} placement="top-start" gap={8} className={styles.sectionHeadTip}>{name}</HoverTip> : name}
      </h2>
    </HomeWidgetHead>
  );
}

/**
 * Home's "최근 방문": the pages the reader opened last in the workspace,
 * newest first (the one-sidebar decision of 2026-10-29, 2: Home holds the
 * documents visited and edited last), as Notion's cards with who edited each
 * last and when, then "+ 새 페이지" (the decision of 2026-10-31).
 */
export function HomeRecentVisits({ session, workspaceId, onCreatePage, canCreatePage = true }: {
  session: Session; workspaceId: string; onCreatePage?: () => void; canCreatePage?: boolean;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("homeView");
  // Each card's edit time as last known, an edit here in a peek with it (lib/pageEditedHere.ts).
  const pages = usePagesWithEditTimes(useProductPages()?.pages);
  // A page opened a moment ago leads them, its visit not kept yet (usePageVisitsReadNow).
  const visitsRead = usePageVisitsReadNow(session, workspaceId);
  const visits = visitsRead.list;
  const pagesById = pagesByIdOf(pages ?? []);
  // A database row's or template's page visited isn't in the page list: the
  // server reads what its card shows, as Notion's Home lists them too, a
  // template under its name and opened full as its page (seen 2026-11-11).
  const unlistedIds = pages ? visits.map((visit) => visit.pageId).filter((id) => !pagesById[id]) : [];
  const unlisted = useLibraryPages(session, workspaceId, unlistedIds).pages;
  // That none were visited is said once the server said so and each visit's
  // page is known; until then the row stays as it is, and without the server
  // that they can't be read now, as "최근 편집" says it (HomeRecentEdits).
  const noneKnown = visitsRead.state === "ready" && pages !== undefined && unlistedIds.every((id) => unlisted.has(id));
  const note = visitsRead.state === "offline" ? t("recentVisitsOffline") : visitsRead.state === "failed" ? t("recentVisitsFailed") : null;
  const shown = visits.flatMap((visit): { page: Page; href: string }[] => {
    const page = pagesById[visit.pageId];
    if (page) return pageTrashRoot(page, (id) => pagesById[id]) ? [] : [{ page, href: pageHref(page.id) }];
    const detail = unlisted.get(visit.pageId);
    const row = detail ? libraryRowPage(detail, workspaceId) : null;
    return detail && row ? [{ page: row, href: libraryRowHref(detail, true) }] : [];
  }).slice(0, HOME_VISITS_LENGTH);
  return (
    <section className={styles.homeSection} aria-labelledby="home-recent-visits-title" data-home-recent-visits>
      <HomeSectionHead id="home-recent-visits-title" icon={<ClockIcon size="small" aria-hidden="true" />}
        tip={t("recentVisitsTip")}>{t("recentVisits")}</HomeSectionHead>
      {note ? <p className={styles.myTasksNote} role="status">{note}</p> : null}
      {shown.length === 0 && !onCreatePage ? (noneKnown ? <p className={styles.myTasksNote}>{t("recentVisitsEmpty")}</p> : null) : (
        <HomeCardRow>
          {shown.map(({ page, href }) => {
            const editor = page.lastEditedBy || page.createdBy || null;
            // Its words are all a card says: a pointer on it shows no tip, no
            // place (Notion's Home, seen 2026-11-18 at 1440 on two cards).
            return (
              <a key={page.id} className={styles.card} href={href} data-home-page={page.id}>
                <span className={styles.cardBand} aria-hidden="true" />
                <span className={styles.cardIcon} aria-hidden="true"><PageIconGlyph page={page} size={28} /></span>
                <span className={styles.cardTitle}>{pageNames.title(page) ? <PageTitleText page={page} /> : t("untitled")}</span>
                <span className={styles.cardFoot}>
                  <PersonAvatar userId={editor} name={editorName(editor, session.user.id)} size={16} />
                  <span>{relativeAgoLabel(page.updatedAt)}</span>
                </span>
              </a>
            );
          })}
          {onCreatePage ? (
            <button type="button" className={styles.card} data-testid="empty-workspace-create" disabled={!canCreatePage} onClick={onCreatePage}>
              <span className={styles.cardBand} aria-hidden="true" />
              <span className={styles.cardIcon} aria-hidden="true"><Plus size="regular" /></span>
              <span className={styles.cardTitle} data-new>{t("newPage")}</span>
            </button>
          ) : null}
        </HomeCardRow>
      )}
    </section>
  );
}

// A row of cards too long for the column fades out where it goes on, with an
// arrow there while the pointer is on it, as Notion's (seen 2026-11-05).
const CARD_ROW_STEP = 416;
function HomeCardRow({ children }: { children: ReactNode }) {
  const { t } = useTranslation("homeView");
  const rowRef = useRef<HTMLDivElement>(null);
  const [goesOn, setGoesOn] = useState({ before: false, after: false });
  const measure = useCallback(() => {
    const row = rowRef.current;
    if (!row) return;
    // Read from the right, a row scrolls to negative scrollLeft: how far along is its size.
    const along = Math.abs(row.scrollLeft);
    const before = along > 1, after = along + row.clientWidth < row.scrollWidth - 1;
    setGoesOn((now) => now.before === before && now.after === after ? now : { before, after });
  }, []);
  useLayoutEffect(() => {
    const row = rowRef.current;
    if (!row) return;
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(row);
    return () => observer.disconnect();
  }, [children, measure]);
  const go = (by: number) => rowRef.current?.scrollBy({ left: by * inlineSign(rowRef.current), behavior: "smooth" });
  return (
    <div className={styles.cardRow} data-before={goesOn.before || undefined} data-after={goesOn.after || undefined}>
      <div ref={rowRef} className={styles.cards} onScroll={measure}>{children}</div>
      {goesOn.before ? <button type="button" className={styles.cardRowArrow} data-side="before" aria-label={t("showPreviousCards")} onClick={() => go(-CARD_ROW_STEP)}><ChevronLeft size="small" aria-hidden="true" /></button> : null}
      {goesOn.after ? <button type="button" className={styles.cardRowArrow} data-side="after" aria-label={t("showNextCards")} onClick={() => go(CARD_ROW_STEP)}><ChevronRight size="small" aria-hidden="true" /></button> : null}
    </div>
  );
}

// Whose edits in which workspace (`of`), as read.
type EditsReady = { state: "ready"; of: string; list: RecentEdit[] };
type EditsLoad = { state: "loading" | "failed" | "offline"; of: string } | EditsReady;

// The edits last read by each reader in each workspace this visit, shown
// while the server doesn't answer.
const editsLastRead = new Map<string, EditsReady>();

/**
 * Home's "최근 편집": the pages the reader edited last (recent_edits.go),
 * each with where it is, as a visit's. Until the server answers the list
 * shown stays (none at first); that there are none is said once it answers
 * so, a refusal as one, and without the server the list last read this visit
 * shows, or that it can't be read offline, until it answers again — as Home's
 * "내 작업" (HomeMyTasks.tsx).
 */
export function HomeRecentEdits({ session, workspaceId }: { session: Session; workspaceId: string }) {
  const pageNames = usePageNames();
  const { t } = useTranslation("homeView");
  const pages = useProductPages()?.pages;
  const key = `${session.user.id}\u0000${workspaceId}`;
  const [load, setLoad] = useState<EditsLoad>({ state: "loading", of: key });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let stopRetry: (() => void) | undefined;
    api.readRecentEdits(session, workspaceId, controller.signal)
      .then((read) => {
        const ready: EditsReady = { state: "ready", of: key, list: read.edits };
        editsLastRead.set(key, ready);
        setLoad(ready);
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (!isServerUnreachable(error)) {
          setLoad({ state: "failed", of: key });
          return;
        }
        setLoad(editsLastRead.get(key) ?? { state: "offline", of: key });
        stopRetry = retryWhenServerMayAnswer(() => setAttempt((value) => value + 1));
      });
    return () => {
      controller.abort();
      stopRetry?.();
    };
  }, [attempt, key, session, workspaceId]);
  const pagesById = pagesByIdOf(pages ?? []);
  const here: EditsLoad = load.of === key ? load : { state: "loading", of: key };
  const shown = (here.state === "ready" ? here.list : []).filter((edit) => {
    const page = pagesById[edit.pageId];
    return !page || !pageTrashRoot(page, (id) => pagesById[id]);
  }).slice(0, HOME_LIST_LENGTH);
  usePathsAboveShown(shown.flatMap((edit) => pagesById[edit.pageId] ? [pagesById[edit.pageId]] : []), pagesById);
  return (
    <section className={styles.homeSection} aria-labelledby="home-recent-edits-title" data-home-recent-edits>
      <HomeSectionHead id="home-recent-edits-title" widget="recentEdits" icon={<Pencil size="small" aria-hidden="true" />}>{t("recentEdits")}</HomeSectionHead>
      {here.state === "failed" ? <p className={styles.myTasksNote} role="status">{t("recentEditsFailed")}</p> : null}
      {here.state === "offline" ? <p className={styles.myTasksNote} role="status">{t("recentEditsOffline")}</p> : null}
      {here.state !== "ready" ? null : shown.length === 0 ? <p className={styles.myTasksNote}>{t("recentEditsEmpty")}</p> : (
        <ul className={styles.myTasksList}>
          {shown.map((edit) => {
            const page = pagesById[edit.pageId];
            return (
              <li key={edit.pageId}>
                <a className={styles.homeRow} href={pageHref(edit.pageId)} data-home-page={edit.pageId}>
                  <span className={styles.myTaskIcon} aria-hidden="true">
                    <PageIconGlyph page={listedPageIcon(page, edit)} size={18} /></span>
                  <span className={styles.myTaskTitle}>{pageNames.listed(page, titleWithClosedMentions(edit.title, edit.titleSpans) || edit.untitledName, undefined,
                    listedPageNameKind(edit))}</span>
                  <span className={styles.homeRowNote}>{page ? placeText(pagePlace(page, pagesById)) : ""}</span>
                </a>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
