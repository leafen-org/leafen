import { useState } from "react";
import { usePageVisitsRead, type PageVisitsRead } from "@/components/library/useLibraryData";
import { visitsHereNow, withVisitsHere } from "@/lib/pageVisitsHere";
import { HOME_PATH, usePathname } from "@/lib/router";
import type { Session } from "@/types";

type RecentsRead = {
  workspaceId: string;
  /** Whether what the app first shows is known: "/" waits for its start page (AuthenticatedProductRoute). */
  settled: boolean;
  homeShown: boolean;
  /** How many times Home has shown since the sidebar came up; each reads the list again. */
  homeShows: number;
  /** The page open as the list was read, put at its top. */
  open: string | null;
};

/**
 * The sidebar's "최근 항목" as Notion's keeps it (seen 2026-11-22 in Chrome
 * at 1440 in its test workspace, two tabs at two times): read as the sidebar
 * comes up, with the page the app opens on at its top whatever its visit
 * says (a page loaded by its address heads the list though the server had
 * another visited later), and read again each time Home is shown. Pages
 * opened in between leave it as it is — 30 seconds and more on one, two in a
 * row — and come in at the next read (a page opened from Home heads the list
 * once Home shows again, however soon). Removing visits from the library
 * reads it again too (usePageVisitsRead). Returns the visits read and the page
 * ids in the order the section lists them.
 */
export function useSidebarRecentsRead(session: Session, workspaceId: string, openPageId: string | null) {
  const pathname = usePathname();
  const homeShown = pathname === HOME_PATH || pathname === `${HOME_PATH}/`;
  const settled = pathname !== "/";
  const [read, setRead] = useState<RecentsRead>(() => ({
    workspaceId, settled, homeShown, homeShows: 0, open: settled && !homeShown ? openPageId : null,
  }));
  // Worked out as it draws: another workspace starts over, what the app first
  // shows is taken once known, and Home shown anew reads with no page open.
  if (read.workspaceId !== workspaceId) {
    setRead({ workspaceId, settled, homeShown, homeShows: read.homeShows, open: settled && !homeShown ? openPageId : null });
  } else if (!read.settled && settled) {
    setRead({ ...read, settled, homeShown, open: homeShown ? null : openPageId });
  } else if (read.settled && read.homeShown !== homeShown) {
    setRead({ ...read, homeShown, homeShows: read.homeShows + (homeShown ? 1 : 0), open: homeShown ? null : read.open });
  }
  const visitsRead: PageVisitsRead = usePageVisitsRead(session, workspaceId, `home:${read.homeShows}`);
  // With the visits made here by the time it was asked for that the server
  // may not have kept yet (lib/pageVisitsHere.ts): a page opened just before
  // Home shows is in; one opened after waits for the next read.
  const asked = visitsHereNow().filter((visit) => visit.at <= visitsRead.readAt);
  const ids = withVisitsHere(visitsRead.list, visitsRead.readAt, session.user.id, workspaceId, asked).map((visit) => visit.pageId);
  const order = read.open ? [read.open, ...ids.filter((id) => id !== read.open)] : ids;
  return { visitsRead, order };
}
