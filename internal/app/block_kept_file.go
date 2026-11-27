package app

import (
	"encoding/json"
	"strings"
)

// A block keeps a stored file when it is a file block naming one
// (graphBlockCarriesManagedFileReference) or a callout whose icon is a picture
// uploaded for it: iconType "image" with the file's ID as its icon, as a page's
// uploaded icon is (2026-11-07 — Notion's callout picker uploads one). The file
// is the block's as a file block's is: the block's reference holds it, block
// trash and page history keep it, and it goes when the last of them does.
// graphBlockKeptFileID is the one place that says which file a block keeps;
// everything that records, trashes, keeps in history or copies a block's file
// reads it, and page history's triggers hold the callout half in SQL
// (leafen_page_history_callout_icon_references_*, from the schema baseline).
const graphCalloutIconTypeImage = "image"

// graphBlockKeptFileID is the stored file a block keeps, or "".
func graphBlockKeptFileID(block *graphBlock) string {
	if block == nil {
		return ""
	}
	if graphBlockCarriesManagedFileReference(block) {
		content, err := parseGraphFileBlockContent(block.Content)
		if err != nil {
			return ""
		}
		return content.FileID
	}
	return graphCalloutIconFileID(block.Type, block.Content)
}

// graphCalloutIconFileID is the uploaded picture a callout keeps as its icon, or "".
func graphCalloutIconFileID(blockType string, content json.RawMessage) string {
	if blockType != graphBlockTypeCallout {
		return ""
	}
	var icon struct {
		Icon     string `json:"icon"`
		IconType string `json:"iconType"`
	}
	if json.Unmarshal(content, &icon) != nil || icon.IconType != graphCalloutIconTypeImage {
		return ""
	}
	if fileID := strings.TrimSpace(icon.Icon); workspaceExportFileRecordIDPattern.MatchString(fileID) {
		return fileID
	}
	return ""
}

// graphCalloutIconFileIDOf is graphCalloutIconFileID over decoded content.
func graphCalloutIconFileIDOf(blockType string, content map[string]any) string {
	if blockType != graphBlockTypeCallout || content["iconType"] != graphCalloutIconTypeImage {
		return ""
	}
	icon, _ := content["icon"].(string)
	if fileID := strings.TrimSpace(icon); workspaceExportFileRecordIDPattern.MatchString(fileID) {
		return fileID
	}
	return ""
}

// validateGraphCalloutIcon refuses a callout whose picture icon names no stored file.
func validateGraphCalloutIcon(block *graphBlock) error {
	if block.Type != graphBlockTypeCallout {
		return nil
	}
	var icon struct {
		IconType string `json:"iconType"`
	}
	if json.Unmarshal(block.Content, &icon) == nil && icon.IconType == graphCalloutIconTypeImage &&
		graphCalloutIconFileID(block.Type, block.Content) == "" {
		return graphInputError(errorCodeGraphInvalid, "Check the callout's icon file.")
	}
	return nil
}
