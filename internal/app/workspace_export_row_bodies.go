package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// An archive keeps a database page's body as row page blocks with their text
// in their content, as archives always have. The body now lives as a page
// body, so the text comes from the row page's document. Its trashed blocks
// are page block trash on the database page and go into an archive as a
// page's do (rowBlockTrash with the blockTrash snapshot: blocks with their
// places and text, and the views their databases own), with the database
// page's row; archives before version 13 wrote them back in the row form a
// database page's body had once, without those views, and a database in
// one failed the archive's import (2026-11-17,
// workspace_import_archive_row_trash.go).

func fillWorkspaceExportRowPageBlockText(app core.App, value map[string]any, documents map[string]*crdt.Doc) error {
	rowID, _ := value["rowId"].(string)
	blockID, _ := value["id"].(string)
	content, ok := value["content"].(map[string]any)
	if rowID == "" || blockID == "" || !ok {
		return errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	document, loaded := documents[rowID]
	if !loaded {
		var err error
		if document, err = loadRowPageTextDocument(app, rowID); err != nil {
			return err
		}
		documents[rowID] = document
	}
	if spans, plain := rowPageBlockTextAsItStands(document, blockID, false); len(spans) > 0 {
		content["notionRichTextSpans"], content["text"] = spans, plain
	}
	return nil
}
