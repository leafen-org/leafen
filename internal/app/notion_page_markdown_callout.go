package app

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A callout reads as Leafen wrote it until 2026-10-29: <callout icon="…"
// color="…">, its own text on the next line one level in, the blocks inside
// it on further lines at that level, and </callout>; one on a single line
// holds its text between the tags. One holding its lines writes them as they
// are, its first whatever block it is, and <empty-block/> for an empty first
// line, as Notion's screen callout (seen 2026-11-12). Notion's current form,
// the blocks after a blank line and <p /> for no text, is put so first
// (notion_page_markdown_containers.go) and written (_writer.go). Its icon is
// the emoji, an image's address, a built-in icon's /icons/name_color.svg, a
// custom emoji's :name: or a picture uploaded for it, named as a file line
// names its stored file (notion_page_markdown_icon_files.go); a callout whose
// icon was never set shows and writes the light bulb, and one without an icon
// writes and reads no icon attribute.
// An icon written as none of these is its first character, as Notion reads
// one (seen 2026-11-11, "v1" and "v2"): an emoji as that emoji, any other as
// an image address of that character alone, "ab" as "a", a space as " " and
// a custom emoji's :name: the workspace hasn't as ":". A background color's
// name ends in _bg.

var (
	notionMarkdownCalloutOpen      = regexp.MustCompile(`^<callout((?:\s+[a-z]+="[^"]*")*)\s*>(.*)$`)
	notionMarkdownCalloutAttribute = regexp.MustCompile(`([a-z]+)="([^"]*)"`)
	notionMarkdownBuiltInIconPath  = regexp.MustCompile(`^/icons/([a-z0-9-]+)_([a-z]+)\.svg$`)
	notionMarkdownCalloutEmojiName = regexp.MustCompile(`^:([^:\s]+):$`)
)

const notionMarkdownCalloutClose = "</callout>"

// calloutIconNone marks a callout whose icon was taken away, as Notion's
// icon picker "제거" leaves one (seen 2026-11-05): it shows and writes none.
const calloutIconNone = "none"

// notionPageMarkdownCalloutOpening writes a callout's opening tag.
func notionPageMarkdownCalloutOpening(content map[string]any, mentions notionMarkdownMentions) string {
	opening := "<callout"
	// One without an icon names none, as Notion's (seen 2026-11-12: a
	// callout made on its screen has none and writes <callout
	// color="gray_bg">); one whose icon was never set shows the light bulb.
	if icon := notionMarkdownCalloutIcon(content, mentions); icon != "" && content["iconType"] != calloutIconNone {
		// A stored file's stable address is written as it is, as a file
		// line's source (notionMarkdownFileSource).
		if !strings.HasPrefix(icon, notionMarkdownFileBlockScheme) {
			icon = notionMarkdownIconEscaped(icon, mentions.APIForm)
		}
		opening += ` icon="` + icon + `"`
	}
	if color := notionBlockColor(content); color != "default" && color != "default_background" {
		if strings.HasSuffix(color, "_background") {
			color = strings.TrimSuffix(color, "_background") + "_bg"
		}
		opening += ` color="` + color + `"`
	}
	return opening + ">"
}

// notionMarkdownIconEscaped writes a callout's icon in its attribute: "v1"
// writes it as it is, as Notion's does (seen 2026-11-11: icon="&",
// icon="<", icon="'", an address's &), but for a quotation mark, which would
// end the attribute and leave the callout a line of text to the reading of
// the page's own Markdown; "v2" escapes it as its attributes.
func notionMarkdownIconEscaped(icon string, version1 bool) string {
	if version1 {
		return strings.ReplaceAll(icon, `"`, `&quot;`)
	}
	return notionMarkdownVersion2AttributeEscaped.Replace(icon)
}

// notionMarkdownVersion2AttributeEscaped escapes an icon's or a date's
// attribute as "v2" writes it (seen 2026-11-11: &amp; &lt; &gt; &quot; &#39;).
var notionMarkdownVersion2AttributeEscaped = strings.NewReplacer(
	`&`, `&amp;`, `<`, `&lt;`, `>`, `&gt;`, `"`, `&quot;`, `'`, `&#39;`,
)

// notionMarkdownCalloutIcon is a callout's icon as Markdown names it; an
// icon Markdown can't name, as a deleted custom emoji, is left out. A picture
// uploaded for it is its stored file's address for the reader, the callout's
// block given by mentions.at, which written back keeps it. An image address
// is written as kept, a character alone or a space too.
func notionMarkdownCalloutIcon(content map[string]any, mentions notionMarkdownMentions) string {
	if emojiID, _ := content["customEmojiId"].(string); strings.TrimSpace(emojiID) != "" {
		if mentions.CustomEmoji == nil {
			return ""
		}
		if name, found := mentions.CustomEmoji(strings.TrimSpace(emojiID)); found {
			return ":" + name + ":"
		}
		return ""
	}
	icon, _ := content["icon"].(string)
	if content["iconType"] == notionIconTypeExternal && icon != "" {
		return icon
	}
	icon = strings.TrimSpace(icon)
	switch {
	case icon == "":
		return "💡"
	case content["iconType"] == nativeIconType:
		if name, color, ok := parseNativeIconValue(icon); ok {
			return "/icons/" + name + "_" + color + ".svg"
		}
		return ""
	case content["iconType"] == graphCalloutIconTypeImage:
		// The picture uploaded as its icon (block_kept_file.go).
		if !workspaceExportFileRecordIDPattern.MatchString(icon) {
			return ""
		}
		if mentions.FileAddress != nil {
			return mentions.FileAddress(mentions.TextBlock, icon, "")
		}
		return workspaceImportFileReference + icon
	case strings.Contains(icon, "/") && notionPageExternalFile(icon) == nil:
		return ""
	}
	return icon
}

// notionMarkdownTabIcon writes a tab's icon attribute, none when it has no
// icon. A built-in icon is icons/pizza_blue as a tab's reads, where a
// callout's is its image path (seen 2026-11-05).
func notionMarkdownTabIcon(content map[string]any, mentions notionMarkdownMentions) string {
	emojiID, _ := content["customEmojiId"].(string)
	icon, _ := content["icon"].(string)
	// A space is an image address's as written (notionMarkdownCalloutIcon).
	if strings.TrimSpace(emojiID+icon) == "" && (content["iconType"] != notionIconTypeExternal || icon == "") {
		return ""
	}
	name := notionMarkdownCalloutIcon(content, mentions)
	if match := notionMarkdownBuiltInIconPath.FindStringSubmatch(name); match != nil {
		name = "icons/" + match[1] + "_" + match[2]
	}
	if name == "" {
		return ""
	}
	// "v1" escapes a tab's, unlike a callout's, but for ' (seen 2026-11-11:
	// icon="&amp;", icon="&quot;", icon="'").
	if mentions.APIForm {
		return ` icon="` + notionMarkdownAttributeEscaped.Replace(name) + `"`
	}
	return ` icon="` + notionMarkdownVersion2AttributeEscaped.Replace(name) + `"`
}

// notionMarkdownCalloutLine reads a callout's opening tag: its content, the
// text that follows the tag on the same line, and whether the line closes
// the callout too.
func notionMarkdownCalloutLine(trimmed string) (json.RawMessage, string, bool, bool) {
	match := notionMarkdownCalloutOpen.FindStringSubmatch(trimmed)
	if match == nil {
		return nil, "", false, false
	}
	fields := map[string]any{}
	named := false
	for _, attribute := range notionMarkdownCalloutAttribute.FindAllStringSubmatch(match[1], -1) {
		// An icon is read with its spaces, as its first character may be one.
		value := html.UnescapeString(attribute[2])
		switch attribute[1] {
		case "icon":
			named = true
			if value == "" {
				fields["icon"], fields["iconType"] = "", calloutIconNone
				continue
			}
			for key, field := range notionMarkdownCalloutIconContent(value) {
				fields[key] = field
			}
		case "color":
			value = strings.TrimSpace(value)
			if strings.HasSuffix(value, "_bg") {
				value = strings.TrimSuffix(value, "_bg") + "_background"
			}
			if validNotionBlockColor(value) && value != "default" && value != "default_background" {
				fields["color"] = value
			}
		}
	}
	// One that names no icon has none, as Notion reads it (seen 2026-11-12);
	// an empty icon="" is none too.
	if !named {
		fields["icon"], fields["iconType"] = "", calloutIconNone
	}
	text := match[2]
	closed := strings.HasSuffix(text, notionMarkdownCalloutClose)
	text = strings.TrimSpace(strings.TrimSuffix(text, notionMarkdownCalloutClose))
	content, err := json.Marshal(fields)
	if err != nil {
		return nil, "", false, false
	}
	return content, text, closed, true
}

// notionMarkdownCalloutIconContent is the content an icon's name gives a
// callout; a custom emoji's :name: waits for the workspace to name it
// (resolveNotionMarkdownCalloutIcons), a stored file's address — an upload's
// file-upload://<ID>, or the address a read gave the callout's own picture —
// for the write to place it (notionMarkdownFiles.placedIcon), and a name that
// is none of these is its first character's: an emoji, or an image address of
// that character (2026-11-26: file-upload://… was saved as the letter "f").
func notionMarkdownCalloutIconContent(value string) map[string]any {
	if value == "" {
		return nil
	}
	if placement, stored := notionMarkdownStoredFile(value); stored {
		return map[string]any{notionMarkdownIconFileKey: placement}
	}
	if match := notionMarkdownCalloutEmojiName.FindStringSubmatch(value); match != nil {
		return map[string]any{"customEmojiName": match[1], "iconType": "custom_emoji"}
	}
	if match := notionMarkdownBuiltInIconPath.FindStringSubmatch(value); match != nil {
		// Only an icon's own name and a color of it are one (seen
		// 2026-11-11: "/icons/pizza_blue.svg", not a tooltip alias, an
		// unknown name or color); any other is refused as the write is read
		// (resolveNotionMarkdownCalloutIcons).
		_, known := nativeIconCatalog[match[1]]
		if icon := match[1] + ":" + match[2]; known && validNativeIconValue(icon) {
			return map[string]any{"icon": icon, "iconType": nativeIconType}
		}
		return map[string]any{notionMarkdownUnsupportedIcon: value}
	}
	if icon, named, err := nativeIconFromNotionIdentifier(value); named {
		if err != nil {
			return nil
		}
		return map[string]any{"icon": icon, "iconType": nativeIconType}
	}
	if notionPageExternalFile(value) != nil && strings.TrimSpace(value) == value {
		return map[string]any{"icon": value, "iconType": notionIconTypeExternal}
	}
	first := notionMarkdownFirstCharacter(value)
	if notionSingleEmoji(first) {
		return map[string]any{"icon": first, "iconType": "emoji"}
	}
	return map[string]any{"icon": first, "iconType": notionIconTypeExternal}
}

// notionMarkdownUnsupportedIcon holds, until the write is read, a built-in
// icon's address naming no icon of the catalog, which Notion refuses.
const notionMarkdownUnsupportedIcon = "unsupportedBuiltInIcon"

// notionIconTypeExternal is the kind of a callout's icon named by an image
// address.
const notionIconTypeExternal = "external"

// notionMarkdownFirstCharacter is the first character value shows: an emoji
// with its skin tone, variation selectors, tags and keycap and the
// pictographs a zero-width joiner joins to it, a flag's two letters, or a
// letter with the marks that follow it.
func notionMarkdownFirstCharacter(value string) string {
	characters := []rune(value)
	if len(characters) == 0 {
		return ""
	}
	end, flag := 1, characters[0] >= 0x1F1E6 && characters[0] <= 0x1F1FF
	for end < len(characters) {
		character := characters[end]
		switch {
		case flag && character >= 0x1F1E6 && character <= 0x1F1FF:
			flag = false
		case character == 0x200D:
			if end+1 < len(characters) && notionEmojiPictograph(characters[end+1]) {
				end++
			}
		case unicode.In(character, unicode.Mn, unicode.Me, unicode.Mc), character >= 0x1F3FB && character <= 0x1F3FF,
			character >= 0xE0020 && character <= 0xE007F, character >= 0x1160 && character <= 0x11FF,
			character >= 0xD7B0 && character <= 0xD7FF:
		default:
			return string(characters[:end])
		}
		end++
	}
	return string(characters)
}

// notionMarkdownCalloutEmoji reports whether an icon's name reads as an
// emoji: a few characters, without spaces, starting past plain ASCII or a
// keycap.
func notionMarkdownCalloutEmoji(value string) bool {
	characters := []rune(value)
	if len(characters) == 0 || len(characters) > 16 {
		return false
	}
	for _, character := range characters {
		if unicode.IsSpace(character) {
			return false
		}
	}
	return characters[0] > unicode.MaxASCII || strings.ContainsRune(value, '⃣')
}

// resolveNotionMarkdownCalloutIcons gives the callouts of Markdown written
// to a page the workspace's custom emoji their :name: names; a name no
// custom emoji has gives no icon.
func resolveNotionMarkdownCalloutIcons(app core.App, workspaceID string, operations []graphOperation) error {
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type != graphBlockTypeCallout && operation.Block.Type != graphBlockTypeParagraph {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal(operation.Block.Content, &content) != nil {
			continue
		}
		if unsupported, named := content[notionMarkdownUnsupportedIcon].(string); named {
			return graphInputError(errorCodeGraphInvalid, unsupported+
				" isn't an icon Leafen has: give an icon name a tool read back, an emoji, or an image URL.")
		}
		name, pending := content["customEmojiName"].(string)
		if !pending {
			continue
		}
		delete(content, "customEmojiName")
		delete(content, "iconType")
		emojis := []struct {
			ID string `db:"id"`
		}{}
		if _, key, err := normalizeCustomEmojiName(name); err == nil {
			if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_custom_emojis}}
				WHERE [[workspace_id]]={:workspaceId} AND [[name_key]]={:key} AND [[workspace_import_job_id]]='' LIMIT 1`).
				Bind(dbx.Params{"workspaceId": workspaceID, "key": key}).All(&emojis); err != nil {
				return err
			}
		}
		if len(emojis) > 0 {
			content["customEmojiId"], content["iconType"] = emojis[0].ID, "custom_emoji"
		} else {
			// A name the workspace hasn't is its first character's, ":".
			content["icon"], content["iconType"] = ":", notionIconTypeExternal
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		operation.Block.Content = encoded
	}
	return nil
}
