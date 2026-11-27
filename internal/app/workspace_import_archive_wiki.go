package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace archive brings a wiki back as a wiki. Its data source names
// the properties holding its pages' owners and verification; the import
// keeps them on the imported source, which stays an ordinary one while the
// import writes the wiki's properties, pages and values, and makes it a
// wiki's again when the import is published. The wiki's page is the wiki,
// its pages show under the pages above them, and each page's verification
// is what its value says. A verification that ran out before the import is
// not announced again.

// workspaceImportStagedWikiSource names, by their imported IDs, the Owner and
// Verification properties of a data source that was a wiki's.
type workspaceImportStagedWikiSource struct {
	OwnerPropertyID        string `json:"ownerPropertyId"`
	VerificationPropertyID string `json:"verificationPropertyId"`
}

// workspaceImportArchiveWikiSource reads the wiki an archived data source
// was, if any.
func workspaceImportArchiveWikiSource(
	db workspaceImportArchiveQueryer,
	value archiveDataSourceValue,
) (*workspaceImportStagedWikiSource, error) {
	if !value.Wiki {
		return nil, nil
	}
	owner, err := workspaceImportArchiveReference(db, value.WikiOwnerPropertyID, workspaceExportItemProperty)
	if err != nil {
		return nil, err
	}
	verification, err := workspaceImportArchiveReference(db, value.WikiVerificationPropertyID, workspaceExportItemProperty)
	if err != nil {
		return nil, err
	}
	return &workspaceImportStagedWikiSource{OwnerPropertyID: owner, VerificationPropertyID: verification}, nil
}

// holdWorkspaceImportArchiveWikiSource keeps a wiki's properties on its
// imported data source until the import is published.
func holdWorkspaceImportArchiveWikiSource(app core.App, sourceID string, wiki workspaceImportStagedWikiSource) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_data_sources}}
		SET [[wiki_owner_property_id]] = {:owner}, [[wiki_verification_property_id]] = {:verification}
		WHERE [[id]] = {:id} AND [[wiki]] = 0
	`).Bind(dbx.Params{
		"id": sourceID, "owner": wiki.OwnerPropertyID, "verification": wiki.VerificationPropertyID,
	}).Execute()
	return err
}

// workspaceImportWikiPropertySQL holds when the property the data source
// aliased names in the column is its own, of the kind and not deleted.
func workspaceImportWikiPropertySQL(column, kind string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[property]].[[id]] = [[source]].[[` + column + `]] AND [[property]].[[data_source_id]] = [[source]].[[id]]
			AND [[property]].[[type]] = '` + kind + `' AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_property_trash}} WHERE [[property_id]] = [[property]].[[id]]))`
}

// publishWorkspaceArchiveWikis makes each data source the import kept a
// wiki's properties on a wiki's again, with its page the wiki, its pages
// where their rows' sub-item parents put them (wiki_subpage_pair.go); a
// source whose properties or database did not come back whole stays an
// ordinary database.
func publishWorkspaceArchiveWikis(txApp core.App, jobID string, authorization workspaceAuthorization, now time.Time) error {
	sources := []struct {
		ID           string `db:"id"`
		PageID       string `db:"page_id"`
		Verification string `db:"wiki_verification_property_id"`
		Whole        bool   `db:"whole"`
	}{}
	// The sources are found by the job's pages; the owner is read after (+),
	// or SQLite, which keeps no statistics, reads the owner's every source.
	if err := txApp.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[page_id]], [[source]].[[wiki_verification_property_id]],
			(` + workspaceImportWikiPropertySQL("wiki_owner_property_id", graphValueTypePerson) + `
				AND ` + workspaceImportWikiPropertySQL("wiki_verification_property_id", graphValueTypeVerification) + `
				AND [[page]].[[data_source_id]] = '' AND EXISTS (
					SELECT 1 FROM {{leafen_blocks}} AS [[block]]
					WHERE [[block]].[[owner]] = [[source]].[[owner]] AND [[block]].[[page_id]] = [[source]].[[page_id]]
						AND [[block]].[[type]] = {:blockType} AND json_valid([[block]].[[content]])
						AND json_extract([[block]].[[content]], '$.dataSourceId') = [[source]].[[id]]
						AND COALESCE(json_extract([[block]].[[content]], '$.linkedDatabaseSource'), 0) = 0)) AS [[whole]]
		FROM {{leafen_workspace_archive_import_pages}} AS [[owned]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[page_id]] = [[owned]].[[page_id]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[source]].[[page_id]]
		WHERE [[owned]].[[job_id]] = {:jobId} AND +[[source]].[[owner]] = {:ownerId} AND [[source]].[[wiki]] = 0
			AND ([[source]].[[wiki_owner_property_id]] != '' OR [[source]].[[wiki_verification_property_id]] != '')
		ORDER BY [[source]].[[page_id]], [[source]].[[id]]
	`).Bind(dbx.Params{
		"jobId": jobID, "ownerId": authorization.OwnerID, "blockType": graphBlockTypeInlineDatabase,
	}).All(&sources); err != nil {
		return err
	}
	for _, source := range sources {
		if !source.Whole {
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_data_sources}} SET [[wiki_owner_property_id]] = '', [[wiki_verification_property_id]] = ''
				WHERE [[id]] = {:id}
			`).Bind(dbx.Params{"id": source.ID}).Execute(); err != nil {
				return err
			}
			continue
		}
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_data_sources}} SET [[wiki]] = 1 WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": source.ID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_pages}}
			SET [[is_wiki]] = 1, [[wiki_revision]] = [[wiki_revision]] + 1, [[wiki_generation]] = [[wiki_revision]] + 1
			WHERE [[id]] = {:id} AND [[is_wiki]] = 0
		`).Bind(dbx.Params{"id": source.PageID}).Execute(); err != nil {
			return err
		}
		if err := publishWorkspaceArchiveWikiVerification(
			txApp, authorization.OwnerID, source.PageID, source.Verification, now,
		); err != nil {
			return err
		}
	}
	return nil
}

// publishWorkspaceArchiveWikiVerification gives each page of an imported
// wiki the verification its value holds, as a new value would, with its
// reminder due when it runs out; one that ran out before is not verified and
// is not announced.
func publishWorkspaceArchiveWikiVerification(
	txApp core.App, ownerID, pageID, propertyID string, now time.Time,
) error {
	values := `SELECT [[row_id]], [[due]],
			COALESCE(json_extract([[value]], '$.date.start'), '') AS [[start]],
			COALESCE(json_extract([[value]], '$.verified_by'), '') AS [[verified_by]],
			COALESCE(json_extract([[value]], '$.date.end'), '') AS [[end]],
			COALESCE(json_extract([[value]], '$.state') = 'verified' AND COALESCE([[due]] > {:now}, TRUE), FALSE) AS [[live]]
		FROM (SELECT [[row_id]], [[value]],
				CAST(unixepoch(json_extract([[value]], '$.date.end'), 'subsec') * 1000 AS INTEGER) AS [[due]]
			FROM {{leafen_database_row_values}}
			WHERE [[owner]] = {:ownerId} AND [[page_id]] = {:pageId} AND [[property_id]] = {:propertyId})`
	params := dbx.Params{"ownerId": ownerID, "pageId": pageID, "propertyId": propertyID, "now": now.UnixMilli()}
	if _, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_pages}} AS [[page]]
		SET [[wiki_verification_generation]] = [[page]].[[wiki_verification_generation]] + 1,
			[[verified_at]] = CASE WHEN [[verification]].[[live]] THEN [[verification]].[[start]] ELSE '' END,
			[[verified_by]] = CASE WHEN [[verification]].[[live]] THEN [[verification]].[[verified_by]] ELSE '' END,
			[[verification_expires_at]] = CASE WHEN [[verification]].[[live]] THEN [[verification]].[[end]] ELSE '' END
		FROM (` + values + `) AS [[verification]]
		WHERE [[page]].[[id]] = [[verification]].[[row_id]]
	`).Bind(params).Execute(); err != nil {
		return err
	}
	// What the value triggers queued as the values came in, before the
	// wiki was published, gives way to this one generation's reminder.
	if _, err := txApp.DB().NewQuery(`
		DELETE FROM {{leafen_wiki_verification_queue}}
		WHERE [[data_source_id]] = '' AND [[status]] = 'ready'
			AND [[page_id]] IN (SELECT [[row_id]] FROM (` + values + `))
	`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err := txApp.DB().NewQuery(`
		INSERT INTO {{leafen_wiki_verification_queue}}
			([[page_id]], [[generation]], [[workspace_id]], [[due_at]], [[status]], [[attempts]], [[next_attempt_at]],
			 [[lease_token]], [[lease_until]], [[error_code]], [[expired_at]], [[completed_at]], [[created_at]], [[updated_at]])
		SELECT [[page]].[[id]], [[page]].[[wiki_verification_generation]], [[page]].[[workspace]],
			[[verification]].[[due]], 'ready', 0, [[verification]].[[due]], '', 0, '', 0, 0, {:now}, {:now}
		FROM (` + values + `) AS [[verification]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[verification]].[[row_id]]
		WHERE [[verification]].[[live]] AND [[verification]].[[due]] IS NOT NULL
	`).Bind(params).Execute()
	return err
}
