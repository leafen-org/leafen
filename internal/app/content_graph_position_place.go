package app

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where a new block given a position and no afterId goes, as
// docs/developers.md says: its position orders it among the blocks the same
// change places by position, and they go after the blocks already there;
// "a0" puts one first, and no position at all puts it last. A stored place
// ("p" and ten digits, or one extended — an automation's blocks extend
// their source's to follow it) sorts among the stored blocks instead.
//
// A change's blocks are found by the positions they were given, whatever
// place they took (2026-11-11: on an empty page the first of several took
// p0000000000, which sorts after "b1", and the others went in before it).
// The old "aN" count — N blocks in — is gone (2026-11-15): "a7" to "a1" sent
// one change each stood 7,1,6,2,5,3,4, and "b2" then "b1" in one change, or
// two children placed under a toggle with a child, stood the wrong way round.

// A block a change placed by position and the position it was given.
type givenPlace struct{ position, id string }

// positionPlaces keeps, for each parent, the blocks a change placed by
// position: those placed first ("a0"), those placed after the blocks
// already there, and the block those went after.
type positionPlaces struct {
	first, last map[string][]givenPlace
	base        map[string]*string
}

func newPositionPlaces() *positionPlaces {
	return &positionPlaces{first: map[string][]givenPlace{}, last: map[string][]givenPlace{}, base: map[string]*string{}}
}

// placedFirst says whether position is the "a0" that puts a block first.
func placedFirst(position string) bool {
	count, err := strconv.Atoi(strings.TrimPrefix(position, "a"))
	return strings.HasPrefix(position, "a") && err == nil && count <= 0
}

// note records a block the change placed by position.
func (places *positionPlaces) note(parentID, position, id string) {
	if position == "" || storedBlockPlace(position) {
		return
	}
	if placedFirst(position) {
		places.first[parentID] = append(places.first[parentID], givenPlace{position: position, id: id})
		return
	}
	places.last[parentID] = append(places.last[parentID], givenPlace{position: position, id: id})
}

// after is the block a new block given only position goes after, or nil for
// its parent's start.
func (places *positionPlaces) after(
	app core.App, orders *structuralOrderView, owner, pageID, parentID, position string, deleted map[string]struct{},
) (*string, error) {
	if storedBlockPlace(position) {
		return storedPlaceBefore(app, owner, pageID, parentID, position)
	}
	if position == "" {
		return lastInOrder(orders, parentID, nil)
	}
	if placedFirst(position) {
		return givenBefore(places.first[parentID], position, deleted), nil
	}
	if found := givenBefore(places.last[parentID], position, deleted); found != nil {
		return found, nil
	}
	// The last of the blocks there before this change placed any by position
	// after them, read once: the change's own go after it in their order. One
	// the change deleted meanwhile, or none yet, is read again as the last
	// block not among them — those it placed first ("a0") included.
	if base := places.base[parentID]; base != nil && !isDeleted(deleted, *base) {
		return base, nil
	}
	own := map[string]struct{}{}
	for _, place := range places.last[parentID] {
		own[place.id] = struct{}{}
	}
	base, err := lastInOrder(orders, parentID, own)
	if err != nil {
		return nil, err
	}
	places.base[parentID] = base
	return base, nil
}

func isDeleted(deleted map[string]struct{}, id string) bool {
	_, gone := deleted[id]
	return gone
}

// givenBefore is the block among places whose given position comes last
// before position, or nil.
func givenBefore(places []givenPlace, position string, deleted map[string]struct{}) *string {
	var found *givenPlace
	for index := range places {
		place := &places[index]
		if isDeleted(deleted, place.id) || place.position >= position {
			continue
		}
		if found == nil || place.position > found.position {
			found = place
		}
	}
	if found == nil {
		return nil
	}
	id := found.id
	return &id
}

// lastInOrder is the last block of the parent's order now, passing over
// those in skip, or nil.
func lastInOrder(orders *structuralOrderView, parentID string, skip map[string]struct{}) (*string, error) {
	entry, err := orders.entry(parentID)
	if err != nil {
		return nil, err
	}
	for index := len(entry.ids) - 1; index >= 0; index-- {
		if _, passed := skip[entry.ids[index]]; passed {
			continue
		}
		id := entry.ids[index]
		return &id, nil
	}
	return nil, nil
}

// storedPlaceBefore is the block stored last before a stored place, or nil.
func storedPlaceBefore(app core.App, owner, pageID, parentID, position string) (*string, error) {
	var id string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[parent_id]] = {:parentId} AND [[position]] < {:position}
		ORDER BY [[position]] DESC, [[id]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "parentId": parentID, "position": position,
	}).Row(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// storedBlockPlace says whether position begins with a stored place, "p" and
// ten digits.
func storedBlockPlace(position string) bool {
	if len(position) < 11 || position[0] != 'p' {
		return false
	}
	for _, digit := range position[1:11] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
