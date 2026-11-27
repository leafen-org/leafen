package app

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/tools/security"
)

// maxNotionDatabaseRowPageOptions is the most options one Notion API request
// names, Notion's own; a property holds more (Notion's grew to 130 as rows
// named new ones, seen 2026-10-13).
const maxNotionDatabaseRowPageOptions = 100

type notionDatabaseRowPageOption struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Description *string `json:"description,omitempty"`
	// A status option drawn in its group's color has none of its own, which
	// the API calls "default" (notionStatusOptionColorFromGroup).
	ColorFromGroup bool `json:"colorFromGroup,omitempty"`
}

var notionDatabaseRowPageOptionColors = map[string]struct{}{
	"default": {}, "gray": {}, "brown": {}, "orange": {}, "yellow": {},
	"green": {}, "blue": {}, "purple": {}, "pink": {}, "red": {},
}

// notionDatabaseRowPageNewOptionColors are the colors a new option takes,
// one at random, as Notion gives an option the API adds.
var notionDatabaseRowPageNewOptionColors = []string{
	"default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red",
}

// notionDatabaseRowPageOptionValue is the option a select or a status sets
// as the row reads it (databaseValueAsRead): a status that sets none reads
// its default, as Notion's API gives it (seen 2026-11-08: "시작 전" on rows
// never set), and an option the property no longer has is not set.
func notionDatabaseRowPageOptionValue(property storedNotionDatabaseRowPageProperty) (string, error) {
	optionID, err := notionDatabaseRowPageStringValue(property)
	if err != nil {
		return "", err
	}
	stored, err := json.Marshal(optionID)
	if err != nil {
		return "", err
	}
	read := ""
	_ = json.Unmarshal(databaseValueAsRead(normalizeNotionDatabaseRowPagePropertyType(property.Type), property.Config, stored, ""), &read)
	return read, nil
}

// notionDatabaseRowPageOptions are a property's options as its values read
// them. One stored odd — made before its settings were checked
// (database_option_names.go), without an ID or a name, in a color that
// isn't one, or with an ID another has — never fails the property: one that
// can't be named is left out, an odd color reads as "default", and a second
// with an ID is left out, so its other options are still written and read
// (2026-11-26: one such option made every write of its column through MCP
// fail as Leafen's own failure, "try once more").
func notionDatabaseRowPageOptions(
	property storedNotionDatabaseRowPageProperty,
) ([]notionDatabaseRowPageOption, error) {
	config := struct {
		Options []json.RawMessage `json:"options"`
	}{}
	// A property may hold more options than one API request adds (100);
	// reading them never stops at that.
	if json.Unmarshal([]byte(property.Config), &config) != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	options := make([]notionDatabaseRowPageOption, 0, len(config.Options))
	byID := make(map[string]struct{}, len(config.Options))
	for _, raw := range config.Options {
		option := notionDatabaseRowPageOption{}
		if json.Unmarshal(raw, &option) != nil {
			continue
		}
		option.ID = strings.TrimSpace(option.ID)
		option.Name = strings.TrimSpace(option.Name)
		option.Color = strings.TrimSpace(option.Color)
		if _, valid := notionDatabaseRowPageOptionColors[option.Color]; !valid || option.ColorFromGroup {
			option.Color = "default"
		}
		option.ColorFromGroup = false
		if _, duplicate := byID[option.ID]; duplicate || option.ID == "" || option.Name == "" {
			continue
		}
		byID[option.ID] = struct{}{}
		options = append(options, option)
	}
	return options, nil
}

func notionDatabaseRowPageOptionByID(
	options []notionDatabaseRowPageOption,
	id string,
) (notionDatabaseRowPageOption, bool) {
	for _, option := range options {
		if option.ID == id {
			return option, true
		}
	}
	return notionDatabaseRowPageOption{}, false
}

// notionDatabaseRowPageRequestOption finds the option a select, status or
// multi-select value names: by id when it names one, else by name as Notion
// finds it, in any case. A select or multi-select name no option has becomes
// a new option with a random color, as Notion adds one, and joins added; a
// status names an option it has. A missing option is refused naming what
// Notion's API names (seen 2026-11-10), in Leafen's words: the property's raw
// ID and the ID given for an ID, of a status too, and a status's name with
// the name given.
func notionDatabaseRowPageRequestOption(
	property storedNotionDatabaseRowPageProperty,
	raw json.RawMessage,
	added *[]databaseRowPageNewOption,
) (notionDatabaseRowPageOption, error) {
	input := struct {
		ID   *string `json:"id"`
		Name *string `json:"name"`
	}{}
	if json.Unmarshal(raw, &input) != nil || input.ID == nil && input.Name == nil {
		return notionDatabaseRowPageOption{}, notionDatabaseRowPageRequestInvalid(
			"database property %q option must contain exactly one of id or name", property.Name,
		)
	}
	options, err := notionDatabaseRowPageOptions(property)
	if err != nil {
		return notionDatabaseRowPageOption{}, err
	}
	for _, option := range *added {
		options = append(options, notionDatabaseRowPageOption{ID: option.ID, Name: option.Name, Color: option.Color})
	}
	nativeType := normalizeNotionDatabaseRowPagePropertyType(property.Type)
	if input.ID != nil {
		if option, found := notionDatabaseRowPageOptionByID(options, strings.TrimSpace(*input.ID)); found {
			return option, nil
		}
		return notionDatabaseRowPageOption{}, notionDatabaseRowPageRequestInvalid(
			"%s has no select option %s.", notionPropertyRawID(property.publicID()), strconvQuoteJS(*input.ID))
	}
	name := strings.TrimSpace(*input.Name)
	for _, option := range options {
		if option.Name == name {
			return option, nil
		}
	}
	for _, option := range options {
		if strings.EqualFold(option.Name, name) {
			return option, nil
		}
	}
	// A name with a comma is refused before any other fault, as Notion's API
	// refuses it for a select, a multi-select and a status alike (measured
	// 2026-11-26).
	if err := notionOptionNameRefusal(notionDatabaseRowPageOfficialPropertyType(nativeType), property.Name, name); err != nil {
		return notionDatabaseRowPageOption{}, err
	}
	switch {
	case nativeType == graphValueTypeStatus:
		return notionDatabaseRowPageOption{}, notionDatabaseRowPageRequestInvalid(
			"The status has no option %s.", strconvQuoteJS(name))
	case name == "":
		return notionDatabaseRowPageOption{}, notionDatabaseRowPageRequestInvalid("database property %q option does not exist", property.Name)
	case utf8.RuneCountInString(name) > maxDatabaseRowPageNewOptionNameRune:
		return notionDatabaseRowPageOption{}, notionDatabaseRowPageRequestInvalid(
			"database property %q option name is too long", property.Name,
		)
	case len(*added) >= maxDatabaseRowPageNewOptions:
		return notionDatabaseRowPageOption{}, notionDatabaseRowPageRequestInvalid(
			"database property %q has too many options", property.Name,
		)
	}
	option := databaseRowPageNewOption{
		ID: security.RandomString(15), Name: name,
		Color: notionDatabaseRowPageNewOptionColors[rand.IntN(len(notionDatabaseRowPageNewOptionColors))],
	}
	*added = append(*added, option)
	return notionDatabaseRowPageOption{ID: option.ID, Name: option.Name, Color: option.Color}, nil
}

// response is an option as a page's value gives it: Notion's names no
// description there (measured 2026-11-09), only its schema does.
func (option notionDatabaseRowPageOption) response() map[string]any {
	return map[string]any{"id": option.ID, "name": option.Name, "color": option.Color}
}

// notionOptionNameRefusal refuses a new option's name holding a comma, as
// Notion's API does for select, multi-select and status options alike
// (measured 2026-11-09 and 2026-11-26: 400 validation_error naming the
// name), in Leafen's words naming the column too (database_option_names.go);
// nil for any other name.
func notionOptionNameRefusal(kind, column, name string) error {
	if reason := databaseOptionCommaReason(column, name); reason != "" {
		return notionDatabaseRowPageRequestInvalid("This %s option can't be made: %s", kind, reason)
	}
	return nil
}

// strconvQuoteJS is text in double quotes as JavaScript's JSON.stringify
// writes it.
func strconvQuoteJS(text string) string {
	var quoted strings.Builder
	notionJSString(text, &quoted)
	return quoted.String()
}
