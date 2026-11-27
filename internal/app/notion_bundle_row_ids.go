package app

import (
	"github.com/pocketbase/dbx"
)

// stagedRowPageIdentities are the staged workspace's database pages, its
// templates' pages too, that the IDs in an answer name. A database page
// answers with its own ID (rowPageOwnPublicID), which the stored identities
// don't hold; the engine is shown the ID made from the connection for it, as
// when it answered with a row identity, so a migration begun then keeps the
// IDs it was shown.
func (ids notionBundleIDs) stagedRowPageIdentities(candidates []string) ([]notionObjectIdentity, error) {
	named := map[string]string{}
	records := []string{}
	for _, candidate := range notionBundleUUIDs(candidates) {
		recordID, err := resolvePageOwnPublicID(candidate)
		if err != nil {
			continue
		}
		if _, seen := named[recordID]; !seen {
			records = append(records, recordID)
		}
		named[recordID] = candidate
	}
	found := []notionObjectIdentity{}
	err := notionBundleIDBatch(records, func(placeholders string, params dbx.Params) error {
		params["workspace"] = ids.stagingID
		rows := []string{}
		if err := ids.app.DB().NewQuery(`
			SELECT [[row_id]] FROM {{leafen_database_row_pages}}
			WHERE [[workspace_id]] = {:workspace} AND [[row_id]] IN (` + placeholders + `)
			UNION
			SELECT [[id]] FROM {{leafen_pages}}
			WHERE [[workspace]] = {:workspace} AND [[database_template]] AND [[id]] IN (` + placeholders + `)
		`).Bind(params).Column(&rows); err != nil {
			return err
		}
		for _, recordID := range rows {
			found = append(found, notionObjectIdentity{
				PublicID: named[recordID], ObjectKind: notionObjectIdentityRowPage,
				Workspace: ids.stagingID, InternalID: recordID,
			})
		}
		return nil
	})
	return found, err
}

// stagedOwnIdentities are the staged workspace's databases, standing or in
// the trash of deleted databases, and views that the IDs in an answer name.
// A database and a view answer with their own IDs (objectOwnPublicID), which
// the stored identities don't hold; the engine is shown the ID made from the
// connection for each, as when they answered with an identity made for the
// API, so a migration begun then keeps the IDs it was shown.
func (ids notionBundleIDs) stagedOwnIdentities(candidates []string) ([]notionObjectIdentity, error) {
	owned := []string{}
	for _, candidate := range notionBundleUUIDs(candidates) {
		if ownPagePublicID(candidate) {
			owned = append(owned, candidate)
		}
	}
	found := []notionObjectIdentity{}
	err := notionBundleIDBatch(owned, func(placeholders string, params dbx.Params) error {
		params["workspace"] = ids.stagingID
		rows := []struct {
			Kind string `db:"kind"`
			ID   string `db:"id"`
		}{}
		if err := ids.app.DB().NewQuery(`
			SELECT 'database' AS [[kind]], [[id]] FROM {{leafen_database_containers}}
			WHERE [[workspace_id]] = {:workspace} AND [[id]] IN (` + placeholders + `)
			UNION
			SELECT 'database' AS [[kind]], [[block_id]] AS [[id]] FROM {{leafen_deleted_databases}}
			WHERE [[workspace_id]] = {:workspace} AND [[block_id]] IN (` + placeholders + `)
			UNION
			SELECT 'view' AS [[kind]], [[id]] FROM {{leafen_database_views}}
			WHERE [[workspace_id]] = {:workspace} AND [[id]] IN (` + placeholders + `)
		`).Bind(params).All(&rows); err != nil {
			return err
		}
		for _, row := range rows {
			found = append(found, notionObjectIdentity{
				PublicID: row.ID, ObjectKind: row.Kind, Workspace: ids.stagingID, InternalID: row.ID,
			})
		}
		return nil
	})
	return found, err
}

// presented records and gives the ID made from the connection for each of
// the staged workspace's identities, by the ID it answers with.
func (ids notionBundleIDs) presented(identities []notionObjectIdentity) (map[string]string, error) {
	shown := map[string]string{}
	for _, row := range identities {
		presented, err := notionCompatibleObjectID(row.ObjectKind, "bundle:"+ids.connectionID, row.InternalID)
		if err != nil {
			return nil, err
		}
		if _, err := ids.app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_bundle_import_ids}} ([[connection_id]], [[presented_id]], [[object_kind]], [[internal_id]])
			VALUES ({:connection}, {:presented}, {:kind}, {:internal})
		`).Bind(dbx.Params{"connection": ids.connectionID, "presented": presented, "kind": row.ObjectKind,
			"internal": row.InternalID}).Execute(); err != nil {
			return nil, err
		}
		shown[row.PublicID] = presented
	}
	return shown, nil
}
