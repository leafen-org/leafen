package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Unsyncing a copy of a synced block leaves ordinary blocks in its place: a
// copy of the original's content, with its text and formatting, where the
// copy was, on the copy's page, and the copy goes. The original may be on
// another page the person can read. Pages, databases and uploaded files do
// not come along; a copy holding them stays synced.

const syncedBlockUnsyncMaxBlocks = maxGraphMutationOperations - 1

var (
	errSyncedBlockUnsyncInvalid     = graphInputError(errorCodeGraphInvalid, "This isn't a synced block copy.")
	errSyncedBlockUnsyncUnavailable = graphConflictError(errorCodeGraphReferenceInvalid, "Couldn't read the source block.")
	errSyncedBlockUnsyncUnsupported = graphPreconditionError(errorCodeGraphInvalid, "A synced block containing pages, databases or files can't be unsynced.")
)

var syncedBlockUnsyncUncopiedTypes = map[string]struct{}{
	graphBlockTypeChildPage: {}, "child_database": {}, "inline_database": {},
}

type storedSyncedBlockSource struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
	Type     string `db:"type"`
	Content  string `db:"content"`
}

func registerSyncedBlockRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.GET(apiRoot+"/pages/{id}/synced-blocks/{blockId}/locations", readSyncedBlockLocations)
	event.Router.POST(apiRoot+"/pages/{id}/synced-blocks/{blockId}/unsync", ygo.unsyncSyncedBlock)
}

func (p *pocketBaseYgoPersistence) unsyncSyncedBlock(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	blockID := strings.TrimSpace(event.Request.PathValue("blockId"))
	fail := func(err error) error {
		return writeGraphError(event, err, "synced-block-unsync", errorCodeGraphMutationFailed, "Couldn't unsync the block.")
	}
	roots, revision, err := p.unsyncSyncedCopy(event.Request.Context(), event.App, user.Id, pageID, blockID)
	if err != nil {
		return fail(err)
	}
	return event.JSON(http.StatusOK, graphRevisionAnswer(map[string]any{"blockIds": roots}, revision))
}

// unsyncSyncedCopy turns a copy on a page the actor can change into
// ordinary blocks holding its original's content.
func (p *pocketBaseYgoPersistence) unsyncSyncedCopy(
	ctx context.Context, app core.App, actorID, pageID, blockID string,
) ([]string, int, error) {
	authorization, err := authorizePageBody(app, actorID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return nil, 0, graphNotFoundError()
	}
	input, roots, err := planSyncedBlockUnsync(app, actorID, authorization, blockID)
	if err != nil {
		return nil, 0, err
	}
	receipt, err := p.applyContentGraphMutationContext(ctx, actorID, pageID, input, contentGraphMutationHooks{})
	if err != nil {
		return nil, 0, err
	}
	return roots, receipt.Cursor, nil
}

// planSyncedBlockUnsync reads the copy and its original and writes the
// original's content after the copy, then removes the copy.
func planSyncedBlockUnsync(
	app core.App, principalID string, authorization pageAuthorization, blockID string,
) (graphMutationInput, []string, error) {
	page := authorization.Page
	copyBlock := storedSyncedBlockSource{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]], [[type]], [[content]] FROM {{leafen_blocks}}
		WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"id": blockID, "pageId": page.Id, "owner": authorization.Workspace.OwnerID}).One(&copyBlock)
	if errors.Is(err, sql.ErrNoRows) {
		return graphMutationInput{}, nil, graphBlockNotFoundError()
	}
	if err != nil {
		return graphMutationInput{}, nil, err
	}
	content := map[string]any{}
	_ = json.Unmarshal([]byte(copyBlock.Content), &content)
	sourceID, _ := content["syncedBlockId"].(string)
	sourcePageID, _ := content["syncedPageId"].(string)
	if copyBlock.Type != graphBlockTypeSyncedBlock || strings.TrimSpace(sourceID) == "" {
		return graphMutationInput{}, nil, errSyncedBlockUnsyncInvalid
	}
	if strings.TrimSpace(sourcePageID) == "" {
		// A copy written in Markdown names only its original's block, on any page.
		sourcePageID = page.Id
		_ = app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} LIMIT 1`).
			Bind(dbx.Params{"id": sourceID}).Row(&sourcePageID)
	}
	source, err := authorizePageBody(app, principalID, sourcePageID, permissionWorkspaceRead)
	if err != nil {
		return graphMutationInput{}, nil, refusedUnlessStorage(errSyncedBlockUnsyncUnavailable, err)
	}
	blocks := []storedSyncedBlockSource{}
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[tree]]([[id]]) AS (
			SELECT [[id]] FROM {{leafen_blocks}}
			WHERE [[parent_id]] = {:sourceId} AND [[page_id]] = {:pageId} AND [[owner]] = {:owner}
			-- However deep the original goes; UNION keeps each block once.
			-- CROSS JOIN walks from the tree by key: SQLite's own order read
			-- the page's or the server's every block (2026-10-18).
			UNION
			SELECT [[block]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_blocks}} [[block]] ON [[block]].[[parent_id]] = [[tree]].[[id]]
			WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[owner]] = {:owner}
			-- The walk stops one block past what a copy can take, so an
			-- original too large is told before it is read and sorted whole
			-- (2026-11-08: a 20,100-block original was, to be refused).
			LIMIT {:limit}
		)
		SELECT [[block]].[[id]], [[block]].[[parent_id]], [[block]].[[position]], [[block]].[[type]], [[block]].[[content]]
		FROM [[tree]] CROSS JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[tree]].[[id]]
		ORDER BY [[block]].[[parent_id]], [[block]].[[position]], [[block]].[[id]]
	`).Bind(dbx.Params{
		"sourceId": sourceID, "pageId": sourcePageID, "owner": source.Workspace.OwnerID,
		"limit": syncedBlockUnsyncMaxBlocks + 1,
	}).All(&blocks); err != nil {
		return graphMutationInput{}, nil, err
	}
	if len(blocks) > syncedBlockUnsyncMaxBlocks {
		return graphMutationInput{}, nil, graphInputError(errorCodeGraphInvalid, "The synced block is too large to unsync.")
	}
	blocks = syncedBlockSourceLevels(sourceID, blocks)
	var original int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"id": sourceID, "pageId": sourcePageID, "owner": source.Workspace.OwnerID}).Row(&original); err != nil {
		return graphMutationInput{}, nil, err
	}
	if original == 0 {
		return graphMutationInput{}, nil, errSyncedBlockUnsyncUnavailable
	}
	document, _, err := loadYgoDoc(app, sourcePageID)
	if err != nil {
		return graphMutationInput{}, nil, err
	}

	targets := map[string]string{sourceID: copyBlock.ParentID}
	for _, block := range blocks {
		digest := sha256.Sum256([]byte(strings.Join([]string{"synced-unsync", principalID, page.Id, blockID, block.ID}, "\x00")))
		targets[block.ID] = "b" + hex.EncodeToString(digest[:12])
	}
	previous := map[string]string{sourceID: blockID}
	operations := make([]graphOperation, 0, len(blocks)+1)
	roots := []string{}
	updates := []graphTextUpdate{}
	for _, block := range blocks {
		cloned := &graphBlock{ID: block.ID, Type: block.Type, Content: json.RawMessage(block.Content)}
		if _, uncopied := syncedBlockUnsyncUncopiedTypes[block.Type]; uncopied || graphBlockCarriesManagedFileReference(cloned) {
			return graphMutationInput{}, nil, errSyncedBlockUnsyncUnsupported
		}
		target := targets[block.ID]
		var parentID *string
		if parent := targets[block.ParentID]; parent != "" {
			parentID = &parent
		}
		var afterID *string
		if after, found := previous[block.ParentID]; found {
			afterID = &after
		}
		previous[block.ParentID] = target
		if block.ParentID == sourceID {
			roots = append(roots, target)
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: target, ParentID: parentID, AfterID: afterID, Position: block.Position, Type: block.Type,
			Content: json.RawMessage(block.Content),
		}})
		if _, textual := graphTextBlockTypes[block.Type]; textual {
			spans := graphTextSpansFromYText(document.GetText(ygoBlockTextRootPrefix + block.ID))
			for index := range spans {
				// Comments stay with the original's page.
				spans[index].CommentID = ""
			}
			if len(spans) > 0 {
				updates = append(updates, graphTextUpdate{BlockID: target, Spans: spans})
			}
		}
	}
	operations = append(operations, graphOperation{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: blockID}})
	graphRevision, err := currentNotionGraphRevision(app, authorization.Workspace.OwnerID, page.Id)
	if err != nil {
		return graphMutationInput{}, nil, err
	}
	pageCursor := page.GetInt("cursor")
	return graphMutationInput{
		MutationID: "synced-block-unsync:" + newDiagnosticReference(), Operations: operations, TextUpdates: updates,
		BasePageCursor: &pageCursor, BaseGraphRevision: &graphRevision,
	}, roots, nil
}

// syncedBlockSourceLevels orders an original's blocks a level at a time,
// each parent before its children and siblings in their order, as the copy
// places them; the rows come by parent and position.
func syncedBlockSourceLevels(sourceID string, blocks []storedSyncedBlockSource) []storedSyncedBlockSource {
	children := map[string][]storedSyncedBlockSource{}
	for _, block := range blocks {
		children[block.ParentID] = append(children[block.ParentID], block)
	}
	ordered := make([]storedSyncedBlockSource, 0, len(blocks))
	placed := map[string]bool{sourceID: true}
	for level := []string{sourceID}; len(level) > 0; {
		next := []string{}
		for _, parentID := range level {
			for _, block := range children[parentID] {
				if !placed[block.ID] {
					placed[block.ID] = true
					ordered = append(ordered, block)
					next = append(next, block.ID)
				}
			}
		}
		level = next
	}
	return ordered
}
