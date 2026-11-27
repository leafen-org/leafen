package app

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Leafen's own preview of a Word, Excel, PowerPoint or PDF file, as Notion's file
// preview: pressing such a file's row shows it on any server, before anyone
// adds a document app for the format (2026-11-05, built-in document viewing).
// The page reads nothing by itself: the Leafen page that frames it posts the
// file's bytes, and it draws them with the light engines the Office app also
// falls back on (docx-preview, SheetJS, pptx-renderer; a copy of the Office
// app's, scripts/generate-document-preview-assets.mjs), and a PDF with the PDF
// app's pdf.js. It has no origin
// (sandbox), loads only its own files and is framed only by Leafen's pages.
const documentPreviewPath = "/document-preview/"

//go:embed documentpreview
var documentPreviewFiles embed.FS

func registerDocumentPreviewRoutes(event *core.ServeEvent) {
	// The page has no origin, so its module scripts load as cross-origin
	// requests; they are the same public files for everyone.
	event.Router.GET(apiRoot+documentPreviewPath+"{path...}", serveDocumentPreview).Unbind(apis.DefaultCorsMiddlewareId)
}

// documentPreviewTags are the files' content tags: a browser asks again each
// time and keeps its copy while the file is the same, so a new version's
// script never meets the last version's styles (2026-11-05: an hour's cache
// kept a phone on the old styles).
var documentPreviewTags sync.Map

func documentPreviewTag(name string, data []byte) string {
	if tag, ok := documentPreviewTags.Load(name); ok {
		return tag.(string)
	}
	sum := sha256.Sum256(data)
	tag := `"` + hex.EncodeToString(sum[:12]) + `"`
	documentPreviewTags.Store(name, tag)
	return tag
}

// documentPreviewPDFJS is the PDF app's pdf.js, which the preview draws a PDF
// with: one copy in the executable, served under the preview's own path.
var documentPreviewPDFJS = sync.OnceValue(func() string {
	found, _ := fs.Glob(documentPreviewAssets, "builtinapps/document-viewer/ui/vendor/pdfjs-dist-*")
	if len(found) == 0 {
		return ""
	}
	return found[0]
})

// documentPreviewPDFWords is the PDF app's folder whose l10n/locale.json maps
// each language to pdf.js's words for it (vendor/pdfjs-locale-*) and Korean to
// the app's own first: a PDF's pdf.js words in the preview (a page's name to a
// screen reader, a note's mark) come in the person's language as in the app.
const documentPreviewPDFWords = "builtinapps/document-viewer/ui/"

// documentPreviewFile reads one of the preview's files: its own, pdf.js at
// pdfjs/, or pdf.js's words at pdfjs-words/ (only l10n/ and pdf.js's translations).
func documentPreviewFile(name string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(name, "documentpreview/pdfjs/"); ok {
		if documentPreviewPDFJS() == "" {
			return nil, fs.ErrNotExist
		}
		return fs.ReadFile(documentPreviewAssets, documentPreviewPDFJS()+"/"+rest)
	}
	if rest, ok := strings.CutPrefix(name, "documentpreview/pdfjs-words/"); ok {
		if !strings.HasPrefix(rest, "l10n/") && !strings.HasPrefix(rest, "vendor/pdfjs-locale-") {
			return nil, fs.ErrNotExist
		}
		return fs.ReadFile(documentPreviewAssets, documentPreviewPDFWords+rest)
	}
	return documentPreviewFiles.ReadFile(name)
}

func serveDocumentPreview(event *core.RequestEvent) error {
	name := path.Clean("documentpreview/" + event.Request.PathValue("path"))
	if !strings.HasPrefix(name, "documentpreview/") {
		return event.NotFoundError("", nil)
	}
	data, err := documentPreviewFile(name)
	if err != nil {
		return event.NotFoundError("", nil)
	}
	header := event.Response.Header()
	kind := mime.TypeByExtension(path.Ext(name))
	if path.Ext(name) == ".js" || path.Ext(name) == ".mjs" {
		kind = "text/javascript; charset=utf-8"
	}
	if path.Ext(name) == ".wasm" {
		kind = "application/wasm"
	}
	if kind == "" {
		kind = "application/octet-stream"
	}
	header.Set("Content-Type", kind)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Cross-Origin-Resource-Policy", "cross-origin")
	header.Set("Cache-Control", "no-cache")
	tag := documentPreviewTag(name, data)
	header.Set("ETag", tag)
	if path.Ext(name) == ".html" {
		origins, err := framingOrigins(event.Request)
		if err != nil {
			return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeFileDownloadFailed, err))
		}
		own := []string{}
		for _, origin := range origins {
			own = append(own, origin+apiRoot+documentPreviewPath)
		}
		files := strings.Join(own, " ")
		header.Set("Content-Security-Policy", strings.Join([]string{
			"sandbox allow-scripts",
			"default-src 'none'",
			// pdf.js decodes some images with WebAssembly, as in the PDF app.
			"script-src " + files + " 'wasm-unsafe-eval'",
			"style-src " + files + " 'unsafe-inline'",
			// pdf.js's own pictures too: a PDF note's mark.
			"img-src " + files + " blob: data:",
			"font-src blob: data:",
			"media-src blob: data:",
			// Its own files only: pdf.js reads its character maps, colour profiles and decoders.
			"connect-src " + files + " blob: data:",
			"worker-src blob:",
			"object-src 'none'",
			"form-action 'none'",
			"base-uri 'none'",
			frameAncestorsDirective(origins),
		}, "; "))
		// The policy says who frames it; the server's SAMEORIGIN would refuse the apps' own page.
		header.Del("X-Frame-Options")
	}
	if event.Request.Header.Get("If-None-Match") == tag {
		event.Response.WriteHeader(http.StatusNotModified)
		return nil
	}
	event.Response.WriteHeader(http.StatusOK)
	_, err = event.Response.Write(data)
	return err
}
