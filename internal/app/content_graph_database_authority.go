package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	databasePropertyMetaRoot = "leafen:database:property-meta"
	databaseRowMetaRoot      = "leafen:database:row-meta"
)

func applyDatabaseGraphMutation(
	app core.App,
	doc *crdt.Doc,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
) error {
	return applyDatabaseGraphMutationWithRowReferences(
		app, doc, owner, workspaceID, pageID, actorID, operations, nil,
	)
}

// databaseRecordMutationOptions says how a write lands on the database
// records beyond its operations.
type databaseRecordMutationOptions struct {
	// PreserveRowEditStamps keeps each existing row's last edited time and
	// editor: the write is the system's (numbering an ID, converting a type,
	// mirroring a relation, moving data to a new model), not a person editing
	// the row.
	PreserveRowEditStamps bool
	// PreserveEditStampsOf keeps them on the named rows alone: those a
	// person's change reaches only as its relation pairs' other ends.
	PreserveEditStampsOf map[string]struct{}
	// CommentMarks keeps a row's change in its page's history as no edit:
	// the write only marked words with a discussion.
	CommentMarks bool
	// SettingsAsAsked gives a property whose type changes just the settings
	// asked for, as Notion's API does (contentGraphMutationHooks).
	SettingsAsAsked bool
	// LimitViews holds a database the write adds views to to the limit
	// (database_view_limit.go): a person's, a connection's or a Notion
	// import's change, not a migration's or a workspace restore's, which
	// bring back what a workspace had.
	LimitViews bool
	// RowVersionsBy is whom the rows' pages' versions are by when not the
	// writer: Leafen for its own upkeep (content_graph_mutation_leafen_own.go).
	RowVersionsBy string
}

func applyDatabaseGraphMutationWithRowReferences(
	app core.App,
	doc *crdt.Doc,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
	rowReferences *graphReferenceCache,
) error {
	return applyDatabaseGraphMutationWithOptions(
		app, doc, owner, workspaceID, pageID, actorID, operations, rowReferences, databaseRecordMutationOptions{},
	)
}

func applyDatabaseGraphMutationWithOptions(
	app core.App,
	doc *crdt.Doc,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
	rowReferences *graphReferenceCache,
	options databaseRecordMutationOptions,
) error {
	if !hasDatabaseGraphOperations(operations) {
		return nil
	}
	kept := false
	for index, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil {
			config, err := recordDatabasePropertyConversion(
				app, owner, workspaceID, pageID, actorID, *operation.Property, options.SettingsAsAsked,
			)
			if err != nil {
				return err
			}
			if config != nil {
				// A property changed back to a type it had takes back that
				// type's settings, and keeps its own as sent; the caller's
				// operations stay as they were.
				if config, err = databasePropertyTakenBackWithOwnSettings(config, operation.Property.Config); err != nil {
					return err
				}
				if !kept {
					operations, kept = append([]graphOperation(nil), operations...), true
				}
				property := *operation.Property
				property.Config = config
				operations[index].Property = &property
			}
		}
	}
	made := []string(nil)
	if options.LimitViews {
		var err error
		if made, err = newGraphViewIDs(app, operations); err != nil {
			return err
		}
	}
	if err := applyDatabaseRecordMutation(
		app, doc, owner, workspaceID, pageID, actorID, operations, rowReferences, options,
	); err != nil {
		return err
	}
	return refuseDatabaseViewsPastLimit(app, pageID, made)
}

func hasDatabaseGraphOperations(operations []graphOperation) bool {
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock, graphOperationKindMoveBlock, graphOperationKindDeleteBlock:
			continue
		default:
			return true
		}
	}
	return false
}

func databaseJSONObject(raw json.RawMessage) (map[string]any, error) {
	value, err := decodeBoundedGraphJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the content structure.")
	}
	return object, nil
}
