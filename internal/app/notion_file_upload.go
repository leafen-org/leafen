package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// notionBlockFileUploadContent is a block's managed file request before the
// mutation resolves it: a completed file_upload or a copied hosted file, and
// the size the block API gives the block (notionBlockSize).
type notionBlockFileUploadContent struct {
	FileUploadID string                   `json:"fileUploadId,omitempty"`
	HostedFile   *notionHostedFileRequest `json:"hostedFile,omitempty"`
	Name         string                   `json:"name,omitempty"`
	Caption      []graphTextSpan          `json:"caption,omitempty"`
	notionBlockSize
}

// notionFileUploadMissingPrefix begins Notion's refusal of a file_upload it
// can't find, which its API says as no object found (seen 2026-11-10: 404
// object_not_found for a page's icon and cover, a new page's, a picture's,
// video's, PDF's, sound's, file's and embed's file, and a files property's),
// naming the ID in its dashed form however it was written
// (notionPageValidationError says it so); one found but not uploaded is
// notionFileUploadUnsent.
const notionFileUploadMissingPrefix = "There's no file_upload with ID: "

func notionFileUploadMissing(publicID string) error {
	return graphInputError(errorCodeGraphReferenceInvalid, notionFileUploadMissingPrefix+notionFileUploadNamed(publicID)+
		" that this integration can reach. Check that the upload's status is `uploaded` and that the integration may update or insert content.")
}

// notionFileUploadUnsent is Notion's refusal of a file_upload found but not
// uploaded (seen 2026-11-10: a pending one, as a page's icon or cover, a
// block's file and a files property's alike), naming its status.
func notionFileUploadUnsent(publicID, status string) error {
	return graphInputError(errorCodeGraphReferenceInvalid, "File upload "+notionFileUploadNamed(publicID)+
		" is "+status+", and only a file that finished uploading goes here.")
}

// notionFileUploadNamed is a file_upload's ID as Notion's API names it, dashed
// however it was written.
func notionFileUploadNamed(publicID string) string {
	publicID = strings.TrimSpace(publicID)
	if dashed, ok := normalizeNotionUUIDRequest(publicID); ok {
		return dashed
	}
	return publicID
}

func resolveNotionCompletedFileUpload(
	app core.App,
	identity notionOAuthBearerIdentity,
	publicID string,
) (graphFileBlockContent, error) {
	resolved, err := resolveNotionObjectIdentity(
		app, notionObjectIdentityFileUpload, strings.TrimSpace(publicID),
	)
	if err != nil || resolved.Workspace != identity.WorkspaceID {
		return graphFileBlockContent{}, notionFileUploadMissing(publicID)
	}
	upload, err := loadProductFileUploadByID(app, identity.PrincipalID, resolved.InternalID)
	if err != nil || upload.WorkspaceID != identity.WorkspaceID {
		return graphFileBlockContent{}, notionFileUploadMissing(publicID)
	}
	if status := notionFileUploadPublicStatus(upload, time.Now().UTC().UnixMilli()); status != "uploaded" {
		return graphFileBlockContent{}, notionFileUploadUnsent(publicID, status)
	}
	if upload.Status != fileUploadStatusCompleted || upload.FileID == "" {
		return graphFileBlockContent{}, notionFileUploadMissing(publicID)
	}
	record, workspace, err := authorizeWorkspaceFile(
		app, identity.PrincipalID, upload.FileID, permissionWorkspaceRead,
	)
	if err != nil || workspace.WorkspaceID != identity.WorkspaceID {
		return graphFileBlockContent{}, notionFileUploadMissing(publicID)
	}
	if upload.NotionMode != "" && app.IsTransactional() {
		if err := markNotionFileUploadAttached(app, upload.FileID, time.Now().UTC().UnixMilli()); err != nil {
			return graphFileBlockContent{}, err
		}
	}
	return canonicalGraphFileContent(record), nil
}

func graphBlockCarriesNotionFileUploadRequest(block *graphBlock) bool {
	if block == nil {
		return false
	}
	content := map[string]json.RawMessage{}
	if json.Unmarshal(block.Content, &content) != nil {
		return false
	}
	_, upload := content["fileUploadId"]
	_, hosted := content["hostedFile"]
	return upload || hosted
}

func prepareNotionBlockFileUploadsInput(
	app core.App,
	identity notionOAuthBearerIdentity,
	input graphMutationInput,
) (graphMutationInput, error) {
	for index := range input.Operations {
		operation := &input.Operations[index]
		if operation.Kind != graphOperationKindUpsertBlock ||
			!graphBlockCarriesNotionFileUploadRequest(operation.Block) {
			continue
		}
		request := notionBlockFileUploadContent{}
		decoder := json.NewDecoder(bytes.NewReader(operation.Block.Content))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil ||
			decoder.Decode(&struct{}{}) != io.EOF ||
			(strings.TrimSpace(request.FileUploadID) == "") == (request.HostedFile == nil) ||
			len([]rune(request.Name)) > maxGraphExternalFileNameRunes ||
			!validGraphBlockCaption(request.Caption) {
			return graphMutationInput{}, graphInputError(
				errorCodeGraphReferenceInvalid, "The file_upload block content is invalid.",
			)
		}
		var canonical graphFileBlockContent
		var err error
		if request.HostedFile != nil {
			canonical, err = resolveNotionHostedFile(app, identity, *request.HostedFile)
		} else {
			canonical, err = resolveNotionCompletedFileUpload(app, identity, request.FileUploadID)
		}
		if err != nil {
			return graphMutationInput{}, err
		}
		if name := strings.TrimSpace(request.Name); name != "" {
			canonical.Name = name
		}
		canonical.Caption = request.Caption
		request.notionBlockSize.onto(operation.Block.Type, &canonical)
		// A picture, video, sound or PDF takes an upload of its kind, as
		// Notion's API names it.
		switch operation.Block.Type {
		case graphBlockTypeImage, graphBlockTypeVideo, graphBlockTypeAudio, graphBlockTypePDF:
			if kind := notionFileUploadKind(canonical.ContentType); request.HostedFile == nil && kind != operation.Block.Type {
				return graphMutationInput{}, notionFileUploadKindRefused(request.FileUploadID, kind, operation.Block.Type)
			}
		}
		if !notionManagedFileContentCompatible(operation.Block.Type, canonical) {
			return graphMutationInput{}, graphInputError(
				errorCodeGraphReferenceInvalid, "The file_upload type is not valid for this block.",
			)
		}
		encoded, err := json.Marshal(canonical)
		if err != nil {
			return graphMutationInput{}, errors.New("file_upload block content could not be prepared")
		}
		operation.Block.Content = encoded
	}
	return input, nil
}
