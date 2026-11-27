import { useRef, useSyncExternalStore } from "react";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import { handRowPageOver } from "@/lib/rowPageHandoff";
import { routeInfoFromPath } from "@/lib/router";
import type { Page, Session } from "@/types";

// A page this tab moves into a database, or between a page and a database's
// page — "옮기기" to a database (⌘⇧P, the page's menu), a database page's
// "실행 취소" putting it back, a wiki's page moved out — leaves the list at
// the press. The open one stays drawn as it was while the server moves it,
// and once it is a row opens as that row's page, read first so it draws at
// once (rowPageHandoff.ts), as Notion's open page turns into its row's page
// (seen 2026-11-14). Its leaving the list is no access taken away
// (2026-11-14: the open page said "이 페이지에 대한 사용 권한 없음" until
// reloaded, AuthenticatedProductRoute's PageProblem). Refused, it is back in
// the list as it was. One moved into a database in another tab or by someone
// else opens as its row's page the same way, once its read says it is a
// database's page (pagesOutsideList.ts): it stayed drawn as a page, without
// its properties, even reloaded (2026-11-14).

const moving = new Map<string, object>();
const madeRows = new Set<string>();
const listeners = new Set<() => void>();
const announce = () => { for (const listener of [...listeners]) listener(); };
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

/**
 * Holds the pages while they move — into a database as its rows, or not
 * (`intoDatabase` false: a database's page going out as a page). Gives the
 * release, called once the server moved them, or refused.
 */
export function holdPageThroughMove(session: Session, pageIds: readonly string[], intoDatabase = true): (refused?: boolean) => void {
  const hold = {};
  for (const id of pageIds) moving.set(id, hold);
  announce();
  const letGo = () => {
    let changed = false;
    for (const id of pageIds) {
      if (moving.get(id) !== hold) continue;
      moving.delete(id);
      changed = true;
    }
    if (changed) announce();
  };
  let released = false;
  return (refused = false) => {
    if (released) return;
    released = true;
    if (refused || !intoDatabase) return letGo();
    for (const id of pageIds) madeRows.add(id);
    const route = typeof window === "undefined" ? null : routeInfoFromPath(window.location.pathname);
    const open = route?.kind === "page" && pageIds.includes(route.pageId) ? route.pageId : "";
    if (!open) return letGo();
    void readDatabaseRowPage(session, open).then((snapshot) => handRowPageOver(snapshot), () => undefined).finally(letGo);
  };
}

/**
 * A page open here that turned out a database's page, moved into one
 * elsewhere: opens as its row's page, read first so it draws at once.
 */
export function openPageAsRow(session: Session, pageId: string): Promise<void> {
  return readDatabaseRowPage(session, pageId).then((snapshot) => handRowPageOver(snapshot), () => undefined).finally(() => {
    madeRows.add(pageId);
    announce();
  });
}

/** Whether the page became a row, here or elsewhere: its leaving the list is no access taken away. */
export function pageBecameRow(pageId: string) {
  return madeRows.has(pageId);
}

/** The open page as listed, or, while it moves and the list no longer holds it, as it was last drawn. */
export function useOpenPageThroughMove(listed: Page | null, routePageId: string | null): Page | null {
  // A page found to be a row draws the screen again, which then opens its row's page.
  const state = useSyncExternalStore(subscribe, () => routePageId === null ? ""
    : moving.has(routePageId) ? "moving" : madeRows.has(routePageId) ? "row" : "", () => "");
  const held = state === "moving";
  const last = useRef<Page | null>(null);
  const page = listed ?? (held && last.current?.id === routePageId ? last.current : null);
  last.current = page;
  return page;
}
