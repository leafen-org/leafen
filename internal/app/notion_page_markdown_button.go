package app

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"

	"github.com/reearth/ygo/crdt"
)

// A page's Markdown writes a button as the screen shows it, its label, and
// names the button by its address on the page, as an unknown block's line
// names its block: <button url="…/p/〈page〉#〈block〉">Label</button>. Every
// form writes it so — the MCP's fetch and leafen_fetch, the public API's "v1"
// and "v2", and Leafen's own Markdown — and every write reads it back: as
// fetch gave it, it is the button it was, kept in its place or moved with its
// line; with another label, the same button renamed, keeping what it runs;
// without url, a new button of that label, which runs nothing until it is
// given steps. A url naming no button of the page the write keeps is refused.
// What the button runs is no line's: its steps are the block's
// buttonActionDocument, read and changed through the native API's content
// graph and run by pressing it there (leafen_operations), as a person sets
// them in the button's settings; the blocks those settings hold for it to
// insert stay with it and are no line of the page either.
//
// Notion's MCP and public API Markdown write a button as unknown,
// <unknown url="…#block" alt="button" />, its label nowhere (its block reads
// as "unsupported" too), and read <button> as text (public API, "v1" and
// "v2", 2026-11-27); so an agent could neither read what a button says nor
// make one, where a person sees its label and makes one from the "/" menu.
// Agents do through Markdown and the API what a person does on the screen,
// so this differs from Notion on purpose.

var notionMarkdownButtonPattern = regexp.MustCompile(
	`^<button((?:[ \t]+[a-z-]+="[^"]*")*)[ \t]*(?:/>|>(.*)</button>)$`)

// notionMarkdownButtonAddressKey holds a button line's address until the
// write finds the button it names (notionMarkdownButtonWrites).
const notionMarkdownButtonAddressKey = "markdownButtonAddress"

// ownLine is a link's or a button's line in its reader's form, false for a
// link naming no page or database it can write.
func (w *notionMarkdownWriter) ownLine(block workspaceExportMarkdownBlock) (string, bool) {
	if block.Type == graphBlockTypeButton {
		return w.button(block), true
	}
	return w.linkToPage(block)
}

// button is a button's line in its reader's form. What the button holds —
// the blocks its settings keep for it to insert — is no line of the page.
func (w *notionMarkdownWriter) button(block workspaceExportMarkdownBlock) string {
	for _, held := range w.children[block.ID] {
		w.visited[held.ID] = true
	}
	escaped, closing := notionMarkdownAttributeEscaped.Replace, " />"
	if w.mentions.APIForm && !w.mentions.Version2 {
		escaped, closing = notionMarkdownAPIAttributeEscaped.Replace, "/>"
	}
	opening := `<button url="` + escaped(w.unknownAddress(w.blockName(block.ID))) + `"`
	label := mapString(block.Content, "label")
	if label == "" {
		return opening + closing
	}
	return opening + ">" + notionMarkdownEscaped.Replace(label) + "</button>"
}

// notionMarkdownButtonLine reads a button's line: its label, and the address
// naming the button it was, until the write finds that button.
func notionMarkdownButtonLine(trimmed string) (string, json.RawMessage, bool) {
	match := notionMarkdownButtonPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return "", nil, false
	}
	content := map[string]string{"label": strings.TrimSpace(unescapeNotionMarkdown(match[2]))}
	if address := strings.TrimSpace(html.UnescapeString(notionMarkdownAttributes(match[1])["url"])); address != "" {
		content[notionMarkdownButtonAddressKey] = address
	}
	encoded, err := json.Marshal(content)
	return graphBlockTypeButton, encoded, err == nil
}

// notionMarkdownButtonNames are the page's buttons its Markdown writes, by
// the name each line's address gives it (notionMarkdownUnknownName).
func notionMarkdownButtonNames(
	blocks []workspaceExportMarkdownBlock, rendered []string, publicBlockID func(string) string,
) map[string]string {
	written := make(map[string]bool, len(rendered))
	for _, blockID := range rendered {
		written[blockID] = true
	}
	names := map[string]string{}
	for _, block := range blocks {
		if block.Type != graphBlockTypeButton || !written[block.ID] {
			continue
		}
		name := publicBlockID(block.ID)
		if name == "" {
			name = block.ID
		}
		names[notionMarkdownUnknownName(name)] = block.ID
	}
	return names
}

// keptButton is the page's button a new line names by its address, which the
// line keeps — moved, renamed or both — when no other line keeps it; "" for
// none.
func (current *notionPageMarkdownProjection) keptButton(item workspaceImportStagedItem, claimed map[string]bool) string {
	if current == nil || item.Type != graphBlockTypeButton {
		return ""
	}
	content := map[string]string{}
	if json.Unmarshal(item.Content, &content) != nil {
		return ""
	}
	blockID := current.Buttons[notionMarkdownUnknownKey(content[notionMarkdownButtonAddressKey])]
	if blockID == "" || claimed[blockID] {
		return ""
	}
	claimed[blockID] = true
	return blockID
}

// notionMarkdownClaimedBlocks are the current blocks the lines that stay, or
// that changed in their places, keep.
func notionMarkdownClaimedBlocks(oldIDs []string, matched, edited []int) map[string]bool {
	claimed := map[string]bool{}
	for index := range matched {
		for _, old := range []int{matched[index], edited[index]} {
			if old >= 0 {
				claimed[oldIDs[old]] = true
			}
		}
	}
	return claimed
}

// notionMarkdownButtonWrites gives each button a write names by its address
// what it held: the steps it runs and the blocks they insert stay, and its
// label, its steps' too, is the line's, as renaming it in its settings does.
// A line naming no button of the page that the write keeps is refused.
func notionMarkdownButtonWrites(doc *crdt.Doc, operations []graphOperation) error {
	var stored map[string]structuralBlockMetadata
	for _, operation := range operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.Type != graphBlockTypeButton {
			continue
		}
		written := map[string]any{}
		if json.Unmarshal(block.Content, &written) != nil {
			continue
		}
		address, addressed := written[notionMarkdownButtonAddressKey].(string)
		if !addressed {
			continue
		}
		if stored == nil {
			var err error
			if stored, err = structuralMetadata(doc); err != nil {
				return err
			}
		}
		current, found := stored[block.ID]
		if !found || current.Type != graphBlockTypeButton {
			return graphInputError(errorCodeGraphReferenceInvalid, "<button url=\"…\"> names no button of this page that "+
				"this write keeps: "+address+". Write a button's line once, with the url fetch gave it; leave url out to "+
				"make a new button.")
		}
		content := make(map[string]any, len(current.Content))
		for key, value := range current.Content {
			content[key] = value
		}
		label, _ := written["label"].(string)
		content["label"] = label
		if steps, ok := content["buttonActionDocument"].(map[string]any); ok {
			renamed := make(map[string]any, len(steps))
			for key, value := range steps {
				renamed[key] = value
			}
			renamed["label"] = strings.TrimSpace(label)
			content["buttonActionDocument"] = renamed
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		block.Content = encoded
	}
	return nil
}
