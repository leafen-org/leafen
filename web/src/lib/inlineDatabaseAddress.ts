/**
 * A database opens at its own ID, as Notion's one address does, which
 * turning it inline or into a page keeps (seen 2026-11-05 and 2026-11-14):
 * /p/<its block>. One in a page has no page of its own, so the screen keeps
 * which page holds it and routes the address to that page, which shows it
 * alone; a full-page one's address opens its page, and its page's links name
 * that address (pageHref) — its page's ID changes as it turns, the block's
 * doesn't.
 */
const holders = new Map<string, string>();
// The page a database is known to be in, by its move in this tab or the
// server's answer for its address: a link drawn from what was read before it
// moved still names the page it left (a favorite, the open page as it was a
// frame ago), and once moved its address went back there whole (2026-11-13).
const sure = new Map<string, string>();
// The databases turned into pages of their own in this tab, whose IDs open
// those pages, as the server says (page_links.go): a link drawn from what was
// read before the turn still names the page each left (2026-11-13: back at
// its address after "페이지로 전환", that page drew whole).
const ownPages = new Set<string>();
// The full-page databases known here, by their blocks, and their pages' addresses.
const fullPages = new Map<string, string>();
const fullPageAddresses = new Map<string, string>();

// Those following which page holds a database (a peek showing one by its ID,
// usePeekedInlineDatabase), told a moment after a change: it is learned while
// links are drawn.
const followers = new Set<() => void>();
let telling = false;
function tellFollowers() {
  if (telling || followers.size === 0) return;
  telling = true;
  queueMicrotask(() => {
    telling = false;
    for (const follower of followers) follower();
  });
}

/** Follows which page holds a database (inlineDatabaseAddressPage). */
export function subscribeInlineDatabaseAddresses(follower: () => void) {
  followers.add(follower);
  return () => { followers.delete(follower); };
}

/** The page holding a database, as a link names it; `known`, as moved here or as the server says. */
export function rememberInlineDatabaseAddress(blockId: string, pageId: string, known = false) {
  if (!known && ownPages.has(blockId)) return;
  const before = inlineDatabaseAddressPage(blockId);
  ownPages.delete(blockId);
  holders.set(blockId, pageId);
  if (known) {
    sure.set(blockId, pageId);
    forgetFullPage(blockId);
  }
  if (inlineDatabaseAddressPage(blockId) !== before) tellFollowers();
}

/** A database turned into a page of its own here: its ID opens that page. */
export function forgetInlineDatabaseAddress(blockId: string) {
  const before = inlineDatabaseAddressPage(blockId);
  holders.delete(blockId);
  sure.delete(blockId);
  ownPages.add(blockId);
  if (before !== undefined) tellFollowers();
}

/** A full-page database's page, by its database's block, as made or turned here or as the server says: the block's ID is its address. */
export function rememberFullPageDatabase(blockId: string, pageId: string) {
  forgetInlineDatabaseAddress(blockId);
  forgetFullPage(blockId);
  const before = fullPageAddresses.get(pageId);
  if (before) fullPages.delete(before);
  fullPages.set(blockId, pageId);
  fullPageAddresses.set(pageId, blockId);
}

function forgetFullPage(blockId: string) {
  const pageId = fullPages.get(blockId);
  if (pageId === undefined) return;
  fullPages.delete(blockId);
  fullPageAddresses.delete(pageId);
}

/**
 * The full-page databases a page list the server read names (a page's
 * databaseBlockId): a database it names anew is its page's, over what this
 * tab kept of it in a page — another tab turned it — and a page it lists as
 * a page again holds none. One in the trash keeps its address, as the server
 * answers it (page_links.go) and Notion opens it there under its trash
 * banner (seen 2026-11-14): forgetting it sent its address to the server and
 * back without end. A page out of the trash naming the same database comes
 * first.
 */
export function noteFullPageDatabases(pages: readonly { id: string; kind?: string; inTrash: boolean; databaseBlockId?: string }[]) {
  const named = new Set<string>();
  for (const page of [...pages.filter((page) => !page.inTrash), ...pages.filter((page) => page.inTrash)]) {
    const blockId = page.kind === "database" ? page.databaseBlockId : undefined;
    if (blockId && !named.has(blockId)) {
      named.add(blockId);
      if (fullPages.get(blockId) !== page.id) rememberFullPageDatabase(blockId, page.id);
      continue;
    }
    const held = page.kind === "database" && !page.inTrash && !blockId ? undefined : fullPageAddresses.get(page.id);
    if (held === undefined) continue;
    forgetFullPage(held);
    ownPages.delete(held);
  }
}

/**
 * A database a page was to start as that the server refused
 * (databaseStartRefusal.ts): the page is a page again and its own ID its
 * address. Answers the address it had, if it had one.
 */
export function forgetFullPageDatabaseOf(pageId: string) {
  const blockId = fullPageAddresses.get(pageId);
  if (blockId !== undefined) forgetFullPage(blockId);
  return blockId;
}

/** The ID a page's address names: a full-page database's its database's block, any other page's its own. */
export function pageAddressId(pageId: string) {
  return fullPageAddresses.get(pageId) ?? pageId;
}

/** The full-page database's page an address's ID opens, if it names one. */
export function fullPageDatabasePage(id: string) {
  return fullPages.get(id);
}

/** The page an address's ID opens: the page holding a database in a page, a full-page database's own, or the ID's. */
export function pageAtAddress(id: string) {
  return inlineDatabaseAddressPage(id) ?? fullPages.get(id) ?? id;
}

export function inlineDatabaseAddressPage(blockId: string) {
  return sure.get(blockId) ?? holders.get(blockId);
}

/** The database an address opens by its own ID. */
export function inlineDatabaseAtAddress(pathname: string) {
  const segment = /^\/p\/([^/?#]+)\/?$/.exec(pathname)?.[1];
  return segment && holders.has(segment) ? segment : null;
}

/** The path of the page an address's database is held in, or the address itself. */
export function inlineDatabaseHolderPath(pathname: string) {
  const blockId = inlineDatabaseAtAddress(pathname);
  return blockId ? `/p/${encodeURIComponent(inlineDatabaseAddressPage(blockId)!)}` : pathname;
}

/** The full-page database's page an address opens, if it opens one by its database's ID. */
export function fullPageDatabaseAtAddress(pathname: string) {
  const segment = /^\/p\/([^/?#]+)\/?$/.exec(pathname)?.[1];
  return segment ? fullPages.get(segment) ?? null : null;
}
