package app

// A sub-page's block leaves its parent's body while the page is in the
// trash. The place it held is kept, so restoring the page puts its block
// back there, as Notion restores a page where it was. Deleting the page
// forgets it.

const (
	childPageBlockPlacesVersion = 306
	childPageBlockPlacesName    = "native_child_page_block_places"
)
