import { fullPageDatabasePage, inlineDatabaseAddressPage, pageAddressId, rememberInlineDatabaseAddress } from "@/lib/inlineDatabaseAddress";
import { addressViewLeftByPeek, noteViewLeftByPeek } from "@/lib/databaseViewChoice";
import { pageHref } from "@/lib/navigation";
import { pushRoute, replaceRoute } from "@/lib/router";

/**
 * A page opened in a peek over the one open, as Notion's address names it:
 * `?p=<page>&pm=s` beside it, `pm=c` over its center.
 */
/**
 * How an address's "pm" opens what "p" names: "s" at the side, "c" in the
 * middle, and the middle too for none or one it doesn't know, as Notion's
 * opens "?p=〈item〉" (seen 2026-11-11: an inline database's item and a
 * page). "f" is Leafen's own: the trash's "열기" opens an item full within
 * its database's page.
 */
export function peekModeOf(param: string | null): "side" | "center" | "full" {
  return param === "s" ? "side" : param === "f" ? "full" : "center";
}

/**
 * An address naming a view (`v`), ahead of the peek it opens as Notion's
 * puts it (seen 2026-11-14: `?v=<view>&p=<page>&pm=s` for a full-page
 * database or a wiki peeked); a view it names already is named in its place.
 */
export function addressWithView(url: URL, viewId: string) {
  const next = new URL(url.toString());
  if (next.searchParams.has("v")) {
    next.searchParams.set("v", viewId);
    return next;
  }
  const rest = [...next.searchParams];
  next.search = "";
  next.searchParams.set("v", viewId);
  for (const [name, value] of rest) next.searchParams.append(name, value);
  return next;
}

/**
 * The page an address's "p" peeks: a full-page database's by its database's
 * ID, which names it there as on its own page (pagePeekHref), any other by its own.
 */
export function peekedPageId(param: string | null) {
  return param ? fullPageDatabasePage(param) ?? param : null;
}

/**
 * Whether the view an address names (`v`) stays as a peek opens `target`
 * over its page: the page's own does — a database's page, a wiki's — and so
 * does the peek's own as it turns another way; a database's peek's goes as
 * another peek takes its place, and so does one a closed peek left, as
 * Notion's (seen 2026-11-25 in Chrome, two tabs: a row's "다음에서 열기 ›
 * 사이드 보기" in an inline database's side peek `?v=<its view>&p=<it>&pm=s`
 * gave `?p=<row>&pm=s`, under the `?v=` its "»" left the same, a page
 * block's "사이드 보기에서 열기" there `?p=<page>&pm=s`; on a full-page
 * database's own `?v=` it gave `?v=<view>&p=<row>&pm=s`).
 */
export function peekKeepsView(url: URL, target: string) {
  const open = url.searchParams.get("p");
  if (open === target) return true;
  if (open) return !url.searchParams.has(INLINE_DATABASE_PEEK_PARAM) && !fullPageDatabasePage(open) && !inlineDatabaseAddressPage(open);
  return !addressViewLeftByPeek();
}

/**
 * A page opened in a peek over the one open: a full-page database by its
 * database's ID, as Notion's names it (seen 2026-11-14: `p=<the database>`),
 * the one address it has wherever it opens (inlineDatabaseAddress.ts). The
 * view the address names stays only where it is still the page's or the
 * peek's (peekKeepsView).
 */
export function pagePeekHref(pageId: string, mode: "side" | "center" = "side") {
  const url = new URL(window.location.href);
  if (!peekKeepsView(url, pageAddressId(pageId))) url.searchParams.delete("v");
  url.searchParams.set("p", pageAddressId(pageId));
  url.searchParams.set("pm", mode === "center" ? "c" : "s");
  url.searchParams.delete(INLINE_DATABASE_PEEK_PARAM);
  url.searchParams.delete(ADD_TO_PEEK_PARAM);
  return `${url.pathname}${url.search}${url.hash}`;
}

/** A new page's center peek that offers the place it goes, by Notion's name for it. */
export const ADD_TO_PEEK_PARAM = "showMoveTo";

/**
 * A page the sidebar's "+" just made, as Notion opens one (seen 2026-11-01:
 * `?p=<page>&showMoveTo=true` over the page left open, "추가 대상 ⌄" in its bar).
 */
export function newPagePeekHref(pageId: string) {
  const url = new URL(pagePeekHref(pageId, "center"), window.location.origin);
  url.searchParams.set(ADD_TO_PEEK_PARAM, "true");
  return `${url.pathname}${url.search}${url.hash}`;
}

/**
 * The inline database an older address's peek shows alone (`?p=<its page>&pdb=<its block>`),
 * still read; a peek names one by its own ID now (inlineDatabasePeekHref).
 */
export const INLINE_DATABASE_PEEK_PARAM = "pdb";

/**
 * An inline database opened alone in a peek, as Notion's "사이드 보기에서
 * 열기" (seen 2026-10-10) names it (seen twice 2026-11-25 in Chrome, two
 * tabs: `?v=<the view it showed>&p=<its block>&pm=s`, a step in the history,
 * over the page it is in or any other): by its own ID, its one address
 * (inlineDatabaseAddress.ts), the view it showed where it was pressed named
 * ahead of it as addressWithView puts it — the peek shows the person's choice
 * all the same (chosenDatabaseView) — and the page holding it kept for the peek.
 */
export function inlineDatabasePeekHref(pageId: string, blockId: string, mode: "side" | "center" = "side", viewId?: string) {
  rememberInlineDatabaseAddress(blockId, pageId);
  const url = new URL(pagePeekHref(blockId, mode), window.location.origin);
  const named = viewId ? addressWithView(url, viewId) : url;
  return `${named.pathname}${named.search}${named.hash}`;
}

/**
 * A database drawn in a page that the address peeks by its own ID: the peek
 * learns where it is as the page under it draws it, as on a reload of the
 * peek's address over the page holding it.
 */
export function notePeekedDatabaseIn(blockId: string, pageId: string) {
  if (typeof window !== "undefined" && new URLSearchParams(window.location.search).get("p") === blockId) {
    rememberInlineDatabaseAddress(blockId, pageId);
  }
}

/**
 * The inline database an address's peek shows alone, by its own ID or an
 * older address's `pdb`, with the page holding it, as far as this tab knows.
 */
export function peekedInlineDatabase(search: URLSearchParams) {
  const named = search.get("p");
  const holder = named ? inlineDatabaseAddressPage(named) : undefined;
  if (named && holder) return { blockId: named, pageId: holder };
  const older = search.get(INLINE_DATABASE_PEEK_PARAM);
  return named && older ? { blockId: older, pageId: peekedPageId(named)! } : null;
}

/**
 * A form's answer page a button opens beside or over the page, as Notion's
 * (seen 2026-11-05: `?p=…&pm=s` beside it, over it in a center window), by
 * its link for the one who pressed — "-" when it takes no answers of theirs.
 */
export const FORM_PEEK_PARAM = "pf";
export const FORM_PEEK_MODE_PARAM = "pfm";

export function formPeekHref(token: string, mode: "side" | "center") {
  const url = new URL(window.location.href);
  for (const name of ["p", "pm", INLINE_DATABASE_PEEK_PARAM, ADD_TO_PEEK_PARAM]) url.searchParams.delete(name);
  url.searchParams.set(FORM_PEEK_PARAM, token || "-");
  url.searchParams.set(FORM_PEEK_MODE_PARAM, mode === "center" ? "c" : "s");
  return `${url.pathname}${url.search}${url.hash}`;
}

/** Closes a form's peek, leaving the page that was open. */
export function closeFormPeek() {
  const url = new URL(window.location.href);
  url.searchParams.delete(FORM_PEEK_PARAM);
  url.searchParams.delete(FORM_PEEK_MODE_PARAM);
  replaceRoute(`${url.pathname}${url.search}${url.hash}`);
}

/** Opens a page in a side peek, as ⌥Click does. */
export function openPagePeek(pageId: string) {
  pushRoute(pagePeekHref(pageId));
}

// A peek on the screen, beside the page or in its middle: a row's
// (DatabaseRowPage), a page's (PagePeek) or a person's (PersonPeek).
const peekIn = (mode: "side" | "center") => ["data-row-peek-panel", "data-page-peek", "data-person-peek"]
  .map((name) => `[${name}][data-mode="${mode}"]`).join(", ");
const SIDE_PEEK = peekIn("side"), CENTER_PEEK = peekIn("center");

/**
 * A page pressed in a side peek — its mention or a link to it as a block —
 * opens in that peek, the page behind staying, and the way back brings the
 * peek's page back, as Notion's (seen 2026-11-12); pressed in a center peek
 * or on a page, or a link to it in the words, it opens whole. Gives whether
 * it opened there.
 */
export function openInSidePeek(pageId: string, pressed: Element | null): boolean {
  if (!pressed?.closest(SIDE_PEEK)) return false;
  pushRoute(pagePeekHref(pageId, "side"));
  return true;
}

/**
 * Where a page made in a page's body opens, when making it opens it
 * ("/페이지", a line turned into a page by its shortcut): where a press on
 * its block would (openInSidePeek) — in the side peek showing the page it
 * was made in, the page behind staying and the way back bringing that
 * peek's page back, as Notion's (seen 2026-11-18 in two tabs: a row's side
 * peek, "/페이지", the address then `p=<the new page>&pm=s`, Back the row's
 * peek); made in a center peek or on a page it opens whole (seen the same
 * day, twice: the tab went to the new page, Back the row's center peek). A
 * full-page database (`database`) opens whole wherever it is made, as
 * Notion's made in a row's side peek does (seen 2026-11-18 in two tabs: the
 * tab went to the new database, its path through the row, Back the row's
 * side peek).
 */
export function hrefOfPageMadeIn(parentId: string, pageId: string, database = false) {
  const search = new URLSearchParams(window.location.search);
  const beside = !database && peekedPageId(search.get("p")) === parentId && peekModeOf(search.get("pm")) === "side";
  return beside ? pagePeekHref(pageId, "side") : pageHref(pageId);
}

/**
 * A person pressed in a peek, beside the page or in its middle, opens in
 * that peek, as Notion's (seen 2026-11-12: a person mention in a side or a
 * center peek). Gives whether it opened there.
 */
export function openPersonInPeek(userId: string, pressed: Element | null | undefined): boolean {
  const mode = pressed?.closest(SIDE_PEEK) ? "side" : pressed?.closest(CENTER_PEEK) ? "center" : null;
  if (!mode) return false;
  pushRoute(pagePeekHref(userId, mode));
  return true;
}

/**
 * Closes the peek, leaving the page that was open. The view its address
 * named stays, as Notion's (seen 2026-11-25: an inline database's peek closed
 * by "»" left `?v=`), naming the peek's view, not one for the page to take
 * (noteViewLeftByPeek).
 */
export function closePagePeek() {
  const url = new URL(window.location.href);
  url.searchParams.delete("p");
  url.searchParams.delete("pm");
  url.searchParams.delete(INLINE_DATABASE_PEEK_PARAM);
  url.searchParams.delete(ADD_TO_PEEK_PARAM);
  // Told first: what follows the address reads it as the address moves.
  if (url.searchParams.has("v")) noteViewLeftByPeek(`${url.pathname}${url.search}`);
  replaceRoute(`${url.pathname}${url.search}${url.hash}`);
}
