// Every walk up a chain of parents — a block's, a page's, the place a
// popover was opened from — goes to the top however deep: neither Leafen
// nor Notion bounds how deep pages and blocks nest, and a walk cut at a
// fixed depth answered as if it had reached the top (2026-11-08: the trashed
// or archived page above a page; 2026-11-14: a block drawn in a synced copy
// inside another). A chain that comes back on itself ends where it would
// take a link a second time, which a parent chain tells. A limit of the
// input itself (a formula's or a filter's nesting) is no chain of parents.
// internal/app/parent_chain.go is the same walk on the server.

/** One walk up a chain: each link climbed says whether it is new — false once the chain comes back to a link it took, where the walk ends. */
export function parentChain<T>(...taken: T[]): (link: T) => boolean {
  const climbed = new Set<T>(taken);
  return (link) => {
    if (climbed.has(link)) return false;
    climbed.add(link);
    return true;
  };
}
