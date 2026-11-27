package app

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// untitled names what has no name in the document's language.
func (document *workspaceExportDocument) untitled() string {
	return workspaceExportUntitledName(document.Language)
}

func workspaceExportDocumentMode(payload workspaceExportJobPayload) string {
	switch {
	case payload.Format == workspaceExportFormatPrintHTML:
		return workspaceExportModePrint
	case payload.Format == workspaceExportFormatHTML,
		payload.NotionAdmin != nil && payload.NotionAdmin.ExportFormat == "html":
		return workspaceExportModeHTML
	default:
		return workspaceExportModeMarkdown
	}
}

// writeWorkspaceExportDocuments writes a readable export: a ZIP of HTML or
// Markdown pages with CSV databases and included files, or one printable
// HTML document with images embedded.
func writeWorkspaceExportDocuments(app core.App, output io.Writer, job storedJob, payload workspaceExportJobPayload) error {
	document, err := collectWorkspaceExportDocument(app, job, payload)
	if err != nil {
		return err
	}
	mode := workspaceExportDocumentMode(payload)
	extension := ".md"
	if mode != workspaceExportModeMarkdown {
		extension = ".html"
	}
	if payload.LinkedView != nil && mode == workspaceExportModeMarkdown && payload.Options.CreateFolders {
		if document.SourceFolder, err = workspaceExportSourceFolder(app, job.Owner, payload.Scope.TargetID, document.untitled()); err != nil {
			return err
		}
	}
	section := ""
	if mode != workspaceExportModePrint {
		if section, err = workspaceExportSectionFolder(app, payload, document.Language); err != nil {
			return err
		}
	}
	document.assignPaths(extension, document.untitled(), section)
	writer := &workspaceExportDocumentWriter{
		app: app, job: job, document: document, mode: mode, section: section,
		files: newWorkspaceExportFilePlacer(), articles: map[string]string{}, anchors: map[string][]string{},
	}
	writer.files.root = section
	if mode == workspaceExportModePrint {
		if err := writer.writePrint(output); err != nil {
			return err
		}
		return document.mentionFailure()
	}
	archive := zip.NewWriter(output)
	writer.archive = archive
	if err := writer.writePages(); err != nil {
		_ = archive.Close()
		return err
	}
	if err := document.mentionFailure(); err != nil {
		_ = archive.Close()
		return err
	}
	if err := writer.writeFiles(); err != nil {
		_ = archive.Close()
		return err
	}
	return archive.Close()
}

type workspaceExportDocumentWriter struct {
	app      core.App
	job      storedJob
	document *workspaceExportDocument
	mode     string
	archive  *zip.Writer
	files    *workspaceExportFilePlacer
	embedded int64
	articles map[string]string
	// anchors names, by line, the sub-pages hanging on it (noteAnchors).
	anchors map[string][]string
	// section is the export's top folder (workspaceExportSectionFolder), part
	// of every path the export places (assignPaths).
	section string
	// katexWritten is whether the export holds its KaTeX yet (equationsHTML).
	katexWritten bool
}

func (writer *workspaceExportDocumentWriter) renderer(from, folder string) *workspaceExportRenderer {
	return &workspaceExportRenderer{
		document: writer.document, mode: writer.mode, from: from, folder: folder,
		files: writer.files, embed: writer.embed, embedBytes: writer.reserveEmbedded, untitled: writer.document.untitled(),
	}
}

// decodePage fills each block of one page with its text from the page's
// Ygo document.
func (writer *workspaceExportDocumentWriter) decodePage(item workspaceExportInventoryItem) error {
	pageID := mapString(item.Value, "pageId")
	blocks := writer.document.Blocks[pageID]
	state, err := hex.DecodeString(mapString(item.Value, "stateHex"))
	if err != nil || len(blocks) == 0 || len(state) == 0 {
		return err
	}
	ydoc := crdt.New()
	if err := crdt.ApplyUpdateV1(ydoc, state, nil); err != nil {
		return err
	}
	for _, block := range blocks {
		if runs := workspaceExportRunsFromYText(ydoc.GetText(ygoBlockTextRootPrefix+block.ID), writer.document.Language, writer.document.closes); len(runs) > 0 {
			block.Runs = runs
		}
	}
	return nil
}

func (writer *workspaceExportDocumentWriter) walkText(visit func(pageID string) error, only map[string]bool) error {
	return walkWorkspaceExportChunks(writer.app, writer.job, func(chunk workspaceExportInventoryChunk) error {
		for _, item := range chunk.Items {
			pageID := mapString(item.Value, "pageId")
			if item.Kind != workspaceExportItemYgo || only != nil && !only[pageID] {
				continue
			}
			if err := writer.decodePage(item); err != nil {
				return err
			}
			if err := visit(pageID); err != nil {
				return err
			}
		}
		return nil
	})
}

// renderPages decodes each page's text once and renders it; pages whose
// text a synced copy elsewhere shows are decoded first and kept.
func (writer *workspaceExportDocumentWriter) renderPages(render func(*workspaceExportDocPage) error) error {
	document := writer.document
	if len(document.SyncedSources) > 0 {
		if err := writer.walkText(func(string) error { return nil }, document.SyncedSources); err != nil {
			return err
		}
	}
	rendered := map[string]bool{}
	err := writer.walkText(func(pageID string) error {
		page := document.Pages[pageID]
		if page != nil && page.Path != "" && !page.Trashed {
			rendered[pageID] = true
			if err := render(page); err != nil {
				return err
			}
		}
		if !document.SyncedSources[pageID] {
			for _, block := range document.Blocks[pageID] {
				block.Runs = nil
			}
		}
		return nil
	}, nil)
	if err != nil {
		return err
	}
	for _, id := range workspaceExportSortedPageIDs(document) {
		if page := document.Pages[id]; !rendered[id] && page.Path != "" && !page.Trashed {
			if err := render(page); err != nil {
				return err
			}
		}
	}
	return nil
}

func workspaceExportSortedPageIDs(document *workspaceExportDocument) []string {
	ids := make([]string, 0, len(document.Pages))
	for id := range document.Pages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (writer *workspaceExportDocumentWriter) writeEntry(name, content string) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Flags: 0x0800}
	header.SetModTime(workspaceExportZipTime(writer.job))
	entry, err := writer.archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.WriteString(entry, content)
	return err
}

func (writer *workspaceExportDocumentWriter) writePages() error {
	document := writer.document
	err := writer.renderPages(func(page *workspaceExportDocPage) error {
		renderer := writer.renderer(page.Path, page.Folder)
		if writer.mode == workspaceExportModeMarkdown {
			return writer.writeEntry(page.Path, renderer.pageMarkdown(page))
		}
		title := workspaceExportTitle(page.Title, document.untitled())
		return writer.writeHTML(page.Path, title, renderer.pageIcon(page.Icon, page.IconType), renderer.pageArticleHTML(page))
	})
	if err != nil {
		return err
	}
	if err := writer.writeLinkedViews(); err != nil {
		return err
	}
	for _, dataSourceID := range workspaceExportSortedKeys(document.DataSources) {
		source := document.DataSources[dataSourceID]
		if source.CSVPath == "" {
			continue
		}
		// Notion's HTML export keeps each database's CSV beside its pages, and
		// its Markdown & CSV export the "_all" file too (seen 2026-11-07).
		csv, err := document.databaseCSVPlacing(dataSourceID, false, writer.files)
		if err != nil {
			return err
		}
		if err := writer.writeEntry(source.CSVPath, csv); err != nil {
			return err
		}
		if writer.mode == workspaceExportModeMarkdown {
			all, err := document.databaseCSVPlacing(dataSourceID, true, writer.files)
			if err != nil {
				return err
			}
			if err := writer.writeEntry(workspaceExportSourceAllCSVPath(source), all); err != nil {
				return err
			}
		} else if document.Payload.Scope.Kind == workspaceExportScopeDatabase && dataSourceID == document.Payload.Scope.TargetID ||
			source.FullPage {
			// A database exported on its own, or a full-page database, is
			// its table's page.
			path := workspaceExportDatabaseHTMLPath(source.CSVPath)
			renderer := writer.renderer(path, source.Folder)
			title := workspaceExportTitle(source.Title, document.untitled())
			icon := renderer.sourceIcon(source)
			body := renderer.databaseArticleOpen(source, icon) + renderer.headerHTML(source.Title, icon, document.sourceCover(source)) + "</header>" +
				`<div class="page-body">` + renderer.databaseHTML(dataSourceID, "", document.viewTable(dataSourceID, document.sourceView(dataSourceID)), false) + "</div></article>"
			if err := writer.writeHTML(path, title, icon, body); err != nil {
				return err
			}
		}
		for _, row := range document.Rows[dataSourceID] {
			if row.Path == "" {
				continue
			}
			if err := writer.writeRow(writer.renderer(row.Path, source.Folder), row); err != nil {
				return err
			}
		}
	}
	// A database page shared on its own is a page file at the top: its
	// title, its properties and its body.
	for _, row := range document.sharedRows() {
		if err := writer.writeRow(writer.renderer(row.Path, row.Folder), row); err != nil {
			return err
		}
	}
	return nil
}

// writeRow writes a row page's file.
func (writer *workspaceExportDocumentWriter) writeRow(renderer *workspaceExportRenderer, row *workspaceExportDocRow) error {
	if writer.mode == workspaceExportModeMarkdown {
		return writer.writeEntry(row.Path, renderer.rowMarkdown(row))
	}
	return writer.writeHTML(row.Path, workspaceExportTitle(row.Title, writer.document.untitled()),
		renderer.pageIcon(row.Icon, row.IconType), renderer.rowArticleHTML(row))
}

func workspaceExportSortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writePrint writes one HTML document: the exported page (or database) and,
// with subpages, every exported subpage and row page after it, each
// starting on a new printed page. What a page's body shows prints in the
// order the body shows it, depth first: a sub-page with its own sub-pages
// and databases, a database with its rows, each row page followed by what
// its body shows (printItems).
func (writer *workspaceExportDocumentWriter) writePrint(output io.Writer) error {
	document := writer.document
	err := writer.renderPages(func(page *workspaceExportDocPage) error {
		writer.articles[page.ID] = writer.renderer("", "").pageArticleHTML(page)
		writer.noteAnchors(page)
		return nil
	})
	if err != nil {
		return err
	}
	var body strings.Builder
	title := document.untitled()
	printed := map[string]bool{}
	var printAll func(items []workspaceExportPrintItem)
	printRow := func(row *workspaceExportDocRow) {
		body.WriteString(writer.renderer("", "").rowArticleHTML(row))
		printAll(writer.rowPrintItems(row))
	}
	printSource := func(dataSourceID string) {
		if printed[dataSourceID] {
			return
		}
		printed[dataSourceID] = true
		for _, row := range document.Rows[dataSourceID] {
			if row.Path != "" {
				printRow(row)
			}
		}
	}
	visit := func(page *workspaceExportDocPage) {
		body.WriteString(writer.articles[page.ID])
		printAll(writer.printItems(page.ID, document.bodyItems(page), document.Blocks[page.ID], page.Children))
	}
	printAll = func(items []workspaceExportPrintItem) {
		for _, item := range items {
			if item.page != nil {
				visit(item.page)
				continue
			}
			for _, dataSourceID := range item.container.DataSourceIDs {
				printSource(dataSourceID)
			}
		}
	}
	for index, root := range document.Roots {
		if index == 0 {
			title = workspaceExportTitle(root.Title, document.untitled())
		}
		visit(root)
	}
	// A database page exported on its own prints as a page, with its
	// properties, then what its body shows.
	for _, row := range document.sharedRows() {
		if len(document.Roots) == 0 {
			title = workspaceExportTitle(row.Title, document.untitled())
		}
		printRow(row)
	}
	if document.Payload.Scope.Kind == workspaceExportScopeDatabase {
		if source := document.DataSources[document.Payload.Scope.TargetID]; source != nil {
			title = workspaceExportTitle(source.Title, document.untitled())
			renderer := writer.renderer("", "")
			icon := renderer.sourceIcon(source)
			body.WriteString(renderer.databaseArticleOpen(source, icon) + renderer.headerHTML(source.Title, icon, document.sourceCover(source)) +
				"</header>" + renderer.databaseHTML(source.ID, "", document.viewTable(source.ID, document.sourceView(source.ID)), false) +
				"</article>")
			printSource(source.ID)
		}
	}
	head, tail := "", ""
	if workspaceExportHasEquations(body.String()) {
		if head, tail, err = writer.equationsHTML(""); err != nil {
			return err
		}
	}
	_, err = io.WriteString(output, workspaceExportHTMLDocument(title, head, body.String()+tail))
	return err
}

// reserveEmbedded takes bytes of the printable export's embedding budget,
// false when they don't fit.
func (writer *workspaceExportDocumentWriter) reserveEmbedded(bytes int64) bool {
	if writer.embedded+bytes > workspaceExportPrintEmbeddedBytes {
		return false
	}
	writer.embedded += bytes
	return true
}

// embed turns an included image into a data URI for the printable export,
// within the export's embedding budget; other files stay out.
func (writer *workspaceExportDocumentWriter) embed(file workspaceExportFileSnapshot) string {
	if !strings.HasPrefix(file.ContentType, "image/") || file.ContentType == "image/svg+xml" ||
		writer.embedded+file.Bytes > workspaceExportPrintEmbeddedBytes {
		return ""
	}
	data, err := writer.readFile(file)
	if err != nil {
		return ""
	}
	writer.embedded += file.Bytes
	return "data:" + file.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (writer *workspaceExportDocumentWriter) readFile(file workspaceExportFileSnapshot) ([]byte, error) {
	filesystem, err := writer.app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer filesystem.Close()
	reader, err := filesystem.GetReader(file.StorageKey)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, file.Bytes+1))
	digest := sha256.Sum256(data)
	if err == nil && (int64(len(data)) != file.Bytes || hex.EncodeToString(digest[:]) != file.SHA256) {
		err = newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged)
	}
	return data, err
}

// writeFiles copies every file the pages link to into the archive, checking
// each against the snapshot the export captured.
func (writer *workspaceExportDocumentWriter) writeFiles() error {
	if len(writer.files.order) == 0 {
		return nil
	}
	filesystem, err := writer.app.NewFilesystem()
	if err != nil {
		return err
	}
	defer filesystem.Close()
	buffer := make([]byte, workspaceExportReaderWindowBytes)
	for _, recordID := range writer.files.order {
		file := writer.document.Files[recordID]
		entry, err := writer.archive.CreateHeader(workspaceExportZipHeader(writer.files.byRecord[recordID], workspaceExportZipTime(writer.job)))
		if err != nil {
			return err
		}
		if contents := writer.files.drawn[recordID]; contents != nil {
			data, err := contents()
			if err != nil {
				return err
			}
			if _, err := entry.Write(data); err != nil {
				return err
			}
			continue
		}
		reader, err := filesystem.GetReader(file.StorageKey)
		if err != nil {
			return err
		}
		hasher := sha256.New()
		written, copyErr := io.CopyBuffer(io.MultiWriter(entry, hasher), io.LimitReader(reader, file.Bytes+1), buffer)
		closeErr := reader.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != file.Bytes || hex.EncodeToString(hasher.Sum(nil)) != file.SHA256 {
			return newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged)
		}
	}
	return nil
}
