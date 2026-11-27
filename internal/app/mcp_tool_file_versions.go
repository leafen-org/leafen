package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// An agent works on a page's file where it runs: it gets the file and the
// version it read, edits a copy, uploads the result and puts it back in the
// same block naming that version. A file someone changed in between is not
// overwritten; the agent hears what is there now.

const errorCodeMCPFileVersionConflict = "LEAFEN_MCP_FILE_VERSION_CONFLICT"

var errMCPFileVersionConflict = errors.New(errorCodeMCPFileVersionConflict)

type leafenMCPGetFileInput struct {
	BlockID  string `json:"block_id,omitempty" jsonschema:"ID of a file, image, PDF, video or audio block, as fetch shows it after #block-; or name a database page's file with page_id, property and file_id"`
	PageID   string `json:"page_id,omitempty" jsonschema:"A database page whose files property holds the file"`
	Property string `json:"property,omitempty" jsonschema:"Name or ID of that page's files property"`
	FileID   string `json:"file_id,omitempty" jsonschema:"file_id of the file in that property, as fetch or leafen_get_file gives it"`
}

type leafenMCPFileOutput struct {
	BlockID           string `json:"block_id,omitempty"`
	PageID            string `json:"page_id,omitempty"`
	Property          string `json:"property,omitempty"`
	FileID            string `json:"file_id"`
	Filename          string `json:"filename"`
	ContentType       string `json:"content_type"`
	ContentLength     int64  `json:"content_length"`
	BaseVersion       string `json:"base_version"`
	DownloadURL       string `json:"download_url,omitempty"`
	DownloadExpiresAt string `json:"download_expires_at,omitempty"`
}

type leafenMCPReplaceFileInput struct {
	BlockID      string `json:"block_id,omitempty" jsonschema:"ID of the block whose file is replaced; or name a database page's file with page_id, property and file_id"`
	PageID       string `json:"page_id,omitempty" jsonschema:"A database page whose files property holds the file"`
	Property     string `json:"property,omitempty" jsonschema:"Name or ID of that page's files property"`
	FileID       string `json:"file_id,omitempty" jsonschema:"file_id of the file in that property to replace"`
	BaseVersion  string `json:"base_version,omitempty" jsonschema:"base_version of the file the new one was made from, as notion-fetch's file_versions or leafen_get_file gives it; not needed with as_copy"`
	FileUploadID string `json:"file_upload_id" jsonschema:"A finished upload's file_upload_id, from notion-create-file-upload or notion-create-attachment"`
	AsCopy       bool   `json:"as_copy,omitzero" jsonschema:"Keep the upload as a new file block right after this one (in a files property, right after that file) instead of replacing its file; base_version is then not needed"`
}

type mcpFileVersionConflict struct {
	current graphFileBlockContent
	savedBy string
}

// The same three ways out a person gets from Leafen when someone saved
// first (document apps decision of 2026-10-10, 11).
func (conflict mcpFileVersionConflict) Error() string {
	by := ""
	if conflict.savedBy != "" {
		by = ", saved by " + conflict.savedBy
	}
	return fmt.Sprintf("%s: the file changed since base_version; it is now %q (%d bytes, base_version %s%s). "+
		"Get it again with leafen_get_file and bring your change to it, replace it anyway naming the new base_version "+
		"(the file it replaces stays in the page's history), or keep yours beside it with as_copy.",
		errorCodeMCPFileVersionConflict, conflict.current.Name, conflict.current.Size, mcpFileBaseVersion(conflict.current), by)
}

func (mcpFileVersionConflict) Unwrap() error { return errMCPFileVersionConflict }

func registerMCPFileVersionTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_get_file", Title: "Get a Leafen page file",
		Description: "Get the file in a page's file, image, PDF, video or audio block, or one file of a database page's files property: its name, type, size, the base_version to name when putting an edited copy back with leafen_replace_file, and a download URL that works for five minutes.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPGetFileInput) (*mcp.CallToolResult, leafenMCPFileOutput, error) {
		return getMCPPageFile(ctx, app, input)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_replace_file", Title: "Replace a Leafen page file",
		Description: "Put a completed upload from this connection in place of a block's file, or of one file in a database page's files property (the property's other files stay), if the file is still the base_version notion-fetch's file_versions or leafen_get_file gave. If someone changed it since, nothing is replaced and the answer names what is there now and who saved it. With as_copy, keep the upload right after it instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPReplaceFileInput) (*mcp.CallToolResult, leafenMCPFileOutput, error) {
		return replaceMCPPageFile(ctx, app, input)
	})
}

func getMCPPageFile(ctx context.Context, app core.App, input leafenMCPGetFileInput) (*mcp.CallToolResult, leafenMCPFileOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	if strings.TrimSpace(input.BlockID) == "" {
		return getMCPRowFile(app, authority, input)
	}
	_, content, err := authorizeMCPPageFileBlock(app, authority, input.BlockID, permissionWorkspaceRead)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	read, err := authorizeProductFileRead(app, authority.PrincipalID, content.FileID)
	if err != nil {
		return nil, leafenMCPFileOutput{}, mcpFileToolError(err)
	}
	read.Filesystem.Close()
	secret, err := fileDownloadCapabilitySecret(app)
	if err != nil {
		return nil, leafenMCPFileOutput{}, failedBecause(errMCPToolFailed, err)
	}
	token, expiresAt, err := sealFileDownloadCapabilityToken(secret, authority.PrincipalID, content.FileID, time.Now().UTC())
	if err != nil {
		return nil, leafenMCPFileOutput{}, failedBecause(errMCPToolFailed, err)
	}
	downloadURL, err := mcpServerURL(authority.Resource,
		apiRoot+strings.Replace(fileTransferDownloadCapabilityPath, "{token}", url.PathEscape(token), 1))
	if err != nil {
		return nil, leafenMCPFileOutput{}, failedBecause(errMCPToolFailed, err)
	}
	output := mcpPageFileOutput(input.BlockID, content)
	output.DownloadURL, output.DownloadExpiresAt = downloadURL, mcpTimestamp(expiresAt, authority.clock())
	return nil, output, nil
}

func replaceMCPPageFile(ctx context.Context, app core.App, input leafenMCPReplaceFileInput) (*mcp.CallToolResult, leafenMCPFileOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	if strings.TrimSpace(input.BlockID) == "" {
		return replaceMCPRowFile(ctx, app, authority, input)
	}
	baseVersion := strings.TrimSpace(input.BaseVersion)
	if baseVersion == "" && !input.AsCopy {
		return nil, leafenMCPFileOutput{}, mcpInvalid("replacing a file needs base_version, the version it replaces (leafen_get_file), or as_copy to keep both")
	}
	block, _, err := authorizeMCPPageFileBlock(app, authority, input.BlockID, permissionWorkspaceWrite)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	record, err := mcpAttachableFile(app, authority, input.FileUploadID, block.WorkspaceID)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	if input.AsCopy {
		if _, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: block.WorkspaceID}, permissionWorkspaceWrite); err != nil {
			return nil, leafenMCPFileOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
		}
		copied, blockID, err := insertPageFileCopy(ctx, app, authority.PrincipalID, block, record, "", "")
		if err != nil {
			return nil, leafenMCPFileOutput{}, mcpFileToolError(err)
		}
		return nil, mcpPageFileOutput(blockID, copied), nil
	}
	replaced, err := replacePageFileBlock(ctx, app, authority.PrincipalID, block, record, baseVersion,
		"mcp-file-replace:"+mcpFileMutationID(authority, "replace"),
		func(txApp core.App) (pageAuthorization, error) {
			authorized, err := authorizePageBody(txApp, authority.PrincipalID, block.PageID, permissionWorkspaceWrite)
			if err != nil || authorized.Workspace.WorkspaceID != block.WorkspaceID {
				return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
			}
			if _, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{WorkspaceID: block.WorkspaceID}, permissionWorkspaceWrite); err != nil {
				return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
			}
			return authorized, nil
		})
	var conflict mcpFileVersionConflict
	if errors.As(err, &conflict) {
		return nil, leafenMCPFileOutput{}, conflict
	}
	if errors.Is(err, errMCPToolInvalid) || errors.Is(err, errMCPScopeDenied) {
		return nil, leafenMCPFileOutput{}, err
	}
	if err != nil {
		return nil, leafenMCPFileOutput{}, mcpFileToolError(err)
	}
	return nil, mcpPageFileOutput(input.BlockID, replaced), nil
}

// replacePageFileBlock puts a stored file in place of a block's file, if the
// block's file is still baseVersion; otherwise nothing changes and the
// conflict names what is there.
func replacePageFileBlock(ctx context.Context, app core.App, principalID string, block storedNotionBlock, record *core.Record,
	baseVersion, mutationID string, authorize func(core.App) (pageAuthorization, error)) (graphFileBlockContent, error) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return graphFileBlockContent{}, errMCPToolFailed
	}
	var replaced graphFileBlockContent
	_, err := persistence.applyContentGraphMutationContext(
		ctx, principalID, block.PageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			Authorize: authorize,
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, found, err := loadStoredNotionBlock(txApp, block.ID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if !found || current.PageID != block.PageID || current.Type != block.Type {
					return contentGraphMutationPreparation{}, errMCPChangedMeanwhile
				}
				content, err := parseGraphFileBlockContent(json.RawMessage(current.Content))
				if err != nil || content.FileID == "" {
					return contentGraphMutationPreparation{}, mcpInvalid("that block holds no file")
				}
				if mcpFileBaseVersion(content) != baseVersion {
					return contentGraphMutationPreparation{}, mcpFileVersionConflict{current: content, savedBy: pageFileSavedBy(txApp, content.FileID)}
				}
				next := canonicalGraphFileContent(record)
				content.FileID, content.Name, content.Size = next.FileID, next.Name, next.Size
				content.ContentType, content.SHA256 = next.ContentType, next.SHA256
				if !notionManagedFileContentCompatible(current.Type, content) {
					return contentGraphMutationPreparation{}, fmt.Errorf("%w: a %s block can't hold a %s file", errMCPToolInvalid, current.Type, content.ContentType)
				}
				encoded, err := json.Marshal(content)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				var parent *string
				if current.ParentID != "" {
					parent = &current.ParentID
				}
				replaced = content
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertBlock,
					Block: &graphBlock{
						ID: current.ID, ParentID: parent, Position: current.Position,
						Type: current.Type, Content: encoded,
					},
				}}}, nil
			},
		},
	)
	return replaced, err
}

// authorizeMCPPageFileBlock finds a block holding a stored file in a page the
// connection may reach.
func authorizeMCPPageFileBlock(
	app core.App,
	authority mcpAuthority,
	requestedBlockID string,
	requirement permissionRequirement,
) (storedNotionBlock, graphFileBlockContent, error) {
	blockID, ok := resolveNotionBlockID(app, strings.TrimSpace(requestedBlockID))
	if !ok {
		return storedNotionBlock{}, graphFileBlockContent{}, mcpInvalid("block_id names a file block, as notion-fetch gives it")
	}
	block, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil {
		return storedNotionBlock{}, graphFileBlockContent{}, failedBecause(errMCPToolFailed, err)
	}
	if !found {
		return storedNotionBlock{}, graphFileBlockContent{}, mcpDenied("that file block is gone")
	}
	if _, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: block.WorkspaceID}, requirement); err != nil {
		return storedNotionBlock{}, graphFileBlockContent{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	page, err := authorizePageBody(app, authority.PrincipalID, block.PageID, requirement)
	if err != nil || page.Workspace.WorkspaceID != block.WorkspaceID {
		return storedNotionBlock{}, graphFileBlockContent{}, mcpDenied("this account can't open or change the page that file is on")
	}
	if _, managed := graphManagedFileBlockTypes[block.Type]; !managed {
		return storedNotionBlock{}, graphFileBlockContent{}, fmt.Errorf("%w: block %s is a %s block, not a file", errMCPToolInvalid, requestedBlockID, block.Type)
	}
	content, err := parseGraphFileBlockContent(json.RawMessage(block.Content))
	if err != nil || content.FileID == "" {
		return storedNotionBlock{}, graphFileBlockContent{}, fmt.Errorf("%w: block %s links to a file elsewhere, not one stored here", errMCPToolInvalid, requestedBlockID)
	}
	return block, content, nil
}

// mcpFileBaseVersion names a file by its content, so that the same bytes put
// back by someone else aren't counted as a change.
func mcpFileBaseVersion(content graphFileBlockContent) string {
	if content.SHA256 != "" {
		return "sha256:" + content.SHA256
	}
	return "file:" + content.FileID
}

func mcpPageFileOutput(blockID string, content graphFileBlockContent) leafenMCPFileOutput {
	return leafenMCPFileOutput{
		BlockID: strings.TrimSpace(blockID), FileID: content.FileID, Filename: content.Name,
		ContentType: content.ContentType, ContentLength: content.Size, BaseVersion: mcpFileBaseVersion(content),
	}
}

// mcpServerURL is a path on the server: at its own address when it has one
// (configuredPublicOrigin), else at the one the connection reached.
func mcpServerURL(resource, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(resource))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", failedBecause(errMCPToolFailed, err)
	}
	// A link a person opens names the server's own address, whichever the
	// agent came in at, as phones' codes, mails and invitations do
	// (outwardServerOrigin; 2026-11-22) — the one address an agent is told
	// (agent_facing_address.go).
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = "", "", "", ""
	return agentFacingOrigin(parsed.String()) + path, nil
}
