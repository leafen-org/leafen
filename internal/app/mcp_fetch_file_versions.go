package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's read gives each stored file on it the base_version
// leafen_replace_file takes, beside what names it there, so replacing a file
// is an upload and a replace, with no leafen_get_file between to learn the
// version (2026-11-26, the person: an agent replaced three files three times
// each in four calls apiece, "페이지를 열 때 첨부마다 버전도 같이 보여 주면 한
// 단계가 준다"). The replace still refuses a file changed since that version,
// so a read's version guards it as leafen_get_file's does. Each file, image,
// PDF, video, audio or uploaded embed block the read's text names by its
// stable address (notion_page_markdown_file_blocks.go) — such a block read
// by its own ID writes itself (notionMarkdownBlocksUnder) — is given by its
// block_id; each stored file in a database page's files properties by
// page_id, property and file_id — the names leafen_replace_file takes. Its
// text bounds the blocks: a read that stops before the page's end names only
// those it holds. The REST list of a page's file blocks gives the same
// version (content_graph_file_blocks.go).

// mcpFetchedFileVersion is one stored file of a read and its version.
type mcpFetchedFileVersion struct {
	BlockID     string `json:"block_id,omitempty"`
	PageID      string `json:"page_id,omitempty"`
	Property    string `json:"property,omitempty"`
	FileID      string `json:"file_id"`
	Filename    string `json:"filename"`
	BaseVersion string `json:"base_version"`
}

// mcpFetchFileFacts gives a notion-fetch answer what it says of its stored
// files: their download addresses, when include_file_urls asks
// (mcp_fetch_file_references.go), and their versions.
func mcpFetchFileFacts(ctx context.Context, app core.App, output *notionMCPFetchOutput, id string, withURLs bool) error {
	var err error
	if withURLs {
		if output.References, err = mcpFetchFileReferences(ctx, app, output.Text); err != nil {
			return err
		}
	}
	pageID := ""
	if output.Metadata.Type == "page" {
		pageID = mcpLocalID(app, id)
	}
	output.FileVersions, err = mcpFetchFileVersions(ctx, app, output.Text, pageID)
	return err
}

// mcpFetchFileVersions are the stored files of a read's text, then of the
// database page pageID names, if it is one ("" for none).
func mcpFetchFileVersions(ctx context.Context, app core.App, text, pageID string) ([]mcpFetchedFileVersion, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	versions, err := mcpFetchBlockFileVersions(app, authority, mcpFetchNamedFileBlocks(text))
	if err != nil || pageID == "" {
		return versions, err
	}
	rowVersions, err := mcpFetchRowFileVersions(app, authority, pageID)
	return append(versions, rowVersions...), err
}

// mcpFetchNamedFileBlocks are the blocks a read's text names by their
// stable addresses, in its order, each once.
func mcpFetchNamedFileBlocks(text string) []string {
	named := []string{}
	seen := map[string]bool{}
	for _, address := range mcpFetchFileBlockPattern.FindAllString(text, -1) {
		if blockID, _, _, ok := notionMarkdownFileBlockReference(address); ok && !seen[blockID] {
			seen[blockID] = true
			named = append(named, blockID)
		}
	}
	return named
}

// mcpFetchBlockFileVersions are the stored files of the blocks named, in
// their order, of the pages the connection may read.
func mcpFetchBlockFileVersions(app core.App, authority mcpAuthority, named []string) ([]mcpFetchedFileVersion, error) {
	if len(named) == 0 {
		return nil, nil
	}
	// The blocks a read writes, by their own IDs, in one query.
	stored := []storedNotionBlock{}
	ids := make([]any, 0, len(named))
	for _, id := range named {
		ids = append(ids, id)
	}
	if err := app.DB().Select("id", "owner", "workspace_id", "page_id", "parent_id", "position", "type", "content",
		"created_by", "last_edited_by", "created", "updated").From("leafen_blocks").Where(dbx.In("id", ids...)).All(&stored); err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	byID := map[string]storedNotionBlock{}
	for _, block := range stored {
		byID[block.ID] = block
	}
	versions := []mcpFetchedFileVersion{}
	readable, given := map[string]bool{}, map[string]bool{}
	for _, id := range named {
		block, found := byID[id]
		if !found {
			// Named another way than by its own ID, as leafen_replace_file reads it.
			resolved, ok := resolveNotionBlockID(app, id)
			if !ok {
				continue
			}
			var err error
			if block, found, err = loadStoredNotionBlock(app, resolved); err != nil {
				return nil, failedBecause(errMCPToolFailed, err)
			}
		}
		if _, managed := graphManagedFileBlockTypes[block.Type]; !found || !managed {
			continue
		}
		content, err := parseGraphFileBlockContent(json.RawMessage(block.Content))
		if err != nil || content.FileID == "" {
			continue
		}
		// The page it is on, as leafen_replace_file checks it, once a page.
		place := block.WorkspaceID + "\x00" + block.PageID
		reachable, checked := readable[place]
		if !checked {
			_, workspaceErr := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: block.WorkspaceID}, permissionWorkspaceRead)
			page, pageErr := authorizePageBody(app, authority.PrincipalID, block.PageID, permissionWorkspaceRead)
			reachable = workspaceErr == nil && pageErr == nil && page.Workspace.WorkspaceID == block.WorkspaceID
			readable[place] = reachable
		}
		if reachable && !given[block.ID] {
			given[block.ID] = true
			versions = append(versions, mcpFetchedFileVersion{
				BlockID: id, FileID: content.FileID, Filename: content.Name, BaseVersion: mcpFileBaseVersion(content),
			})
		}
	}
	return versions, nil
}

// mcpFetchRowFileVersions are the stored files of a database page's files
// properties, property by property in their order; none for another page.
func mcpFetchRowFileVersions(app core.App, authority mcpAuthority, pageID string) ([]mcpFetchedFileVersion, error) {
	row, err := authorizeDatabaseRowPage(app, authority.PrincipalID, pageID, permissionWorkspaceRead)
	if err != nil {
		// Not a database page, or one the read itself would have refused.
		return nil, nil
	}
	properties := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[type]] = {:files} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]`,
	).Bind(dbx.Params{"source": row.DataSourceID, "files": graphValueTypeFiles}).All(&properties); err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	if len(properties) == 0 {
		return nil, nil
	}
	values, err := loadGraphRowValuesByIDs(app, row.OwnerID, []string{row.RowID})
	if err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	byProperty := map[string]string{}
	for _, value := range values {
		byProperty[value.PropertyID] = value.Value
	}
	versions := []mcpFetchedFileVersion{}
	for _, property := range properties {
		if byProperty[property.ID] == "" {
			continue
		}
		items, err := parseGraphFileValueItems(json.RawMessage(byProperty[property.ID]))
		if err != nil {
			return nil, failedBecause(errMCPToolFailed, err)
		}
		for _, item := range items {
			if item.FileID == "" {
				continue // a link to a file elsewhere, which no upload replaces
			}
			content := graphFileBlockContent{FileID: item.FileID, Name: item.Name, Size: item.Size, ContentType: item.ContentType, SHA256: item.SHA256}
			versions = append(versions, mcpFetchedFileVersion{
				PageID: row.RowID, Property: property.Name, FileID: item.FileID, Filename: item.Name, BaseVersion: mcpFileBaseVersion(content),
			})
		}
	}
	return versions, nil
}
