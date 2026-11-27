package app

import (
	"cmp"
	"context"
	"encoding/json"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's get-file-download-urls (seen 2026-11-13) gives fresh download
// addresses for the files a fetch names. Leafen's fetch names a file by its
// stable notion-file-block:// address, as Notion's (seen 2026-11-22;
// notion_page_markdown_file_blocks.go), and before 2026-11-22 by a download
// address of the reader's own that names the file and its block after "#"
// (notionMarkdownReader.fileAddresses), which expires: given either, or a
// leafen-file: reference, this gives a fresh address, as a block's
// file menu downloads it (leafen_get_file); so it does for a comment's file
// get-comments names, as the comment's file list downloads it, and for a
// file named by its ID as an upload names it (mcp_file_references.go).

const mcpFileDownloadMaximumReferences = 25

type notionMCPFileDownloadURLsInput struct {
	References []string `json:"references" jsonschema:"1 to 25 file addresses exactly as notion-fetch wrote them in the page's Markdown (notion-file-block://…) or notion-get-comments in attachment-urls, or an uploaded file by its ID, file-upload://<ID> or leafen-file:<ID>"`
}

type notionMCPFileDownloadURL struct {
	Reference string `json:"reference"`
	URL       string `json:"url"`
	Name      string `json:"name,omitempty"`
	ExpiresAt string `json:"expires_at"`
}

type notionMCPFileUnresolved struct {
	Reference string `json:"reference"`
	Reason    string `json:"reason"`
}

type notionMCPFileDownloadURLsOutput struct {
	Files      []notionMCPFileDownloadURL `json:"files"`
	Unresolved []notionMCPFileUnresolved  `json:"unresolved"`
}

func registerMCPFileDownloadURLsTool(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-file-download-urls", Title: "Get Leafen file download addresses",
		Description: "Fresh download addresses for up to 25 files a fetch names, given exactly as the page's Markdown wrote them, a comment's files, as notion-get-comments names them in attachment-urls, or uploaded files by their ID or markdown_source (file-upload://<ID>). Each resolves on its own: files lists those that did, unresolved the rest with why. The addresses expire after a while: call again for new ones, and keep the Markdown's addresses, not these, when editing or copying a page.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPFileDownloadURLsInput) (*mcp.CallToolResult, notionMCPFileDownloadURLsOutput, error) {
		return getMCPFileDownloadURLs(ctx, app, input)
	})
}

// mcpFileReference reads the file, block or comment, and name a fetch's
// or get-comments' file address names.
func mcpFileReference(raw string) (fileID, blockID, commentID, name string) {
	// A stable address is taken exactly as fetch writes it, its & as it is
	// and its workspace named, as Notion's (seen 2026-11-22: one with &amp;
	// or without its query was invalid_reference).
	if stable := strings.TrimSpace(raw); strings.HasPrefix(stable, notionMarkdownFileBlockScheme) {
		if strings.Contains(stable, "&amp;") || !strings.Contains(stable, "?space_id=") {
			return "", "", "", ""
		}
	}
	// As the Markdown writes it, in an attribute: "&" as "&amp;".
	raw = html.UnescapeString(strings.TrimSpace(raw))
	// One named by its ID alone, as an upload or an import names it.
	if id := mcpUploadedFileID(raw); id != "" {
		return id, "", "", ""
	}
	if blockID, fileID, name, stable := notionMarkdownFileBlockReference(raw); stable {
		return fileID, blockID, "", name
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Fragment == "" {
		return "", "", "", ""
	}
	values, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		return "", "", "", ""
	}
	return strings.TrimSpace(values.Get("file")), strings.TrimSpace(values.Get("block")),
		strings.TrimSpace(values.Get("comment")), values.Get("name")
}

func getMCPFileDownloadURLs(ctx context.Context, app core.App, input notionMCPFileDownloadURLsInput) (*mcp.CallToolResult, notionMCPFileDownloadURLsOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPFileDownloadURLsOutput{}, err
	}
	if len(input.References) == 0 || len(input.References) > mcpFileDownloadMaximumReferences {
		return nil, notionMCPFileDownloadURLsOutput{}, mcpInvalid("references names 1 to %d files", mcpFileDownloadMaximumReferences)
	}
	secret, err := fileDownloadCapabilitySecret(app)
	if err != nil {
		return nil, notionMCPFileDownloadURLsOutput{}, failedBecause(errMCPToolFailed, err)
	}
	output := notionMCPFileDownloadURLsOutput{Files: []notionMCPFileDownloadURL{}, Unresolved: []notionMCPFileUnresolved{}}
	for _, reference := range input.References {
		file, reason, err := resolveMCPFileDownload(app, authority, secret, reference)
		if err != nil {
			return nil, notionMCPFileDownloadURLsOutput{}, err
		}
		if reason != "" {
			output.Unresolved = append(output.Unresolved, notionMCPFileUnresolved{Reference: reference, Reason: reason})
			continue
		}
		output.Files = append(output.Files, file)
	}
	return nil, output, nil
}

// resolveMCPFileDownload is a fresh download address for a file a fetch or
// get-comments names, or why there is none.
func resolveMCPFileDownload(app core.App, authority mcpAuthority, secret, reference string) (notionMCPFileDownloadURL, string, error) {
	fileID, blockID, commentID, name := mcpFileReference(reference)
	if fileID == "" {
		return notionMCPFileDownloadURL{}, "not a file address notion-fetch or notion-get-comments gave", nil
	}
	// A file on a block is read as the block's page is, a narrowed
	// connection's pages among them, and one on a comment as the
	// comment is; one named alone, as its workspace's.
	if commentID != "" {
		if !mcpCommentFileReadable(app, authority, commentID, fileID) {
			return notionMCPFileDownloadURL{}, "this connection can't read that comment, or the file is gone from it", nil
		}
	} else if blockID != "" {
		if !mcpBlockFileReadable(app, authority, blockID, fileID) {
			return notionMCPFileDownloadURL{}, "this connection can't open that file, or it is gone from its block", nil
		}
	} else if authority.PagesNarrowed {
		return notionMCPFileDownloadURL{}, "give the file's address as notion-fetch wrote it, which names its block", nil
	}
	read, err := authorizeProductFileRead(app, authority.PrincipalID, fileID)
	if err != nil {
		return notionMCPFileDownloadURL{}, "this connection can't open that file, or it is gone", nil
	}
	read.Filesystem.Close()
	if !authority.reaches(read.Record.GetString("workspace")) {
		return notionMCPFileDownloadURL{}, "that file is outside the workspaces this connection was given", nil
	}
	token, expiresAt, err := sealFileDownloadCapabilityToken(secret, authority.PrincipalID, fileID, time.Now().UTC())
	if err != nil {
		return notionMCPFileDownloadURL{}, "", failedBecause(errMCPToolFailed, err)
	}
	address, err := mcpServerURL(authority.Resource,
		apiRoot+strings.Replace(fileTransferDownloadCapabilityPath, "{token}", url.PathEscape(token), 1))
	if err != nil {
		return notionMCPFileDownloadURL{}, "", failedBecause(errMCPToolFailed, err)
	}
	if name == "" {
		name = cmp.Or(read.Record.GetString("name"), read.Record.GetString("original_name"))
	}
	return notionMCPFileDownloadURL{Reference: reference, URL: address, Name: name, ExpiresAt: mcpTimestamp(expiresAt, authority.clock())}, "", nil
}

// mcpBlockFileReadable is whether the connection reads the page of the block
// an address names a file by, and the block keeps that file: a file block's
// own, or a callout's uploaded icon, which fetch names by the callout
// (notionMarkdownCalloutIcon).
func mcpBlockFileReadable(app core.App, authority mcpAuthority, blockID, fileID string) bool {
	if _, content, err := authorizeMCPPageFileBlock(app, authority, blockID, permissionWorkspaceRead); err == nil {
		return content.FileID == fileID
	}
	resolved, ok := resolveNotionBlockID(app, strings.TrimSpace(blockID))
	if !ok {
		return false
	}
	block, found, err := loadStoredNotionBlock(app, resolved)
	if err != nil || !found || graphCalloutIconFileID(block.Type, json.RawMessage(block.Content)) != fileID {
		return false
	}
	if _, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: block.WorkspaceID}, permissionWorkspaceRead); err != nil {
		return false
	}
	page, err := authorizePageBody(app, authority.PrincipalID, block.PageID, permissionWorkspaceRead)
	return err == nil && page.Workspace.WorkspaceID == block.WorkspaceID
}
