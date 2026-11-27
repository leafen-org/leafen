package app

import (
	"encoding/json"
	"strings"
)

// A welcome page the server writes in a language read from the right reads
// from the right in every line, a line that starts with a Latin name too
// ("Leafen میں خوش آمدید!", "Notion: ترتیبات → …"), as the language's own
// lines do: each text block is set to that direction, the block menu's
// "텍스트 방향", where it would otherwise find its way from its first letter
// (dir="auto", web/src/lib/textDirection.ts) and read from the left. A page
// a client sends its own words for finds its way from them.

// The languages written from the right, as the web app's
// documentDirectionForLanguage (web/src/i18n/index.ts) knows them.
var rightToLeftLanguageBases = map[string]bool{
	"ar": true, "dv": true, "fa": true, "he": true, "ku": true, "ps": true, "ur": true, "yi": true,
}

// The blocks a direction can be set on, as the web app's
// TEXT_DIRECTION_BLOCK_TYPES (web/src/lib/textDirection.ts).
var welcomeTextDirectionBlockTypes = map[string]bool{
	"paragraph": true, "heading_1": true, "heading_2": true, "heading_3": true, "heading_4": true,
	"bulleted_list_item": true, "numbered_list_item": true, "to_do": true, "toggle": true,
	"toggle_heading_1": true, "toggle_heading_2": true, "toggle_heading_3": true, "toggle_heading_4": true,
	"quote": true, "callout": true,
}

func languageReadsFromRight(tag string) bool {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
	return rightToLeftLanguageBases[base]
}

// workspaceWelcomeReadsFromRight tells whether a welcome page is the one the
// server writes in a language read from the right.
func workspaceWelcomeReadsFromRight(welcome *workspaceWelcomePageInput) bool {
	for language, text := range workspaceWelcomeTexts {
		if text == *welcome {
			return languageReadsFromRight(language)
		}
	}
	return false
}

// setWelcomeLinesFromRight sets each text block the welcome body writes to
// read from the right.
func setWelcomeLinesFromRight(operations []graphOperation) error {
	for _, operation := range operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || !welcomeTextDirectionBlockTypes[block.Type] {
			continue
		}
		content := map[string]any{}
		if len(block.Content) > 0 && string(block.Content) != "null" {
			if err := json.Unmarshal(block.Content, &content); err != nil {
				return err
			}
		}
		content["textDirection"] = "rtl"
		written, err := json.Marshal(content)
		if err != nil {
			return err
		}
		block.Content = written
	}
	return nil
}
