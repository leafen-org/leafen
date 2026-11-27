package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A wiki's database belongs to its wiki, as in Notion, where a wiki is undone
// rather than its database deleted: the database's block stays on the wiki's
// page as it is, its data source stays in it, and its Owner and Verification
// properties stay with their kind. Undoing the wiki is what removes them.

const (
	wikiDatabaseKeptMessage = "Undo the wiki to remove its database."
	wikiPropertyKeptMessage = "A wiki's Owner and Verification properties stay with the wiki."
)

var (
	errWikiDatabaseKept = graphPreconditionError(errorCodeGraphReferenceInvalid, wikiDatabaseKeptMessage)
	errWikiPropertyKept = graphPreconditionError(errorCodeGraphReferenceInvalid, wikiPropertyKeptMessage)
)

// wikiDatabaseOnPage is what a page holds of wikis: the data sources that
// are a wiki's, their Owner and Verification properties, and the blocks of
// the databases they are in.
type wikiDatabaseOnPage struct {
	Sources    map[string]struct{}
	Properties map[string]struct{}
	Blocks     map[string]string
}

func loadWikiDatabaseOnPage(app core.App, ownerID, pageID string) (wikiDatabaseOnPage, error) {
	found := wikiDatabaseOnPage{
		Sources: map[string]struct{}{}, Properties: map[string]struct{}{}, Blocks: map[string]string{},
	}
	sources := []struct {
		ID           string `db:"id"`
		Owner        string `db:"wiki_owner_property_id"`
		Verification string `db:"wiki_verification_property_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[wiki_owner_property_id]], [[wiki_verification_property_id]] FROM {{leafen_data_sources}}
		WHERE [[owner]] = {:ownerId} AND [[page_id]] = {:pageId} AND [[wiki]] = 1
	`).Bind(dbx.Params{"ownerId": ownerID, "pageId": pageID}).All(&sources); err != nil || len(sources) == 0 {
		return found, err
	}
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		found.Sources[source.ID] = struct{}{}
		found.Properties[source.Owner] = struct{}{}
		found.Properties[source.Verification] = struct{}{}
		sourceIDs = append(sourceIDs, source.ID)
	}
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return found, err
	}
	blocks := []struct {
		ID     string `db:"id"`
		Source string `db:"source_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], json_extract([[content]], '$.dataSourceId') AS [[source_id]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:ownerId} AND [[page_id]] = {:pageId} AND [[type]] = {:type}
			AND json_valid([[content]]) AND COALESCE(json_extract([[content]], '$.linkedDatabaseSource'), 0) = 0
			AND json_extract([[content]], '$.dataSourceId') IN (SELECT [[value]] FROM json_each({:sources}))
	`).Bind(dbx.Params{
		"ownerId": ownerID, "pageId": pageID, "type": graphBlockTypeInlineDatabase, "sources": string(encoded),
	}).All(&blocks); err != nil {
		return found, err
	}
	for _, block := range blocks {
		found.Blocks[block.ID] = block.Source
	}
	return found, nil
}

// guardWikiDatabaseOperations refuses graph operations that would take a
// wiki's database, its block or its Owner and Verification properties from
// the wiki.
func guardWikiDatabaseOperations(app core.App, ownerID, pageID string, operations []graphOperation) error {
	wiki, err := loadWikiDatabaseOnPage(app, ownerID, pageID)
	if err != nil || len(wiki.Sources) == 0 {
		return err
	}
	deleted := []string{}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindDeleteDataSource:
			if operation.DataSourceDeletion != nil {
				if _, kept := wiki.Sources[operation.DataSourceDeletion.ID]; kept {
					return errWikiDatabaseKept
				}
			}
		case graphOperationKindDeleteProperty:
			if operation.PropertyDeletion != nil {
				if _, kept := wiki.Properties[operation.PropertyDeletion.ID]; kept {
					return errWikiPropertyKept
				}
			}
		case graphOperationKindUpsertProperty:
			if operation.Property == nil {
				continue
			}
			if _, kept := wiki.Properties[operation.Property.ID]; !kept {
				continue
			}
			var kind, sourceID string
			if err := app.DB().NewQuery(`SELECT [[type]], [[data_source_id]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": operation.Property.ID}).Row(&kind, &sourceID); err != nil {
				return err
			}
			if kind != operation.Property.Type || sourceID != operation.Property.DataSourceID {
				return errWikiPropertyKept
			}
		case graphOperationKindMoveBlock:
			if operation.Move != nil {
				if _, kept := wiki.Blocks[operation.Move.BlockID]; kept {
					return errWikiDatabaseKept
				}
			}
		case graphOperationKindUpsertBlock:
			if operation.Block == nil {
				continue
			}
			sourceID, kept := wiki.Blocks[operation.Block.ID]
			if !kept {
				continue
			}
			content, err := parseDatabaseContainerBlockContent(operation.Block.Content)
			if err != nil || operation.Block.Type != graphBlockTypeInlineDatabase ||
				operation.Block.ParentID != nil && *operation.Block.ParentID != "" ||
				content.DataSourceID != sourceID || content.LinkedDatabaseSource || len(content.AdditionalDataSourceIDs) != 0 {
				return errWikiDatabaseKept
			}
		case graphOperationKindDeleteBlock:
			if operation.Deletion != nil {
				deleted = append(deleted, operation.Deletion.BlockID)
			}
		}
	}
	if len(deleted) == 0 {
		return nil
	}
	within, err := blocksWithinDeletion(app, ownerID, pageID, deleted)
	if err != nil {
		return err
	}
	for _, blockID := range within {
		if _, kept := wiki.Blocks[blockID]; kept {
			return errWikiDatabaseKept
		}
	}
	return nil
}

// blocksWithinDeletion lists the blocks deleting the given blocks deletes:
// them and the blocks inside them.
func blocksWithinDeletion(app core.App, ownerID, pageID string, blockIDs []string) ([]string, error) {
	encoded, err := json.Marshal(blockIDs)
	if err != nil {
		return nil, err
	}
	within := []string{}
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[deleted_blocks]]([[id]]) AS (
			SELECT [[value]] FROM json_each({:blockIds})
			UNION
			-- CROSS JOIN steps from the tree by key (owner, page, parent): SQLite's
			-- own order read the page's every block for each block (2026-11-08).
			SELECT [[child]].[[id]] FROM [[deleted_blocks]] AS [[parent]]
			CROSS JOIN {{leafen_blocks}} AS [[child]] ON [[child]].[[parent_id]] = [[parent]].[[id]]
			WHERE [[child]].[[owner]] = {:ownerId} AND [[child]].[[page_id]] = {:pageId}
		)
		SELECT [[id]] FROM [[deleted_blocks]]
	`).Bind(dbx.Params{"ownerId": ownerID, "pageId": pageID, "blockIds": string(encoded)}).Column(&within)
	return within, err
}

// guardWikiDatabaseDeletion keeps a wiki's data source and its Owner and
// Verification properties out of the database deletion lifecycle.
func guardWikiDatabaseDeletion(app core.App, targetKind, targetID string) error {
	switch targetKind {
	case databaseMutationTargetSource:
		if _, wiki, err := loadWikiSourceProperties(app, targetID); err != nil || wiki {
			if err == nil {
				err = errWikiDatabaseKept
			}
			return err
		}
	case databaseMutationTargetProperty:
		if kept, err := wikiPropertyKept(app, targetID); err != nil || kept {
			if err == nil {
				err = errWikiPropertyKept
			}
			return err
		}
	}
	return nil
}

// wikiPropertyKept reports whether a property is a wiki's Owner or
// Verification property.
func wikiPropertyKept(app core.App, propertyID string) (bool, error) {
	var kept int
	err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_data_sources}} WHERE [[wiki]] = 1 AND [[wiki_owner_property_id]] = {:id}
		) OR EXISTS (
			SELECT 1 FROM {{leafen_data_sources}} WHERE [[wiki]] = 1 AND [[wiki_verification_property_id]] = {:id}
		)
	`).Bind(dbx.Params{"id": propertyID}).Row(&kept)
	return kept == 1, err
}

// wikiBlocksWithin reports whether a block, or a block inside it, is a wiki's
// database block.
func wikiBlocksWithin(app core.App, ownerID, pageID, blockID string) (bool, error) {
	wiki, err := loadWikiDatabaseOnPage(app, ownerID, pageID)
	if err != nil || len(wiki.Blocks) == 0 {
		return false, err
	}
	within, err := blocksWithinDeletion(app, ownerID, pageID, []string{blockID})
	if err != nil {
		return false, err
	}
	for _, id := range within {
		if _, kept := wiki.Blocks[id]; kept {
			return true, nil
		}
	}
	return false, nil
}
