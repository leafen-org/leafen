package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A callout's icon in a page's Markdown names a picture stored for it the way
// a file line names its file (notion_page_markdown_files.go): an upload the
// writer's connection finished, file-upload://<ID>, or the address a read gave
// the callout's own picture — fetch's stable notion-file-block://, a download
// address, leafen-file:<ID> — which places it again only while the page has
// it. Placed, it is the callout's uploaded icon, iconType "image" with the
// file's ID (block_kept_file.go), which the callout keeps as a file block
// keeps its file; the graph's one check takes only a picture whole in the
// workspace (content_graph_given_body_files.go).
// Before (2026-11-26), file-upload://… read as an icon of its first
// character, the letter "f" as an image address, and a read wrote no icon for
// an uploaded one, so a callout read and written back lost it. A tab's name
// reads its icon as a callout's does, but a tab keeps no file: an uploaded
// picture there is refused, saying what a tab's icon may be.

// notionMarkdownIconFileKey holds, until the write places it, the stored file
// a callout's icon names.
const notionMarkdownIconFileKey = "markdownIconFile"

// notionMarkdownIconFile is the stored file a block's icon names, when it
// names one.
func notionMarkdownIconFile(content json.RawMessage) (notionMarkdownFilePlacement, bool) {
	if !strings.Contains(string(content), `"`+notionMarkdownIconFileKey+`"`) {
		return notionMarkdownFilePlacement{}, false
	}
	fields := struct {
		File *notionMarkdownFilePlacement `json:"markdownIconFile"`
	}{}
	if json.Unmarshal(content, &fields) != nil || fields.File == nil {
		return notionMarkdownFilePlacement{}, false
	}
	return *fields.File, true
}

// placedIcon is a block's content with the stored file its callout's icon
// names, by fileOf, made its uploaded icon.
func (files notionMarkdownFiles) placedIcon(app core.App, blockType string, content json.RawMessage, fileOf notionMarkdownFileOf) (json.RawMessage, error) {
	placement, named := notionMarkdownIconFile(content)
	if !named {
		return content, nil
	}
	if blockType != graphBlockTypeCallout {
		return nil, graphInputError(errorCodeGraphInvalid,
			"A tab's icon is an emoji, an icons/… name, a custom emoji's :name: or an image address (https://…); an uploaded picture can be a callout's icon.")
	}
	fileID, err := fileOf(app, placement)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(content, &fields); err != nil {
		return nil, notionMarkdownFileUnavailable()
	}
	delete(fields, notionMarkdownIconFileKey)
	delete(fields, "customEmojiId")
	fields["icon"], fields["iconType"] = fileID, graphCalloutIconTypeImage
	return json.Marshal(fields)
}

// storedFileID is the file a Markdown line names: an upload Upload finds for
// the writer's connection, or one already on the page.
func (files notionMarkdownFiles) storedFileID(app core.App, placement notionMarkdownFilePlacement) (string, error) {
	switch {
	case placement.Upload != "" && files.Upload != nil:
		uploaded, err := files.Upload(app, files.WorkspaceID, placement.Upload)
		if errors.Is(err, errMCPToolInvalid) || errors.Is(err, errMCPScopeDenied) || errors.Is(err, errMCPToolFailed) {
			return "", err // an MCP refusal says which and why itself
		} else if err != nil {
			return "", notionMarkdownFileUnavailable()
		}
		return uploaded, nil
	case placement.FileID != "" && files.OnPage[placement.FileID]:
		return placement.FileID, nil
	}
	return "", notionMarkdownFileUnavailable()
}
