package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func upsertStructuralBlockProjection(app core.App, owner, workspaceID, pageID string, block *graphBlock, parentID string) error {
	return upsertStructuralBlockProjectionAt(app, owner, workspaceID, pageID, owner, block, parentID, "")
}

func upsertStructuralBlockProjectionAt(
	app core.App,
	owner, workspaceID, pageID, actorID string,
	block *graphBlock,
	parentID, position string,
) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_blocks}}
			([[id]], [[owner]], [[workspace_id]], [[page_id]], [[parent_id]], [[position]], [[type]], [[content]],
			 [[created_by]], [[last_edited_by]], [[created]], [[updated]])
		VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:parentId}, {:position}, {:type}, {:content},
			{:actorId}, {:actorId}, {:now}, {:now})
		ON CONFLICT ([[id]]) DO UPDATE SET [[parent_id]] = excluded.[[parent_id]],
			[[type]] = excluded.[[type]], [[content]] = excluded.[[content]],
			[[last_edited_by]] = excluded.[[last_edited_by]], [[updated]] = excluded.[[updated]]
	`).Bind(dbx.Params{
		"id": block.ID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"parentId": parentID, "position": position,
		"type": block.Type, "content": string(block.Content), "actorId": actorID, "now": now,
	}).Execute()
	return err
}

func deleteStructuralBlockProjections(app core.App, owner, pageID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	params := dbx.Params{"owner": owner, "pageId": pageID}
	bindings := make([]string, len(ids))
	for index, id := range ids {
		key := fmt.Sprintf("block%d", index)
		params[key] = id
		bindings[index] = "{:" + key + "}"
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[id]] IN (` + strings.Join(bindings, ", ") + `)
	`).Bind(params).Execute()
	return err
}

func publishStructuralOrderProjection(
	app core.App,
	doc *crdt.Doc,
	owner, pageID, parentID, _ string,
) error {
	order, err := structuralOrder(doc, parentID)
	if err != nil || len(order) == 0 {
		return err
	}
	// The siblings' places read at once, and only those that moved written
	// (2026-10-13: a statement a sibling made adding a page under a parent
	// with a thousand sub-pages slow). Scanned as they come and compared as
	// numbers (2026-10-21: mapped field by field and formatted for each, a long
	// page's siblings were a sixth of a block's write while it held the one
	// write connection). A sibling whose place only shifted around the blocks
	// the change put in, moved or took out keeps its last edit — its time and
	// who made it (2026-11-07: a conflict copy kept beside one PDF gave every
	// file after it the copier and the copy's time); a block the change
	// itself writes or moves takes them from its own write.
	rows, err := app.DB().NewQuery(`
		SELECT [[id]], [[position]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[parent_id]] = {:parentId}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "parentId": parentID}).Rows()
	if err != nil {
		return err
	}
	places := make(map[string]string, len(order))
	for rows.Next() {
		var id, place string
		if err := rows.Scan(&id, &place); err != nil {
			_ = rows.Close()
			return err
		}
		places[id] = place
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for index, id := range order {
		place, found := places[id]
		if !found || structuralPlaceIs(place, index) {
			continue
		}
		position := fmt.Sprintf("p%010d", index)
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_blocks}}
			SET [[position]] = {:position}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
				AND [[parent_id]] = {:parentId} AND [[id]] = {:id}
		`).Bind(dbx.Params{
			"position": position, "owner": owner,
			"pageId": pageID, "parentId": parentID, "id": id,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// structuralPlaceIs is whether a stored place is the one an order gives its
// index, p and ten digits.
func structuralPlaceIs(place string, index int) bool {
	if len(place) != 11 || place[0] != 'p' {
		return false
	}
	value := 0
	for _, digit := range place[1:] {
		if digit < '0' || digit > '9' {
			return false
		}
		value = value*10 + int(digit-'0')
	}
	return value == index
}
