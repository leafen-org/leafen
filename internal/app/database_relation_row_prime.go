package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent's answer judges each related row its relation cells name as the
// agent may open it (notionRelationRowOpen): the row's page, its record,
// whether its shares are unlinked, its place in a wiki and whether a
// pending change removes it, each a statement a row (2026-11-12: 3,000 of
// the 3,400 statements of an agent's read of 40 contracts naming 500
// related rows). Within a read scope they are read for all of them at once
// into the scope's memo (access_memo.go), where each judgment finds them;
// what a row has more to say — a row of a wiki — it still reads on its own.

// primeRelationRowsRemembered reads what judging rowIDs asks into the read
// scope's memo; outside a read scope it reads nothing.
func primeRelationRowsRemembered(app core.App, rowIDs []string) error {
	holder, ok := app.(accessMemoHolder)
	if !ok {
		return nil
	}
	memo := holder.accessMemo()
	wanted := make([]string, 0, len(rowIDs))
	seen := make(map[string]bool, len(rowIDs))
	for _, id := range rowIDs {
		if _, known := memo[rowPageAuthorityMemoKey{id}]; !known && id != "" && !seen[id] {
			seen[id] = true
			wanted = append(wanted, id)
		}
	}
	for start := 0; start < len(wanted); start += 500 {
		if err := primeRelationRowWindow(app, memo, wanted[start:min(start+500, len(wanted))]); err != nil {
			return err
		}
	}
	return nil
}

func primeRelationRowWindow(app core.App, memo map[any]accessMemoEntry, ids []string) error {
	// findRecordRemembered
	if err := primeRecordsRemembered(app, pagesCollection, ids); err != nil {
		return err
	}
	// loadDatabaseRowPageAuthority; a row with none is read on its own.
	authorities, err := readDatabaseRowPageAuthorities(app, ids, databaseSourceNotTrashedSQL("[[page]].[[data_source_id]]"))
	if err != nil {
		return err
	}
	for _, authority := range authorities {
		memo[rowPageAuthorityMemoKey{authority.RowID}] = accessMemoEntry{value: authority}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	// pageShareUnlinked
	unlinked := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[page_id]] FROM {{leafen_page_share_unlinks}}
		WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&unlinked); err != nil {
		return err
	}
	shareUnlinked := make(map[string]bool, len(unlinked))
	for _, id := range unlinked {
		shareUnlinked[id] = true
	}
	for _, id := range ids {
		if _, known := memo[pageShareUnlinkedMemoKey{id}]; !known {
			memo[pageShareUnlinkedMemoKey{id}] = accessMemoEntry{value: shareUnlinked[id]}
		}
	}
	// databaseRowPageAbove: a row page of a source that is no wiki has no
	// place in one.
	recordSources := map[string]string{}
	for _, id := range ids {
		if entry, found := memo[recordMemoKey{collection: pagesCollection, id: id}]; found && entry.err == nil {
			if record, ok := entry.value.(*core.Record); ok && record != nil {
				recordSources[id] = record.GetString("data_source_id")
			}
		}
	}
	sourceIDs := make([]string, 0, len(recordSources))
	for _, sourceID := range recordSources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	encodedSources, err := json.Marshal(sourceIDs)
	if err != nil {
		return err
	}
	wikis := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_data_sources}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[wiki]] = 1
	`).Bind(dbx.Params{"ids": string(encodedSources)}).Column(&wikis); err != nil {
		return err
	}
	wiki := make(map[string]bool, len(wikis))
	for _, id := range wikis {
		wiki[id] = true
	}
	for id, sourceID := range recordSources {
		key := rowWikiParentMemoKey{id, sourceID}
		if _, known := memo[key]; !known && !wiki[sourceID] {
			memo[key] = accessMemoEntry{value: ""}
		}
	}
	// notionDatabaseRowPageDeletionPending, for each database's rows.
	byDatabase := map[[2]string][]string{}
	for _, authority := range authorities {
		key := [2]string{authority.OwnerID, authority.ContainerPageID}
		byDatabase[key] = append(byDatabase[key], authority.RowID)
	}
	for database, rowIDs := range byDatabase {
		encodedRows, err := json.Marshal(rowIDs)
		if err != nil {
			return err
		}
		pending := []string{}
		if err := app.DB().NewQuery(`
			SELECT DISTINCT [[target_id]] FROM {{leafen_database_mutation_jobs}}
			WHERE [[owner_id]]={:ownerId} AND [[page_id]]={:pageId}
				AND [[target_kind]]='row' AND [[target_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))
				AND [[status]]='pending'
		`).Bind(dbx.Params{"ownerId": database[0], "pageId": database[1], "rowIds": string(encodedRows)}).Column(&pending); err != nil {
			return err
		}
		removed := make(map[string]bool, len(pending))
		for _, id := range pending {
			removed[id] = true
		}
		for _, id := range rowIDs {
			key := rowDeletionPendingMemoKey{database[0], database[1], id}
			if _, known := memo[key]; !known {
				memo[key] = accessMemoEntry{value: removed[id]}
			}
		}
	}
	return nil
}

// databaseRowWikiParent is the row a wiki's page sits under, none for a row
// of a source that is no wiki, once a read scope (access_memo.go).
func databaseRowWikiParent(app core.App, rowPage *core.Record) (string, error) {
	sourceID := rowPage.GetString("data_source_id")
	return remembered(app, rowWikiParentMemoKey{rowPage.Id, sourceID}, func() (string, error) {
		var wikiParentID string
		err := app.DB().NewQuery(`
			SELECT COALESCE([[row]].[[subitem_parent_id]], '') FROM {{leafen_data_sources}} AS [[source]]
			JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]] = {:rowId}
			WHERE [[source]].[[id]] = {:sourceId} AND [[source]].[[wiki]] = 1
		`).Bind(dbx.Params{"rowId": rowPage.Id, "sourceId": sourceID}).Row(&wikiParentID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return wikiParentID, err
	})
}

type rowWikiParentMemoKey struct{ rowID, sourceID string }
