package app

import (
	"io"

	"github.com/pocketbase/pocketbase/core"
)

// writeWorkspaceExportNotionMarkdown writes the page — or the block and what
// is under it — as Notion's page Markdown, the same Markdown the page's API
// read answers (notion_page_markdown.go), as the person exporting reads it.
// Past the record limit it says some blocks were left out.
func writeWorkspaceExportNotionMarkdown(app core.App, output io.Writer, job storedJob, payload workspaceExportJobPayload,
	pageID string, blocks []workspaceExportMarkdownBlock) error {
	publicPageID, err := notionPagePublicID(app, pageID)
	if err != nil {
		return err
	}
	reader := notionMarkdownReader{Origin: configuredPublicOrigin(), PrincipalID: job.Owner,
		Readable: func(app core.App, target string) (bool, error) {
			// A page an operation holds a while is still theirs, and named so (page_access_hold.go).
			_, err := pageAccessDespiteHold(authorizePage(app, job.Owner, target, permissionWorkspaceRead))
			if isPermissionDenied(err) {
				return false, nil
			}
			return err == nil, err
		}}
	subPages := map[string]bool{}
	for _, block := range blocks {
		if block.Type == graphBlockTypeChildPage {
			subPages[block.ID] = true
		}
	}
	publicBlockID := func(blockID string) string {
		if subPages[blockID] {
			publicID, _ := notionPagePublicID(app, blockID)
			return publicID
		}
		if validNotionBlockID(blockID) {
			return blockID
		}
		publicID, _ := notionBlockPublicID(app, job.WorkspaceID, blockID)
		return publicID
	}
	references, err := loadNotionMarkdownReferences(app, payload.AuthorityOwnerID, job.WorkspaceID, pageID, blocks, reader)
	if err != nil {
		return err
	}
	var mentionErr error
	mentions := reader.mentions(app, job.WorkspaceID, &mentionErr)
	mentions.PageID = pageID
	projection := renderNotionPageMarkdown(blocks, publicPageID, false, publicBlockID, references, mentions, false)
	if mentionErr != nil {
		return mentionErr
	}
	markdown := projection.Markdown
	if projection.Truncated {
		markdown += "\n" + workspaceExportNotionMarkdownOmittedOverflowMarker
	}
	_, err = io.WriteString(output, markdown+"\n")
	return err
}
