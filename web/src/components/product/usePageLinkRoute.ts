import { useEffect, useRef, useState } from "react";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { inlineDatabaseAtAddress, rememberFullPageDatabase, rememberInlineDatabaseAddress } from "@/lib/inlineDatabaseAddress";
import { pageHref } from "@/lib/navigation";
import { PEOPLE_DIRECTORY_HREF } from "@/lib/peopleProfiles";
import { replaceRoute, routeInfoFromPath } from "@/lib/router";
import type { Page, Session } from "@/types";
import { useHeldSignIn } from "@/lib/heldSignIn";
import { rowPageHandedOver } from "@/lib/rowPageHandoff";

// A page URL from the Notion-compatible API names a page or a database row
// page by its public ID, and a row opened as a full page names the row. When
// the route names a page the workspace data does not hold, the server says
// which route opens it for this reader, and the address is replaced with it.
// A database in a page opens at its own ID too, held by the page it names,
// and a full-page one at its database's ID: an address naming its page is
// replaced with that one, as Notion has one address for it. People's database
// and its rows open as People and its people (page_links_people.go). The
// question is asked as soon as the list shown — a cached copy at first —
// lacks the page, and kept while the server's list lacks it too; the address
// it is out for is given back, so that the row the address may name is read
// only once the server said it is one: read first, People's own page came
// back "not found" before its address turned to People. The answer comes
// with the list, so the row is read no later than before. It is asked once
// for the person: their sign-in handed over or renewed asks nothing again
// (2026-11-16: the row opened whole went while it was asked again). Its
// answer — where the address opens, or that it opens nothing for them —
// holds for the address it leaves on screen until the address changes or
// the page enters the list: another tab's or person's page added, renamed or
// moved changes the list and asks nothing again (2026-11-16: the row opened
// whole, and the screen saying a page can't be found, went for a frame at
// each such change while it was asked again). A question that failed is no
// answer and is asked again at the next change.
export function usePageLinkRoute(session: Session, pathname: string, pages: readonly Page[], settled: boolean) {
  const [asking, setAsking] = useState<string | null>(null);
  const signIn = useHeldSignIn(session);
  const question = useRef<{ id: string; controller: AbortController } | null>(null);
  // The page the address on screen opens that the server answered for.
  const answered = useRef<string | null>(null);
  useEffect(() => {
    const route = routeInfoFromPath(pathname);
    const listed = route.kind === "page" && pages.some(({ id }) => id === route.pageId);
    if (route.kind !== "page" || listed || answered.current !== route.pageId) answered.current = null;
    if (route.kind !== "page" || listed || question.current && question.current.id !== route.pageId) {
      question.current?.controller.abort();
      question.current = null;
      setAsking(null);
    }
    if (route.kind !== "page") return;
    if (listed) {
      showAtDatabaseAddress(route.pageId);
      return;
    }
    if (question.current || answered.current) return;
    // A row its peek just handed over opens as the row it is, drawn at once
    // from what the peek drew (rowPageHandoff.ts): asked first, the page
    // stood empty till the answer (some 300 ms on a NAS, 2026-11-17).
    if (rowPageHandedOver(route.pageId)) {
      answered.current = route.pageId;
      return;
    }
    const controller = new AbortController();
    const asked = { id: route.pageId, controller };
    question.current = asked;
    setAsking(route.pageId);
    // A database at its own address held by a page outside the list — a
    // row's — is asked by its own ID, which opens it alone there: asked by
    // the page's, the address went to the page whole.
    void askPageLink(signIn(), inlineDatabaseAtAddress(window.location.pathname) ?? route.pageId, controller.signal).then((answer) => {
      if (controller.signal.aborted) return;
      followPageLink(answer);
      answered.current = pageAtAddressOnScreen();
    }, (error: unknown) => {
      if (!controller.signal.aborted && error instanceof LeafenApiError && error.status === 404) answered.current = asked.id;
    }).finally(() => {
      if (question.current !== asked) return;
      question.current = null;
      setAsking(null);
    });
  }, [pages, pathname, signIn]);
  useEffect(() => () => question.current?.controller.abort(), []);
  return settled ? asking : null;
}

// The page the address on screen opens once an answer was followed: the
// row or page it went to, or the page holding the database it names.
function pageAtAddressOnScreen() {
  const route = routeInfoFromPath(window.location.pathname);
  return route.kind === "page" ? route.pageId : null;
}

// A full-page database's page named by its own ID shows at its database's.
function showAtDatabaseAddress(pageId: string) {
  const address = pageHref(pageId);
  if (address !== window.location.pathname && window.location.pathname === `/p/${encodeURIComponent(pageId)}`) {
    replaceRoute(`${address}${window.location.search}${window.location.hash}`);
  }
}

/**
 * Where an address opens for this reader, as the server says
 * (page_links.go), with the view it names: the API's view url names it by
 * its public ID, which the server names the app's way.
 */
export function askPageLink(session: Session, id: string, signal: AbortSignal) {
  return api.resolvePageLink(session, id, signal, new URLSearchParams(window.location.search).get("v"));
}

/**
 * Goes where the server said an address opens, in its place, keeping what
 * else the address asks for; false when that is no page or the address as it
 * is. A database opens at its own ID, shown alone by the page it is in or as
 * its own full page, which the address is told again once known.
 */
export function followPageLink({ path, holderPageId, databasePageId }: { path: string; holderPageId?: string; databasePageId?: string }) {
  // People's database and its rows open as People and its people (page_links_people.go).
  if (path === PEOPLE_DIRECTORY_HREF || path.startsWith(`${PEOPLE_DIRECTORY_HREF}/`)) {
    replaceRoute(`${path}${window.location.hash}`);
    return true;
  }
  if (!path.startsWith("/p/")) return false;
  const next = new URL(path, window.location.origin);
  const blockId = decodeURIComponent(next.pathname.slice("/p/".length));
  if (holderPageId) rememberInlineDatabaseAddress(blockId, holderPageId, true);
  else if (databasePageId) rememberFullPageDatabase(blockId, databasePageId);
  new URLSearchParams(window.location.search).forEach((value, key) => {
    if (!next.searchParams.has(key)) next.searchParams.set(key, value);
  });
  // A block's address opens its page at the block the answer names (page_link_blocks.go).
  const address = `${next.pathname}${next.search}${next.hash || window.location.hash}`;
  if (!holderPageId && !databasePageId && address === `${window.location.pathname}${window.location.search}${window.location.hash}`) return false;
  replaceRoute(address);
  return true;
}
