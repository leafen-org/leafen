package app

import (
	"bytes"
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A stored file a Markdown line names is placed as its block's file, and a
// callout's icon as its uploaded picture, in the graph's form, before the
// change is written. A changed file line keeps its block, only what the line
// changed changing (notion_page_markdown_plan.go): the block's content is
// merged from the line as the page was read (its base) to the line as written
// onto the block as stored (resolveGraphBlockContentUpdates). So the base is
// placed as the line is, both in one form — what a line says of its file and
// no more — and the merge sees only what the line changed. Before
// (2026-11-26, a place giving the file's size, type and digest as zero and
// leaving the base as read), every caption a file, image, video, audio or PDF
// line added, changed or removed was refused as a conflict, in the MCP's
// update_content and the public API's "v1" and "v2" alike.

// notionMarkdownPlacedFile is what a file line says of its block's file: the
// file's ID, the name the line gives and its caption. What a line doesn't
// say — the file's size, type and digest, the block's size, alignment and
// the rest — it leaves out, never zero: a change merged onto a stored block
// keeps them as they are, and the graph's one check gives a block the file's
// own (content_graph_given_body_files.go).
type notionMarkdownPlacedFile struct {
	FileID  string          `json:"fileId"`
	Name    string          `json:"name,omitempty"`
	Caption []graphTextSpan `json:"caption,omitempty"`
}

// notionMarkdownFileOf names the file a line's placement is.
type notionMarkdownFileOf func(app core.App, placement notionMarkdownFilePlacement) (string, error)

// notionMarkdownFileAsRead names a base's file as the read named it: a base
// is the writer's observation, never written, so it takes no upload and is
// never refused.
func notionMarkdownFileAsRead(_ core.App, placement notionMarkdownFilePlacement) (string, error) {
	return placement.FileID, nil
}

// place makes each stored file a Markdown line names that block's file, and
// a callout's icon its uploaded picture, in the block's content and in the
// base it changes from. It holds no rule of its own: the graph's one check
// takes each as it takes every block's file, whole in the workspace and of
// the block's kind, the block then taking the file's name, size and type. A
// block the Markdown makes — one with no base — takes the size a block new
// through the API takes (sizeNewNotionMarkdownBlock); before (2026-11-27) an
// upload placed so took none, drawn as one uploaded in the editor.
func (files notionMarkdownFiles) place(app core.App, operations []graphOperation) error {
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil {
			continue
		}
		block := operation.Block
		unchanged := len(block.BaseContent) != 0 && bytes.Equal(block.BaseContent, block.Content)
		brought := notionMarkdownNamesFile(block.Content)
		var err error
		if block.Content, err = files.placed(app, block.Type, block.Content, files.storedFileID); err != nil {
			return err
		}
		if len(block.BaseContent) == 0 {
			if block.Content, err = sizeNewNotionMarkdownBlock(block.Type, block.Content, brought); err != nil {
				return err
			}
		}
		switch {
		case unchanged:
			block.BaseContent = block.Content
		case len(block.BaseContent) != 0:
			if block.BaseContent, err = files.placed(app, block.Type, block.BaseContent, notionMarkdownFileAsRead); err != nil {
				return err
			}
		}
	}
	return nil
}

// placed is a block's content with the stored files it names placed, each
// named by fileOf.
func (files notionMarkdownFiles) placed(app core.App, blockType string, content json.RawMessage, fileOf notionMarkdownFileOf) (json.RawMessage, error) {
	// A callout's icon names its picture as a file line names its file.
	content, err := files.placedIcon(app, blockType, content, fileOf)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(content, &fields) != nil || fields["markdownFile"] == nil {
		return content, nil
	}
	request := notionMarkdownFileRequest{}
	if json.Unmarshal(content, &request) != nil {
		return nil, notionMarkdownFileUnavailable()
	}
	fileID, err := fileOf(app, request.File)
	if err != nil {
		return nil, err
	}
	return json.Marshal(notionMarkdownPlacedFile{FileID: fileID, Name: request.File.Name, Caption: request.Caption})
}

// notionMarkdownNamesFile says whether a block's content holds a stored file
// a Markdown line names, until place puts it.
func notionMarkdownNamesFile(content json.RawMessage) bool {
	fields := map[string]json.RawMessage{}
	return json.Unmarshal(content, &fields) == nil && fields["markdownFile"] != nil
}
