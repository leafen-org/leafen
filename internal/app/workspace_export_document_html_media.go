package app

import (
	"strings"
)

// The figures an HTML export writes a code block, a picture, a file and a
// block at a web address in, as Notion's HTML export writes each (seen
// 2026-11-14 on a page of every kind): a code block with a caption is a
// figure holding its <pre> and the caption; a picture links itself; a
// bookmark is its card; a file, a PDF, a video, an audio, an embed and a
// link preview are a "source" box linking the address, a file's link marked
// as a file's. Leafen's import reads each back as its block
// (importDocumentConverters.ts).

// codeHTML writes a code block as a <pre> of the "code code-wrap" class, its
// language its syntax by Prism's name (codeLanguagePrismNames; none for plain
// text and ASCII art), and with a caption as a figure holding it and the
// caption, which Notion's export names by the block.
func (renderer *workspaceExportRenderer) codeHTML(block *workspaceExportDocBlock, id string) string {
	syntax, language := "", strings.TrimSpace(mapString(block.Content, "language"))
	codeClass := ""
	if _, unmarked := unmarkedCodeLanguageNames[language]; language != "" && language != plainTextCodeLanguage && !unmarked {
		if prism := codeLanguagePrismNames[language]; prism != "" {
			language = prism
		}
		syntax = ` data-notion-code-syntax="` + workspaceExportEscape(language) + `"`
		codeClass = ` class="language-` + workspaceExportEscape(language) + `"`
	}
	code := `<code` + codeClass + ` style="white-space:pre-wrap;word-break:break-all">` +
		workspaceExportEscape(workspaceExportPlainText(block.Runs)) + "</code></pre>"
	caption := renderer.captionHTML(block)
	if caption == "" {
		return `<pre ` + id + ` class="code code-wrap"` + syntax + ">" + code
	}
	return `<figure ` + id + syntax + `><pre class="code code-wrap"` + syntax + ">" + code + caption + "</figure>"
}

// mediaHTML writes a picture, a file or a block at a web address with its
// caption. An external block links its address as its words, a file its
// name; one whose file the export leaves out names it.
func (renderer *workspaceExportRenderer) mediaHTML(block *workspaceExportDocBlock, id string) string {
	href := workspaceExportSafeLink(mapString(block.Content, "url"))
	uploaded := mapString(block.Content, "fileId") != ""
	if uploaded {
		href = renderer.fileHref(mapString(block.Content, "fileId"), block.Type)
	}
	source := workspaceExportEscape(href)
	caption := renderer.captionHTML(block)
	switch {
	case block.Type == graphBlockTypeImage && href != "":
		alt := ""
		if text := mapString(block.Content, "altText"); text != "" {
			alt = ` alt="` + workspaceExportEscape(text) + `"`
		}
		return `<figure ` + id + ` class="image" data-notion-image="` + source + `" dir="ltr"><a href="` + source +
			`"><img src="` + source + `"` + alt + "/></a>" + caption + "</figure>"
	case block.Type == graphBlockTypeBookmark && href != "":
		return `<figure ` + id + ` dir="ltr">` + renderer.bookmarkHTML(block, href) + caption + "</figure>"
	}
	name := renderer.mediaName(block)
	if href == "" {
		if name == "" {
			return ""
		}
		return `<figure ` + id + ` dir="ltr"><div class="source">` + workspaceExportEscape(name) + "</div>" + caption + "</figure>"
	}
	words, link := href, `<a href="`+source+`">`
	if block.Type == graphBlockTypeFile {
		words = name
		link = `<a href="` + source + `" data-notion-file="` + source + `" data-notion-file-alias="` + workspaceExportEscape(name) + `">`
	} else if uploaded {
		words = name
	}
	// A link preview is a block at its address, as an embed; its class
	// keeps it one when the export is read back.
	figure := `<figure ` + id + ` dir="ltr">`
	if block.Type == graphBlockTypeLinkPreview {
		figure = `<figure ` + id + ` class="link-preview" dir="ltr">`
	}
	return figure + `<div class="source">` + link + workspaceExportEscape(words) + "</a></div>" + caption + "</figure>"
}

// bookmarkHTML writes a bookmark's card as Notion's export does: a link of
// the "bookmark source" class holding the page's title (empty until it is
// read), its description, its address after its icon and its picture.
func (renderer *workspaceExportRenderer) bookmarkHTML(block *workspaceExportDocBlock, href string) string {
	var builder strings.Builder
	builder.WriteString(`<a href="` + workspaceExportEscape(href) + `" class="bookmark source"><div class="bookmark-info">` +
		`<div class="bookmark-text"><div class="bookmark-title">` + workspaceExportEscape(mapString(block.Content, "title")) + "</div>")
	if description := strings.TrimSpace(mapString(block.Content, "description")); description != "" {
		builder.WriteString(`<div class="bookmark-description">` + workspaceExportEscape(description) + "</div>")
	}
	builder.WriteString(`</div><div class="bookmark-href">`)
	if icon := workspaceExportSafeLink(mapString(block.Content, "iconUrl")); strings.HasPrefix(icon, "http") {
		builder.WriteString(`<img src="` + workspaceExportEscape(icon) + `" class="icon bookmark-icon"/>`)
	}
	builder.WriteString(workspaceExportEscape(href) + "</div></div>")
	if image := workspaceExportSafeLink(mapString(block.Content, "imageUrl")); strings.HasPrefix(image, "http") {
		builder.WriteString(`<img src="` + workspaceExportEscape(image) + `" class="bookmark-image"/>`)
	}
	builder.WriteString("</a>")
	return builder.String()
}

func (renderer *workspaceExportRenderer) captionHTML(block *workspaceExportDocBlock) string {
	caption := workspaceExportRunsFromSpans(block.Content["caption"], renderer.document.Language)
	if len(caption) == 0 {
		return ""
	}
	return "<figcaption>" + renderer.runsHTML(caption) + "</figcaption>"
}

func (renderer *workspaceExportRenderer) mediaName(block *workspaceExportDocBlock) string {
	for _, key := range []string{"name", "fileName"} {
		if name := strings.TrimSpace(mapString(block.Content, key)); name != "" {
			return name
		}
	}
	if file, exists := renderer.document.Files[mapString(block.Content, "fileId")]; exists {
		return file.Name
	}
	return mapString(block.Content, "url")
}
