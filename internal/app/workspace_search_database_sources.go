package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database held in a page's body with two or more sources is listed by
// search once for each source, as Notion's search lists it (seen 2026-11-11:
// "A 및 B / 🗄 A" and "A 및 B / 🗄 B", each source's own icon in place of the
// cylinder where it has one (seen 2026-11-15), the database's own name before the
// "/" once it has one, no "데이터베이스" after it). The result names its
// sources in order, each with the first view showing it there, which its row
// opens, and says whether the database has a name of its own; without one the
// reader's language lists its sources' names (untitledDatabaseSourcesName),
// one it shows from another database by its linked view's name, which the
// source says (Linked, database_source_titles.go).

// fillWorkspaceSearchDatabaseSources names the sources of a found database
// that holds several, as its reader reads their names.
func fillWorkspaceSearchDatabaseSources(
	app core.App, closer func() searchMentionCloser, database *workspaceSearchDatabaseResult,
) error {
	_, sources, err := pagePathContainer(app, database.ContainerID)
	if err != nil || len(sources) < 2 {
		return err
	}
	for _, source := range sources {
		row := workspaceSearchDatabaseSource{
			DataSourceID: source.ID, Title: source.name(closer).Title,
			ViewID: pagePathView(app, database.ContainerID, source.ID), Linked: source.Linked,
		}
		// Its own icon after the "/", the cylinder where it has none (seen 2026-11-15).
		if source.Icon != "" && source.IconType != "" && source.IconType != graphDataSourceIconNone {
			row.Icon, row.IconType = source.Icon, source.IconType
		}
		database.Sources = append(database.Sources, row)
	}
	names, err := inlineDatabaseNamesOfSeveral(app, closer, []string{database.ContainerID})
	if err != nil {
		return err
	}
	database.Named = names[database.ContainerID].Title != ""
	return nil
}

// searchSeveralSourceWords are the words of the sources' names a database of
// several sources is found by, each source's row being found by its own name;
// none for a database of one source, which its name finds.
func searchSeveralSourceWords(app core.App, containerID string) (string, error) {
	_, sources, err := pagePathContainer(app, containerID)
	if err != nil || len(sources) < 2 {
		return "", err
	}
	words := ""
	for _, source := range sources {
		words += " " + searchWordsOf(decodeStoredTextSpans(source.Spans), source.Title)
	}
	return words, nil
}

// searchUntitledDatabaseWords are the words an untitled full-page database
// is found by: its sources' names, as the one rule names it by them
// (databaseNameFrom) and as Notion's search finds it by them (seen
// 2026-11-18: an untitled full-page database of two sources found by
// either's name); none for one with a title or of one source, which its
// title names.
func searchUntitledDatabaseWords(app core.App, page *core.Record, title string) string {
	if strings.TrimSpace(title) != "" {
		return ""
	}
	containerID := ""
	if app.DB().NewQuery(`SELECT `+fullPageDatabaseContainerSQL+` FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] = {:id}`).
		Bind(dbx.Params{"id": page.Id}).Row(&containerID) != nil || containerID == "" {
		return ""
	}
	names, err := databaseContainerNames(app, []string{containerID})
	if err != nil {
		return ""
	}
	words := []string{}
	for _, source := range names[containerID].Sources {
		words = append(words, searchWordsOf(source.Spans, source.Title))
	}
	return strings.Join(words, " ")
}
