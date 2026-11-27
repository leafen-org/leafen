package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedNotionApplyWindowSource struct {
	Sequence       int64  `db:"sequence"`
	SourceID       string `db:"source_id"`
	Kind           string `db:"kind"`
	ParentSourceID string `db:"parent_source_id"`
	ParentKind     string `db:"parent_kind"`
	Position       int    `db:"position"`
	Title          string `db:"title"`
	Text           string `db:"text"`
	CandidateCount int    `db:"candidate_count"`
}

type notionApplySourceWindow struct {
	Sources        []storedNotionSource
	CandidateCount int
}

// loadNotionApplySourceWindow loads up to items of what the job read after
// cursor, within one change's bytes.
func loadNotionApplySourceWindow(app core.App, jobID string, cursor int64, items int) (notionApplySourceWindow, error) {
	stored := []storedNotionApplyWindowSource{}
	err := app.DB().NewQuery(`
		WITH [[candidate_base]] AS (
			SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]],
				[[position]], [[title]], [[text]],
				length(CAST([[source_id]] AS BLOB)) +
				length(CAST([[kind]] AS BLOB)) +
				length(CAST([[parent_source_id]] AS BLOB)) +
				length(CAST([[parent_kind]] AS BLOB)) +
				length(CAST([[title]] AS BLOB)) +
				length(CAST([[text]] AS BLOB)) AS [[source_bytes]]
			FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]] = {:jobId} AND [[sequence]] > {:cursor}
			ORDER BY [[sequence]]
			LIMIT {:limit}
		), [[candidates]] AS (
			SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]],
				[[position]], [[title]], [[text]], [[source_bytes]],
				ROW_NUMBER() OVER (ORDER BY [[sequence]]) AS [[apply_index]],
				SUM([[source_bytes]]) OVER (
					ORDER BY [[sequence]] ROWS UNBOUNDED PRECEDING
				) AS [[cumulative_source_bytes]],
				COUNT(*) OVER () AS [[candidate_count]]
			FROM [[candidate_base]]
		)
		SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]],
			[[position]], [[title]], [[text]], [[candidate_count]]
		FROM [[candidates]]
		WHERE [[apply_index]] = 1 OR (
			[[apply_index]] <= {:maxItems}
			AND [[cumulative_source_bytes]] <= {:maxBytes}
		)
		ORDER BY [[sequence]]
	`).Bind(dbx.Params{
		"jobId": jobID, "cursor": cursor,
		"limit": items + 1, "maxItems": items,
		"maxBytes": maxGraphChangeBytes,
	}).All(&stored)
	if err != nil {
		return notionApplySourceWindow{}, err
	}
	window := notionApplySourceWindow{Sources: make([]storedNotionSource, 0, len(stored))}
	for _, source := range stored {
		window.CandidateCount = source.CandidateCount
		window.Sources = append(window.Sources, storedNotionSource{
			Sequence:       source.Sequence,
			SourceID:       source.SourceID,
			Kind:           source.Kind,
			ParentSourceID: source.ParentSourceID,
			ParentKind:     source.ParentKind,
			Position:       source.Position,
			Title:          source.Title,
			Text:           source.Text,
		})
	}
	return window, nil
}

func loadNotionApplyPageCollection(
	app core.App,
	sources []storedNotionSource,
	mappings *notionApplyMappings,
) (*core.Collection, error) {
	for _, source := range sources {
		if source.Kind != "page" {
			continue
		}
		if _, found := mappings.nativeID(source.SourceID, "page"); found {
			continue
		}
		return app.FindCollectionByNameOrId(pagesCollection)
	}
	return nil, nil
}
