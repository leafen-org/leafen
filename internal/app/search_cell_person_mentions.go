package app

import (
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database item's text cells index a person mention with the person's
// name now (database_row_search.go), as its body does. The search index
// keeps, beside each item's entry, the people its cells mention, so a
// rename reads again only the items that mention them
// (person_mention_names.go). The entry goes with the item's index row.

// keepSearchCellPersonMentions keeps whom an item's indexed cells mention,
// in the write of its index entry.
func keepSearchCellPersonMentions(app core.App, pageID string, userIDs []string) error {
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_search_cell_person_mentions}} WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).Execute(); err != nil {
		return err
	}
	unique := slices.Clone(userIDs)
	slices.Sort(unique)
	for _, userID := range slices.Compact(unique) {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_search_cell_person_mentions}} ([[page_id]], [[user_id]]) VALUES ({:pageId}, {:userId})
		`).Bind(dbx.Params{"pageId": pageID, "userId": userID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
