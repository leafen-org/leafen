package app

import (
	"net/url"
	"strings"
)

// An MCP read names a stored file on a page by a stable address, as Notion's
// MCP names an uploaded file (seen 2026-11-22, notion-fetch of a page holding
// an uploaded file and picture): notion-file-block://<block>/<file> with its
// workspace and name after it, ?space_id=<ID>&name=<name>, its & written as
// it is in a tag's source as well, where a web address's is &amp;. Every read
// writes a file alike, so a line copied from any fetch is the page's own and
// keeps its block; written back, the address places the same file
// (notionMarkdownStoredFile). notion-get-file-download-urls gives a short
// lived download address for one, and fetch's include_file_urls a map of
// them (mcp_fetch_file_references.go). A reader of the public API's Markdown
// is given a download address sealed for its read instead
// (notionMarkdownReader.fileAddresses), as before.

const notionMarkdownFileBlockScheme = "notion-file-block://"

// notionMarkdownFileBlockAddress is a stored file's stable address, its
// name percent-encoded as Notion's (%20 for a space, not +).
func notionMarkdownFileBlockAddress(workspaceID, blockID, fileID, name string) string {
	address := notionMarkdownFileBlockScheme + blockID + "/" + fileID
	query := []string{}
	if workspaceID != "" {
		query = append(query, "space_id="+notionMarkdownFileBlockEscaped(workspaceID))
	}
	if name != "" {
		query = append(query, "name="+notionMarkdownFileBlockEscaped(name))
	}
	if len(query) > 0 {
		address += "?" + strings.Join(query, "&")
	}
	return address
}

func notionMarkdownFileBlockEscaped(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// notionMarkdownFileBlockReference reads a stable address: its block, file
// and name; false for anything else. Its & may be written either way.
func notionMarkdownFileBlockReference(address string) (blockID, fileID, name string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(address), notionMarkdownFileBlockScheme)
	if !found {
		return "", "", "", false
	}
	path, query, _ := strings.Cut(rest, "?")
	blockID, fileID, found = strings.Cut(path, "/")
	if !found || !workspaceImportReferenceIDPattern.MatchString(blockID) || !workspaceImportReferenceIDPattern.MatchString(fileID) {
		return "", "", "", false
	}
	values, err := url.ParseQuery(strings.ReplaceAll(query, "&amp;", "&"))
	if err == nil {
		name = strings.TrimSpace(values.Get("name"))
	}
	return blockID, fileID, name, true
}

// notionMarkdownFileSource is a file's address as its tag's source writes
// it: a stable one as it is, as Notion's; any other escaped
// (notionMarkdownSourceWritten).
func notionMarkdownFileSource(address string) string {
	if strings.HasPrefix(address, notionMarkdownFileBlockScheme) {
		return address
	}
	return notionMarkdownSourceWritten.Replace(address)
}
