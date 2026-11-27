import { personValue, storePersonValue } from "./browserStorage";

/**
 * The page the product opens with in a workspace, as Notion's "시작 페이지"
 * (seen 2026-10-02): the last page visited there (the default), the
 * sidebar's top page, or the library. Notion keeps it on the device for
 * each person and workspace; its fourth choice, Notion AI, isn't in Leafen.
 */
export const START_PAGES = ["last_visited_page", "sidebar_top_page", "library"] as const;
export type StartPage = typeof START_PAGES[number];

const START_PAGE_KEY = "leafen:start-page";
const LAST_OPENED_PAGE_KEY = "leafen:last-opened-page";

function place(userId: string, workspaceId: string) {
  return { userId, part: encodeURIComponent(workspaceId) };
}

export function readStartPage(userId: string, workspaceId: string): StartPage {
  const value = personValue(START_PAGE_KEY, {
    ...place(userId, workspaceId),
    previous: [`${START_PAGE_KEY}:${encodeURIComponent(userId)}:${encodeURIComponent(workspaceId)}`],
  });
  return (START_PAGES as readonly string[]).includes(value ?? "") ? value as StartPage : "last_visited_page";
}

export function saveStartPage(userId: string, workspaceId: string, value: StartPage) {
  // Without storage the default stays.
  storePersonValue(START_PAGE_KEY, value, place(userId, workspaceId));
}

/**
 * The page last opened in a workspace on this device, which the product
 * opens with when it can't read the visits the server keeps, as when it
 * starts without its server.
 */
export function readLastOpenedPage(userId: string, workspaceId: string): string | null {
  return personValue(LAST_OPENED_PAGE_KEY, place(userId, workspaceId));
}

export function saveLastOpenedPage(userId: string, workspaceId: string, pageId: string) {
  storePersonValue(LAST_OPENED_PAGE_KEY, pageId, place(userId, workspaceId));
}
