package app

import (
	"context"
	"regexp"

	"github.com/pocketbase/pocketbase/core"
)

// fetch's include_file_urls gives, beside the Markdown, a download address
// for each stored file it names by its stable address, as Notion's fetch
// gives its references map (seen 2026-11-22: include_file_urls true answers
// "references", each notion-file-block:// address the page's Markdown names
// to a signed download address of a few minutes; left out, no references).
// Each address is the one notion-get-file-download-urls gives for it, under
// the same checks; one the connection can't download is left out.

// mcpFetchFileBlockPattern finds the stable addresses a fetch's text names
// (notion_page_markdown_file_blocks.go).
var mcpFetchFileBlockPattern = regexp.MustCompile(`notion-file-block://[A-Za-z0-9_-]+/[A-Za-z0-9_-]+(?:\?[A-Za-z0-9%._~=&-]*)?`)

// mcpFetchFileReferences maps each stored file a fetch's text names to a
// fresh download address.
func mcpFetchFileReferences(ctx context.Context, app core.App, text string) (map[string]string, error) {
	found := mcpFetchFileBlockPattern.FindAllString(text, -1)
	if len(found) == 0 {
		return nil, nil
	}
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, err
	}
	secret, err := fileDownloadCapabilitySecret(app)
	if err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	references := map[string]string{}
	for _, reference := range found {
		if _, done := references[reference]; done {
			continue
		}
		file, reason, err := resolveMCPFileDownload(app, authority, secret, reference)
		if err != nil {
			return nil, err
		}
		if reason == "" {
			references[reference] = file.URL
		}
	}
	return references, nil
}
