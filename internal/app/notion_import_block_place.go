package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where a new block goes in its parent (2026-10-13: a line put between two
// others in Notion, or in a bundle's source, landed at the page's end when
// the migration ran again). An import adds in the source's order, so a new
// block follows its parent's last one. A migration's run starts from what
// earlier runs made (notion_migration_rerun.go), so a block new to it goes
// after the nearest one before it in the source that a run made and that
// still stands in the same parent, or first when there is none; a block
// somebody added in Leafen right after that one stays after the new block.
// A block a run made keeps its place, wherever the source moved it.

// notionImportBlockPlaceWindow is how many of a block's earlier siblings in
// the source one read looks at.
const notionImportBlockPlaceWindow = 64

// placeNotionImportBlock gives the block the run makes its place in parent:
// the block it follows, or nil with the block marked to go first.
func placeNotionImportBlock(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	batch *structuralMutationBatch,
	parentID, blockID string,
	mappings *notionApplyMappings,
) (*string, error) {
	if payload.MigrationID == "" {
		return notionAppendAfter(batch, parentID)
	}
	order, err := structuralOrder(batch.doc, parentID)
	if err != nil || len(order) == 0 {
		return nil, err
	}
	standing := make(map[string]struct{}, len(order))
	for _, id := range order {
		standing[id] = struct{}{}
	}
	// A parent's children follow it in the staged sources, in their order.
	cursor := source.Sequence
	for {
		earlier := []struct {
			Sequence int64  `db:"sequence"`
			SourceID string `db:"source_id"`
			Kind     string `db:"kind"`
			NativeID string `db:"native_id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[sibling]].[[sequence]], [[sibling]].[[source_id]], [[sibling]].[[kind]],
				COALESCE([[mapping]].[[native_id]], '') AS [[native_id]]
			FROM {{leafen_notion_import_sources}} AS [[sibling]]
			LEFT JOIN {{leafen_notion_import_mappings}} AS [[mapping]]
				ON [[mapping]].[[job_id]] = [[sibling]].[[job_id]]
				AND [[mapping]].[[kind]] = CASE WHEN [[sibling]].[[kind]] = 'page' THEN 'page' ELSE 'block' END
				AND [[mapping]].[[source_id]] = CASE WHEN [[sibling]].[[kind]] = 'database'
					THEN [[sibling]].[[source_id]] || ':block' ELSE [[sibling]].[[source_id]] END
			WHERE [[sibling]].[[job_id]] = {:jobId} AND [[sibling]].[[sequence]] < {:cursor}
				AND [[sibling]].[[sequence]] > COALESCE((
					SELECT [[parent]].[[sequence]] FROM {{leafen_notion_import_sources}} AS [[parent]]
					WHERE [[parent]].[[job_id]] = {:jobId} AND [[parent]].[[source_id]] = {:parentSourceId}
				), 0)
				AND [[sibling]].[[parent_source_id]] = {:parentSourceId}
				AND [[sibling]].[[parent_kind]] = {:parentKind}
				AND [[sibling]].[[position]] < {:position}
			ORDER BY [[sibling]].[[sequence]] DESC
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": job.ID, "cursor": cursor, "parentSourceId": source.ParentSourceID,
			"parentKind": source.ParentKind, "position": source.Position, "limit": notionImportBlockPlaceWindow,
		}).All(&earlier); err != nil {
			return nil, err
		}
		for _, sibling := range earlier {
			nativeID := sibling.NativeID
			if nativeID == "" {
				// One this turn made is not written down yet.
				switch sibling.Kind {
				case "page":
					nativeID, _ = mappings.nativeID(sibling.SourceID, "page")
				case "database":
					nativeID, _ = mappings.nativeID(sibling.SourceID+":block", "block")
				default:
					nativeID, _ = mappings.nativeID(sibling.SourceID, "block")
				}
			}
			if _, found := standing[nativeID]; found && nativeID != "" {
				return &nativeID, nil
			}
		}
		if len(earlier) < notionImportBlockPlaceWindow {
			break
		}
		cursor = earlier[len(earlier)-1].Sequence
	}
	if batch.starts == nil {
		batch.starts = map[string]struct{}{}
	}
	batch.starts[blockID] = struct{}{}
	return nil, nil
}
