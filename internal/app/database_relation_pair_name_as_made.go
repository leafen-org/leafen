package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseRelationPairNameAsMade is the name a two-way relation's other side
// takes once the server makes it, right after the change that asks for it
// (databaseRelationPairEnsureOperations): the name asked for, numbered as
// any property's is when its data source has it already
// (distinctDatabasePropertyName, the screen's rule) — as Notion's API names
// it and says in its answer (seen 2026-11-24: "메모" beside a text "메모"
// became "메모 1", the answer's synced_property_name "메모 1").
func databaseRelationPairNameAsMade(app core.App, ownerID, targetDataSourceID, pairID, asked string) (string, error) {
	asked = strings.TrimSpace(asked)
	names := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[name]] FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[data_source_id]] = {:source} AND [[id]] != {:pair}
			AND ` + databasePropertyNotTrashedSQL("[[id]]"),
	).Bind(dbx.Params{"owner": ownerID, "source": targetDataSourceID, "pair": pairID}).Column(&names); err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(names))
	for _, name := range names {
		taken[name] = true
	}
	return distinctDatabasePropertyName(asked, taken), nil
}
