package app

// A sub-page made from a line's "@" or "+" menu hangs on that line, as
// Notion's does (mention_anchored_pages.go): the page names the line's block
// it is anchored to, and whether it went to the trash because its line or
// its mention there went.

const (
	pageAnchorBlockField = "anchor_block_id"
	pageAnchorLostField  = "anchor_lost"
)
