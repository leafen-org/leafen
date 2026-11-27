package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An inline database's own lock is its block's "locked", as the app's
// "데이터베이스 잠금" writes it; a full-page database locks with its page.
// Notion's API and MCP show either as the database's lock (seen 2026-10-01:
// a locked inline database reads is_locked true and fetches as "locked").

type storedDatabaseBlock struct {
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
}

func loadStoredDatabaseBlock(app core.App, ownerID, pageID, blockID string) (storedDatabaseBlock, bool, error) {
	block := storedDatabaseBlock{}
	err := app.DB().NewQuery(`
		SELECT [[parent_id]], [[position]], [[type]], [[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id} LIMIT 1
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "id": blockID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return block, false, nil
	}
	return block, err == nil, err
}

// databaseLockedFor is the one answer to whether a database's own lock holds
// its properties — the options its values would add among them — its views
// and its pages' layout for this person: a full-page database's or a wiki's
// page lock, unless they unlocked it for themselves
// (page_lock_database_structure.go), or a block on the page holding its
// source that shows it locked. It holds for whoever reaches the database,
// someone a row page alone is shared with too, who can't unlock it. An
// inline database unlocked for oneself ("잠금" › "잠금 해제") is so in that
// reader's view alone (InlineDatabase.tsx); the server reads it locked.
func databaseLockedFor(app core.App, principalID, ownerID string, container *core.Record, dataSourceID string) (bool, error) {
	if container == nil {
		return false, nil
	}
	if pageLockHoldsDatabaseStructure(container) {
		if locked, err := pageLockedFor(app, container, principalID); err != nil || locked {
			return locked, err
		}
	}
	var lockedBlocks int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId} AND [[type]]='inline_database'
			AND json_extract([[content]], '$.dataSourceId')={:dataSourceId}
			AND json_extract([[content]], '$.locked')=1
	`).Bind(dbx.Params{"ownerId": ownerID, "pageId": container.Id, "dataSourceId": dataSourceID}).Row(&lockedBlocks)
	return lockedBlocks > 0, err
}

// databaseDescriptionShowingAlone reports whether a database's write only
// shows or hides its description. Its lock holds the description's words,
// not whether they show, as Notion's (seen 2026-11-18 in Chrome, two tabs:
// a locked database took no typed letter in its description and offered
// "설명 숨기기", then "설명 표시", which held; no "설명 추가" without one).
func databaseDescriptionShowingAlone(source *graphDataSource) bool {
	if source == nil || source.DescriptionHidden == nil || !source.TitleOmitted {
		return false
	}
	rest := *source
	rest.ID, rest.PageID, rest.DescriptionHidden = "", "", nil
	return reflect.DeepEqual(rest, graphDataSource{TitleOmitted: true})
}

// databaseRowPageDatabaseLockedFor is databaseLockedFor of a row's database.
func databaseRowPageDatabaseLockedFor(app core.App, principalID string, authority databaseRowPageAuthority) (bool, error) {
	container, err := findRecordRemembered(app, pagesCollection, authority.ContainerPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return databaseLockedFor(app, principalID, authority.OwnerID, container, authority.DataSourceID)
}

// databaseBlockLocked reads an inline database's own lock, which its block
// keeps in the trash too (storedDatabaseBlockContent).
func databaseBlockLocked(app core.App, ownerID, pageID, blockID string) (bool, error) {
	stored, found, err := storedDatabaseBlockContent(app, ownerID, pageID, blockID)
	if err != nil || !found {
		return false, err
	}
	content := struct {
		Locked bool `json:"locked"`
	}{}
	if json.Unmarshal([]byte(stored), &content) != nil {
		return false, nil
	}
	return content.Locked, nil
}

// setNotionInlineDatabaseLock locks or unlocks an inline database for
// everyone through its page's graph, as the app's toolbar does.
func setNotionInlineDatabaseLock(
	ctx context.Context, app core.App, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, locked bool,
) error {
	block, found, err := loadStoredDatabaseBlock(app, container.Owner, container.PageID, container.BlockID)
	if err != nil {
		return err
	}
	if !found {
		return errDatabaseRowPageNotFound
	}
	content := map[string]any{}
	if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
		return err
	}
	if current, _ := content["locked"].(bool); current == locked {
		return nil
	}
	if locked {
		content["locked"] = true
	} else {
		delete(content, "locked")
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	var parentID *string
	if block.ParentID != "" {
		parentID = &block.ParentID
	}
	_, err = ygo.applyContentGraphMutationContext(ctx, identity.PrincipalID, container.PageID, graphMutationInput{
		MutationID: "notion-public-database-lock:" + newDiagnosticReference(),
		Operations: []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: container.BlockID, ParentID: parentID, Position: block.Position, Type: block.Type, Content: raw,
		}}},
	}, contentGraphMutationHooks{Authorize: authorizeNotionPageMutation(identity, container.PageID)})
	return err
}
