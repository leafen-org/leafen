package app

import "github.com/pocketbase/pocketbase/core"

// A wiki made before wikis became databases listed its pages in a table of
// its own. A wiki's pages have been its database's pages since before the
// schema baseline, and nothing on a database that began with the baseline
// writes that list, so migration 832 takes the table away, its indexes and
// triggers with it (schemaBaselineRetired).

const earlierWikiListingTable = "leafen_wiki_pages"

func dropEarlierWikiListing(app core.App) error {
	return dropRetiredTable(app, earlierWikiListingTable)
}

func validateEarlierWikiListingDropped(app core.App) error {
	return validateRetiredTableDropped(app, earlierWikiListingTable)
}
