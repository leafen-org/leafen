package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's CREATE TABLE lets a column say more after its type (seen in its
// MCP's create-database, 2026-10-07): a select's or multi-select's options
// as ('name':color, …), a number's FORMAT 'dollar' and any column's
// COMMENT 'description'. Leafen reads those; a modifier it doesn't know
// still fails the schema rather than being dropped.

const mcpDatabaseSchemaDescriptionMaxRunes = 2000

// parseMCPDatabaseSchemaModifiers reads what follows a column's type up to
// the next comma or the end.
func parseMCPDatabaseSchemaModifiers(body string, offset int, property *mcpDatabaseSchemaProperty) (int, bool) {
	offset = skipMCPDatabaseSchemaSpace(body, offset)
	if offset < len(body) && body[offset] == '(' {
		var ok bool
		switch property.Type {
		case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
			offset, ok = parseMCPDatabaseSchemaOptions(body, offset+1, property)
		case graphValueTypeFormula, graphValueTypeRelation, graphValueTypeRollup:
			offset, ok = parseMCPDatabaseSchemaArguments(body, offset+1, property)
		}
		if !ok {
			return offset, false
		}
	} else if mcpDatabaseSchemaArgumentTypes[property.Type] {
		return offset, false
	}
	for {
		offset = skipMCPDatabaseSchemaSpace(body, offset)
		if offset == len(body) || body[offset] == ',' {
			return offset, true
		}
		start := offset
		for offset < len(body) && isMCPDatabaseSchemaTypeByte(body[offset]) {
			offset++
		}
		word := strings.ToUpper(body[start:offset])
		offset = skipMCPDatabaseSchemaSpace(body, offset)
		text, next, ok := readMCPDatabaseSchemaQuoted(body, offset)
		if !ok {
			return offset, false
		}
		offset = next
		switch {
		case word == "FORMAT" && property.Type == graphValueTypeNumber && property.NumberFormat == "":
			format, supported := notionImportNumberFormats[text]
			if !supported {
				return offset, false
			}
			property.NumberFormat = format
		case word == "PREFIX" && property.Type == graphValueTypeUniqueID && property.IDPrefix == "":
			// Kept in capitals, as Notion keeps it; one Notion refuses is
			// refused, in Leafen's words, as the change is made
			// (guardDatabaseUniqueIDProperties).
			if text == "" {
				return offset, false
			}
			property.IDPrefix = text
			if kept, valid := uniqueIDPrefix(text); valid {
				property.IDPrefix = kept
			}
		case word == "COMMENT" && property.Description == "":
			text = strings.TrimSpace(text)
			if text == "" || utf8.RuneCountInString(text) > mcpDatabaseSchemaDescriptionMaxRunes {
				return offset, false
			}
			property.Description = text
		default:
			return offset, false
		}
	}
}

// mcpDatabaseSchemaModifierForms are how each type that takes more after it
// is written, in a refusal: the type's options and other text in single
// quotes, as Notion's create-database and update-data-source descriptions
// write them (read 2026-11-26: names double-quoted, type options in single
// quotes).
var mcpDatabaseSchemaModifierForms = map[string]string{
	graphValueTypeSelect:      "SELECT('High':red, 'Medium':yellow, 'Low'): options in single quotes, each with a color or not",
	graphValueTypeMultiSelect: "MULTI_SELECT('Eng':blue, 'Design'): options in single quotes, each with a color or not",
	graphValueTypeStatus: "STATUS('Backlog':gray:to_do, 'Building':blue:in_progress, 'Shipped':green:complete): " +
		"options in single quotes, each with a color and a group (to_do, in_progress or complete) or not",
	graphValueTypeNumber:   "NUMBER FORMAT 'dollar'",
	graphValueTypeFormula:  "FORMULA('expression')",
	graphValueTypeRelation: "RELATION('data source ID'), or RELATION('data source ID', DUAL) or RELATION('data source ID', DUAL 'Name') for a two-way one",
	graphValueTypeRollup:   "ROLLUP('relation', 'property', 'function')",
	graphValueTypeUniqueID: "UNIQUE_ID PREFIX 'PR' (2-10 characters: a letter, then letters, digits or hyphens)",
}

// mcpDatabaseSchemaModifierRefusal says how what follows a column's type is
// written, for that type, when parseMCPDatabaseSchemaModifiers stopped at
// failedAt in body: notion-create-database's schema and
// notion-update-data-source's statements say it alike.
func mcpDatabaseSchemaModifierRefusal(column mcpDatabaseSchemaProperty, body string, failedAt int) string {
	lead := "what follows its type didn't read"
	if failedAt >= 0 && failedAt < len(body) && body[failedAt] == '"' {
		lead = "what follows its type goes in single quotes, double quotes being for names"
	}
	form := mcpDatabaseSchemaModifierForms[column.Type]
	if form == "" {
		return fmt.Sprintf("column %q: %s — this type takes only COMMENT 'description' after it", column.Name, lead)
	}
	if column.Type == graphValueTypeSelect || column.Type == graphValueTypeMultiSelect || column.Type == graphValueTypeStatus {
		form += " (colors: default, gray, brown, orange, yellow, green, blue, purple, pink, red)"
	}
	return fmt.Sprintf("column %q: %s — write %s; any column may end with COMMENT 'description'", column.Name, lead, form)
}

// parseMCPDatabaseSchemaOptions reads 'name' or 'name':color, separated by
// commas, up to the closing parenthesis; a status's 'name':color:group names
// its group too, as Notion's does (to_do, in_progress or complete).
func parseMCPDatabaseSchemaOptions(body string, offset int, property *mcpDatabaseSchemaProperty) (int, bool) {
	names := map[string]bool{}
	for {
		offset = skipMCPDatabaseSchemaSpace(body, offset)
		name, next, ok := readMCPDatabaseSchemaQuoted(body, offset)
		name = strings.TrimSpace(name)
		if !ok || name == "" || utf8.RuneCountInString(name) > maxGraphIdentifierLength || names[name] {
			return offset, false
		}
		names[name] = true
		offset = skipMCPDatabaseSchemaSpace(body, next)
		color := "default"
		if offset < len(body) && body[offset] == ':' {
			offset = skipMCPDatabaseSchemaSpace(body, offset+1)
			start := offset
			for offset < len(body) && body[offset] >= 'a' && body[offset] <= 'z' {
				offset++
			}
			color = body[start:offset]
			if _, valid := notionDatabaseRowPageOptionColors[color]; !valid {
				return offset, false
			}
			offset = skipMCPDatabaseSchemaSpace(body, offset)
		}
		group := ""
		if property.Type == graphValueTypeStatus && offset < len(body) && body[offset] == ':' {
			offset = skipMCPDatabaseSchemaSpace(body, offset+1)
			start := offset
			for offset < len(body) && (body[offset] >= 'a' && body[offset] <= 'z' || body[offset] == '_') {
				offset++
			}
			group = body[start:offset]
			if _, valid := notionStatusSchemaGroups[group]; !valid || group == "" {
				return offset, false
			}
			offset = skipMCPDatabaseSchemaSpace(body, offset)
		}
		property.Options = append(property.Options, mcpDatabaseSchemaOption{Name: name, Color: color, Group: group})
		if len(property.Options) > maxNotionDatabaseRowPageOptions || offset == len(body) {
			return offset, false
		}
		switch body[offset] {
		case ',':
			offset++
		case ')':
			return offset + 1, true
		default:
			return offset, false
		}
	}
}

// readMCPDatabaseSchemaQuoted reads a single-quoted string, where two
// quotes in a row stand for one.
func readMCPDatabaseSchemaQuoted(value string, offset int) (string, int, bool) {
	if offset >= len(value) || value[offset] != '\'' {
		return "", offset, false
	}
	offset++
	var text strings.Builder
	for offset < len(value) {
		if value[offset] != '\'' {
			text.WriteByte(value[offset])
			offset++
			continue
		}
		if offset+1 < len(value) && value[offset+1] == '\'' {
			text.WriteByte('\'')
			offset += 2
			continue
		}
		return text.String(), offset + 1, true
	}
	return "", offset, false
}

// mcpDatabaseSchemaConfig is a new column's settings; its options' ids come
// from the column's, so a call made again makes the same ones.
func mcpDatabaseSchemaConfig(propertyID string, property mcpDatabaseSchemaProperty) json.RawMessage {
	config := map[string]any{}
	if len(property.Options) > 0 && property.Type != graphValueTypeStatus {
		options := make([]map[string]string, 0, len(property.Options))
		for index, option := range property.Options {
			digest := sha256.Sum256([]byte(propertyID + "\x00option\x00" + strconv.Itoa(index)))
			options = append(options, map[string]string{"id": hex.EncodeToString(digest[:])[:15], "name": option.Name, "color": option.Color})
		}
		config["options"] = options
	}
	if property.NumberFormat != "" {
		config["numberFormat"] = property.NumberFormat
	}
	if property.Description != "" {
		config["description"] = property.Description
	}
	switch property.Type {
	case graphValueTypeFormula:
		config["formula"] = property.Formula
	case graphValueTypeRelation:
		config["relationDatabaseId"] = property.relationSource
		if property.pairID != "" {
			config["relatedPropertyId"] = property.pairID
			if property.SyncedName != "" {
				config["reciprocalName"] = property.SyncedName
			}
		}
	case graphValueTypeRollup:
		config["rollupRelationPropertyId"], config["rollupTargetPropertyId"], config["rollupFunction"] =
			property.rollupRelationID, property.rollupTargetID, property.RollupFunction
	case graphValueTypeStatus:
		// A status column comes with Notion's three groups and, unless it
		// names its own, Notion's three options; a new row starts on the first.
		config["options"], config["statusGroups"] = notionStatusNewConfig(property.Options, property.translate, func(kind string, index int) string {
			return mcpDatabaseSchemaOptionID(propertyID, kind, index)
		})
	case graphValueTypeUniqueID:
		if property.IDPrefix != "" {
			config["idPrefix"] = property.IDPrefix
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

// mcpDatabaseSchemaOptionID is a status's option's or group's ID, from its
// column's, so a call made again makes the same ones.
func mcpDatabaseSchemaOptionID(propertyID, kind string, index int) string {
	digest := sha256.Sum256([]byte(propertyID + "\x00" + kind + "\x00" + strconv.Itoa(index)))
	return hex.EncodeToString(digest[:])[:15]
}

// mcpPersonWords puts the words a new database starts with in a person's
// language, where Leafen's server has them: the one they chose, or for one
// following their device or not chosen, Korean, as an agent's path carries
// no request of theirs (personLanguageCode).
func mcpPersonWords(app core.App, principalID string) func(string) string {
	same := func(word string) string { return word }
	catalog, err := readDatabaseStarterCatalog()
	if err != nil {
		return same
	}
	words := databaseStarterLanguage(catalog, personLanguageCode(app, principalID, nil))
	return func(word string) string {
		if translated, found := words[word]; found {
			return translated
		}
		return word
	}
}

// newStatusWords puts a new status's three options and groups in the words
// of the person who makes it, as Notion names a status property it makes
// (seen 2026-11-14 through its MCP: 시작 전, 진행 중, 완료), not as its
// Korean starters, which keep "Done" (database_starter_catalog.go).
func newStatusWords(app core.App, actorID string) func(string) string {
	names := generatedNamesIn(generatedNameLanguage(context.Background(), app, actorID)).Status
	words := map[string]string{
		"Not started": names.NotStarted, "In progress": names.InProgress, "Done": names.Done,
		"To-do": names.ToDo, "Complete": names.Complete,
	}
	return func(word string) string {
		if translated := words[word]; translated != "" {
			return translated
		}
		return word
	}
}
