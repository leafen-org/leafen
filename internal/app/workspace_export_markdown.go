package app

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const workspaceExportNotionMarkdownOmittedOverflowMarker = `<unknown alt="additional omitted blocks"/>`

type workspaceExportMarkdownBlock struct {
	ID       string
	ParentID string
	Position string
	Type     string
	Content  map[string]any
	// Spans are the block's formatted text, when a page's Markdown writes it.
	Spans []graphTextSpan
}

// writeWorkspaceExportMarkdown writes one page — or one block and what is
// under it — as a Markdown file: "page_markdown" as Notion's Markdown export
// writes a page (writeWorkspaceExportPageMarkdown), and "notion_markdown" as
// Notion's page Markdown (the page Markdown its API answers,
// notion_page_markdown.go).
func writeWorkspaceExportMarkdown(app core.App, output io.Writer, job storedJob, payload workspaceExportJobPayload) error {
	if payload.Format != workspaceExportFormatNotionMarkdown {
		return writeWorkspaceExportPageMarkdown(app, output, job, payload)
	}
	pageID := ""
	blocks := []workspaceExportMarkdownBlock{}
	blockIndexesByPage := map[string][]int{}
	err := walkWorkspaceExportChunks(app, job, func(chunk workspaceExportInventoryChunk) error {
		for _, item := range chunk.Items {
			switch item.Kind {
			case workspaceExportItemYgo:
				// Block text lives in the page's Ygo document, captured after
				// the page's blocks.
				documentPageID, _ := item.Value["pageId"].(string)
				stateHex, _ := item.Value["stateHex"].(string)
				state, err := hex.DecodeString(stateHex)
				if err != nil || len(state) == 0 || len(blockIndexesByPage[documentPageID]) == 0 {
					continue
				}
				document := crdt.New()
				if err := crdt.ApplyUpdateV1(document, state, nil); err != nil {
					return err
				}
				for _, index := range blockIndexesByPage[documentPageID] {
					// Notion's page Markdown keeps suggested text, as its API does.
					spans, text := rowPageBlockTextAsItStands(document, blocks[index].ID, true)
					if text != "" || len(spans) > 0 {
						blocks[index].Content["text"], blocks[index].Spans = text, spans
					}
				}
			case workspaceExportItemBlock:
				block := workspaceExportMarkdownBlock{Content: map[string]any{}}
				block.ID, _ = item.Value["id"].(string)
				block.ParentID, _ = item.Value["parentId"].(string)
				block.Position, _ = item.Value["position"].(string)
				block.Type, _ = item.Value["type"].(string)
				if content, ok := item.Value["content"].(map[string]any); ok {
					block.Content = content
				}
				// The export shows the page as it stands, without suggested blocks.
				if suggestionID, _ := block.Content[graphBlockSuggestionKey].(string); suggestionID != "" {
					continue
				}
				blockPageID, _ := item.Value["pageId"].(string)
				if pageID == "" {
					pageID = blockPageID
				}
				blockIndexesByPage[blockPageID] = append(blockIndexesByPage[blockPageID], len(blocks))
				blocks = append(blocks, block)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for index := range blocks {
		if len(blocks[index].Spans) == 0 {
			blocks[index].Spans = workspaceExportMarkdownRichSpans(blocks[index].Content)
		}
	}
	if payload.Scope.Kind == workspaceExportScopePage {
		pageID = payload.Scope.TargetID
	}
	if payload.Scope.Kind == workspaceExportScopeBlockSubtree {
		subtree, ok := workspaceExportMarkdownSubtree(blocks, payload.Scope.TargetID)
		if !ok {
			return newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
		}
		blocks = subtree
	}
	return writeWorkspaceExportNotionMarkdown(app, output, job, payload, pageID, blocks)
}

// writeWorkspaceExportPageMarkdown writes a page as its file in the Markdown
// & CSV export — one block Markdown rule for both (renderer.pageMarkdown):
// its title, a database page's properties, then its blocks, tables, pictures
// and files among them. A single file holds no other page and no file, so a
// page it links to, a sub-page or a mention, is linked at this server's
// address (renderer.link), and a picture or file uploaded to the page is
// named; a link a block keeps stays a link. It ends with a line end.
func writeWorkspaceExportPageMarkdown(app core.App, output io.Writer, job storedJob, payload workspaceExportJobPayload) error {
	document, err := collectWorkspaceExportDocument(app, job, payload)
	if err != nil {
		return err
	}
	document.Payload.Options.IncludeFiles = false
	writer := &workspaceExportDocumentWriter{app: app, job: job, document: document, mode: workspaceExportModeMarkdown,
		files: newWorkspaceExportFilePlacer(), articles: map[string]string{}}
	if err := writer.walkText(func(string) error { return nil }, nil); err != nil {
		return err
	}
	page := document.Pages[payload.Scope.TargetID]
	if page == nil {
		page = &workspaceExportDocPage{ID: payload.Scope.TargetID}
	}
	text := writer.renderer("", "").pageMarkdown(page)
	if err := document.mentionFailure(); err != nil {
		return err
	}
	_, err = io.WriteString(output, text+"\n")
	return err
}

// workspaceExportMarkdownSubtree is the block at rootID and what is under
// it, the block taking the top.
func workspaceExportMarkdownSubtree(blocks []workspaceExportMarkdownBlock, rootID string) ([]workspaceExportMarkdownBlock, bool) {
	kept := map[string]bool{rootID: true}
	found := false
	for changed := true; changed; {
		changed = false
		for _, block := range blocks {
			if !kept[block.ID] && kept[block.ParentID] {
				kept[block.ID], changed = true, true
			}
		}
	}
	subtree := []workspaceExportMarkdownBlock{}
	for _, block := range blocks {
		if !kept[block.ID] {
			continue
		}
		if block.ID == rootID {
			block.ParentID, found = "", true
		}
		subtree = append(subtree, block)
	}
	return subtree, found
}

// workspaceExportMarkdownRichSpans is the formatted text a block keeps with
// itself — a block written through the content graph (an import, an app,
// an agent) before its page's text document holds it.
func workspaceExportMarkdownRichSpans(content map[string]any) []graphTextSpan {
	raw, ok := content["rich"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	encoded, err := json.Marshal(raw)
	spans := []graphTextSpan{}
	if err != nil || json.Unmarshal(encoded, &spans) != nil {
		return nil
	}
	return spans
}

func workspaceExportRichText(value any) string {
	spans, ok := value.([]any)
	if !ok {
		return ""
	}
	var result strings.Builder
	for _, raw := range spans {
		span, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text, _ := span["text"].(string)
		if text == "" {
			text, _ = span["plainText"].(string)
		}
		result.WriteString(text)
	}
	return result.String()
}

func workspaceExportJSONText(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
