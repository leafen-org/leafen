package app

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

// An app's block in a page (docs/app-platform.md 2) is an embed whose
// service is "app": which app (its id, the same in every organization that
// installs it), which of its blocks, the settings the app keeps for this
// block, and the name the block had when it was added — what exports and
// Notion's API, which have no app blocks, are left with.

const (
	graphAppEmbedService      = "app"
	graphAppEmbedNameMaxRunes = 200
)

type graphAppEmbedContent struct {
	Service string          `json:"service"`
	App     string          `json:"app"`
	Block   string          `json:"block"`
	Name    string          `json:"name,omitempty"`
	Props   json.RawMessage `json:"props,omitempty"`
	Caption []graphTextSpan `json:"caption,omitempty"`
}

// graphAppEmbed reports whether a block is an app's block.
func graphAppEmbed(block *graphBlock) bool {
	if block.Type != graphBlockTypeEmbed {
		return false
	}
	head := struct {
		Service string `json:"service"`
	}{}
	return json.Unmarshal(block.Content, &head) == nil && head.Service == graphAppEmbedService
}

// parseGraphAppEmbedContent checks an app block's content: a known app id
// and block key, a short name, and settings that are a small JSON object.
func parseGraphAppEmbedContent(raw json.RawMessage) (graphAppEmbedContent, error) {
	content := graphAppEmbedContent{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&content) != nil || !appIDPattern.MatchString(content.App) || len(content.App) > 200 ||
		!appKeyPattern.MatchString(content.Block) || utf8.RuneCountInString(content.Name) > graphAppEmbedNameMaxRunes ||
		(len(content.Caption) != 0 && !validGraphBlockCaption(content.Caption)) {
		return content, graphInputError(errorCodeGraphInvalid, "Check the app block.")
	}
	if len(content.Props) != 0 {
		props := map[string]any{}
		if json.Unmarshal(content.Props, &props) != nil {
			return content, graphInputError(errorCodeGraphInvalid, "An app block's settings are a JSON object.")
		}
		compact := bytes.Buffer{}
		if json.Compact(&compact, content.Props) != nil || compact.Len() > appBlockPropsMaxBytes {
			return content, graphInputError(errorCodeGraphInvalid, "An app block's settings are at most 8 KB.")
		}
		content.Props = compact.Bytes()
	}
	return content, nil
}

// normalizeGraphPropertyAppEditor checks the app a property's cells are
// drawn by, when it names one: the app's id and its editor's key, nothing
// else. The value keeps the property's type, so an app that goes away
// leaves the cells as they were.
func normalizeGraphPropertyAppEditor(config json.RawMessage) error {
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(config, &fields) != nil {
		return nil
	}
	raw, present := fields["appEditor"]
	if !present {
		return nil
	}
	editor := struct {
		App    string `json:"app"`
		Editor string `json:"editor"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&editor) != nil || !appIDPattern.MatchString(editor.App) || len(editor.App) > 200 || !appKeyPattern.MatchString(editor.Editor) {
		return graphInputError(errorCodeGraphInvalid, "Check the app that draws the property.")
	}
	return nil
}
