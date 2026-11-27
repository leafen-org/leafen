package app

import (
	"bytes"
	"encoding/json"
)

// normalizeGraphBlockTypedContent checks and canonicalizes the content of the
// block types whose content has its own shape: file blocks, PDFs, link
// previews, tables and their rows, inline database containers and apps' blocks.
func normalizeGraphBlockTypedContent(block *graphBlock) error {
	if err := validateGraphCalloutIcon(block); err != nil {
		return err
	}
	if err := validateGraphBlockCaptionSize(block); err != nil {
		return err
	}
	if err := validateGraphTodoTask(block); err != nil {
		return err
	}
	if graphAppEmbed(block) {
		content, err := parseGraphAppEmbedContent(block.Content)
		if err != nil {
			return err
		}
		block.Content, err = json.Marshal(content)
		return err
	} else if graphBlockCarriesManagedFileReference(block) {
		if _, err := parseGraphFileBlockContentOf(block.Content, true); err != nil {
			return err
		}
	} else if caption, name, placeholder := graphFileBlockPlaceholder(block); placeholder {
		// A file or PDF block chosen from the editor, or one whose file is
		// being replaced, waits for its upload or link as an empty block
		// that keeps its caption and caption visibility, and the name a file
		// is given through Notion's API (notionEmptyFileMutation).
		content := map[string]any{}
		if len(caption) != 0 {
			content["caption"] = caption
		}
		if name != "" {
			content["name"] = name
		}
		var visibility struct {
			CaptionVisible *bool `json:"captionVisible"`
		}
		if err := json.Unmarshal(block.Content, &visibility); err != nil {
			return graphInputError(errorCodeGraphInvalid, "Check the file caption visibility.")
		}
		if visibility.CaptionVisible != nil {
			content["captionVisible"] = *visibility.CaptionVisible
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		block.Content = encoded
	} else if block.Type == graphBlockTypeFile {
		content, err := parseGraphExternalFileBlockContent(block.Content)
		if err != nil {
			return err
		}
		block.Content, err = json.Marshal(content)
		if err != nil {
			return err
		}
	} else if block.Type == graphBlockTypePDF {
		content, err := parseGraphExternalPDFBlockContent(block.Content)
		if err != nil {
			return err
		}
		block.Content, err = json.Marshal(content)
		if err != nil {
			return err
		}
	} else if block.Type == graphBlockTypeLinkPreview {
		content, err := parseGraphLinkPreviewBlockContent(block.Content)
		if err != nil {
			return err
		}
		block.Content, err = json.Marshal(content)
		if err != nil {
			return err
		}
	} else if block.Type == graphBlockTypeTable {
		content, err := parseGraphTableBlockContent(block.Content)
		if err != nil {
			return err
		}
		block.Content, err = json.Marshal(content)
		if err != nil {
			return err
		}
	} else if block.Type == graphBlockTypeTableRow {
		content, err := parseGraphTableRowBlockContent(block.Content)
		if err != nil {
			return err
		}
		block.Content, err = json.Marshal(content)
		if err != nil {
			return err
		}
	} else if block.Type == graphBlockTypeInlineDatabase {
		return validateDatabaseContainerBlockShape(block.Content)
	}
	return nil
}

// graphFileBlockPlaceholder reports whether a file or PDF block names no file
// or link yet, such as a paragraph just turned into a file block or one whose
// file is being replaced, and returns the caption it keeps and a file's name.
func graphFileBlockPlaceholder(block *graphBlock) ([]graphTextSpan, string, bool) {
	if block.Type != graphBlockTypeFile && block.Type != graphBlockTypePDF {
		return nil, "", false
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(block.Content, &fields) != nil {
		return nil, "", false
	}
	for _, key := range []string{"url", "source", "fileId"} {
		var text string
		if value, present := fields[key]; present && (json.Unmarshal(value, &text) != nil || text != "") {
			return nil, "", false
		}
	}
	name := ""
	if value, present := fields["name"]; present && (json.Unmarshal(value, &name) != nil ||
		name != "" && block.Type != graphBlockTypeFile || len([]rune(name)) > maxGraphExternalFileNameRunes) {
		return nil, "", false
	}
	caption := []graphTextSpan{}
	if value, present := fields["caption"]; present {
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&caption) != nil || !validGraphBlockCaption(caption) {
			return nil, "", false
		}
	}
	return caption, name, true
}

// validateGraphBlockCaptionSize bounds a caption whatever its block's kind —
// an image's, a video's, an embed's, a bookmark's or a code block's as a
// file's — to a rich text's size (graphRichTextFits). Only its size is
// checked here; a file's or a PDF's caption is checked whole with its content.
func validateGraphBlockCaptionSize(block *graphBlock) error {
	if !bytes.Contains(block.Content, []byte(`"caption"`)) {
		return nil
	}
	var fields struct {
		Caption json.RawMessage `json:"caption"`
	}
	if json.Unmarshal(block.Content, &fields) != nil || len(fields.Caption) == 0 || string(fields.Caption) == "null" {
		return nil
	}
	// One that doesn't read as spans is its kind's own to refuse.
	caption := []graphTextSpan{}
	if json.Unmarshal(fields.Caption, &caption) == nil && !graphRichTextFits(caption, maxGraphTextCharacters) {
		return graphInputError(errorCodeGraphInvalid, "Check the block caption.")
	}
	return nil
}
