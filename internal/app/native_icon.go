package app

import (
	"errors"
	"regexp"
	"strings"
)

// Built-in icons are stored as iconType "icon" with the value "name:color",
// where name is an internal name from the versioned catalog
// (native_icon_catalog.generated.go). Requests may name an icon by internal
// name or picker tooltip alias, case-insensitively and with spaces,
// underscores and hyphens treated alike; responses return the internal name.
// Colors default to gray.
const (
	nativeIconType         = "icon"
	maxNativeIconNameRunes = 64
)

var (
	nativeIconColors           = map[string]struct{}{}
	nativeIconAliases          = map[string]string{}
	nativeIconSeparatorPattern = regexp.MustCompile(`[\s_-]+`)
	nativeIconNamePattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	errNativeIconInvalid       = errors.New("built-in icon is invalid")
)

func init() {
	for _, color := range nativeIconColorOrder {
		nativeIconColors[color] = struct{}{}
	}
	for name, alias := range nativeIconCatalog {
		if alias != "" {
			nativeIconAliases[alias] = name
		}
	}
}

// nativeIconKey applies the official name matching rule.
func nativeIconKey(raw string) string {
	key := nativeIconSeparatorPattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(raw)), "-")
	return strings.Trim(key, "-")
}

// resolveNativeIconName finds the internal name a request means. Internal
// names win over another icon's tooltip alias.
func resolveNativeIconName(raw string) (string, bool) {
	key := nativeIconKey(raw)
	if _, known := nativeIconCatalog[key]; known {
		return key, true
	}
	name, known := nativeIconAliases[key]
	return name, known
}

// nativeIconRequestValue validates an icon object's name and optional color
// against the catalog and returns the stored value.
func nativeIconRequestValue(rawName string, rawColor *string) (string, error) {
	name, ok := resolveNativeIconName(rawName)
	if !ok {
		return "", errNativeIconInvalid
	}
	color := nativeIconDefaultColor
	if rawColor != nil {
		color = strings.TrimSpace(*rawColor)
		if _, valid := nativeIconColors[color]; !valid {
			return "", errNativeIconInvalid
		}
	}
	return name + ":" + color, nil
}

// Notion names a built-in icon and its color, pizza_blue, in its icon set's
// image URL, https://www.notion.so/icons/pizza_blue.svg, and as the
// identifier its MCP fetch gives, icons/pizza_blue.
var (
	notionIconURL        = regexp.MustCompile(`^https://(?:www\.)?notion\.so/icons/([^/?#]+)\.svg$`)
	notionIconIdentifier = regexp.MustCompile(`^icons/([^/?#.]+)$`)
)

// nativeIconFromNotionURL reads an image URL of Notion's icon set as the
// built-in icon's stored value, as Notion sets that icon from it; notion is
// false for any other URL, and err names an icon or color the set doesn't
// have.
func nativeIconFromNotionURL(url string) (value string, notion bool, err error) {
	return nativeIconFromNotionName(notionIconURL, url)
}

// nativeIconFromNotionIdentifier reads a Notion icon identifier as
// nativeIconFromNotionURL reads its URL.
func nativeIconFromNotionIdentifier(identifier string) (value string, notion bool, err error) {
	return nativeIconFromNotionName(notionIconIdentifier, identifier)
}

func nativeIconFromNotionName(pattern *regexp.Regexp, address string) (string, bool, error) {
	match := pattern.FindStringSubmatch(strings.TrimSpace(address))
	if match == nil {
		return "", false, nil
	}
	separator := strings.LastIndex(match[1], "_")
	if separator <= 0 {
		return "", true, errNativeIconInvalid
	}
	color := match[1][separator+1:]
	value, err := nativeIconRequestValue(match[1][:separator], &color)
	return value, true, err
}

// parseNativeIconValue reads a stored built-in icon value. Stored names keep
// the catalog form; the first built-in icon release stored underscores for
// hyphens, so those read as the same internal name. Names outside the current
// catalog still round-trip.
func parseNativeIconValue(value string) (string, string, bool) {
	stored, color, found := strings.Cut(value, ":")
	if !found || len([]rune(stored)) > maxNativeIconNameRunes || stored != strings.ToLower(stored) {
		return "", "", false
	}
	name := strings.ReplaceAll(stored, "_", "-")
	if !nativeIconNamePattern.MatchString(name) {
		return "", "", false
	}
	_, valid := nativeIconColors[color]
	return name, color, valid
}

// validStoredPresentationIcon accepts a non-empty stored presentation icon.
func validStoredPresentationIcon(icon, iconType string) bool {
	switch iconType {
	case "image", "emoji", "custom_emoji":
		return true
	case nativeIconType:
		return validNativeIconValue(icon)
	}
	return false
}

func validNativeIconValue(value string) bool {
	_, _, ok := parseNativeIconValue(value)
	return ok
}

// nativeIconObject is the official icon response for a stored value.
func nativeIconObject(value string) any {
	name, color, ok := parseNativeIconValue(value)
	if !ok {
		return nil
	}
	return map[string]any{"type": nativeIconType, "icon": map[string]any{"name": name, "color": color}}
}
