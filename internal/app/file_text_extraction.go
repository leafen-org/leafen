package app

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The words of pdf, docx, xlsx, pptx, HWP, HWPX, OpenDocument (odt, ods,
// odp) and older Office (doc, xls, ppt) files reach search, so an
// agent or a person finds a document nobody has opened (document apps
// decision of 2026-10-10, 4). The server reads each such file once, lightly —
// the text only, no layout — and keeps it beside the file; a page's search
// body takes the text of its file blocks as it takes their place in the page,
// and a database page's that of the files its properties hold. What can read
// the page can find the text, nothing else can: the text lives in the page's
// search entry, and it goes with the file. A file read only in part says so
// (partial: "length" past fileTextMaxRunes, "size" past
// fileTextMaxSourceBytes, unread), which the documents list and search show.

const (
	fileTextTable          = "leafen_file_text_extracts"
	fileTextMaxSourceBytes = 64 << 20
	fileTextMaxRunes       = 2_000_000
	fileTextMaxPartBytes   = 32 << 20
	fileTextBatch          = 8
	fileTextIdleDelay      = 30 * time.Second
	fileTextFailureDelay   = time.Minute
	fileTextStatusDone     = "done"
	fileTextStatusSkipped  = "skipped"
	fileTextStatusFailed   = "failed"
	fileTextPartialLength  = "length"
	fileTextPartialSize    = "size"
)

// extractFileText reads a file's words, at most fileTextMaxRunes of them,
// and whether it stopped short of the rest. A reader that trips over a file
// fails it rather than the server.
func extractFileText(kind string, source io.ReaderAt, size int64) (text string, cut bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text, cut, err = "", false, fmt.Errorf("the %s reader failed: %v", kind, recovered)
		}
	}()
	collected := &fileTextCollector{}
	switch kind {
	case "pdf":
		reader, err := pdf.NewReader(source, size)
		if err != nil {
			return "", false, err
		}
		collected.add(pdfPlainText(reader, fileTextMaxRunes))
	case "docx", "xlsx", "pptx", "hwpx":
		archive, err := zip.NewReader(source, size)
		if err != nil {
			return "", false, err
		}
		if kind == "hwpx" {
			err = hwpxText(archive, collected)
		} else {
			err = ooxmlText(archive, kind, collected)
		}
		if err != nil {
			return "", false, err
		}
	case "hwp":
		if err := hwpText(source, size, collected); err != nil {
			return "", false, err
		}
	case "odt", "ods", "odp":
		archive, err := zip.NewReader(source, size)
		if err != nil {
			return "", false, err
		}
		if err := odfText(archive, kind, collected); err != nil {
			return "", false, err
		}
	case "doc", "xls", "ppt":
		if err := legacyOfficeText(kind, source, size, collected); err != nil {
			return "", false, err
		}
	default:
		return "", false, errors.New("no reader for this file")
	}
	return collected.String(), collected.cut || collected.full(), nil
}

// fileTextCollector gathers text lines up to the limit, and notes words it
// had no room for.
type fileTextCollector struct {
	builder strings.Builder
	runes   int
	cut     bool
}

func (collector *fileTextCollector) add(text string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if collector.full() {
			collector.cut = true
			continue
		}
		if collector.builder.Len() > 0 {
			collector.builder.WriteByte('\n')
			collector.runes++
		}
		if room := fileTextMaxRunes - collector.runes; utf8.RuneCountInString(line) > room {
			line = string([]rune(line)[:room])
			collector.cut = true
		}
		collector.builder.WriteString(line)
		collector.runes += utf8.RuneCountInString(line)
	}
}

func (collector *fileTextCollector) full() bool { return collector.runes >= fileTextMaxRunes }

func (collector *fileTextCollector) String() string { return collector.builder.String() }

var ooxmlNumbered = regexp.MustCompile(`(\d+)\.xml$`)

// ooxmlText reads the words of an Office Open XML file: a document's body,
// headers and footers; a workbook's sheet names, shared strings and cells;
// a presentation's slides in their order.
func ooxmlText(archive *zip.Reader, kind string, collected *fileTextCollector) error {
	parts := map[string]*zip.File{}
	for _, file := range archive.File {
		parts[file.Name] = file
	}
	matching := func(prefix string) []string {
		names := []string{}
		for name := range parts {
			if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".xml") && !strings.Contains(name[len(prefix):], "/") {
				names = append(names, name)
			}
		}
		sort.Slice(names, func(left, right int) bool {
			a, b := ooxmlNumbered.FindStringSubmatch(names[left]), ooxmlNumbered.FindStringSubmatch(names[right])
			if a != nil && b != nil {
				x, _ := strconv.Atoi(a[1])
				y, _ := strconv.Atoi(b[1])
				if x != y {
					return x < y
				}
			}
			return names[left] < names[right]
		})
		return names
	}
	read := func(name string, text map[string]bool, line map[string]bool, attribute string) error {
		file, ok := parts[name]
		if !ok || collected.full() {
			return nil
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		defer reader.Close()
		return xmlText(io.LimitReader(reader, fileTextMaxPartBytes), text, line, attribute, collected)
	}
	switch kind {
	case "docx":
		names := append([]string{"word/document.xml"}, matching("word/header")...)
		for _, name := range append(names, matching("word/footer")...) {
			if err := read(name, map[string]bool{"t": true}, map[string]bool{"p": true, "tab": true, "br": true}, ""); err != nil {
				return err
			}
		}
	case "xlsx":
		if err := read("xl/workbook.xml", nil, nil, "name"); err != nil {
			return err
		}
		if err := read("xl/sharedStrings.xml", map[string]bool{"t": true}, map[string]bool{"si": true}, ""); err != nil {
			return err
		}
		for _, name := range matching("xl/worksheets/") {
			if err := read(name, map[string]bool{"t": true, "v": true}, map[string]bool{"c": true}, "t"); err != nil {
				return err
			}
		}
	case "pptx":
		for _, name := range matching("ppt/slides/") {
			if err := read(name, map[string]bool{"t": true}, map[string]bool{"p": true}, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// xmlText collects the character data of the elements named in text, ending
// a line at the elements named in line. With attribute "name" it takes the
// sheet names; with "t" it skips cells whose value is a shared string's
// number, the string itself being read from the shared strings.
func xmlText(reader io.Reader, text, line map[string]bool, attribute string, collected *fileTextCollector) error {
	decoder := xml.NewDecoder(reader)
	decoder.Strict = false
	current := strings.Builder{}
	inText, sharedCell := false, false
	flush := func() {
		collected.add(current.String())
		current.Reset()
	}
	for !collected.full() {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			name := element.Name.Local
			if attribute == "name" && name == "sheet" {
				for _, attr := range element.Attr {
					if attr.Name.Local == "name" {
						collected.add(attr.Value)
					}
				}
			}
			if attribute == "t" && name == "c" {
				sharedCell = false
				for _, attr := range element.Attr {
					if attr.Name.Local == "t" && attr.Value == "s" {
						sharedCell = true
					}
				}
			}
			inText = text[name] && !(name == "v" && sharedCell)
		case xml.EndElement:
			name := element.Name.Local
			if text[name] {
				inText = false
			}
			if line[name] {
				if name == "tab" {
					current.WriteByte(' ')
				} else {
					flush()
				}
			}
		case xml.CharData:
			if inText {
				current.Write(element)
				current.WriteByte(' ')
			}
		}
	}
	flush()
	return nil
}

const fileTextRunnerStoreKey = "leafen.fileTextRunner"

// notifyFileText wakes the reader of documents, as when a file finished
// uploading.
func notifyFileText(app core.App) {
	if runner, ok := app.Store().Get(fileTextRunnerStoreKey).(*fileTextRunner); ok && runner != nil {
		runner.notify()
	}
}

// fileTextRunner reads the documents not read yet, a few at a time, and
// has the pages that hold them indexed again.
type fileTextRunner struct {
	app  core.App
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func newFileTextRunner(app core.App) *fileTextRunner {
	return &fileTextRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *fileTextRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done = cancel, make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *fileTextRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify wakes the runner, as when a file finishes uploading.
func (runner *fileTextRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *fileTextRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		read, err := runFileTextTurn(ctx, runner.app)
		wait := fileTextIdleDelay
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			runner.app.Logger().Warn("leafen file text extraction failed", "error", err)
			wait = fileTextFailureDelay
		} else if read == fileTextBatch {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-runner.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// runFileTextTurn reads up to a batch of documents and answers how many.
func runFileTextTurn(ctx context.Context, app core.App) (int, error) {
	candidates := []struct {
		ID string `db:"id"`
	}{}
	params := dbx.Params{"pending": productFileUploadPendingDigest, "limit": fileTextBatch}
	if err := app.DB().NewQuery(`
		SELECT [[file]].[[id]] FROM {{leafen_files}} [[file]]
		LEFT JOIN {{leafen_file_text_extracts}} [[text]] ON [[text]].[[file_id]] = [[file]].[[id]]
		WHERE [[text]].[[file_id]] IS NULL AND [[file]].[[workspace]] != '' AND [[file]].[[sha256]] != {:pending}
			AND ` + fileTextCandidateSQL("[[file]]", params) + `
		ORDER BY [[file]].[[created]], [[file]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&candidates); err != nil {
		return 0, err
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := readFileText(app, candidate.ID); err != nil {
			return 0, err
		}
	}
	return len(candidates), nil
}

// readFileText reads one file's words, keeps them and has its pages
// indexed again.
func readFileText(app core.App, fileID string) error {
	record, err := app.FindRecordById(filesCollection, fileID)
	if err != nil {
		return nil
	}
	status, text, partial := fileTextStatusSkipped, "", ""
	kind := fileTextKind(record.GetString("content_type"), record.GetString("original_name"))
	size := int64(record.GetInt("size"))
	switch {
	case kind != "" && size > fileTextMaxSourceBytes:
		partial = fileTextPartialSize
	case kind != "" && size > 0:
		cut := false
		text, cut, err = readStoredFileText(app, record, kind)
		status = fileTextStatusDone
		if cut {
			partial = fileTextPartialLength
		}
		if err != nil {
			app.Logger().Info("leafen file text unreadable", "file", fileID, "kind", kind, "error", err)
			status, text, partial = fileTextStatusFailed, "", ""
		}
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_file_text_extracts}} ([[file_id]], [[workspace_id]], [[status]], [[text]], [[partial]], [[extracted_at]])
		VALUES ({:fileId}, {:workspaceId}, {:status}, {:text}, {:partial}, {:now})
		ON CONFLICT ([[file_id]]) DO NOTHING
	`).Bind(dbx.Params{"fileId": fileID, "workspaceId": record.GetString("workspace"), "status": status, "text": text,
		"partial": partial, "now": time.Now().UnixMilli()}).Execute(); err != nil {
		return err
	}
	if text == "" && partial == "" {
		return nil
	}
	// The pages holding the file in a block, and the database pages whose
	// file properties hold it.
	pages := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[page_id]] FROM {{leafen_blocks}}
		WHERE [[type]] IN ('file', 'pdf') AND json_extract([[content]], '$.fileId') = {:fileId}
	`).Bind(dbx.Params{"fileId": fileID}).Column(&pages); err != nil {
		return err
	}
	rows := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[stored]].[[row_id]] FROM {{leafen_database_row_values}} [[stored]]
		WHERE [[stored]].[[workspace_id]] = {:workspaceId} AND [[stored]].[[type]] = 'files'
			AND instr([[stored]].[[value]], {:fileId}) > 0 AND json_valid([[stored]].[[value]]) AND EXISTS (
				SELECT 1 FROM json_each([[stored]].[[value]]) AS [[attachment]]
				WHERE json_extract([[attachment]].[[value]], '$.fileId') = {:fileId})
	`).Bind(dbx.Params{"fileId": fileID, "workspaceId": record.GetString("workspace")}).Column(&rows); err != nil {
		return err
	}
	if len(rows) > 0 {
		if err := markDatabaseRowsSearchDirty(context.Background(), app, record.GetString("workspace"), rows); err != nil {
			return err
		}
	}
	for _, pageID := range pages {
		if err := markSearchDirtyByPageID(app, pageID); err != nil {
			return err
		}
	}
	return nil
}

// readStoredFileText copies a kept file to a temporary file, which the
// readers need to seek in, and reads it.
func readStoredFileText(app core.App, record *core.Record, kind string) (string, bool, error) {
	fsys, err := app.NewFilesystem()
	if err != nil {
		return "", false, err
	}
	defer fsys.Close()
	reader, err := fsys.GetReader(productFileStorageKey(record))
	if err != nil {
		return "", false, err
	}
	defer reader.Close()
	temporary, err := os.CreateTemp("", "leafen-file-text-")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	size, err := io.Copy(temporary, io.LimitReader(reader, fileTextMaxSourceBytes+1))
	if err != nil {
		return "", false, err
	}
	if size > fileTextMaxSourceBytes {
		return "", false, errors.New("the file is larger than its record says")
	}
	return extractFileText(kind, temporary, size)
}

// searchFileText is a file block's line and a document's words for a search
// body: the file block's name, as the page shows it, and its words, and
// whether they are only its beginning or none of it for its size.
type searchFileText struct {
	Name    string
	Text    string
	Partial bool
}

// searchFileTexts are a page's file blocks by block, for its search body: a
// file block's name, which Notion's search finds its page by (seen
// 2026-11-14: a file block named "….txt" found by its name, a PDF's or an
// image's file name not), and the words of a file or PDF block's document.
// A file whose words weren't read is still found by its name (2026-11-12:
// "docx" found nothing, though the page held plan.docx).
func searchFileTexts(app core.App, pageID string) (map[string]searchFileText, error) {
	rows := []struct {
		BlockID string `db:"block_id"`
		Name    string `db:"name"`
		Text    string `db:"text"`
		Partial string `db:"partial"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[block]].[[id]] AS [[block_id]],
			CASE WHEN [[block]].[[type]] = 'file' AND json_valid([[block]].[[content]])
				THEN COALESCE(json_extract([[block]].[[content]], '$.name'), '') ELSE '' END AS [[name]],
			COALESCE([[text]].[[text]], '') AS [[text]], COALESCE([[text]].[[partial]], '') AS [[partial]]
		FROM {{leafen_blocks}} [[block]]
		LEFT JOIN {{leafen_file_text_extracts}} [[text]] ON [[text]].[[file_id]] = json_extract([[block]].[[content]], '$.fileId')
			AND ([[text]].[[status]] = 'done' OR [[text]].[[partial]] != '')
		WHERE [[block]].[[page_id]] = {:pageId} AND [[block]].[[type]] IN ('file', 'pdf')
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows); err != nil {
		return nil, err
	}
	texts := make(map[string]searchFileText, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" && row.Text == "" && row.Partial == "" {
			continue
		}
		texts[row.BlockID] = searchFileText{Name: name, Text: row.Text, Partial: row.Partial != ""}
	}
	return texts, nil
}

// searchPropertyFileTexts are the words of the files a database page's
// properties hold, in the properties' order and each value's, leaving out
// properties the database's access rules keep from search.
func searchPropertyFileTexts(app core.App, rowID string, hidden func(propertyID string) bool) ([]searchFileText, error) {
	rows := []struct {
		PropertyID string `db:"property_id"`
		Text       string `db:"text"`
		Partial    string `db:"partial"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[value]].[[property_id]], [[text]].[[text]], [[text]].[[partial]]
		FROM {{leafen_database_row_values}} [[value]]
		JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]] = [[value]].[[property_id]]
		JOIN json_each(CASE WHEN json_valid([[value]].[[value]]) THEN [[value]].[[value]] ELSE '[]' END) AS [[attachment]]
		JOIN {{leafen_file_text_extracts}} [[text]] ON [[text]].[[file_id]] = json_extract([[attachment]].[[value]], '$.fileId')
		WHERE [[value]].[[row_id]] = {:rowId} AND [[value]].[[type]] = 'files'
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
			AND ([[text]].[[status]] = 'done' OR [[text]].[[partial]] != '')
		ORDER BY [[property]].[[position]], [[property]].[[id]], [[attachment]].[[key]]
	`).Bind(dbx.Params{"rowId": rowID}).All(&rows); err != nil {
		return nil, err
	}
	texts := make([]searchFileText, 0, len(rows))
	for _, row := range rows {
		if !hidden(row.PropertyID) {
			texts = append(texts, searchFileText{Text: row.Text, Partial: row.Partial != ""})
		}
	}
	return texts, nil
}
