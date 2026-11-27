package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// An option's name holds no comma, as Notion's: its API refuses one in a
// select's, multi-select's or status's option it makes, naming the property
// and the name (measured 2026-11-26: a new database's, a data source's and a
// page value's new option alike, 400 validation_error), and its screen
// leaves the comma out as it is typed (2026-11-26, Chrome: "Errands,
// groceries" was offered as "Errands groceries", ", garden" typed into a
// name kept "Home garden"). Leafen refuses a new name with one wherever a
// person or an agent makes or renames an option — its screen's fields leave
// the comma out first (optionNameText.ts) — saying the column, the option and
// the character, so the agent changes what it sent. A name an option already
// has stays as it is, comma and all (made before this rule, or brought in by
// an import): it is chosen, written and read as any other. A new option is
// also refused, saying which and why, without an ID or a name, in a color
// that isn't one, or twice in one column: one such option a column held made
// every later write to the column fail as the server's own failure
// (2026-11-26, an agent tidying 200 rows through MCP was told only to try
// again, and found the cause by taking options out one at a time).

// The codes a refused option goes out with, so the screen says its cause in
// the reader's words (web productErrorOptionMeaning.ts): a new name with a
// comma, a name the column has, and options pages hold left out
// (database_option_removal.go). Any other fault of a new option is the
// content change's own (LEAFEN_GRAPH_INVALID).
const (
	errorCodeDatabaseOptionNameComma = "LEAFEN_DATABASE_OPTION_NAME_COMMA"
	errorCodeDatabaseOptionNameTaken = "LEAFEN_DATABASE_OPTION_NAME_TAKEN"
	errorCodeDatabaseOptionsInUse    = "LEAFEN_DATABASE_OPTIONS_IN_USE"
)

// databaseOptionCommaReason says why a column's option can't be named so: it
// holds a comma. "" for a name that can be.
func databaseOptionCommaReason(column, name string) string {
	if !strings.Contains(name, ",") {
		return ""
	}
	return fmt.Sprintf("column %s: option %s has a comma (,), which an option name can't hold — "+
		"take it out (%s) or split the name into options", strconvQuoteJS(column), strconvQuoteJS(name),
		strconvQuoteJS(databaseOptionNameWithoutCommas(name)))
}

// databaseOptionNameWithoutCommas is a name as Notion's screen keeps it when
// it is typed with commas: without them, its spaces as they were but for
// runs of them, which become one.
func databaseOptionNameWithoutCommas(name string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(name, ",", "")), " ")
}

// databaseNewOptionsReason says why the options a column's settings (next)
// give it can't be, against the ones it holds (stored, "" for a new
// column): a new name with a comma, or a new option without an ID or a name,
// in a color that isn't one, or named twice; with the code it goes out with.
// "" when they can.
func databaseNewOptionsReason(column string, stored, next json.RawMessage) (string, string) {
	type option struct {
		ID    any `json:"id"`
		Name  any `json:"name"`
		Color any `json:"color"`
	}
	read := func(raw json.RawMessage) []option {
		config := struct {
			Options []option `json:"options"`
		}{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &config)
		}
		return config.Options
	}
	text := func(value any) string {
		held, _ := value.(string)
		return strings.TrimSpace(held)
	}
	// An option kept as it was — its ID and name, an old one's comma or
	// missing ID too — is the column's own, never refused for being there.
	heldNames, heldColors := map[string]bool{}, map[string]string{}
	pair := func(given option) string { return text(given.ID) + "\x00" + text(given.Name) }
	for _, held := range read(stored) {
		heldNames[text(held.Name)] = true
		heldColors[pair(held)] = fmt.Sprint(held.Color)
	}
	// The names and IDs the options kept as they were take, in any case.
	options := read(next)
	ids, names := map[string]bool{}, map[string]bool{}
	for _, given := range options {
		if _, held := heldColors[pair(given)]; held {
			ids[text(given.ID)], names[strings.ToLower(text(given.Name))] = true, true
		}
	}
	for index, given := range options {
		id, name := text(given.ID), text(given.Name)
		heldColor, held := heldColors[pair(given)]
		switch {
		case held:
		case id == "":
			return errorCodeGraphInvalid, fmt.Sprintf("column %s: option %d has no id — each option has its own, as the column's settings give it", strconvQuoteJS(column), index+1)
		case name == "":
			return errorCodeGraphInvalid, fmt.Sprintf("column %s: option %s has no name", strconvQuoteJS(column), strconvQuoteJS(id))
		case ids[id]:
			return errorCodeGraphInvalid, fmt.Sprintf("column %s: option id %s is there twice", strconvQuoteJS(column), strconvQuoteJS(id))
		case names[strings.ToLower(name)]:
			return errorCodeDatabaseOptionNameTaken, fmt.Sprintf("column %s: option %s is there twice — an option's name is its own", strconvQuoteJS(column), strconvQuoteJS(name))
		case !heldNames[name] && databaseOptionCommaReason(column, name) != "":
			return errorCodeDatabaseOptionNameComma, databaseOptionCommaReason(column, name)
		}
		ids[id], names[strings.ToLower(name)] = true, true
		if given.Color == nil || held && fmt.Sprint(given.Color) == heldColor {
			continue
		}
		if color, colored := given.Color.(string); !colored || !knownDatabaseOptionColor(color) {
			return errorCodeGraphInvalid, fmt.Sprintf("column %s: option %s has color %s, which isn't one — default, gray, brown, orange, yellow, green, "+
				"blue, purple, pink or red", strconvQuoteJS(column), strconvQuoteJS(name), strconvQuoteJS(fmt.Sprint(given.Color)))
		}
	}
	return "", ""
}

// knownDatabaseOptionColor is whether an option's color is one of the ten.
func knownDatabaseOptionColor(color string) bool {
	_, known := notionDatabaseRowPageOptionColors[strings.TrimSpace(color)]
	return known
}

// databaseOptionNamesIn are the names a comma-separated list gives: its
// pieces, trimmed, a run of them that together is a name held (an older
// option's, comma and all) taken as that name.
func databaseOptionNamesIn(text string, held func(string) bool) []string {
	parts := strings.Split(text, ",")
	names := make([]string, 0, len(parts))
	for start := 0; start < len(parts); {
		end := len(parts)
		for ; end > start+1; end-- {
			if held(strings.TrimSpace(strings.Join(parts[start:end], ","))) {
				break
			}
		}
		names = append(names, strings.TrimSpace(strings.Join(parts[start:end], ",")))
		start = end
	}
	return names
}

// listedNames are the names a CSV cell of the column lists, a multi-select's
// options' names whole (databaseOptionNamesIn).
func (column *workspaceCSVMergeColumn) listedNames(text string) []string {
	if column.property.Type != graphValueTypeMultiSelect {
		return strings.Split(text, ",")
	}
	return databaseOptionNamesIn(text, func(name string) bool {
		for _, option := range column.options {
			if strings.EqualFold(option.Name, name) {
				return true
			}
		}
		return false
	})
}
