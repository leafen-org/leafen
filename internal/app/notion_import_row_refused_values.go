package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A row the graph refuses as Notion gave it comes without the values it
// refuses, each reported, as a block it refuses comes as a paragraph
// (notion_import_block_apply.go): a formatted title or text that comes as
// its words alone, else no value (2026-11-23, a move stopped on a thing the
// graph refused — notion_import_left_out.go). Only a row that still won't
// go is left out of the run.

// notionImportRowValuesTaken is a refused row's values less those the graph
// refuses, each checked alone; sourceOf names each value's property source,
// for its report.
func notionImportRowValuesTaken(
	app core.App, job storedJob, payload notionImportJobPayload, pageID string,
	row graphDatabaseRow, references notionGraphApplyReferences, sourceOf map[string]string,
) (map[string]graphRowValue, error) {
	refuses := func(values map[string]graphRowValue) bool {
		alone := row
		alone.Values = values
		_, err := validateGraphMutationWithAllReferenceCachesMode(
			app, payload.AuthorityOwnerID, job.WorkspaceID, pageID,
			[]graphOperation{{Kind: graphOperationKindUpsertRow, Row: &alone}}, references.runtimeWritable,
			references.sources, nil, references.properties, references.rows, references.externalRows, false, false,
		)
		refusal := &graphRequestError{}
		return errors.As(err, &refusal)
	}
	taken := make(map[string]graphRowValue, len(row.Values))
	for propertyID, value := range row.Values {
		if !refuses(map[string]graphRowValue{propertyID: value}) {
			taken[propertyID] = value
			continue
		}
		reason := notionImportIssuePropertyValueOmitted
		if plain := (graphRowValue{Type: value.Type, Value: value.Value}); len(value.Spans) > 0 &&
			!refuses(map[string]graphRowValue{propertyID: plain}) {
			taken[propertyID], reason = plain, notionImportIssuePropertyFormattingOmitted
		}
		if err := recordNotionImportPropertyIssue(app, job.ID, sourceOf[propertyID], reason, true); err != nil {
			return nil, err
		}
	}
	return taken, nil
}
