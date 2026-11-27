package app

import (
	"encoding/json"
	"time"
	"unicode/utf16"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A thread on a media block's caption words marks them as one on a block's
// text does (Notion's, seen 2026-11-09): the caption's runs, which the page's
// block structure holds rather than its collaborative text, take the
// thread's id in their commentId. The change is the server's, recorded so
// open pages take it up, and leaves the block's last editor as it was.

// commentCaptionMarks reads and marks the captions of one page's blocks, its
// structure read once.
type commentCaptionMarks struct {
	pageID string
	batch  *structuralMutationBatch
	view   *structuralMetadataView
}

func (marks *commentCaptionMarks) load(app core.App) error {
	if marks.batch != nil {
		return nil
	}
	batch, err := loadStructuralMutationBatch(app, marks.pageID, map[string]*structuralMutationBatch{})
	if err != nil {
		return err
	}
	marks.batch, marks.view = batch, newStructuralMetadataView(batch.doc)
	return nil
}

// captionRuns is a block's caption runs, copied so marking them leaves the
// document's own as they are, and the block's other parts; none for a block
// the page doesn't hold.
func (marks *commentCaptionMarks) captionRuns(app core.App, blockID string) ([]map[string]any, structuralBlockMetadata, bool, error) {
	return marks.fieldRuns(app, blockID, 0)
}

// fieldRuns is a block's caption runs, or with cell its table row's runs in
// that cell (from 1), as captionRuns reads a caption's.
func (marks *commentCaptionMarks) fieldRuns(app core.App, blockID string, cell int) ([]map[string]any, structuralBlockMetadata, bool, error) {
	if err := marks.load(app); err != nil {
		return nil, structuralBlockMetadata{}, false, err
	}
	entry, found, err := marks.view.get(blockID)
	if err != nil || !found {
		return nil, entry, false, err
	}
	encoded, err := json.Marshal(entry.Content)
	if err != nil {
		return nil, entry, false, err
	}
	content := map[string]any{}
	if err := json.Unmarshal(encoded, &content); err != nil {
		return nil, entry, false, err
	}
	entry.Content = content
	listed, _ := content["caption"].([]any)
	if cell > 0 {
		listed = commentTableCellRuns(content, cell)
	}
	runs := make([]map[string]any, 0, len(listed))
	for _, value := range listed {
		run, isRun := value.(map[string]any)
		if _, hasText := run["text"].(string); !isRun || !hasText {
			return nil, entry, false, nil
		}
		runs = append(runs, run)
	}
	return runs, entry, true, nil
}

// units is a caption's text in UTF-16 units, or nil where the place can't
// be marked: a block without that caption, or an end inside a mention or an
// equation, which is one piece of text.
func (marks *commentCaptionMarks) units(app core.App, place commentTextPlace) ([]uint16, error) {
	runs, _, found, err := marks.fieldRuns(app, place.blockID, place.cell)
	if err != nil || !found {
		return nil, err
	}
	units := []uint16{}
	for _, run := range runs {
		text := utf16.Encode([]rune(run["text"].(string)))
		from, to := len(units), len(units)+len(text)
		if captionRunWhole(run) && (from < place.start && place.start < to || from < place.end && place.end < to) {
			return nil, nil
		}
		units = append(units, text...)
	}
	return units, nil
}

// captionRunWhole is whether a run is one piece that can't be cut.
func captionRunWhole(run map[string]any) bool {
	mention, _ := run["mention"].(string)
	equation, _ := run["equation"].(string)
	return mention != "" || equation != ""
}

// markedCaptionRuns adds a thread to the runs over [start, end), cutting the
// runs it begins or ends in.
func markedCaptionRuns(runs []map[string]any, start, end int, threadID string) []any {
	marked := []any{}
	at := 0
	for _, run := range runs {
		text := utf16.Encode([]rune(run["text"].(string)))
		from, to := at, at+len(text)
		at = to
		cuts := []int{from, min(max(start, from), to), min(max(end, from), to), to}
		for index := 0; index+1 < len(cuts); index++ {
			left, right := cuts[index], cuts[index+1]
			if left >= right {
				continue
			}
			piece := map[string]any{}
			for key, value := range run {
				piece[key] = value
			}
			piece["text"] = string(utf16.Decode(text[left-from : right-from]))
			if left >= start && right <= end {
				existing, _ := run["commentId"].(string)
				piece["commentId"] = commentThreadsWith(existing, threadID)
			}
			marked = append(marked, piece)
		}
	}
	return marked
}

// commentCaptionChange is a change to one block's caption runs: their new
// runs, or none when they stay as they are.
type commentCaptionChange struct {
	blockID string
	cell    int
	runs    func([]map[string]any) ([]any, bool)
}

// mark marks the thread's caption places.
func (marks *commentCaptionMarks) mark(
	app core.App, authorization commentAuthorization, actorID string, places []commentTextPlace, threadID string,
) error {
	changes := []commentCaptionChange{}
	for _, place := range places {
		if place.caption || place.cell > 0 {
			changes = append(changes, commentCaptionChange{blockID: place.blockID, cell: place.cell, runs: func(runs []map[string]any) ([]any, bool) {
				return markedCaptionRuns(runs, place.start, place.end, threadID), true
			}})
		}
	}
	return marks.write(app, authorization, actorID, "comment-anchor:"+threadID, changes)
}

// write changes captions and keeps the change, as a page's blocks the
// server changes are kept: in its structure, its stored blocks and the
// changes its open pages read. A block the page no longer holds is left.
func (marks *commentCaptionMarks) write(
	app core.App, authorization commentAuthorization, actorID, changeKey string, changes []commentCaptionChange,
) error {
	changed := false
	for _, change := range changes {
		runs, entry, found, err := marks.fieldRuns(app, change.blockID, change.cell)
		if err != nil {
			return err
		}
		next, write := change.runs(runs)
		if !found || !write {
			continue
		}
		if change.cell > 0 {
			commentTableCellRunsSet(entry.Content, change.cell, next)
		} else {
			entry.Content["caption"] = next
		}
		encoded, err := json.Marshal(entry.Content)
		if err != nil {
			return err
		}
		marks.batch.doc.Transact(func(txn *crdt.Transaction) {
			txn.GetMap(ygoBlockMetadataRoot).Set(txn, change.blockID, map[string]any{
				"parentId": entry.ParentID, "type": entry.Type, "content": entry.Content,
			})
		})
		marks.view.set(change.blockID, entry)
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_blocks}} SET [[content]] = {:content}, [[updated]] = {:updated}
			WHERE [[page_id]] = {:pageId} AND [[id]] = {:id}
		`).Bind(dbx.Params{
			"content": string(encoded), "updated": time.Now().UTC().UnixMilli(),
			"pageId": marks.pageID, "id": change.blockID,
		}).Execute(); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	page, err := app.FindRecordById(pagesCollection, marks.pageID)
	if err != nil {
		return err
	}
	if err := saveServerStructuralBatch(app, page.GetString("owner"), marks.pageID, actorID, changeKey, marks.batch, nil); err != nil {
		return err
	}
	if authorization.Target.Kind == commentTargetDatabaseRow {
		return noteDatabaseRowBodyMarks(app, marks.pageID)
	}
	return nil
}
