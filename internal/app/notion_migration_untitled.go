package app

import "strings"

// A Notion page or database with no title reads to an agent as Notion shows
// it, "New page" or "New database" (the migration report names them so too),
// not as an empty title it has to explain (2026-11-16: a real move's root
// list gave Notion's new page as ""). Only what the tools show: the scan and
// the run keep the title Notion gives.
const (
	notionMigrationUntitledPage     = "New page"
	notionMigrationUntitledDatabase = "New database"
)

func notionMigrationShownTitle(title, kind string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	if kind == "database" {
		return notionMigrationUntitledDatabase
	}
	return notionMigrationUntitledPage
}

func notionMigrationRootsShown(roots []notionImportRoot) []notionImportRoot {
	for index := range roots {
		roots[index].Title = notionMigrationShownTitle(roots[index].Title, roots[index].Kind)
	}
	return roots
}

func (output *leafenMCPNotionMigrationItemsOutput) showTitles() {
	for index := range output.Pages {
		output.Pages[index].Title = notionMigrationShownTitle(output.Pages[index].Title, "page")
	}
	for index := range output.Databases {
		output.Databases[index].Title = notionMigrationShownTitle(output.Databases[index].Title, "database")
	}
}
