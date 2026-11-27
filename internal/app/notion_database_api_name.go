package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's API names a database — its object's title and its block's
// child_database title — by the one rule its name follows everywhere
// (databaseNameFrom): one of one source by that source's name, a full-page
// one of several by its page's title, an inline one of several by the name
// it keeps, either without one by its sources' names listed ("A and B",
// "New data source" for an unnamed one among them). Its own sources alone
// count, as its data_sources lists them: one of one source of its own and
// one it links goes by its own's name, a name it took on the screen as one
// of two aside (seen 2026-11-25). A source sent to the trash with its views
// counts no more there than anywhere: one of two left so goes by the
// other's name (seen 2026-11-19, the API title of a full-page one;
// 2026-11-11, an inline one's).

// notionDatabaseNamed is a database's name by that rule, its own sources
// alone counting (databaseContainerAPINames); a deleted one's as it went by
// on its page (deletedDatabaseAPINames).
func notionDatabaseNamed(app core.App, container notionDatabaseContainer) (databaseContainerName, error) {
	read := databaseContainerAPINames
	if container.Deleted {
		read = deletedDatabaseAPINames
	}
	names, err := read(app, []string{container.ID})
	return names[container.ID], err
}

// notionDatabaseNamedBySources is the name of a database without one of its
// own that its sources' names make, each as read says it, listed as Notion's
// API lists them (notionListedSourceNames).
func notionDatabaseNamedBySources(sources []databaseNameRead, read func(databaseNameRead) string) string {
	names := make([]string, len(sources))
	for index, source := range sources {
		names[index] = notionSourceShownName(read(source), len(sources))
	}
	return notionListedSourceNames(names)
}

// notionNamesReadBy says a source's name as a connection reads it
// (notionNameAsRead).
func notionNamesReadBy(app core.App, identity notionOAuthBearerIdentity) func(databaseNameRead) string {
	return func(source databaseNameRead) string {
		return notionNameAsRead(app, identity, source.Title, source.Spans)
	}
}

// notionNameAsWritten says a source's name as written, for a read with no
// reader to read mentions for.
func notionNameAsWritten(source databaseNameRead) string {
	return source.Title
}

// notionInlineDatabaseName is an inline database's name of its own, with its
// formatting, as its block keeps it (pageTitleSpans, which the API's rename
// of a database of several sources writes), or else its sources' names
// listed.
func notionInlineDatabaseName(
	app core.App, reader notionBlockAuthorization, files *notionBlockFileCapabilityIssuer, name databaseContainerName,
) ([]map[string]any, error) {
	if len(name.Sources) > 0 {
		return notionPageTitleRichText(notionDatabaseNamedBySources(name.Sources, notionNamesReadBy(app, reader.Identity))), nil
	}
	return notionFormattedTitle(app, reader, files, name.Title, name.Spans)
}

// notionDatabasePageName is a full-page database's name that its page's
// title is, with its formatting; a linked one without a name of its own, its
// page titled for it, is "Untitled", and one of several sources of its own
// without a name its sources' names listed (seen 2026-11-13).
func notionDatabasePageName(
	app core.App, reader notionBlockAuthorization, files *notionBlockFileCapabilityIssuer,
	container notionDatabaseContainer, page *core.Record, linked bool, name databaseContainerName,
) ([]map[string]any, error) {
	content, err := notionDatabaseBlockContent(app, container)
	if err != nil {
		return nil, err
	}
	title := page.GetString("title")
	generated := struct {
		Title string `json:"generatedPageTitle"`
	}{}
	_ = json.Unmarshal([]byte(content), &generated)
	if linked && (strings.TrimSpace(title) == "" || generated.Title == title) {
		return notionPageTitleRichText("Untitled"), nil
	}
	if !linked && strings.TrimSpace(title) == "" {
		return notionPageTitleRichText(notionDatabaseNamedBySources(name.Sources, notionNamesReadBy(app, reader.Identity))), nil
	}
	return notionFormattedTitle(app, reader, files, title, databasePageNameSpans([]byte(content), title))
}

// notionDatabaseBlockTitle is the title of a database's child_database block
// by that rule, its sources' names said as read says them, for a database
// of its own sources; found is false for any other, which its first source
// names.
func notionDatabaseBlockTitle(app core.App, containerID string, read func(databaseNameRead) string) (string, bool, error) {
	names, err := databaseContainerAPINames(app, []string{containerID})
	name, found := names[containerID]
	if err != nil || !found || name.Kind != databaseContainerKindSource || !name.Sourced {
		return "", false, err
	}
	switch {
	case len(name.Sources) > 0:
		return notionDatabaseNamedBySources(name.Sources, read), true, nil
	case name.Several:
		return name.Title, true, nil
	}
	return notionDatabaseShownTitle(name.Title), true, nil
}
