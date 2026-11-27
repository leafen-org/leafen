package app

import "github.com/pocketbase/pocketbase/core"

// Where a page shows its cover. Notion shows a cover no one has moved in its
// middle (object-position center 50%, seen 2026-11-15 on a cover an
// integration put on a page), and a cover the API changes keeps the place it
// was moved to (seen 2026-11-15: moved to 41%, changed, still 41%). Notion's
// API never says where a cover shows, so a cover put on a page that shows
// none, with no place given — through the API, MCP or a Notion import — shows
// in the middle, and a changed cover keeps its page's place. The web gives
// the middle with each cover it picks (PageCover.tsx).

// pageCoverMiddle is a cover position in the middle of the cover's image.
const pageCoverMiddle = 50

// applyPageCover sets a page record's cover and where it shows: the place
// given, else the middle when the page showed no cover, else where it was.
func applyPageCover(page *core.Record, cover *string, position *int) {
	if cover != nil {
		if position == nil && page.GetString("cover") == "" && *cover != "" {
			middle := pageCoverMiddle
			position = &middle
		}
		page.Set("cover", *cover)
	}
	if position != nil {
		page.Set("cover_position", *position)
	}
}
