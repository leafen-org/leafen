import { Suspense, useSyncExternalStore, type ComponentProps } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
// A database row's page opens in the peek when asked for (standaloneRowPageScreen.ts).
import { StandaloneRowPageScreen as StandaloneRowPage } from "@/components/database/standaloneRowPageScreen";
import { pageDrawnAsRowPage } from "@/lib/pageDrawnAsRow";
import { PagePeek } from "@/components/page/PagePeek";
import type { PagePresentationChange } from "@/components/page/pagePresentation";
import { pageHref } from "@/lib/navigation";
import { openPageInNewTab } from "@/lib/databasePageLink";
import { ADD_TO_PEEK_PARAM, FORM_PEEK_MODE_PARAM, FORM_PEEK_PARAM, closePagePeek, inlineDatabasePeekHref, pagePeekHref, peekedPageId, peekModeOf } from "@/lib/pagePeek";
import { isFreshPage, keepFreshPage, leaveFreshPage } from "@/components/page/freshPages";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { pushRoute, replaceRoute, routeInfoFromPath, usePathname, useSearchParams } from "@/lib/router";
import type { Page, Session } from "@/types";
import type { PageTitleBase } from "@/lib/pageTitleFollow";
import type { TextSpan } from "@/lib/types";
import { usePageActionAccess } from "@/lib/pageActionAccessContext";
import { useRowHasOpener } from "@/components/database/databaseRowOpeners";
import { useLentRowSiblings } from "@/components/database/lentRowSiblings";
import { RowPeekSiblingKeys } from "@/components/database/rowPeekSiblingKeys";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { WIKI_HOME_VIEW_ID } from "@/lib/wikiHome";
import { useProductPages } from "./productPagesContext";
import { personName, subscribePeopleDirectory } from "@/lib/peopleDirectory";
import { workspacePeopleListed } from "@/lib/workspacePeopleDirectory";
import { isKnownPeopleDatabasePage, subscribePeopleDatabasePages } from "@/lib/peopleDatabasePage";
import { usePublishWikiPageMenuHost } from "./wikiPageMenuHost";
import { usePageLockState } from "./usePageLockState";
import { PeekedInlineDatabaseAsk, usePeekedInlineDatabase } from "./usePeekedInlineDatabase";

// A form a button opened, beside or over the page (`?pf=<link>&pfm=s|c`).
const FormPeek = lazyScreen(() => import("@/components/forms/FormPeek").then((module) => ({ default: module.FormPeek })));
// A person a mention in a peek opened there (openPersonInPeek).
// Fetched once the app is idle, so it takes the peek's place at once, as Notion's swaps it.
const PersonPeek = lazyScreen(() => import("@/components/people/PersonPeek").then((module) => ({ default: module.PersonPeek })), { typedOnOpen: true });
// People beside the page, its own page named by the address (peoplePeekHref).
const PeoplePeek = lazyScreen(() => import("@/components/people/PeoplePeek").then((module) => ({ default: module.PeoplePeek })));

/**
 * The page the address opens in a peek (`?p=<page>&pm=s|c`), over whatever
 * is open; a database page opens in it too, as a sidebar database view
 * opens its rows, unless the open page holds its database, whose own peek
 * opens it.
 */
export function AuthenticatedPagePeek({
  pages,
  session,
  organizationId,
  onTitleChange,
  onTitleTyped,
  onPresentationChange,
  onPageChange,
  favoritePageIds,
  onFavoriteChange,
  actions,
}: {
  pages: Page[];
  session: Session;
  organizationId?: string;
  onTitleChange: (page: Page, title: string, base?: PageTitleBase, spans?: TextSpan[]) => unknown;
  /** A title as it is typed, shown at once where the page is named (PageHeader). */
  onTitleTyped?: (page: Page, title: string) => void;
  onPresentationChange: (page: Page, change: PagePresentationChange) => void;
  onPageChange: (page: Page) => void;
  favoritePageIds: readonly string[];
  onFavoriteChange: (pageId: string, favorite: boolean) => Promise<void>;
  /** The page's menu operations, as the page's own top bar uses them. */
  actions: Pick<ComponentProps<typeof PagePeek>["actions"], "onPageMetadataChange" | "onDuplicatePage" | "onTrashChange" | "onStatus">;
}) {
  const search = useSearchParams();
  const route = routeInfoFromPath(usePathname());
  // A full-page database by its database's ID (pagePeekHref).
  const pageId = peekedPageId(search.get("p"));
  // An address's peek, at the side or in the middle (peekModeOf); Leafen's "f" isn't one.
  const peekMode = peekModeOf(search.get("pm"));
  const modeParam = peekMode === "side" ? "s" : "c";
  const peeked = pageId && peekMode !== "full";
  // An inline database it shows alone (inlineDatabasePeekHref), its page holding it.
  const database = usePeekedInlineDatabase(search);
  const shownPageId = database?.pageId ?? pageId;
  const page = peeked ? pages.find((candidate) => candidate.id === shownPageId && !candidate.inTrash) : undefined;
  // Read for a page this peek opens: a database row a view opens isn't one,
  // and its read stood in line with the row page's (2026-11-12).
  const access = usePageActionAccess(shownPageId ?? "", Boolean(page));
  // A locked page it shows is locked for the person as on its own page, and
  // says so, so text written here that its room refuses is let go and
  // offered to copy (lib/refusedTextLetGo.ts); a database page shown as one
  // says so itself (DatabaseRowPage).
  usePageLockState(session, page && !pageDrawnAsRowPage(page) ? page : null);
  // A database page opened from inside another opened page, or one a view on
  // the screen opens, opens in that view's peek above the page it came from.
  // One opened to be put somewhere (My tasks, the sidebar's "+") is this peek's, whatever view is on the screen.
  const heldByView = (useRowHasOpener(peeked ? pageId : null) || Boolean(search.get("pp"))) && search.get(ADD_TO_PEEK_PARAM) !== "true";
  // One a view on the screen opens its own way (My tasks, Home's) steps through that view's rows with ⌃⇧K and ⌃⇧J,
  // its bar showing no ∧ ∨, as Notion's (RowPeekSiblingKeys).
  const lentSiblings = useLentRowSiblings(peeked && !database ? pageId : null);
  const productPages = useProductPages();
  // A wiki's page in a peek — this one's, or a view's — takes the page menu with these (WikiPageMenu).
  usePublishWikiPageMenuHost({ pages, session, favorite: (id) => favoritePageIds.includes(id), actions: (shown) => ({
    ...actions, onPageChange, onFavoriteChange: (favorite) => onFavoriteChange(shown.id, favorite),
    onPresentationChange: (change) => onPresentationChange(shown, change),
    onImport: productPages?.openImport ? (into) => productPages.openImport?.({ pageId: into.id, title: into.title || untitledPageDisplayTitle(into) }) : undefined,
  }) });
  // Whether the address names a person of the workspace, as its people come.
  const person = useSyncExternalStore(subscribePeopleDirectory, () => Boolean(pageId && personName(pageId)), () => false);
  const peopleListed = useSyncExternalStore(subscribePeopleDirectory, workspacePeopleListed, () => false);
  // Whether the address names People's own page, which is in no page list.
  const people = useSyncExternalStore(subscribePeopleDatabasePages, () => isKnownPeopleDatabasePage(pageId), () => false);
  const formToken = search.get(FORM_PEEK_PARAM);
  const formMode = search.get(FORM_PEEK_MODE_PARAM);
  if (formToken && (formMode === "s" || formMode === "c")) {
    return <Suspense fallback={null}><FormPeek token={formToken} mode={formMode === "c" ? "center" : "side"} session={session} /></Suspense>;
  }
  // The people list's peek is its person's, which it opens itself (PeopleView): read here as
  // a page or a row too, its person's ID came back "not found" twice beside it.
  if (route.kind === "people" && !route.userId) return null;
  // A person, by their ID, in the workspace that knows them.
  const workspace = productPages?.workspaces?.find((candidate) => candidate.id === productPages.workspaceId);
  if (peeked && !page && workspace && people) {
    return <Suspense fallback={null}>
      <PeoplePeek key={pageId} session={session} workspace={workspace} pageId={pageId} mode={modeParam === "c" ? "center" : "side"} onClose={closePagePeek} />
    </Suspense>;
  }
  if (peeked && !page && workspace && route.kind !== "people" && person) {
    return <Suspense fallback={null}>
      <PersonPeek key={pageId} session={session} workspace={workspace} userId={pageId} mode={modeParam === "c" ? "center" : "side"} onClose={closePagePeek} />
    </Suspense>;
  }
  // A wiki's page is its database's row and opens as one here too, though it
  // is among the pages, with its properties, as Notion's (2026-11-02: peeked
  // from another page it showed as a plain page).
  const rowPage = pageDrawnAsRowPage(page);
  // One a view on the screen opens — the wiki's own "모든 페이지" — is that
  // view's peek alone, as its other rows: Notion opens one peek (seen
  // 2026-11-17 at 1440, twice), and this one stood over the view's.
  if (peeked && heldByView && rowPage) return null;
  // An ID no listed page has may be a person's until the workspace's people
  // come (a person's peek read again): asked as a row before, it came back
  // "not found".
  const unlisted = !page && !pages.some((candidate) => candidate.id === shownPageId);
  if (peeked && !heldByView && unlisted && !peopleListed) return null;
  if (peeked && !heldByView && (rowPage || unlisted)) {
    // An inline database whose page is a database's shows that page, as an
    // older address's (`pdb`) did; an ID that is no database page may be an
    // inline database whose page isn't known here yet (PeekedInlineDatabaseAsk).
    return <Suspense fallback={null}>
      {lentSiblings ? <RowPeekSiblingKeys siblings={lentSiblings} /> : null}
      <StandaloneRowPage key={database?.pageId ?? pageId} session={session} rowId={database?.pageId ?? pageId} organizationId={organizationId}
        fallback={database ? null : <PeekedInlineDatabaseAsk session={session} id={pageId} />}
        onClose={closePagePeek} peek={{
          mode: modeParam === "c" ? "center" : "side",
          // A wiki's Home (`v=home`) shows no view of its database to peek its pages: this peek does.
          openPageId: route.kind === "page" && search.get("v") !== WIKI_HOME_VIEW_ID ? route.pageId : undefined,
          onOpenMode: (mode) => {
            if (mode === "full") pushRoute(pageHref(pageId));
            else if (mode === "new_tab") {
              openPageInNewTab(pages.find((candidate) => candidate.id === pageId) ?? { id: pageId }, session.user.id);
              closePagePeek();
            } else replaceRoute(pagePeekHref(pageId, mode === "center" ? "center" : "side"));
          },
        }} />
    </Suspense>;
  }
  if (!page) return null;
  // An inline database of the page, opened alone in the peek.
  const databaseBlockId = database?.blockId;
  if (databaseBlockId) {
    return (
      <PagePeek page={page} mode={modeParam === "c" ? "center" : "side"} session={session} inlineDatabaseBlockId={databaseBlockId}
        organizationId={organizationId} canEdit={access.canEdit} readOnly={Boolean(page.isLocked) || !access.canEdit}
        onClose={closePagePeek}
        onOpenMode={(mode) => {
          const full = inlineDatabaseFullPageHref(page.id, databaseBlockId);
          if (mode === "full") pushRoute(full);
          else if (mode === "new_tab") {
            window.open(full, "_blank", "noopener,noreferrer");
            closePagePeek();
          } else replaceRoute(inlineDatabasePeekHref(page.id, databaseBlockId, mode === "center" ? "center" : "side"));
        }}
        onTitleChange={(title, base, spans) => onTitleChange(page, title, base, spans)}
        onPresentationChange={(change) => onPresentationChange(page, change)}
        onPageChange={onPageChange}
        // Its ☆ makes the database a favorite, not the page (PagePeekActions).
        actions={{ ...actions, pages, favorite: favoritePageIds.includes(page.id), onFavoriteChange: (favorite) => onFavoriteChange(page.id, favorite) }} />
    );
  }
  // A page the sidebar's "+" just made, closed with nothing in it, goes (freshPages.ts).
  const addTo = search.get(ADD_TO_PEEK_PARAM) === "true";
  const close = () => {
    closePagePeek();
    if (addTo && isFreshPage(page.id)) {
      void leaveFreshPage(session, page, (made) => actions.onTrashChange(made, true, new AbortController().signal)).catch(() => undefined);
    }
  };
  return (
    <PagePeek page={page} mode={modeParam === "c" ? "center" : "side"} session={session}
      organizationId={organizationId} canEdit={access.canEdit} canComment={access.canComment} readOnly={Boolean(page.isLocked) || !access.canEdit}
      addTo={addTo} onClose={close}
      onOpenMode={(mode) => {
        keepFreshPage(page.id);
        if (mode === "full") pushRoute(pageHref(page.id));
        else if (mode === "new_tab") {
          openPageInNewTab(page, session.user.id);
          closePagePeek();
        } else replaceRoute(pagePeekHref(page.id, mode === "center" ? "center" : "side"));
      }}
      onTitleChange={(title, base, spans) => onTitleChange(page, title, base, spans)} onTitleTyped={(title) => onTitleTyped?.(page, title)}
      onPresentationChange={(change) => onPresentationChange(page, change)}
      onPageChange={onPageChange}
      actions={{
        ...actions, pages, favorite: favoritePageIds.includes(page.id),
        onFavoriteChange: (favorite) => onFavoriteChange(page.id, favorite),
      }} />
  );
}
