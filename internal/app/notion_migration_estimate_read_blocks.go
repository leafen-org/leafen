package app

import (
	"math"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Once a run has read Notion it knows every block it read, as Notion counts
// them (notion_import_counted.go): the blocks it makes, and those it asks for
// their comments, are what it read from then on, not the scan's reckoning
// from its sample (notionMigrationEstimateOf), so the time left of making and
// the comments goes by them before those stages begin — not only once the
// blocks' comments are counted, when it jumped (2026-11-20: 402 blocks
// reckoned, 761 asked, and the time left went from 2 minutes to 16).

// notionMigrationReadBlocks puts the blocks a run read into its estimate: its
// blocks, and its blocks to ask in the same proportion (a reading of the
// person's Notion may have named only some to ask).
func notionMigrationReadBlocks(app core.App, jobID string, estimate *notionMigrationEstimate) error {
	if estimate.Blocks <= 0 {
		return nil
	}
	read := 0
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND (` + notionImportCountedKindSQL + `) = 'block'
	`).Bind(notionImportCountedParams(dbx.Params{"jobId": jobID})).Row(&read); err != nil || read <= 0 {
		return err
	}
	estimate.CommentBlocks = int(math.Round(float64(estimate.CommentBlocks) * float64(read) / float64(estimate.Blocks)))
	estimate.Blocks = read
	return nil
}
