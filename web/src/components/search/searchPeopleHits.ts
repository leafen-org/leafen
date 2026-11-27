import { pageHref } from "@/lib/navigation";
import { isPeopleDatabasePage } from "@/lib/peopleDatabasePage";
import { LEAFEN_SYSTEM_USER_ID } from "@/lib/peopleDirectory";
import { personProfileHref } from "@/lib/peopleProfiles";
import { pushRoute } from "@/lib/router";
import type { Page } from "@/types";
import type { SearchHit } from "./searchModel";

// Search finds People and its people's pages, as Notion's does (seen
// 2026-11-15 in a free workspace: "People 데이터베이스 · Notion App" and each
// person, "People · Notion App"); the server made them as its own, so they
// are in no reader's list of pages (people_database.go).

/** People's database, which opens as People, as Notion's search opens its People. */
export function peopleDatabaseHit(page: Page) {
  return isPeopleDatabasePage(page);
}

/** Opens a page a search found: People's database as People, any other as the app opens a page. */
export function openSearchedPage(page: Page, newTab: boolean, open: (page: Page, newTab: boolean) => void) {
  if (!peopleDatabaseHit(page)) return open(page, newTab);
  if (newTab) window.open("/people", "_blank", "noopener,noreferrer");
  else pushRoute("/people");
}

/**
 * Opens a database item a search found whose database's page the reader's
 * list lacks — a person's page in People — as the person's profile it is
 * (the server names them on the row); false for any other.
 */
export function openUnlistedItem(hit: SearchHit, newTab: boolean, close: () => void) {
  if (!hit.row || hit.page.createdBy !== LEAFEN_SYSTEM_USER_ID) return false;
  const href = hit.row.person ? personProfileHref(hit.row.person) : pageHref(hit.row.containerPageId, new URLSearchParams({ p: hit.row.rowId, pm: "f" }));
  if (newTab) window.open(href, "_blank", "noopener,noreferrer");
  else pushRoute(href);
  close();
  return true;
}
