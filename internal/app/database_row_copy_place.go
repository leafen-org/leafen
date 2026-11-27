package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database item's copy goes right after the item it copies, as a row made
// there would: between its place and the next place of its database's order
// after it, the trash left out, both left as they are. The app draws the copy
// in the same place at the press (web/src/lib/rowCopiesHere.ts), with the same
// rule (web/src/lib/graphPosition.ts graphPositionBetween), so the copy the
// server makes takes the drawn one's place. A copy's place was its item's with
// '~' and the copy's ID added, so a copy of a copy of a copy grew by 37 bytes
// each time and the seventh was refused, past the 255 bytes a place may hold
// (2026-11-25); copies made one after another right after the same item, or
// each right after the last, now grow by a character in every six or seven.

// Places are written in the printable characters after the space and run to
// at most graphPositionLongest bytes, as the app's are.
const (
	graphPositionLowest  = 0x21
	graphPositionBeyond  = 0x7f
	graphPositionLongest = 200
)

var errGraphPositionNoRoom = errors.New("no place fits between the neighbours")

// graphPositionBetween is the app's graphPositionBetween (web
// lib/graphPosition.ts), byte for byte: a place after before and before
// after, either empty for none. Neighbours that share a place, or come out of
// order, leave no bound above: the place goes after before.
func graphPositionBetween(before, after string) (string, error) {
	if before == "" && after == "" {
		return "m", nil
	}
	lower, upper, bounded := before, after, after != "" && before < after
	prefix := make([]byte, 0, len(lower)+1)
	for index := 0; index < graphPositionLongest; index++ {
		low := graphPositionLowest
		if index < len(lower) {
			low = max(int(lower[index]), graphPositionLowest)
		}
		high := graphPositionBeyond
		if bounded && index < len(upper) {
			high = min(int(upper[index]), graphPositionBeyond)
		}
		if high-low > 1 {
			return string(append(prefix, byte((low+high)/2))), nil
		}
		// No character fits here: keep the lower one and look one further.
		kept := graphPositionLowest
		if index < len(lower) {
			kept = int(lower[index])
		} else if bounded && index < len(upper) {
			kept = min(graphPositionLowest, int(upper[index]))
		}
		if bounded && (index >= len(upper) || kept > int(upper[index])) {
			return "", errGraphPositionNoRoom
		}
		if bounded && kept < int(upper[index]) {
			bounded = false
		}
		prefix = append(prefix, byte(kept))
	}
	return "", errGraphPositionNoRoom
}

// databaseRowCopyPlace is the place of a copy of the row at position: right
// after it, before the first row of its database whose place sorts after it.
func databaseRowCopyPlace(app core.App, authority databaseRowPageAuthority, position string) (string, error) {
	next := ""
	err := app.DB().NewQuery(`
		SELECT [[row]].[[position]] FROM {{leafen_database_rows}} AS [[row]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:pageId}
			AND [[row]].[[data_source_id]] = {:sourceId} AND [[row]].[[position]] > {:position}
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + `
		ORDER BY [[row]].[[position]], [[row]].[[id]] LIMIT 1
	`).Bind(dbx.Params{
		"owner": authority.OwnerID, "pageId": authority.ContainerPageID,
		"sourceId": authority.DataSourceID, "position": position,
	}).Row(&next)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	place, err := graphPositionBetween(position, next)
	if err != nil {
		return "", graphRefused(errorCodeGraphInvalid, "Check the content order.",
			"no place is left for a copy between %q and %q", position, next)
	}
	return place, nil
}
