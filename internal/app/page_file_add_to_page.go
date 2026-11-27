package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// Notion's file preview has "페이지에 추가": the person picks a page and the
// file is added at that page's end as an embed that shows the document in
// the page (seen 2026-11-06: an embed block of the same file). The person
// reads the file where it is and edits the page it goes to; a file no page
// can show inline goes as a file row.

type pageFileAddInput struct {
	DestinationPageID string `json:"destinationPageId"`
}

type pageFileAddResult struct {
	PageID  string `json:"pageId"`
	BlockID string `json:"blockId"`
}

func addPageBlockFileToPageRoute(event *core.RequestEvent) error {
	personID, block, content, ok, err := pageFileBlock(event, permissionWorkspaceRead)
	if !ok {
		return err
	}
	input := pageFileAddInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.DestinationPageID) == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "Choose the page to add the file to.")
	}
	record, err := event.App.FindRecordById(filesCollection, content.FileID)
	if err != nil || record.GetString("workspace") != block.WorkspaceID {
		return apiError(event, http.StatusNotFound, errorCodePageFileSaveInvalid, "No stored file in that block.")
	}
	embed := canonicalGraphFileContent(record)
	if content.Name != "" {
		embed.Name = content.Name
	}
	destination := strings.TrimSpace(input.DestinationPageID)
	blockID, err := insertPageFileAtEnd(event.Request.Context(), event.App, personID, destination, record.GetString("workspace"), embed)
	if isPermissionDenied(err) {
		// The window that asked says why: the person can't edit that page.
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You can't edit that page.")
	}
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "The file couldn't be added to that page.")
	}
	return event.JSON(http.StatusOK, pageFileAddResult{PageID: destination, BlockID: blockID})
}

// previewAddedDocumentHeight is the frame of a document added to a page from its preview.
const previewAddedDocumentHeight = 480

// insertPageFileAtEnd adds a file to the end of a page of the file's
// workspace, as an embed where the page can show it inline.
func insertPageFileAtEnd(
	ctx context.Context, app core.App, actorID, pageID, workspaceID string, content graphFileBlockContent,
) (string, error) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return "", errors.New(errorCodePageFileSaveInvalid)
	}
	blockType := graphBlockTypeEmbed
	if !notionManagedFileContentCompatible(blockType, content) {
		blockType = graphBlockTypeFile
	} else if officeDocumentPreviewable(strings.TrimSpace(strings.SplitN(strings.ToLower(content.ContentType), ";", 2)[0]), content.Name) {
		// A document added from its preview shows 480px of it, as Notion's
		// (seen 2026-11-07: 320 for one uploaded into an embed).
		content.Height = previewAddedDocumentHeight
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	blockID := uuid.NewString()
	_, err = persistence.applyContentGraphMutationContext(ctx, actorID, pageID,
		graphMutationInput{MutationID: "file-add:" + security.RandomString(20)},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizePageBody(txApp, actorID, pageID, permissionWorkspaceWrite)
			},
			Prepare: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				// A file stays in its workspace, as Notion's picker lists that workspace's pages.
				if authorization.Workspace.WorkspaceID != workspaceID {
					return contentGraphMutationPreparation{}, errors.New(errorCodePageFileSaveInvalid)
				}
				last := ""
				if err := txApp.DB().NewQuery(`
					SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_blocks}}
					WHERE [[page_id]] = {:page} AND COALESCE([[parent_id]], '') = ''
				`).Bind(dbx.Params{"page": pageID}).Row(&last); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				position, err := databasePositionBetween(last, "")
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind:  graphOperationKindUpsertBlock,
					Block: &graphBlock{ID: blockID, Position: position, Type: blockType, Content: encoded},
				}}}, nil
			},
		},
	)
	return blockID, err
}
