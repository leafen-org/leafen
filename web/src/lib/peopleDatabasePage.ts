import { LEAFEN_SYSTEM_USER_ID } from "@/lib/peopleDirectory";
import { pagePeekHref } from "@/lib/pagePeek";

/**
 * People's icon wherever it is drawn, as Notion's People carries it: the
 * built-in person in a ring in gray (Notion's "/icons/user-circle_gray.svg",
 * seen 2026-11-19 in Chrome at 1440, its Business-trial owner: its search
 * result 19.2px in the 20px slot, its bar's path 18, its title 36, and the
 * lists of sources — "데이터 소스 연결", "관계형 대상" — 19.2 in their 20px
 * box). The server reads its source with it (people_database_icon.go), so
 * the lists draw it as a source's own icon (DataSourceMark).
 */
export const PEOPLE_DATABASE_ICON = "user-circle:gray";

/**
 * Whether a page is the workspace's People database: a database the server
 * made as its own (people_database.go), in no reader's page list, which
 * opens as People wherever it is listed — search, the library, favorites.
 */
export function isPeopleDatabasePage(page: { kind?: string; createdBy?: string | null }) {
  return page.kind === "database" && page.createdBy === LEAFEN_SYSTEM_USER_ID;
}

// The People pages the app has met — listed among favorites, or read as
// People — which an address's peek (`?p=〈People〉`) opens as People, not as
// a page: it is in no page list to tell it by.
const known = new Set<string>();
const listeners = new Set<() => void>();

export function rememberPeopleDatabasePage(pageId: string) {
  if (!pageId || known.has(pageId)) return;
  known.add(pageId);
  for (const listener of listeners) listener();
}

export function isKnownPeopleDatabasePage(pageId: string | null) {
  return Boolean(pageId && known.has(pageId));
}

export function subscribePeopleDatabasePages(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/**
 * People beside the page open, as Notion's ⌥-pressed favorite and its
 * "사이드 보기에서 열기" open it (seen 2026-11-16 in Chrome at 1440, in two
 * tabs at two times: `?p=〈People〉&pm=s`, the page left where it was).
 */
export function peoplePeekHref(pageId: string, mode: "side" | "center" = "side") {
  rememberPeopleDatabasePage(pageId);
  return pagePeekHref(pageId, mode);
}
