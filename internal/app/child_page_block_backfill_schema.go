package app

// The pages whose bodies wait to be filled with their sub-pages' blocks:
// every page with sub-pages or earlier child_page blocks when sub-pages
// became blocks, and later the pages an import or an undone wiki brings.

const (
	childPageBlockBackfillVersion = 307
	childPageBlockBackfillName    = "native_child_page_block_backfill"
)
