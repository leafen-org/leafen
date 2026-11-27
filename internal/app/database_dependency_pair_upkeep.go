package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// A data source with dependencies on and no dependency binding gets its
// Blocked by and Blocking, bound, through the job runner's look that pairs
// sub-items (database_subitem_pair_upkeep.go): one whose turning on did not
// make them, to be tried again, or one an import brought with dependencies
// on and no relation pair it could bind, as an archive from before
// dependencies became a relation carries. A source on a page in the trash
// waits for the page to come back, without holding back the sources after
// it (pairDatabaseTaskSources).

// migrateDatabaseDependencyPairs binds a few sources and reports whether
// any is left.
func (p *pocketBaseYgoPersistence) migrateDatabaseDependencyPairs(ctx context.Context) (bool, error) {
	made, waiting, err := p.pairDatabaseTaskSources(ctx, `
		SELECT [[features]].[[data_source_id]], [[features]].[[page_id]], [[features]].[[workspace_id]],
			[[features]].[[owner]], '' AS [[parent]]
		FROM {{leafen_database_task_features}} AS [[features]]
		WHERE json_extract([[features]].[[dependencies_json]], '$.enabled') = 1
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_dependency_bindings}} AS [[binding]]
				WHERE [[binding]].[[data_source_id]] = [[features]].[[data_source_id]])
			AND [[features]].[[data_source_id]] > {:after}
		ORDER BY [[features]].[[data_source_id]] LIMIT {:limit}
	`, func(source databaseSubitemMigrationSource, actorID string, authorize func(core.App) (pageAuthorization, error)) error {
		return p.ensureDatabaseDependencyPair(ctx, actorID, source.Page, source.Source, authorize, false)
	})
	if err != nil {
		return true, err
	}
	return made > 0 || waiting, nil
}
