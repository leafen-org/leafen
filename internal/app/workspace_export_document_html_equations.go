package app

import (
	"embed"
	"encoding/base64"
	"regexp"
	"strings"
	"sync"
)

// An HTML export's equations as Notion's HTML export names them (seen
// 2026-11-14): a block equation a figure of the "equation" class holding its
// TeX in data-notion-equation, an inline one a span holding it in
// data-notion-inline-equation. Notion writes KaTeX's drawing of each into the
// file (its HTML only, no MathML) and loads KaTeX's style from a CDN; Leafen
// has no script engine to draw them while it writes, so it writes the TeX and
// carries the web app's own KaTeX in the export (exportkatex, a copy of the
// web app's made by scripts/generate-export-katex.mjs), which draws them when the file is
// opened, with no request outside the export. Where scripts don't
// run, the TeX is what shows. KaTeX runs as the editor's does: an equation it
// can't read stays as its TeX, and commands that would link or load anything
// are refused (trust off).

//go:embed exportkatex
var workspaceExportKaTeXFiles embed.FS

// An export with equations holds KaTeX's style and script, each with
// KaTeX's license at its head, in its section's folder beside the top
// pages, which name their files "<title> <ID>.html" and so never these.
// They stand in no folder of their own, which an import would make a page;
// it reads them as the files its documents draw with (fileImportSources.ts).
const (
	workspaceExportKaTeXStylePath  = "katex.css"
	workspaceExportKaTeXScriptPath = "katex.min.js"
)

// workspaceExportEquationsDraw draws each equation of the file with KaTeX,
// leaving its TeX where it can't.
const workspaceExportEquationsDraw = `(function(){if(!window.katex)return;` +
	`document.querySelectorAll("[data-notion-equation],[data-notion-inline-equation]").forEach(function(e){` +
	`var d=e.hasAttribute("data-notion-equation"),box=d&&e.querySelector(".equation-container")||e;` +
	`try{katex.render(e.getAttribute(d?"data-notion-equation":"data-notion-inline-equation"),box,` +
	`{displayMode:d,throwOnError:true,trust:false})}catch(x){}})})()`

// workspaceExportKaTeXFile is one of the export's KaTeX files.
func workspaceExportKaTeXFile(name string) string {
	content, err := workspaceExportKaTeXFiles.ReadFile("exportkatex/" + name)
	if err != nil {
		panic("workspace export KaTeX: " + err.Error())
	}
	return string(content)
}

// workspaceExportKaTeXFontSources are where KaTeX's style finds each font:
// its woff2 file, then its other formats, which every browser that draws
// KaTeX does without.
var workspaceExportKaTeXFontSources = regexp.MustCompile(
	`url\(fonts/([A-Za-z0-9_-]+\.woff2)\) format\("woff2"\)(?:,url\(fonts/[A-Za-z0-9_-]+\.(?:woff|ttf)\) format\("(?:woff|truetype)"\))*`)

// workspaceExportKaTeXStyle is KaTeX's style with its fonts inside it: an
// exported file opened from a disk can't load a font file beside it in
// every browser.
var workspaceExportKaTeXStyle = sync.OnceValue(func() string {
	style := workspaceExportKaTeXFontSources.ReplaceAllStringFunc(workspaceExportKaTeXFile("katex.min.css"), func(source string) string {
		name := workspaceExportKaTeXFontSources.FindStringSubmatch(source)[1]
		return "url(data:font/woff2;base64," + base64.StdEncoding.EncodeToString([]byte(workspaceExportKaTeXFile("fonts/"+name))) +
			`) format("woff2")`
	})
	if strings.Contains(style, "url(fonts/") {
		panic("workspace export KaTeX: a font of its style is not inside it")
	}
	return style
})

// workspaceExportKaTeXNotice is KaTeX's license as a comment, which every
// copy of KaTeX an export holds carries.
var workspaceExportKaTeXNotice = sync.OnceValue(func() string {
	return "/*! KaTeX\n" + strings.ReplaceAll(workspaceExportKaTeXFile("LICENSE"), "*/", "* /") + "*/\n"
})

// workspaceExportHasEquations is whether a file's body holds an equation.
// Its words can't name one: a quotation mark in them is written escaped.
func workspaceExportHasEquations(body string) bool {
	return strings.Contains(body, ` data-notion-equation="`) || strings.Contains(body, ` data-notion-inline-equation="`)
}

// workspaceExportInlineEquationHTML is an inline equation as Notion's export
// names it, its TeX inside it for KaTeX to draw over.
func workspaceExportInlineEquationHTML(tex string) string {
	escaped := workspaceExportEscape(tex)
	return `<span data-notion-inline-equation="` + escaped + `" class="notion-text-equation-token" contenteditable="false">` +
		escaped + "</span>"
}

// equationsHTML is what a file at path with equations adds to its head and
// to its body's end: the export's KaTeX style and script, written into the
// export once, then the drawing. A printable export, one document, holds
// the style in itself; the app prints it in a frame that runs no script and
// draws its equations there with its own KaTeX (printExportEquations.ts).
func (writer *workspaceExportDocumentWriter) equationsHTML(path string) (string, string, error) {
	notice := workspaceExportKaTeXNotice()
	if writer.mode == workspaceExportModePrint {
		return "<style>" + notice + workspaceExportKaTeXStyle() + "</style>", "", nil
	}
	if !writer.katexWritten {
		if err := writer.writeEntry(writer.section+workspaceExportKaTeXStylePath, notice+workspaceExportKaTeXStyle()); err != nil {
			return "", "", err
		}
		if err := writer.writeEntry(writer.section+workspaceExportKaTeXScriptPath, notice+workspaceExportKaTeXFile("katex.min.js")); err != nil {
			return "", "", err
		}
		writer.katexWritten = true
	}
	return `<link rel="stylesheet" href="` + workspaceExportEscape(workspaceExportRelativeHref(path, writer.section+workspaceExportKaTeXStylePath)) + `"/>`,
		`<script src="` + workspaceExportEscape(workspaceExportRelativeHref(path, writer.section+workspaceExportKaTeXScriptPath)) + `"></script><script>` +
			workspaceExportEquationsDraw + "</script>", nil
}

// writeHTML writes a page's file: its title, its icon (workspaceExportIcon)
// and, when it holds equations, the KaTeX that draws them.
func (writer *workspaceExportDocumentWriter) writeHTML(path, title string, icon workspaceExportIcon, body string) error {
	head, tail := icon.headHTML(), ""
	if workspaceExportHasEquations(body) {
		style, script, err := writer.equationsHTML(path)
		if err != nil {
			return err
		}
		head, tail = head+style, script
	}
	return writer.writeEntry(path, workspaceExportHTMLDocument(title, head, body+tail))
}
