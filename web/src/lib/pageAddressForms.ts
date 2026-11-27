// A page's address in a form other than the app's own /p/<ID>, and the
// app's address it opens as.

const OLD_PAGE_PATH = /^\/pages\/([^/?#]+)\/?$/;
// Notion's link to a page, its ID with no dashes, as its API takes one.
const NOTION_PAGE_PATH = /^\/([0-9a-f]{32})$/;
// A /p/ address naming its page by the UUID's 32 hexadecimal digits without
// dashes alone — as this server's API names a page or a database (a
// mention's url, MCP's) and as Notion's links do. One with a title before
// the digits (/p/Title-ID) is not read here: some of this server's own IDs
// end so too — a database template's database and rows, "starter-<32 hex>",
// and any ID given through the API — and such an address names that ID
// whole, which only the server can tell (2026-11-23: a template's database
// at its own address went to the dashed digits and opened nothing). The
// server redirects a titled address that names none of its IDs to the
// dashed one before the app loads (page_address_title_form.go), and the
// page link route reads one the app was opened at all the same.
const NOTION_FORM_PAGE_PATH = /^\/p\/([0-9a-f]{32})$/;

// Every reader on the server puts a UUID's dashes back before it looks
// (normalizeNotionUUIDRequest), so the dashed ID names what the 32 digits
// name.
function withUuidDashes(id: string) {
  if (!/^[0-9a-f]{32}$/.test(id)) return id;
  return `${id.slice(0, 8)}-${id.slice(8, 12)}-${id.slice(12, 16)}-${id.slice(16, 20)}-${id.slice(20)}`;
}

/**
 * The app's path for a page named as agents' answers and page Markdown name
 * it (/pages/ID), or as a link in Notion's form names it (/ID or /p/ID with
 * the ID's dashes left out), or null for any other path (2026-10-16: an
 * agent's link to a page didn't open). The ID takes its dashes back from the
 * start, before the page has loaded: the browser keeps a page's icon for the
 * addresses it had while it loaded, and one the app moved from afterwards —
 * the page link route's answer, which names the page by its dashed ID —
 * kept Leafen's mark in its history and bookmarks (2026-11-23, Chrome and
 * Edge 158, as Notion's slug-less address keeps its page's icon). The page
 * link route asks the server which page a public ID opens, which the dashed
 * ID answers as the 32 digits did.
 */
export function pagePathForOldPath(pathname: string): string | null {
  const match = OLD_PAGE_PATH.exec(pathname) ?? NOTION_PAGE_PATH.exec(pathname) ?? NOTION_FORM_PAGE_PATH.exec(pathname);
  return match ? `/p/${withUuidDashes(match[1])}` : null;
}
