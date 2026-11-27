package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// The MCP writes that place uploads bring those from other workspaces into
// their target's before their transaction (mcp_file_attach.go): a body's
// Markdown names them plainly, by file-upload://<ID>; a files property only
// its data source's schema tells from another, so a database page's write
// stops when its transaction meets one, and is made again once it is brought.

var mcpMarkdownUploadPattern = regexp.MustCompile(`file-upload://([A-Za-z0-9_-]{15})`)

// mcpBringMarkdownUploads brings the uploads Markdown places into
// workspaceID; one it can't, the write refuses in its place, saying why.
func mcpBringMarkdownUploads(app core.App, authority mcpAuthority, workspaceID string, texts ...string) {
	for _, text := range texts {
		for _, match := range mcpMarkdownUploadPattern.FindAllStringSubmatch(text, -1) {
			_, _ = mcpAttachableFile(app, authority, match[1], workspaceID)
		}
	}
}

// mcpBringPageUploads brings those of notion-update-page's content into the
// page's workspace.
func mcpBringPageUploads(
	app core.App, authority mcpAuthority, authorize func(core.App) (pageAuthorization, error), input mcpUpdatePageInput,
) {
	texts := []string{input.NewString, input.Content}
	for _, update := range input.ContentUpdates {
		texts = append(texts, update.NewString)
	}
	if !strings.Contains(strings.Join(texts, "\n"), notionMarkdownUploadReference) {
		return
	}
	if current, err := authorize(app); err == nil {
		mcpBringMarkdownUploads(app, authority, current.Workspace.WorkspaceID, texts...)
	}
}

// mcpCreatePagesUploads are the texts of notion-create-pages' content.
func mcpCreatePagesUploads(pages []notionMCPCreatePageDefinition) []string {
	texts := make([]string, 0, len(pages))
	for _, page := range pages {
		texts = append(texts, page.Content)
	}
	return texts
}

// updateMCPPagePropertiesBringing is updateMCPPageProperties, made again once
// the uploads its files properties name from other workspaces are brought.
func updateMCPPagePropertiesBringing(
	ctx context.Context, app core.App, authority mcpAuthority, pageID, mutationID string, input mcpUpdatePageInput,
	authorize func(core.App) (pageAuthorization, error),
) (*mcp.CallToolResult, mcpPageMutationOutput, error) {
	var output mcpPageMutationOutput
	err := mcpWithBroughtUploads(app, authority, func() error {
		var err error
		_, output, err = updateMCPPageProperties(ctx, app, authority, pageID, mutationID, input, authorize)
		return err
	})
	return nil, output, err
}

// createMCPDatabasePagesBringing is createMCPDatabasePages with the uploads
// its content and files properties name from other workspaces brought into
// the data source's: made again for those only its transaction meets, before
// any page is made.
func createMCPDatabasePagesBringing(
	ctx context.Context, app core.App, authority mcpAuthority, dataSourceID string, definitions []notionMCPCreatePageDefinition,
) (notionMCPCreatePagesOutput, error) {
	if location, err := loadMCPDataSourceLocation(app, dataSourceID); err == nil {
		mcpBringMarkdownUploads(app, authority, location.WorkspaceID, mcpCreatePagesUploads(definitions)...)
	}
	var output notionMCPCreatePagesOutput
	err := mcpWithBroughtUploads(app, authority, func() error {
		var err error
		output, err = createMCPDatabasePages(ctx, app, authority, dataSourceID, definitions)
		if bring := (mcpUploadsToBring{}); len(output.Pages) > 0 && errors.As(err, &bring) {
			return mcpInvalid("file_upload %s is in another workspace than the database: the pages before it were made; attach it to the rest with notion-update-page",
				strings.Join(bring.ids, ", "))
		}
		return err
	})
	return output, err
}

// mcpBringOperationUploads is a leafen_call's change to a page (its content
// graph, its database) or to a database's row (its values), the uploads it
// names by fileId from other workspaces brought into the page's or the row's
// and named by their copies there, as the MCP tools bring theirs; the body
// as it was when it names none.
func mcpBringOperationUploads(app core.App, authority mcpAuthority, method, path string, body []byte) []byte {
	if method != http.MethodPost || !bytes.Contains(body, []byte(`"fileId"`)) {
		return body
	}
	workspaceID := ""
	if pageID, onPage := strings.CutPrefix(path, "/pages/"); onPage {
		pageID, _, _ = strings.Cut(pageID, "/")
		if page, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceRead); err == nil {
			workspaceID = page.Workspace.WorkspaceID
		}
	} else if rowID, onRow := strings.CutPrefix(path, "/database-rows/"); onRow {
		rowID, _, _ = strings.Cut(rowID, "/")
		if row, err := authorizeDatabaseRowPage(app, authority.PrincipalID, nativeIDWithDashes(rowID), permissionWorkspaceRead); err == nil {
			workspaceID = row.WorkspaceID
		}
	}
	if workspaceID == "" {
		return body
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if decoder.Decode(&decoded) != nil {
		return body
	}
	brought := false
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, inner := range typed {
				if id, named := inner.(string); named && key == "fileId" {
					if record, err := mcpAttachableFile(app, authority, id, workspaceID); err == nil && record.Id != id {
						typed[key], brought = record.Id, true
					}
					continue
				}
				walk(inner)
			}
		case []any:
			for _, inner := range typed {
				walk(inner)
			}
		}
	}
	walk(decoded)
	if encoded, err := json.Marshal(decoded); brought && err == nil {
		return encoded
	}
	return body
}
