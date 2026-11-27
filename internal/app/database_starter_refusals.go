package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What a refused database template says to fix (request_refusal.go): the
// field as the request names it and why, after "Check the database template
// choices." — the choices, the page the template goes on and the database it
// replaces each say their own reason. The template's own operations are the
// server's, so a refusal of one of them says the words alone.

const databaseStarterRefusalMessage = "Check the database template choices."

// databaseStarterRefused refuses a template's use for the reason given.
func databaseStarterRefused(format string, args ...any) error {
	return graphRefused(errorCodeDatabaseStarterInvalid, databaseStarterRefusalMessage, format, args...)
}

// databaseStarterPageNotEmpty refuses a template for a page that holds
// content: it goes on an empty page, as Notion's does.
func databaseStarterPageNotEmpty() error {
	return databaseStarterRefused("the page has content: a template goes on an empty page, or names an empty inline database with replaceBlockId")
}

// databaseStarterInputRefusal is why the request's own fields can't be used,
// or nil.
func databaseStarterInputRefusal(pageID string, input databaseStarterApplyInput) error {
	switch {
	case pageID == "":
		return databaseStarterRefused("the page ID in the path is empty")
	case input.MutationID == "" || len(input.MutationID) > maxGraphIdentifierLength:
		return databaseStarterRefused("mutationId is a new id of yours, 1–%d characters, so the same request sent again isn't made twice",
			maxGraphIdentifierLength)
	case input.BasePageCursor < 0:
		return databaseStarterRefused("basePageCursor is the page's cursor as it was read, 0 or more")
	case input.BaseGraphRevision < 0:
		return databaseStarterRefused("baseGraphRevision is the page's revision as it was read, 0 or more")
	case len(input.Language) > 64:
		return databaseStarterRefused("language is a language tag of at most 64 characters, such as ko or en")
	case len(input.ReplaceBlockID) > maxGraphIdentifierLength:
		return databaseStarterRefused("replaceBlockId is at most %d characters", maxGraphIdentifierLength)
	case len(input.ReplaceDataSourceID) > maxGraphIdentifierLength:
		return databaseStarterRefused("replaceDataSourceId is at most %d characters", maxGraphIdentifierLength)
	case input.ReplaceDataSourceID != "" && input.ReplaceBlockID == "":
		return databaseStarterRefused("replaceDataSourceId goes with replaceBlockId, the database block whose data source it is")
	}
	return nil
}

// databaseStarterChoiceRefusal is why the template, its features or its views
// can't be used, or nil.
func databaseStarterChoiceRefusal(catalog databaseStarterCatalog, template *databaseStarterTemplate, input databaseStarterApplyInput) error {
	if template == nil {
		keys := make([]string, 0, len(catalog.Templates))
		for _, known := range catalog.Templates {
			keys = append(keys, known.Key)
		}
		return databaseStarterRefused("templateId is one of %s, not %q", strings.Join(keys, ", "), input.TemplateID)
	}
	features := make([]string, 0, len(template.Features))
	for _, feature := range template.Features {
		features = append(features, feature.Key)
	}
	if reason := databaseStarterChoiceReason("features", input.Features, features); reason != "" {
		return databaseStarterRefused("%s", reason)
	}
	views := make([]string, 0, len(template.ViewChoices))
	for _, view := range template.ViewChoices {
		views = append(views, view.Key)
	}
	if len(input.Views) == 0 {
		return databaseStarterRefused("views is empty: choose at least one of %s", strings.Join(views, ", "))
	}
	if reason := databaseStarterChoiceReason("views", input.Views, views); reason != "" {
		return databaseStarterRefused("%s", reason)
	}
	return nil
}

// databaseStarterChoiceReason is why a list of a template's choices isn't
// the template's, each at most once, or "".
func databaseStarterChoiceReason(field string, choices, known []string) string {
	seen := map[string]bool{}
	for index, choice := range choices {
		switch {
		case seen[choice]:
			return fmt.Sprintf("%s[%d] %q is named twice", field, index, choice)
		case !containsString(known, choice):
			if len(known) == 0 {
				return fmt.Sprintf("%s is empty for this template, not %q", field, choice)
			}
			return fmt.Sprintf("%s[%d] is one of %s, not %q", field, index, strings.Join(known, ", "), choice)
		}
		seen[choice] = true
	}
	return ""
}

// writeDatabaseStarterError answers a refused template with what to fix where
// the refusal is the route's own, and as writeGraphError does otherwise.
func writeDatabaseStarterError(event *core.RequestEvent, err error, stage string) error {
	var refusal *graphRequestError
	if errors.As(err, &refusal) && refusal.code == errorCodeDatabaseStarterInvalid {
		return writeGraphChangeError(event, err, stage, errorCodeDatabaseStarterFailed, "Couldn't create the database.")
	}
	return writeGraphError(event, err, stage, errorCodeDatabaseStarterFailed, "Couldn't create the database.")
}
