import { i18next } from "@/i18n";
import { forgetFullPageDatabaseOf } from "./inlineDatabaseAddress";
import { replaceRoute } from "./router";

// A page's database start the server refused, whoever makes its database:
// the server (an existing source, a template, a form — databaseStartKept.ts)
// or this device first (a blank database, a view, "/데이터베이스 - 전체
// 페이지" — fullPageDatabase.ts). Every such start ends here when refused:
// the page is the page it was, at its own address, and it is said once.

// The pages whose start a sender in this document follows to its end; it
// says the refusal itself.
const followed = new Map<string, number>();

// The pages whose start was refused here, till one starts again: drawn as
// the database they were to be till they are read again, that database's
// view named no address of theirs (databaseStartRefused).
const refused = new Set<string>();

/**
 * Whether the page's database start was refused here, the page a page
 * again: the database drawn meanwhile, till the page is read again, names
 * its view in no address (useDatabaseViewController) — it put its view back
 * on the page's own address once that went back (2026-11-18).
 */
export function databaseStartRefused(pageId: string) {
  return refused.has(pageId);
}

/**
 * The page is a page again: its database's ID, made up at the press, was its
 * address from then on (inlineDatabaseAddress.ts) and stayed on screen once
 * the page was listed a page again, naming nothing — "페이지 찾지 못함"
 * (2026-11-18). The address goes back to the page's own, in its place,
 * without the view it named, which was that database's — opened at the
 * page's own ID, as a line's "/데이터베이스 - 전체 페이지" opens it, the
 * view goes alike.
 */
function pageStaysPage(pageId: string) {
  refused.add(pageId);
  const address = forgetFullPageDatabaseOf(pageId);
  if (typeof window === "undefined") return;
  const { pathname, search, hash } = window.location, own = `/p/${encodeURIComponent(pageId)}`;
  if (pathname !== own && (address === undefined || pathname !== `/p/${encodeURIComponent(address)}`)) return;
  const rest = new URLSearchParams(search);
  rest.delete("v");
  const next = `${own}${rest.size ? `?${rest}` : ""}${hash}`;
  if (next !== `${pathname}${search}${hash}`) replaceRoute(next);
}

/**
 * Follows a page's database start to its end, `made` telling whether the
 * server took it: refused, the page stays the page it was (pageStaysPage)
 * before its sender, which hears the same answer after, says so and reads
 * the page again. `replaces` for a template taking an empty database's
 * place, whose page is a database either way and keeps its address.
 */
export function databaseStartFollowed(pageId: string, made: Promise<boolean>, replaces = false): Promise<boolean> {
  followed.set(pageId, (followed.get(pageId) ?? 0) + 1);
  refused.delete(pageId);
  return made.then((done) => {
    if (!done && !replaces) pageStaysPage(pageId);
    return done;
  }).finally(() => {
    const left = (followed.get(pageId) ?? 1) - 1;
    if (left > 0) followed.set(pageId, left);
    else followed.delete(pageId);
  });
}

/**
 * What the outbox's lane says of a kept change the server refused when it
 * was a page's database start, sent in another change's turn or after a
 * reload: null for any other change, "" where its sender here says it
 * (databaseStartFollowed) — said by both, one refusal showed two notices
 * (2026-11-18) — and what to say where no sender is left to. The page stays
 * the page it was either way.
 */
export function keptDatabaseStartRefusal(pageId: string, written: unknown): string | null {
  const value = written && typeof written === "object" ? written as Record<string, unknown> : {};
  const start = value.start && typeof value.start === "object" ? value.start as Record<string, unknown> : null;
  if (!start && value.pageKind !== "database") return null;
  if (!start?.replace) pageStaysPage(pageId);
  return followed.has(pageId) ? "" : i18next.t("syncStatusBadge:status.databaseStartFailed");
}
