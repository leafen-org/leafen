package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// An upload an empty file block asked for lands in that block though no
// screen shows the block when it ends (2026-11-07: a file sent while the
// person went to another page reached the server, and the block — which only
// the page showing it filled — stayed empty for everyone, and for good once
// that tab was read again). The block must still be the empty one the upload
// was for; one that holds a file or a link by then keeps it.
type pageFileAttachInput struct {
	FileID string `json:"fileId"`
}

var errPageFileAttachTaken = errors.New("the block holds a file or a link now")

// emptyFileBlockContent says whether a file block's content holds no file
// and no link.
func emptyFileBlockContent(raw string) bool {
	content := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return true
	}
	if json.Unmarshal([]byte(raw), &content) != nil {
		return false
	}
	for _, key := range []string{"fileId", "url"} {
		if value, ok := content[key].(string); ok && strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func attachPageBlockFileRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID, blockID := strings.TrimSpace(event.Request.PathValue("pageId")), strings.TrimSpace(event.Request.PathValue("blockId"))
	if _, err := authorizePageBody(event.App, user.Id, pageID, permissionWorkspaceWrite); err != nil {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	input := pageFileAttachInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.FileID) == "" {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "Give the uploaded file to put in the block.")
	}
	block, found, err := loadStoredNotionBlock(event.App, blockID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageFileSaveInvalid, "The block couldn't be read.", err)
	}
	if _, managed := graphManagedFileBlockTypes[block.Type]; !found || block.PageID != pageID || !managed {
		return apiError(event, http.StatusNotFound, errorCodePageFileSaveInvalid, "No file block there.")
	}
	record, ok, err := pageFileUploaded(event, user.Id, block, input.FileID)
	if !ok {
		return err
	}
	persistence, ok := event.App.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return apiError(event, http.StatusInternalServerError, errorCodePageFileSaveInvalid, "The file couldn't be put in the block.")
	}
	var attached graphFileBlockContent
	_, err = persistence.applyContentGraphMutationContext(
		event.Request.Context(), user.Id, block.PageID,
		graphMutationInput{MutationID: "file-attach:" + security.RandomString(20)},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizePageBody(txApp, user.Id, block.PageID, permissionWorkspaceWrite)
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, found, err := loadStoredNotionBlock(txApp, block.ID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if !found || current.PageID != block.PageID || current.Type != block.Type || !emptyFileBlockContent(current.Content) {
					return contentGraphMutationPreparation{}, errPageFileAttachTaken
				}
				content := canonicalGraphFileContent(record)
				if !notionManagedFileContentCompatible(current.Type, content) {
					return contentGraphMutationPreparation{}, errors.New(errorCodePageFileSaveInvalid)
				}
				encoded, err := json.Marshal(content)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				var parent *string
				if current.ParentID != "" {
					parent = &current.ParentID
				}
				attached = content
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind:  graphOperationKindUpsertBlock,
					Block: &graphBlock{ID: current.ID, ParentID: parent, Position: current.Position, Type: current.Type, Content: encoded},
				}}}, nil
			},
		},
	)
	if errors.Is(err, errPageFileAttachTaken) {
		return apiError(event, http.StatusConflict, errorCodePageFileSaveInvalid, "The block holds another file or link now.")
	}
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodePageFileSaveInvalid, "The file couldn't be put in the block.")
	}
	return event.JSON(http.StatusOK, pageFileVersionOf(block.ID, attached))
}
