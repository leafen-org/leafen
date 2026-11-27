package app

// Every walk up a chain of parents — a page's, a block's, the page holding a
// database's item, the connection that made a connection — goes to the top
// however deep: neither Leafen nor Notion bounds how deep pages and blocks
// nest, and a walk cut at a fixed depth answered as if it had reached the
// top (2026-11-08: a merged database's sources, the trashed
// or archived page above a page; 2026-11-14: a landing page's holder, an
// export's list marks and its folders). A chain that comes back on itself
// ends where it would take a link a second time, which parentChain tells; a
// limit of the input itself (a formula's or a filter's nesting, Notion's
// append depth) is no chain of parents. web/src/lib/parentChain.ts is the
// same walk in the browser.

// parentChain is one walk up a chain of parents.
type parentChain[K comparable] map[K]struct{}

// climb takes the next link and reports whether it is new: false once the
// chain comes back to a link it took, where the walk ends.
func (chain parentChain[K]) climb(link K) bool {
	if _, again := chain[link]; again {
		return false
	}
	chain[link] = struct{}{}
	return true
}
