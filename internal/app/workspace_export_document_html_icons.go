package app

import (
	_ "embed"
	"encoding/json"
	"io/fs"
	"net/url"
	"strings"
	"sync"

	"leafen.org/leafen/web"
)

// The icons an HTML export writes for a page, a database row and a callout,
// as Notion's HTML export writes them (seen 2026-11-14): an emoji drawn from
// its attribute, a built-in icon a picture marked a static icon, a linked or
// uploaded picture its image; each named by its emoji, its built-in
// picture's path in Notion's icon set or its picture's address. Notion's
// built-in pictures load from app.notion.com; Leafen's are its own glyphs
// drawn inside the file, so opening an export asks no server for
// an icon a person did not link.

//go:embed workspace_export_builtin_icons.generated.json
var workspaceExportBuiltInIconsJSON []byte

// workspaceExportBuiltInIconSet is the web app's built-in icon glyphs and
// their light theme colors (scripts/generate-export-property-icons.mjs).
type workspaceExportBuiltInIconSet struct {
	Colors  map[string]string `json:"colors"`
	Neutral string            `json:"neutral"`
	Glyphs  map[string]string `json:"glyphs"`
}

var workspaceExportBuiltInIcons = sync.OnceValue(func() workspaceExportBuiltInIconSet {
	var set workspaceExportBuiltInIconSet
	if err := json.Unmarshal(workspaceExportBuiltInIconsJSON, &set); err != nil {
		panic("workspace export built-in icons: " + err.Error())
	}
	return set
})

// workspaceExportBuiltInIconPicture is a built-in icon as a picture inside
// the file: its glyph as the web app draws it (BuiltInIconGlyph) in its
// color, the neutral mark for a name with no glyph, as an SVG of Notion's
// icon set's 20 by 20 box.
func workspaceExportBuiltInIconPicture(name, color string) string {
	set := workspaceExportBuiltInIcons()
	glyph, drawn := set.Glyphs[name]
	if !drawn {
		glyph = set.Neutral
	}
	paint := set.Colors[color]
	if paint == "" {
		paint = set.Colors[nativeIconDefaultColor]
	}
	return workspaceExportSVGDataURI(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">` +
		strings.ReplaceAll(glyph, "currentColor", paint) + "</svg>")
}

// workspaceExportSVGDataURI is an SVG as a data address, its characters
// escaped as Notion's export escapes its emoji favicon's.
func workspaceExportSVGDataURI(svg string) string {
	return "data:image/svg+xml," + strings.ReplaceAll(url.QueryEscape(svg), "+", "%20")
}

// workspaceExportIcon is an icon as an HTML export writes it.
type workspaceExportIcon struct {
	// Name is how the export names it (data-notion-page-icon,
	// data-notion-callout-icon): an emoji as it is, a built-in icon by its
	// picture's path in Notion's icon set, a linked picture by its address,
	// an uploaded one as its file ("attachment:<file ID>:<file name>").
	Name string
	// Emoji is an emoji icon, drawn from its attribute.
	Emoji string
	// Picture is where a picture icon is drawn from: a built-in icon's
	// glyph inside the file, a linked picture's address, an uploaded one's
	// place in the export.
	Picture string
	// BuiltIn marks a built-in icon, which Notion's export marks a static
	// icon and names in the document's head.
	BuiltIn bool
}

// HTML is the icon's element.
func (icon workspaceExportIcon) HTML() string {
	switch {
	case icon.Emoji != "":
		return `<span class="icon" data-emoji="` + workspaceExportEscape(icon.Emoji) + `"></span>`
	case icon.BuiltIn:
		return `<img class="icon notion-static-icon" src="` + workspaceExportEscape(icon.Picture) + `"/>`
	case icon.Picture != "":
		return `<img class="icon" src="` + workspaceExportEscape(icon.Picture) + `"/>`
	}
	return ""
}

// headHTML names an emoji or built-in icon in the document's head and draws
// it as the document's favicon, as Notion's export does (an emoji as text in
// an SVG); a picture is named in neither (seen 2026-11-14).
func (icon workspaceExportIcon) headHTML() string {
	favicon := ""
	switch {
	case icon.Emoji != "":
		favicon = workspaceExportSVGDataURI(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><text y=".9em" font-size="90">` +
			workspaceExportEscape(icon.Emoji) + "</text></svg>")
	case icon.BuiltIn:
		favicon = icon.Picture
	default:
		return ""
	}
	return `<meta name="data-notion-page-icon" content="` + workspaceExportEscape(icon.Name) + `"/><link rel="icon" href="` +
		workspaceExportEscape(favicon) + `"/>`
}

// nameAttr names the icon on a page's article (data-notion-page-icon).
func (icon workspaceExportIcon) nameAttr() string {
	if icon.Name == "" {
		return ""
	}
	return ` data-notion-page-icon="` + workspaceExportEscape(icon.Name) + `"`
}

// workspaceExportEmojiIcon is an emoji icon.
func workspaceExportEmojiIcon(emoji string) workspaceExportIcon {
	return workspaceExportIcon{Name: emoji, Emoji: emoji}
}

// workspaceExportNativeIcon is a built-in icon by its stored value
// ("name:color"), none for one unread.
func workspaceExportNativeIcon(value string) workspaceExportIcon {
	name, color, ok := parseNativeIconValue(strings.TrimSpace(value))
	if !ok {
		return workspaceExportIcon{}
	}
	return workspaceExportIcon{Name: "/icons/" + name + "_" + color + ".svg",
		Picture: workspaceExportBuiltInIconPicture(name, color), BuiltIn: true}
}

// workspaceExportPictureIcon is a picture icon at an address.
func workspaceExportPictureIcon(address string) workspaceExportIcon {
	if address == "" {
		return workspaceExportIcon{}
	}
	return workspaceExportIcon{Name: address, Picture: address}
}

// workspaceExportUploadedIcon is an uploaded picture icon placed at href,
// named as its file, as Notion's export names an uploaded icon on the page's
// article (seen 2026-11-14: data-notion-page-icon="attachment:<ID>:
// w1065-icon.png" — its attachment's ID and name, not its place in the
// export); a callout's uploaded icon is named by nothing.
func workspaceExportUploadedIcon(fileID string, file workspaceExportFileSnapshot, href string) workspaceExportIcon {
	return workspaceExportIcon{Name: "attachment:" + fileID + ":" + file.Name, Picture: href}
}

// pageIcon is a page's or a row's icon: its emoji, its built-in icon, or
// its picture — uploaded (its file's ID, placed in the export when files
// are included) or linked (its address). A workspace emoji is left out.
func (renderer *workspaceExportRenderer) pageIcon(icon, iconType string) workspaceExportIcon {
	icon = strings.TrimSpace(icon)
	switch iconType {
	case "emoji":
		if icon != "" {
			return workspaceExportEmojiIcon(icon)
		}
	case nativeIconType:
		return workspaceExportNativeIcon(icon)
	case "image", notionIconTypeExternal:
		if picture, bundled := workspaceExportBundledIcon(icon); bundled {
			return workspaceExportIcon{Name: icon, Picture: picture}
		}
		if href := renderer.fileHref(icon, graphBlockTypeImage); href != "" {
			return workspaceExportUploadedIcon(icon, renderer.document.Files[icon], href)
		}
		if link := workspaceExportSafeLink(icon); strings.HasPrefix(link, "http") {
			return workspaceExportPictureIcon(link)
		}
	}
	return workspaceExportIcon{}
}

// pageIconOf is the icon of a page or a row page the export names, none for
// one it does not hold.
func (renderer *workspaceExportRenderer) pageIconOf(pageID string) workspaceExportIcon {
	if page := renderer.document.Pages[pageID]; page != nil {
		return renderer.pageIcon(page.Icon, page.IconType)
	}
	if row := renderer.document.RowByID[pageID]; row != nil {
		return renderer.pageIcon(row.Icon, row.IconType)
	}
	return workspaceExportIcon{}
}

// workspaceExportBundledIcon is a picture icon of the web build's own
// (taskDatabaseIcon), carried in the export as its drawing, as a built-in
// icon is, since its address is the server's alone.
func workspaceExportBundledIcon(icon string) (string, bool) {
	if icon != taskDatabaseIcon {
		return "", false
	}
	drawing, err := fs.ReadFile(web.Dist(), strings.TrimPrefix(icon, "/"))
	if err != nil {
		return "", false
	}
	return workspaceExportSVGDataURI(string(drawing)), true
}
