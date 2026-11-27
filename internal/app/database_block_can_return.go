package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseBlockCanReturn says whether a database block brought back without
// the content graph's checks — by a restored version, or planned by a revert
// before the graph sees it — can stand as its database: every source it names
// is there, its own sources are on its page and no other database holds them,
// and each source it links is shown by a database out of the trash. The
// graph would refuse one that can't, and with it the whole revert or restore:
// a revert leaves it out, named as a database, and a restored version brings
// it back as a block alone, as its page couldn't show it.
func databaseBlockCanReturn(
	app core.App, owner, workspaceID, pageID, blockID string, content databaseContainerBlockContent,
) (bool, error) {
	members := content.memberDataSourceIDs()
	if len(members) == 0 {
		return false, nil
	}
	encoded, err := json.Marshal(members)
	if err != nil {
		return false, err
	}
	own, err := json.Marshal(append([]string{}, content.ownDataSourceIDs()...))
	if err != nil {
		return false, err
	}
	linked := []string{}
	for _, member := range content.linkedMemberList() {
		linked = append(linked, member.DataSourceID)
	}
	shown, err := json.Marshal(linked)
	if err != nil {
		return false, err
	}
	var missing int
	err = app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(DISTINCT [[member]].[[value]]) FROM json_each({:members}) AS [[member]]
				WHERE NOT EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[source]]
					WHERE [[source]].[[id]] = [[member]].[[value]] AND [[source]].[[owner]] = {:owner}
						AND [[source]].[[workspace_id]] = {:workspaceId}))
			+ (SELECT COUNT(*) FROM json_each({:own}) AS [[member]]
				WHERE NOT EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[source]]
					WHERE [[source]].[[id]] = [[member]].[[value]] AND [[source]].[[page_id]] = {:pageId})
				OR EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} AS [[membership]]
					JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
					WHERE [[membership]].[[data_source_id]] = [[member]].[[value]] AND [[membership]].[[source_container_id]] = ''
						AND [[container]].[[kind]] = 'source' AND [[container]].[[id]] != {:blockId}))
			+ (SELECT COUNT(*) FROM json_each({:linked}) AS [[member]]
				WHERE NOT EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} AS [[membership]]
					JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
					WHERE [[membership]].[[data_source_id]] = [[member]].[[value]] AND [[membership]].[[source_container_id]] = ''
						AND [[container]].[[kind]] = 'source' AND [[container]].[[owner]] = {:owner}
						AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
							WHERE [[trash]].[[container_id]] = [[container]].[[id]])))
	`).Bind(dbx.Params{
		"members": string(encoded), "own": string(own), "linked": string(shown),
		"owner": owner, "workspaceId": workspaceID, "pageId": pageID, "blockId": blockID,
	}).Row(&missing)
	return missing == 0, err
}
