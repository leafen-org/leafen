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
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A file in a database page's "Files & media" property is got and put back
// the same way as a block's: named by the page, the property and the file's
// ID in it, its base version its content. Only that file of the property
// changes; the property's other files stay as they are, and a copy goes in
// right after it.

type mcpRowFileTarget struct {
	Row          databaseRowPageAuthority
	PropertyID   string
	PropertyName string
	Items        graphFilesValue
	Index        int
}

func (target mcpRowFileTarget) content() graphFileBlockContent {
	item := target.Items[target.Index]
	return graphFileBlockContent{FileID: item.FileID, Name: item.Name, Size: item.Size, ContentType: item.ContentType, SHA256: item.SHA256}
}

func mcpRowFileOutput(target mcpRowFileTarget, content graphFileBlockContent) leafenMCPFileOutput {
	output := mcpPageFileOutput("", content)
	output.PageID, output.Property = target.Row.RowID, target.PropertyName
	return output
}

// authorizeMCPRowFile finds a stored file in a database page's files
// property the connection may reach.
func authorizeMCPRowFile(
	app core.App, authority mcpAuthority, pageID, property, fileID string, requirement permissionRequirement,
) (mcpRowFileTarget, error) {
	rowID, property, fileID := mcpLocalID(app, pageID), strings.TrimSpace(property), strings.TrimSpace(fileID)
	if rowID == "" || property == "" || fileID == "" {
		return mcpRowFileTarget{}, mcpInvalid("page_id names the database item, property the file property, and file_id the file in it")
	}
	row, err := authorizeDatabaseRowPage(app, authority.PrincipalID, rowID, requirement)
	if err != nil {
		return mcpRowFileTarget{}, mcpDenied("this account can't open or change that database item, or it is gone")
	}
	if _, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: row.WorkspaceID}, requirement); err != nil {
		return mcpRowFileTarget{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	properties := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
		Type string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND ([[id]] = {:property} OR [[name]] = {:property})
			AND ` + databasePropertyNotTrashedSQL("[[id]]"),
	).Bind(dbx.Params{"source": row.DataSourceID, "property": property}).All(&properties); err != nil {
		return mcpRowFileTarget{}, failedBecause(errMCPToolFailed, err)
	}
	if len(properties) != 1 || properties[0].Type != graphValueTypeFiles {
		return mcpRowFileTarget{}, fmt.Errorf("%w: the page has no one files property %q", errMCPToolInvalid, property)
	}
	target := mcpRowFileTarget{Row: row, PropertyID: properties[0].ID, PropertyName: properties[0].Name, Index: -1}
	values, err := loadGraphRowValuesByIDs(app, row.OwnerID, []string{row.RowID})
	if err != nil {
		return mcpRowFileTarget{}, failedBecause(errMCPToolFailed, err)
	}
	for _, value := range values {
		if value.PropertyID != target.PropertyID || value.Value == "" {
			continue
		}
		if target.Items, err = parseGraphFileValueItems(json.RawMessage(value.Value)); err != nil {
			return mcpRowFileTarget{}, failedBecause(errMCPToolFailed, err)
		}
	}
	for index, item := range target.Items {
		if item.FileID == fileID {
			target.Index = index
		}
	}
	if target.Index < 0 {
		return mcpRowFileTarget{}, fmt.Errorf("%w: %q holds no stored file %s", errMCPToolInvalid, target.PropertyName, fileID)
	}
	return target, nil
}

func getMCPRowFile(app core.App, authority mcpAuthority, input leafenMCPGetFileInput) (*mcp.CallToolResult, leafenMCPFileOutput, error) {
	target, err := authorizeMCPRowFile(app, authority, input.PageID, input.Property, input.FileID, permissionWorkspaceRead)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	content := target.content()
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
	output := mcpRowFileOutput(target, content)
	output.DownloadURL, output.DownloadExpiresAt = downloadURL, mcpTimestamp(expiresAt, authority.clock())
	return nil, output, nil
}

func replaceMCPRowFile(ctx context.Context, app core.App, authority mcpAuthority, input leafenMCPReplaceFileInput) (*mcp.CallToolResult, leafenMCPFileOutput, error) {
	baseVersion := strings.TrimSpace(input.BaseVersion)
	if baseVersion == "" && !input.AsCopy {
		return nil, leafenMCPFileOutput{}, mcpInvalid("replacing a file needs base_version, the version it replaces (leafen_get_file), or as_copy to keep both")
	}
	target, err := authorizeMCPRowFile(app, authority, input.PageID, input.Property, input.FileID, permissionWorkspaceWrite)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	record, err := mcpAttachableFile(app, authority, input.FileUploadID, target.Row.WorkspaceID)
	if err != nil {
		return nil, leafenMCPFileOutput{}, err
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, leafenMCPFileOutput{}, errMCPToolFailed
	}
	identity := mcpNotionIdentity(authority, target.Row.WorkspaceID)
	next := canonicalGraphFileContent(record)
	var written graphFileBlockContent
	_, err = persistence.applyContentGraphMutationContext(
		ctx, authority.PrincipalID, target.Row.ContainerPageID,
		graphMutationInput{MutationID: "mcp-file-replace:" + mcpFileMutationID(authority, "replace")},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := authorizeNotionDatabaseRowPageCurrent(txApp, identity, target.Row.RowID, permissionWorkspaceWrite)
				if err != nil || current.Authority.ContainerPageID != target.Row.ContainerPageID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				if _, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{WorkspaceID: target.Row.WorkspaceID}, permissionWorkspaceWrite); err != nil {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
				}
				return current.Page, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := authorizeMCPRowFile(txApp, authority, target.Row.RowID, target.PropertyID, input.FileID, permissionWorkspaceWrite)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				was := current.content()
				if !input.AsCopy && mcpFileBaseVersion(was) != baseVersion {
					return contentGraphMutationPreparation{}, mcpFileVersionConflict{current: was, savedBy: pageFileSavedBy(txApp, was.FileID)}
				}
				item := graphFileValueItem{FileID: next.FileID, Name: next.Name, Size: next.Size, ContentType: next.ContentType, SHA256: next.SHA256}
				items := append(graphFilesValue{}, current.Items...)
				if input.AsCopy {
					items = append(items[:current.Index+1], append(graphFilesValue{item}, current.Items[current.Index+1:]...)...)
				} else {
					items[current.Index] = item
				}
				encoded, err := json.Marshal(items)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				written = next
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID: current.Row.RowID, DataSourceID: current.Row.DataSourceID,
						SubitemParentID: optionalDatabaseRowParent(current.Row.SubitemParentID), Position: current.Row.Position,
						Values: map[string]graphRowValue{current.PropertyID: {Type: graphValueTypeFiles, Value: encoded}},
					},
				}}}, nil
			},
		},
	)
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
	return nil, mcpRowFileOutput(target, written), nil
}
