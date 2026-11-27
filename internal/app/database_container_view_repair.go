package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database block names the view it shows. A view deleted without the
// block — by a client older than the one that moves the block in the same
// change, or before it did — left the block naming nothing, and every later
// write of the block (its lock, its archive) was refused as a broken link.
// A write of such a block now names the first view, by position, the block
// may show instead; a block with no such view is refused as before. A write
// that picks another database's view — one the block didn't name — is
// refused (validateDatabaseContainerMemberReferences), not answered as done
// with the view it named dropped (2026-11-10).

func repairDatabaseContainerViews(app core.App, owner, pageID string, projection graphProjection, operations []graphOperation) error {
	deleted := map[string]bool{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindDeleteView && operation.ViewDeletion != nil {
			deleted[operation.ViewDeletion.ID] = true
		}
	}
	for index := range operations {
		block := operations[index].Block
		if operations[index].Kind != graphOperationKindUpsertBlock || block == nil || block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		content, err := parseDatabaseContainerBlockContent(block.Content)
		if err != nil || content.ViewID == "" {
			continue
		}
		if view, exists := projection.views[content.ViewID]; exists && graphViewShownByContainer(view, block.ID, pageID, content) {
			continue
		}
		if _, exists := projection.views[content.ViewID]; exists && !deleted[content.ViewID] {
			stored, err := storedDatabaseBlockViewID(app, owner, pageID, block.ID)
			if err != nil {
				return err
			}
			if stored != content.ViewID {
				continue
			}
		}
		candidates := []struct {
			graphEntityReference
			Position string `db:"position"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[owner]], [[page_id]], [[data_source_id]], [[container_id]], [[type]], [[config]], [[position]]
			FROM {{leafen_database_views}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			ORDER BY [[position]], [[id]]
		`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).All(&candidates); err != nil {
			return err
		}
		for _, candidate := range candidates {
			if deleted[candidate.ID] || !graphViewShownByContainer(candidate.graphEntityReference, block.ID, pageID, content) {
				continue
			}
			raw := map[string]json.RawMessage{}
			if err := json.Unmarshal(block.Content, &raw); err != nil {
				break
			}
			raw["viewId"], _ = json.Marshal(candidate.ID)
			repaired, err := json.Marshal(raw)
			if err != nil {
				return err
			}
			block.Content = repaired
			projection.views[candidate.ID] = candidate.graphEntityReference
			break
		}
	}
	return nil
}

// storedDatabaseBlockViewID is the view a database block names as stored, or
// "" for a block not stored yet.
func storedDatabaseBlockViewID(app core.App, owner, pageID, blockID string) (string, error) {
	var raw string
	err := app.DB().NewQuery(`SELECT [[content]] FROM {{leafen_blocks}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}`).
		Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": blockID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	stored := struct {
		ViewID string `json:"viewId"`
	}{}
	_ = json.Unmarshal([]byte(raw), &stored)
	return stored.ViewID, nil
}
