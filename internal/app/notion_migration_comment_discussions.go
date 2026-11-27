package app

import (
	"encoding/json"
	"slices"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's API lists a block's comments by the block alone and says nothing
// of which blocks have any, so a run asked every block (2026-11-16: 11,300
// blocks one by one, over an hour, with comments on a handful). The reading
// of the person's Notion (notion_migration_formats_read.go) sees each
// block's discussions as it loads a page's or row's body; it hands in the
// bodies it loaded whole and their blocks with a discussion still open (or
// one it couldn't tell) — the API gives no resolved one. The run then asks,
// in those bodies, only those blocks, and every block of a body the reading
// didn't load whole. A comment made after the reading is in a block it
// didn't name: read again before the run when Notion changed (the guide's
// part 2).

// notionMigrationDiscussions is a reading's bodies loaded whole (pages and
// rows, by Notion id) and the blocks in them with comments; and, of the
// discussions it saw on them, how many are resolved by the page, row or block
// they are on (nil: a reading from before it counted them) and how many it
// couldn't tell.
type notionMigrationDiscussions struct {
	Bodies   []string       `json:"bodies"`
	Blocks   []string       `json:"blocks"`
	Resolved map[string]int `json:"resolved"`
	Unknown  int            `json:"unknown,omitzero"`
}

// notionMigrationDiscussionsKept is a reading's discussions as kept: Notion's
// ids in one form, each once, within the reading's bound.
func notionMigrationDiscussionsKept(read *notionMigrationDiscussions) *notionMigrationDiscussions {
	if read == nil {
		return nil
	}
	clean := func(ids []string) []string {
		kept := make([]string, 0, len(ids))
		for _, id := range ids {
			if id = canonicalNotionID(id); id != "" {
				kept = append(kept, id)
			}
		}
		slices.Sort(kept)
		kept = slices.Compact(kept)
		if len(kept) > notionMigrationFormatsMostItems {
			kept = kept[:notionMigrationFormatsMostItems]
		}
		return kept
	}
	kept := &notionMigrationDiscussions{Bodies: clean(read.Bodies), Blocks: clean(read.Blocks), Unknown: max(0, read.Unknown)}
	if read.Resolved != nil {
		kept.Resolved = map[string]int{}
		for id, count := range read.Resolved {
			if id = canonicalNotionID(id); id != "" && count > 0 && len(kept.Resolved) < notionMigrationFormatsMostItems {
				kept.Resolved[id] += count
			}
		}
	}
	return kept
}

// notionMigrationDiscussionsSQL keeps, of a run's blocks, those to ask for
// comments: with {:narrow} 0 every one; otherwise those the reading named
// ({:asked}) and those of a body it didn't load whole ({:bodies}).
const notionMigrationDiscussionsSQL = `
			AND ({:narrow} = 0 OR [[mapping]].[[source_id]] IN (SELECT [[value]] FROM json_each({:asked}))
				OR NOT EXISTS (SELECT 1 FROM {{leafen_notion_import_mappings}} AS [[holder]]
					WHERE [[holder]].[[job_id]]={:jobId} AND [[holder]].[[kind]] IN ('page', 'row')
						AND [[holder]].[[native_id]]=[[block]].[[page_id]]
						AND [[holder]].[[source_id]] IN (SELECT [[value]] FROM json_each({:bodies}))))`

// notionMigrationCommentAsking is what notionMigrationDiscussionsSQL binds
// for a migration: whether a reading narrows the asking, and its sets.
func notionMigrationCommentAsking(app core.App, migrationID string) (dbx.Params, error) {
	params := dbx.Params{"narrow": 0, "asked": "[]", "bodies": "[]"}
	reader, found, err := loadNotionMigrationFormatsReader(app, migrationID)
	if err != nil || !found || reader.Discussions == nil {
		return params, err
	}
	asked, err := json.Marshal(reader.Discussions.Blocks)
	if err != nil {
		return nil, err
	}
	bodies, err := json.Marshal(reader.Discussions.Bodies)
	if err != nil {
		return nil, err
	}
	params["narrow"], params["asked"], params["bodies"] = 1, string(asked), string(bodies)
	return params, nil
}

// notionMigrationReadDiscussions is, for the estimate, how many blocks a
// reading named with comments and how many bodies it loaded whole; ok false
// without a reading of them.
func notionMigrationReadDiscussions(app core.App, migrationID string) (blocks, bodies int, ok bool) {
	reader, found, err := loadNotionMigrationFormatsReader(app, migrationID)
	if err != nil || !found || reader.Discussions == nil {
		return 0, 0, false
	}
	return len(reader.Discussions.Blocks), len(reader.Discussions.Bodies), true
}

// notionMigrationResolvedLeft is what a run's report says of the resolved
// discussions on what the run moved: Notion's API never gives them (seen
// 2026-11-16 — of a synthetic page's discussions on the page, a row, a row's
// block and two blocks, it gave only the one not resolved), so a reading of
// the person's Notion counts them. Counted is false without such a reading;
// Partial when the reading couldn't read some pages or discussions, so there
// may be more.
type notionMigrationResolvedLeft struct {
	Counted bool
	Count   int
	Partial bool
}

func loadNotionMigrationResolvedLeft(app core.App, jobID, migrationID string) (notionMigrationResolvedLeft, error) {
	left := notionMigrationResolvedLeft{}
	reader, found, err := loadNotionMigrationFormatsReader(app, migrationID)
	if err != nil || !found || reader.Discussions == nil || reader.Discussions.Resolved == nil {
		return left, err
	}
	left.Counted, left.Partial = true, reader.Discussions.Unknown > 0
	for _, unread := range reader.Unread {
		left.Partial = left.Partial || unread.Kind != "view"
	}
	if len(reader.Discussions.Resolved) == 0 {
		return left, nil
	}
	ids := make([]string, 0, len(reader.Discussions.Resolved))
	for id := range reader.Discussions.Resolved {
		ids = append(ids, id)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return left, err
	}
	moved := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[source_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]] IN ('page','row','block')
			AND [[source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"jobId": jobID, "ids": string(encoded)}).Column(&moved); err != nil {
		return left, err
	}
	for _, id := range moved {
		left.Count += reader.Discussions.Resolved[id]
	}
	return left, nil
}
