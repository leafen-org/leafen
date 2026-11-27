package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"image/png"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"leafen.org/leafen/web"
)

// A cover as an HTML export writes it, as Notion's export does (seen
// 2026-11-14): a picture at the head of its page's header, a page's, a
// database page's and a full-page database's alike, moved as the page shows
// it (object-position, its place from its top; 50% where it was not moved).
// Notion's gallery covers load from notion.so; Leafen's gallery pictures and
// its color and gradient covers are pictures of the export itself,
// placed beside the page as an uploaded cover is and named by the picture: a
// gallery picture as the web app carries it, a color or gradient as the
// 1500×600 picture it stands for (coverGradient.ts), painted as a shared
// page's link preview paints one. A printable export holds them in itself. A
// cover linked from the web stays its link, which the person put there.

const (
	workspaceExportGradientCoverWidth  = 1500
	workspaceExportGradientCoverHeight = 600
)

// workspaceExportCover is a cover and where it is moved to, in percent
// from the picture's top (cover_position).
type workspaceExportCover struct {
	Value    string
	Position float64
}

// workspaceExportCoverOf is the cover an exported item carries.
func workspaceExportCoverOf(value map[string]any) workspaceExportCover {
	position, moved := value["coverPosition"].(float64)
	if !moved || position < 0 || position > 100 {
		position = 50
	}
	return workspaceExportCover{Value: strings.TrimSpace(mapString(value, "cover")), Position: position}
}

// coverHTML is a cover's picture, "" for none it can show, as Notion's
// export writes it (seen 2026-11-14: <img class="page-cover-image"
// src="…" style="object-position:center 10.73%"/>, no words for it).
func (renderer *workspaceExportRenderer) coverHTML(cover workspaceExportCover) string {
	href := renderer.coverHref(cover.Value)
	if href == "" {
		return ""
	}
	return `<img class="page-cover-image" src="` + workspaceExportEscape(href) + `" style="object-position:center ` +
		strconv.FormatFloat(cover.Position, 'f', -1, 64) + `%"/>`
}

// workspaceExportDrawnCover is a gallery, color or gradient cover as a
// picture: its file's name and how to draw it; false for any other cover.
func workspaceExportDrawnCover(cover string) (string, func() ([]byte, error), bool) {
	if bundledGalleryCoverPaths[cover] {
		return path.Base(cover), func() ([]byte, error) {
			return fs.ReadFile(web.Dist(), strings.TrimPrefix(cover, "/"))
		}, true
	}
	match := publicCoverGradientPattern.FindStringSubmatch(cover)
	if match == nil {
		return "", nil, false
	}
	from, to := strings.ToLower(strings.TrimPrefix(match[2], "#")), strings.ToLower(strings.TrimPrefix(match[3], "#"))
	name := "gradient-" + from + "-" + to + ".png"
	if from == to {
		name = "color-" + from + ".png"
	}
	return name, func() ([]byte, error) {
		picture, _ := publicGradientCover(cover, workspaceExportGradientCoverWidth, workspaceExportGradientCoverHeight)
		encoded := bytes.Buffer{}
		err := png.Encode(&encoded, picture)
		return encoded.Bytes(), err
	}, true
}

// coverHref is where a page's cover is drawn from, "" for none it can show:
// an uploaded picture placed in the export, a picture the export draws, or
// the cover's own link.
func (renderer *workspaceExportRenderer) coverHref(cover string) string {
	cover = strings.TrimSpace(cover)
	if cover == "" {
		return ""
	}
	if href := renderer.fileHref(cover, graphBlockTypeImage); href != "" {
		return href
	}
	if name, draw, drawn := workspaceExportDrawnCover(cover); drawn {
		if renderer.mode == workspaceExportModePrint {
			return renderer.embedDrawn(name, draw)
		}
		digest := sha256.Sum256([]byte(cover))
		return workspaceExportRelativeHref(renderer.from,
			renderer.files.placeDrawn("cover-"+hex.EncodeToString(digest[:4]), name, draw, renderer.folder))
	}
	if link := workspaceExportSafeLink(cover); strings.HasPrefix(link, "http") {
		return link
	}
	return ""
}

// embedDrawn is a drawn picture inside a printable export, within its
// embedding budget as an uploaded picture is.
func (renderer *workspaceExportRenderer) embedDrawn(name string, draw func() ([]byte, error)) string {
	if renderer.embedBytes == nil {
		return ""
	}
	data, err := draw()
	if err != nil || !renderer.embedBytes(int64(len(data))) {
		return ""
	}
	return "data:image/" + strings.TrimPrefix(path.Ext(name), ".") + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// sourceCover is the cover over a database's own page: a full-page
// database's page's, or, for one exported alone, its own.
func (document *workspaceExportDocument) sourceCover(source *workspaceExportDocDataSource) workspaceExportCover {
	if page := document.Pages[source.PageID]; page != nil && page.Kind == pageKindDatabase {
		return page.Cover
	}
	return source.Cover
}

// sourceIcon is the icon over a database's own page: a full-page database's
// page's, which its page's head shows (2026-11-15: an export left it out,
// naming only its data source's), else its data source's own.
func (renderer *workspaceExportRenderer) sourceIcon(source *workspaceExportDocDataSource) workspaceExportIcon {
	if page := renderer.document.Pages[source.PageID]; page != nil && page.Kind == pageKindDatabase {
		if icon := renderer.pageIcon(page.Icon, page.IconType); icon.HTML() != "" {
			return icon
		}
	}
	return renderer.pageIcon(source.Icon, source.IconType)
}
