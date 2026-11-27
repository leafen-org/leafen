package app

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page settings change made on a device while its earlier changes of the
// page were not answered yet names them (afterMutationIds): it was made on the
// page as they leave it, whose cursor the device doesn't know yet. It holds on
// the page when the page's cursor is its base, or when the page's last change
// is one of those it names (that one's receipt holds the page's cursor). One
// that carries what they change too — a page's unanswered changes sent as one
// as the page goes (carriesAfter) — takes those it names with it: one of them
// sent again later is answered as taken (2026-11-27: a title's letter on its
// way and its ⌘Z sent as the tab closed, the ⌘Z refused for the letter
// landing first and the letter left until Leafen was opened again; a ⌘Z made
// before the letter's answer, refused for its cursor, was merged away).

// pageMetadataAfterMost is the most earlier changes one change names.
const pageMetadataAfterMost = 64

// pageMetadataTakenAlongHash begins the receipt of a change taken with a
// later one that named it, whatever its own request was: a receipt's hash is
// 64 characters, and no request's hash (hex) holds these letters.
const pageMetadataTakenAlongHash = "taken-along-"

// pageMetadataTakenAlongBy is the receipt hash of a change taken with the change `by`.
func pageMetadataTakenAlongBy(by string) string {
	digest := sha256.Sum256([]byte(by))
	return (pageMetadataTakenAlongHash + hex.EncodeToString(digest[:]))[:64]
}

func pageMetadataNamedAfter(input pageMetadataInput) []string {
	named := make([]string, 0, len(input.AfterMutationIDs))
	for _, id := range input.AfterMutationIDs {
		id = strings.TrimSpace(id)
		if id != "" && id != input.MutationID && len([]rune(id)) <= 255 && !slices.Contains(named, id) && len(named) < pageMetadataAfterMost {
			named = append(named, id)
		}
	}
	return named
}

// pageMetadataBaseHolds tells whether a change holds on the page as it is.
func pageMetadataBaseHolds(app core.App, actorID string, page *core.Record, input pageMetadataInput) (bool, error) {
	if page.GetInt("cursor") == input.BaseCursor {
		return true, nil
	}
	named := pageMetadataNamedAfter(input)
	if len(named) == 0 {
		return false, nil
	}
	last, err := app.FindRecordsByFilter(receiptsCollection, "owner = {:owner} && page = {:page} && cursor = {:cursor}", "", 8, 0,
		dbx.Params{"owner": actorID, "page": page.Id, "cursor": page.GetInt("cursor")})
	if err != nil {
		return false, err
	}
	for _, receipt := range last {
		if slices.Contains(named, receipt.GetString("mutation_id")) {
			return true, nil
		}
	}
	return false, nil
}

// pageMetadataTakenAlong tells whether a receipt is of a change taken with a
// later one that named it, answered as taken whatever is sent under its ID.
func pageMetadataTakenAlong(receipt *core.Record) bool {
	return strings.HasPrefix(receipt.GetString("request_hash"), pageMetadataTakenAlongHash)
}

// savePageMetadataReceipts keeps a change's receipt, and for one carrying
// those it names, one for each the server hasn't taken, as taken with it.
func savePageMetadataReceipts(app core.App, receipt *core.Record, input pageMetadataInput) error {
	if err := app.Save(receipt); err != nil || !input.CarriesAfter {
		return err
	}
	for _, id := range pageMetadataNamedAfter(input) {
		known, err := app.FindRecordsByFilter(receiptsCollection, "owner = {:owner} && mutation_id = {:mutationId}", "", 1, 0,
			dbx.Params{"owner": receipt.GetString("owner"), "mutationId": id})
		if err != nil {
			return err
		}
		if len(known) > 0 {
			continue
		}
		along := core.NewRecord(receipt.Collection())
		along.Set("owner", receipt.GetString("owner"))
		along.Set("page", receipt.GetString("page"))
		along.Set("mutation_id", id)
		along.Set("request_hash", pageMetadataTakenAlongBy(input.MutationID))
		along.Set("cursor", receipt.GetInt("cursor"))
		if err := app.Save(along); err != nil {
			return err
		}
	}
	return nil
}
