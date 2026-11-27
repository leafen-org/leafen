package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type structuralAppendTail struct {
	ID       string `db:"id"`
	Position string `db:"position"`
}

type structuralAppendState struct {
	tailID string
	next   int
	fast   bool
}

type structuralAppendProjection struct {
	enabled bool
	states  map[string]structuralAppendState
}

func newStructuralAppendProjection(enabled bool) *structuralAppendProjection {
	return &structuralAppendProjection{
		enabled: enabled,
		states:  map[string]structuralAppendState{},
	}
}

func (projection *structuralAppendProjection) insert(
	app core.App,
	doc *crdt.Doc,
	owner, pageID, blockID, parentID string,
	afterID *string,
) (string, bool, error) {
	if !projection.enabled || parentID != "" {
		return "", false, nil
	}
	state, loaded := projection.states[parentID]
	if !loaded {
		var tail structuralAppendTail
		err := app.DB().NewQuery(`
			SELECT [[id]], [[position]] FROM {{leafen_blocks}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[parent_id]] = {:parentId}
			ORDER BY [[position]] DESC, [[id]] DESC LIMIT 1
		`).Bind(dbx.Params{
			"owner": owner, "pageId": pageID, "parentId": parentID,
		}).One(&tail)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
		state = structuralAppendState{tailID: tail.ID, fast: true}
		if tail.ID != "" {
			if !strings.HasPrefix(tail.Position, "p") || len(tail.Position) != 11 {
				state.fast = false
			} else if index, parseErr := strconv.Atoi(strings.TrimPrefix(tail.Position, "p")); parseErr != nil {
				state.fast = false
			} else {
				state.next = index + 1
			}
		}
		array := doc.GetArray(blockOrderRoot(parentID))
		if array.Len() != state.next ||
			(state.next > 0 && array.Get(state.next-1) != state.tailID) {
			state.fast = false
		}
	}
	expectedAfter := ""
	if afterID != nil {
		expectedAfter = *afterID
	}
	if !state.fast || expectedAfter != state.tailID {
		projection.states[parentID] = structuralAppendState{fast: false}
		return "", false, nil
	}
	doc.Transact(func(txn *crdt.Transaction) {
		txn.GetArray(blockOrderRoot(parentID)).Push(txn, []any{blockID})
	})
	position := fmt.Sprintf("p%010d", state.next)
	state.tailID = blockID
	state.next++
	projection.states[parentID] = state
	return position, true, nil
}
