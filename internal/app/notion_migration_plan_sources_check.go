package app

// The plan's properties checked against the scan: each planned database's
// own, then those of each of its data sources after the first, which the
// plan maps, preserves or reports as it does a database's. A data source's
// columns are its own, as Notion keeps each source's properties apart: no
// two of one source share a name, while two sources may each have one.

func (c *notionMigrationPlanChecker) checkProperties(plan notionMigrationPlan, planned map[string]notionMigrationPlanDatabase) {
	// A target database's columns by name; merged databases add to them.
	targets := map[string]map[string]notionMigrationTargetColumn{}
	for _, merging := range []bool{false, true} {
		for _, database := range plan.Databases {
			key := notionMigrationSourceKey(database.SourceID)
			if _, ok := planned[key]; !ok || planned[key].SourceID != database.SourceID || (database.Action == "merge") != merging ||
				(database.Action != "import" && database.Action != "merge") {
				continue
			}
			target := key
			if merging {
				target = notionMigrationSourceKey(database.Into)
			}
			if targets[target] == nil {
				targets[target] = map[string]notionMigrationTargetColumn{}
			}
			c.checkDatabaseProperties(c.databases[key], database, targets[target], merging)
		}
	}
	// A split's rows bring their columns to the database they go to.
	for _, database := range plan.Databases {
		key := notionMigrationSourceKey(database.SourceID)
		if planned[key].SourceID != database.SourceID || database.Action != "import" {
			continue
		}
		for _, rule := range database.Split {
			if columns := targets[notionMigrationSourceKey(rule.Into)]; columns != nil {
				c.checkSplitColumns(c.databases[key], database, columns)
			}
		}
	}
	for _, database := range plan.Databases {
		if key := notionMigrationSourceKey(database.SourceID); planned[key].SourceID == database.SourceID {
			c.checkDataSources(c.databases[key], database)
		}
	}
}

// checkDataSources checks a planned database's entries for its data sources
// after the first: each one the scan found, once, and, for a database that
// comes, each such data source planned with every property.
func (c *notionMigrationPlanChecker) checkDataSources(source notionMigrationDatabase, database notionMigrationPlanDatabase) {
	scanned := map[string]notionMigrationDataSource{}
	for _, dataSource := range source.DataSources {
		scanned[notionMigrationSourceKey(dataSource.ID)] = dataSource
	}
	comes := database.Action == "import" || database.Action == "merge"
	seen := map[string]bool{}
	for _, entry := range database.DataSources {
		key := notionMigrationSourceKey(entry.SourceID)
		dataSource, known := scanned[key]
		id := notionImportDataSourceKey(database.SourceID, entry.SourceID)
		switch {
		case !known:
			c.problem(notionMigrationProblemSourceUnknown, id, "", "data_source")
			continue
		case seen[key]:
			c.problem(notionMigrationProblemSourceDuplicate, id, "", dataSource.Name)
			continue
		}
		seen[key] = true
		if !comes {
			continue
		}
		from := len(c.check.Problems)
		c.checkDatabaseProperties(
			notionMigrationDatabase{ID: id, Title: dataSource.Name, Properties: dataSource.Properties},
			notionMigrationPlanDatabase{SourceID: id, Action: database.Action, Properties: entry.Properties},
			map[string]notionMigrationTargetColumn{}, false,
		)
		c.nameProblems(from, dataSource.Name)
	}
	if !comes {
		return
	}
	for _, dataSource := range source.DataSources {
		if seen[notionMigrationSourceKey(dataSource.ID)] {
			continue
		}
		from := len(c.check.Problems)
		id := notionImportDataSourceKey(source.ID, dataSource.ID)
		for _, property := range dataSource.Properties {
			c.problem(notionMigrationProblemPropertyUnplanned, id, property.ID, property.Name)
		}
		c.nameProblems(from, dataSource.Name)
	}
}

// nameProblems names the data source the problems from from on are about.
func (c *notionMigrationPlanChecker) nameProblems(from int, name string) {
	for index := from; index < len(c.check.Problems); index++ {
		if c.check.Problems[index].SourceTitle == "" {
			c.check.Problems[index].SourceTitle = name
		}
	}
}
