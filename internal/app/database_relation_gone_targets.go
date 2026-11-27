package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A row deleted for good leaves every relation value that names it, as a page
// deleted for good leaves Notion's relations. Its own page's values lose it
// with the row (the database deletion lifecycle's incoming turns, a row a
// graph change takes out, a page's records deleted with it). Other pages'
// values lose it through the relation pair runner, which takes it out of each
// value on that value's page as it takes a row out of a two-way relation's
// other end. Whatever deletes a row for good asks for that before the row
// goes. A row only in trash keeps its place (database_row_trash_relations.go),
// and so do the rows of a page deleted for good in its 30 days, which can
// still come back; relation reads leave both out, and any id left of a row
// already gone.

// relationTargetsOutOfSight sorts the rows a relation names that no one sees:
// `away` are in trash, or on a page deleted for good whose 30 days are not up
// (page_deletion_hold.go) or that is being deleted, and come back with it;
// `gone` are deleted for good.
func relationTargetsOutOfSight(app core.App, rowIDs []string) (map[string]struct{}, map[string]struct{}, error) {
	away, err := trashedDatabaseRowIDs(app, rowIDs)
	if err != nil || len(rowIDs) == 0 {
		return away, map[string]struct{}{}, err
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, nil, err
	}
	rows := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, nil, err
	}
	gone := make(map[string]struct{}, len(rowIDs))
	for _, id := range rowIDs {
		gone[id] = struct{}{}
	}
	fenced := map[string]bool{}
	for _, row := range rows {
		delete(gone, row.ID)
		held, known := fenced[row.PageID]
		if !known {
			if held, err = pagePermanentDeleteFenceActive(app, row.PageID); err != nil {
				return nil, nil, err
			}
			fenced[row.PageID] = held
		}
		if held {
			away[row.ID] = struct{}{}
		}
	}
	return away, gone, nil
}

// goneRelationTargetHoldersPerWork bounds the rows one work item changes.
const goneRelationTargetHoldersPerWork = databaseRelationPairBackfillRows

// admitGoneRelationTargetWork queues taking `rowIDs`, rows about to be
// deleted for good, out of the relation values on other pages that name them.
// A row holding one is left to its own deletion when it goes too: it is one
// of `rowIDs`, it is on one of `skipPageIDs`, or it is being deleted from the
// trash.
func admitGoneRelationTargetWork(
	app core.App, ownerID, workspaceID, actorID, origin string, rowIDs, skipPageIDs []string,
) error {
	if len(rowIDs) == 0 {
		return nil
	}
	if skipPageIDs == nil {
		skipPageIDs = []string{}
	}
	encodedRows, err := json.Marshal(rowIDs)
	if err != nil {
		return err
	}
	encodedSkip, err := json.Marshal(skipPageIDs)
	if err != nil {
		return err
	}
	held := []struct {
		GoneSourceID   string `db:"gone_source_id"`
		HolderSourceID string `db:"data_source_id"`
		PropertyID     string `db:"property_id"`
		HolderID       string `db:"row_id"`
		TargetID       string `db:"target_row_id"`
	}{}
	// The gone rows are found by id, then the relations to each one's source,
	// then the items naming the row by their page and target (CROSS JOIN
	// keeps that order): the row's owner and the item's property are read
	// after (+), or SQLite, which keeps no statistics, reads the owner's
	// every item for each gone row.
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[gone]].[[data_source_id]] AS [[gone_source_id]], [[items]].[[data_source_id]],
			[[items]].[[property_id]], [[items]].[[row_id]], [[items]].[[target_row_id]]
		FROM {{leafen_database_rows}} AS [[gone]]
		CROSS JOIN {{leafen_database_relation_property_targets}} AS [[target]]
			ON [[target]].[[owner]] = [[gone]].[[owner]]
			AND [[target]].[[target_source_id]] = [[gone]].[[data_source_id]]
			AND [[target]].[[page_id]] != [[gone]].[[page_id]]
		CROSS JOIN {{leafen_database_relation_items}} AS [[items]]
			ON [[items]].[[owner]] = [[target]].[[owner]] AND [[items]].[[page_id]] = [[target]].[[page_id]]
			AND [[items]].[[target_row_id]] = [[gone]].[[id]]
			AND +[[items]].[[property_id]] = [[target]].[[property_id]]
		WHERE +[[gone]].[[owner]] = {:owner} AND [[gone]].[[id]] IN (SELECT [[value]] FROM json_each({:rows}))
			AND [[target]].[[page_id]] NOT IN (SELECT [[value]] FROM json_each({:skip}))
			AND [[items]].[[row_id]] NOT IN (SELECT [[value]] FROM json_each({:rows}))
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_row_trash}} AS [[leaving]]
				WHERE [[leaving]].[[row_id]] = [[items]].[[row_id]] AND [[leaving]].[[deleting]] = 1)
		ORDER BY [[gone]].[[data_source_id]], [[items]].[[data_source_id]], [[items]].[[property_id]],
			[[items]].[[target_row_id]], [[items]].[[row_id]]
	`).Bind(dbx.Params{
		"owner": ownerID, "rows": string(encodedRows), "skip": string(encodedSkip),
	}).All(&held); err != nil || len(held) == 0 {
		return err
	}
	// Each holding property takes its rows out in bounded turns.
	work := []databaseRelationPairWork{}
	holders := 0
	for _, item := range held {
		last := len(work) - 1
		if last < 0 || holders == goneRelationTargetHoldersPerWork ||
			work[last].Payload.SourceDataSourceID != item.GoneSourceID ||
			work[last].Payload.TargetDataSourceID != item.HolderSourceID ||
			work[last].Payload.PairPropertyID != item.PropertyID {
			work = append(work, databaseRelationPairWork{Kind: databaseRelationPairLinks, Payload: databaseRelationPairPayload{
				SourceDataSourceID: item.GoneSourceID,
				TargetDataSourceID: item.HolderSourceID, PairPropertyID: item.PropertyID,
			}})
			last, holders = last+1, 0
		}
		links := work[last].Payload.Links
		if len(links) == 0 || links[len(links)-1].RowID != item.TargetID {
			links = append(links, databaseRelationPairLink{RowID: item.TargetID})
		}
		links[len(links)-1].Remove = append(links[len(links)-1].Remove, item.HolderID)
		work[last].Payload.Links = links
		holders++
	}
	return admitDatabaseRelationPairWork(app, ownerID, workspaceID, actorID, origin, work)
}
