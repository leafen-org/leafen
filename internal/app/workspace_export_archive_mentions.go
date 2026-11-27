package app

import (
	"encoding/hex"

	"github.com/reearth/ygo/crdt"
)

// A Leafen archive — a workspace's or a page's, the copy made to bring it
// back — holds what the person exporting may read, as every other export
// does: a page or database mention they can't open is written as their app
// reads it, its title's length in no-access or not-found marks
// (collaboration_reader_mentions.go), in a page's text document, a block's
// own text, captions and table cells, a trashed block's, a database value,
// a database's title and description, a comment and a template; and a page
// link or sub-page block naming a page closed to them keeps neither its
// title nor its icon (graphBlockAsRead). Brought back, such a mention reads
// "사용 권한 없음" or "찾을 수 없음". The marks reach the archive alone: the
// reading's own keys ("unavailable", "pageLinkUnavailable") stay out of it,
// as the app's writes leave them out. An owner's archive of their whole
// workspace keeps every title: each page there opens to them, in the trash
// too, and a mention of a page nothing answers to reads "찾을 수 없음"
// there already.

// mentionsAsRead puts an archive item's mentions as its exporter reads them.
func (access *workspaceExportArchiveAccess) mentionsAsRead(item workspaceExportInventoryItem) error {
	closer := access.closes
	value := item.Value
	switch item.Kind {
	case workspaceExportItemBlock, workspaceExportItemRowPageBlock:
		archiveBlockAsRead(value, closer)
	case workspaceExportItemBlockTrash, workspaceExportItemRowBlockTrash:
		snapshot, _ := value["snapshot"].(map[string]any)
		blocks, _ := snapshot["blocks"].([]any)
		for _, raw := range blocks {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			archiveBlockAsRead(map[string]any{"type": block["type"], "pageId": value["pageId"], "content": block["content"]}, closer)
			deltas, _ := block["text"].([]any)
			for _, raw := range deltas {
				if delta, ok := raw.(map[string]any); ok {
					archiveDeltaAsRead(delta, closer)
				}
			}
		}
	case workspaceExportItemRow:
		values, _ := value["values"].(map[string]any)
		for _, raw := range values {
			if cell, ok := raw.(map[string]any); ok {
				graphCellAsRead(cell, closer)
				archiveSpansWithoutReading(cell["spans"])
			}
		}
	case workspaceExportItemPage:
		// An ordinary page's title, with its spans (page_title_spans.go); a
		// full-page database's, as its name reads (page_name_mentions.go).
		if archiveSpansAsRead(value["titleSpans"], closer) {
			value["title"] = graphSpansWords(value["titleSpans"].([]any))
		} else if mapString(value, "kind") == pageKindDatabase {
			value["title"] = pageTitleAsReadBy(access.app, closer, mapString(value, "id"), mapString(value, "title"))
		}
	case workspaceExportItemComment:
		if body, ok := value["body"].(map[string]any); ok {
			archiveSpansAsRead(body["rich"], closer)
		}
	case workspaceExportItemDataSource:
		for words, field := range map[string]string{"title": "titleSpans", "description": "descriptionSpans"} {
			if archiveSpansAsRead(value[field], closer) {
				value[words] = graphSpansWords(value[field].([]any))
			}
		}
	case workspaceExportItemTemplate:
		archiveTemplateAsRead(value, closer)
	case workspaceExportItemYgo:
		stateHex, err := archiveDocumentAsRead(mapString(value, "stateHex"), closer())
		if err != nil {
			return err
		}
		if stateHex != "" {
			value["stateHex"] = stateHex
		}
	}
	return nil
}

// archiveBlockAsRead is graphBlockAsRead for an archive, without the
// reading's own keys.
func archiveBlockAsRead(block map[string]any, closer func() searchMentionCloser) {
	graphBlockAsRead(block, closer)
	content, ok := block["content"].(map[string]any)
	if !ok {
		return
	}
	delete(content, publicPageLinkUnavailableKey)
	for _, field := range graphReaderSpanFields {
		archiveSpansWithoutReading(content[field])
	}
	if cells, ok := content["cells"].([]any); ok {
		for _, cell := range cells {
			archiveSpansWithoutReading(cell)
		}
	}
}

// archiveSpansAsRead marks spans' closed mentions, reporting whether any was.
func archiveSpansAsRead(value any, closer func() searchMentionCloser) bool {
	spans, ok := value.([]any)
	if !ok || !graphSpansAsRead(spans, closer) {
		return false
	}
	archiveSpansWithoutReading(spans)
	return true
}

func archiveSpansWithoutReading(value any) {
	spans, _ := value.([]any)
	for _, raw := range spans {
		if span, ok := raw.(map[string]any); ok {
			delete(span, publicMentionUnavailableKey)
		}
	}
}

// archiveDeltaAsRead is a trashed block's text insert, a crdt.Delta as JSON
// writes it, as its exporter reads it.
func archiveDeltaAsRead(delta map[string]any, closer func() searchMentionCloser) {
	text, ok := delta["Insert"].(string)
	attributes, _ := delta["Attributes"].(map[string]any)
	if !ok || attributes == nil {
		return
	}
	reach := searchMentionReach(closer(), mapString(attributes, "mention"), mapString(attributes, "pageId"), mapString(attributes, "databaseId"))
	if reach != mentionOpens {
		delta["Insert"] = unnamedMentionWords(reach, text, true)
	}
}

// archiveTemplateAsRead reads a database template's values and body: a
// title or text value's words again from its spans when one was closed.
func archiveTemplateAsRead(template map[string]any, closer func() searchMentionCloser) {
	properties, _ := template["properties"].(map[string]any)
	spans, _ := template["propertySpans"].(map[string]any)
	for propertyID, raw := range spans {
		if !archiveSpansAsRead(raw, closer) {
			continue
		}
		words := graphSpansWords(raw.([]any))
		if _, plain := properties[propertyID].(string); plain {
			if properties[propertyID] == template["title"] {
				template["title"] = words
			}
			properties[propertyID] = words
		}
	}
	blocks, _ := template["blocks"].([]any)
	for _, raw := range blocks {
		if block, ok := raw.(map[string]any); ok {
			archiveBlockAsRead(map[string]any{"type": block["type"], "pageId": template["pageId"], "content": block["content"]}, closer)
		}
	}
}

// archiveDocumentAsRead is a page's text document, as a V1 update in hex,
// with its closed mentions in marks: "" for one that holds none, which goes
// as it is. A mention keeps its place and length, so the document's
// positions are the page's.
func archiveDocumentAsRead(stateHex string, closes searchMentionCloser) (string, error) {
	state, err := hex.DecodeString(stateHex)
	if err != nil || len(state) == 0 {
		return "", nil
	}
	closed := false
	for target := range collaborationMentionTargets(state) {
		if closes(target[0], target[1]) != mentionOpens {
			closed = true
			break
		}
	}
	if !closed {
		return "", nil
	}
	doc := crdt.New()
	if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
		return "", err
	}
	rewritten, err := crdt.RewriteTextRunsV1(doc, state, readerTextRun(closes))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(rewritten), nil
}
