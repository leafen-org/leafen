package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The workspace owner's own lists of pages no one else sees — the pages in
// their 30 days after a permanent deletion (보안 › 데이터 보존) and the
// trash's pages and databases a legal hold keeps — name only the pages the
// owner reads as a person, as anyone in their place does (a98d54a95): a
// private teamspace they have not joined keeps its pages' titles from them,
// as another member's private pages do. Bringing such a page back or deleting
// it at once reaches only those too. The 30 days of the rest still end, as
// the system's work (system_job_access.go).
//
// A list's pages are judged together (listedPagesPersonReads): what judging
// each one asks is read for all of them at once, so a window of such a list
// asks a few statements however many pages it names, as a search's matches
// are judged (workspace_search_readable.go). Judged one at a time, each page
// read its record, and each database item's page its People place and its
// workspace, on its own (2026-11-18: with 5,000 held pages of a private
// teamspace its owner had not joined, the 30-day list asked 5,051 statements
// in one request, and the trash's legal-hold list 10,034).

// listedPagesJudgedTogether is how many pages one batch of the judgment
// reads at once.
const listedPagesJudgedTogether = 500

// personReadsListedPage says whether a person reads a page such a list
// names, whatever its state: in the trash, in its 30 days, being copied or
// moved.
func personReadsListedPage(app core.App, principalID, pageID string) (bool, error) {
	page, err := findRecordRemembered(app, pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if isDatabaseRowPageRecord(page) {
		_, err = authorizeRowPageRecord(app, principalID, page, pageAccessRoleView)
	} else if _, handled, peopleErr := authorizeIfPeoplePage(app, principalID, page, pageAccessRoleView); handled {
		err = peopleErr
	} else {
		var workspace *core.Record
		if workspace, err = findRecordRemembered(app, workspacesCollection, page.GetString("workspace")); err == nil {
			_, err = decidePersonPageRole(app, principalID, page, workspace, pageAccessRoleView)
		}
	}
	if isPermissionDenied(err) || isGuestMFARequired(err) || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// listedPagesPersonReads are those of a list's pages the person reads,
// judged together: by personReadsListedPage, once what it asks of them is in
// the read's memo.
func listedPagesPersonReads(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	app = withAccessMemo(app)
	reads := make(map[string]bool, len(pageIDs))
	for start := 0; start < len(pageIDs); start += listedPagesJudgedTogether {
		batch := pageIDs[start:min(start+listedPagesJudgedTogether, len(pageIDs))]
		found, err := primeListedPages(app, principalID, batch)
		if err != nil {
			return nil, err
		}
		for _, pageID := range batch {
			// A page no longer there is read by no one.
			if !found[pageID] || reads[pageID] {
				continue
			}
			read, err := personReadsListedPage(app, principalID, pageID)
			if err != nil {
				return nil, err
			}
			if read {
				reads[pageID] = true
			}
		}
	}
	return reads, nil
}

// pagesPersonReads keeps the pages of a list the person reads, in order.
func pagesPersonReads(app core.App, principalID string, pageIDs []string) ([]string, error) {
	reads, err := listedPagesPersonReads(app, principalID, pageIDs)
	if err != nil {
		return nil, err
	}
	kept := make([]string, 0, len(reads))
	for _, pageID := range pageIDs {
		if reads[pageID] {
			kept = append(kept, pageID)
		}
	}
	return kept, nil
}

// primeListedPages reads into a read scope's memo, at once, what judging
// some pages asks: their records; a database item's page's item, its
// sharing, its place in a wiki and its People place; and the paths above
// them all with their shares. It says which pages are there.
func primeListedPages(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	if err := primeRecordsRemembered(app, pagesCollection, pageIDs); err != nil {
		return nil, err
	}
	memo := app.(accessMemoHolder).accessMemo()
	found := make(map[string]bool, len(pageIDs))
	pages, rows := []string{}, []*core.Record{}
	for _, pageID := range pageIDs {
		entry, held := memo[recordMemoKey{collection: pagesCollection, id: pageID}]
		record, _ := entry.value.(*core.Record)
		if !held || entry.err != nil || record == nil || found[pageID] {
			continue
		}
		found[pageID] = true
		if isDatabaseRowPageRecord(record) {
			rows = append(rows, record)
		} else {
			pages = append(pages, pageID)
		}
	}
	rowIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		rowIDs = append(rowIDs, row.Id)
	}
	if err := primeRelationRowsRemembered(app, rowIDs); err != nil {
		return nil, err
	}
	if err := primePeopleRowPlaces(app, rows); err != nil {
		return nil, err
	}
	if err := primePageShareRoles(app, principalID, append(pages, rowIDs...)); err != nil {
		return nil, err
	}
	return found, nil
}

// primePeopleRowPlaces keeps, for database items' pages, that those of a
// data source that is no People database are no one's in People
// (peopleDatabasePageOf), read for their sources at once; one of People's
// is read on its own.
func primePeopleRowPlaces(app core.App, rows []*core.Record) error {
	if len(rows) == 0 {
		return nil
	}
	memo := app.(accessMemoHolder).accessMemo()
	sources, seen := []string{}, map[string]bool{}
	for _, row := range rows {
		if source := row.GetString("data_source_id"); !seen[source] {
			seen[source] = true
			sources = append(sources, source)
		}
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	people := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[data_source_id]] FROM {{leafen_workspace_people_databases}}
		WHERE [[data_source_id]] IN (SELECT [[value]] FROM json_each({:sources}))
	`).Bind(dbx.Params{"sources": string(encoded)}).Column(&people); err != nil {
		return err
	}
	ofPeople := make(map[string]bool, len(people))
	for _, source := range people {
		ofPeople[source] = true
	}
	for _, row := range rows {
		key := peopleDatabasePageMemoKey{row.Id}
		if _, known := memo[key]; !known && !ofPeople[row.GetString("data_source_id")] {
			memo[key] = accessMemoEntry{value: peopleDatabasePlace{}}
		}
	}
	return nil
}
