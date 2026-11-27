package app

import (
	"encoding/json"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where the source moved a block to another page (2026-10-15: a block moved
// to another page in Notion stayed on its old page in Leafen after the run
// again). A run again sends a block the source moved to another page there,
// as a person's move would — with what is inside it, its files and
// discussions — when nobody moved it in Leafen since: it lands after the
// nearest block before it in the source that stands at the top of that page,
// or right after the top block holding where the source put it. The moves go
// one after another once the run has put every page's blocks in order, since
// a page waits while a block moves from or to it. The run after puts a block
// that arrived in its place there, inside a block too, unless somebody moved
// it since. A block somebody moved to another page in Leafen stays where
// they put it.

// rehomeNotionMigrationBlocks tells the ledger the page each of the
// migration's blocks stands on now, where that changed: a block a run sent
// there arrived, and one somebody moved starts over from where it is.
func rehomeNotionMigrationBlocks(app core.App, migrationID string) error {
	moved := []struct {
		SourceID string `db:"source_id"`
		Place    string `db:"place"`
		PageID   string `db:"block_page_id"`
		ParentID string `db:"block_parent_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[ledger]].[[source_id]], [[ledger]].[[place]], [[block]].[[page_id]] AS [[block_page_id]],
			[[block]].[[parent_id]] AS [[block_parent_id]]
		FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
		JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[ledger]].[[native_id]]
		WHERE [[ledger]].[[migration_id]]={:migrationId} AND [[ledger]].[[kind]]='block'
			AND [[ledger]].[[page_id]]!='' AND [[block]].[[page_id]]!=[[ledger]].[[page_id]]
	`).Bind(dbx.Params{"migrationId": migrationID}).All(&moved); err != nil {
		return err
	}
	for _, block := range moved {
		before := notionMigrationBlockPlace{}
		_ = json.Unmarshal([]byte(block.Place), &before)
		place := ""
		if before.MovingTo != "" && before.MovingTo == block.PageID {
			encoded, err := json.Marshal(notionMigrationBlockPlace{Parent: block.ParentID, Arrived: true})
			if err != nil {
				return err
			}
			place = string(encoded)
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_migration_ledger}} SET [[page_id]]={:pageId}, [[place]]={:place}
			WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]='block'
		`).Bind(dbx.Params{"pageId": block.PageID, "place": place, "migrationId": migrationID, "sourceId": block.SourceID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// markNotionMigrationBlocksLeaving keeps in the ledger which of a page's
// blocks the source moved to other pages, for the moves after every page.
func markNotionMigrationBlocksLeaving(app core.App, migrationID string, page *notionMigrationPagePlaces, away []string) error {
	for _, id := range away {
		entry := page.ledger[id]
		place := notionMigrationBlockPlace{}
		_ = json.Unmarshal([]byte(entry.Place), &place)
		place.Leaving = true
		if err := setNotionMigrationBlockPlace(app, migrationID, entry.SourceID, place); err != nil {
			return err
		}
	}
	return nil
}

// sendNotionMigrationBlocksAway starts the moves of the blocks the source
// moved to other pages, one after another in the source's order, and
// answers how many it started.
func sendNotionMigrationBlocksAway(app core.App, job storedJob, payload *notionImportJobPayload) (int, error) {
	pages := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[page_id]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='block' AND [[place]] LIKE '%"leaving":true%'
		ORDER BY [[page_id]]
	`).Bind(dbx.Params{"migrationId": payload.MigrationID}).Column(&pages); err != nil {
		return 0, err
	}
	type leaving struct {
		sourceID string
		place    notionMigrationBlockPlace
		staged   notionMigrationStagedPlace
		move     blockMoveReturn
	}
	blocks := []leaving{}
	for _, pageID := range pages {
		page, err := loadNotionMigrationPagePlaces(app, payload.AuthorityOwnerID, job.ID, payload.MigrationID, pageID)
		if err != nil {
			return 0, err
		}
		for nativeID, entry := range page.ledger {
			place := notionMigrationBlockPlace{}
			if json.Unmarshal([]byte(entry.Place), &place) != nil || !place.Leaving {
				continue
			}
			place.Leaving = false
			next := leaving{sourceID: entry.SourceID, place: place, staged: page.staged[nativeID]}
			if _, standing := page.parents[nativeID]; standing {
				destination, after, before, err := notionMigrationAwaySpot(app, job.ID, next.staged)
				if err != nil {
					return 0, err
				}
				if destination != "" && destination != pageID {
					next.move = blockMoveReturn{
						SourcePageID: pageID, BlockID: nativeID, After: after, AfterSource: before, DestinationPageID: destination,
					}
				}
			}
			blocks = append(blocks, next)
		}
	}
	sort.Slice(blocks, func(i, j int) bool {
		a, b := blocks[i].staged, blocks[j].staged
		if a.parent() != b.parent() {
			return a.parent() < b.parent()
		}
		return a.Position < b.Position
	})
	returns := []blockMoveReturn{}
	for _, block := range blocks {
		if block.move.BlockID != "" {
			returns = append(returns, block.move)
		}
	}
	admitted := false
	if len(returns) > 0 {
		var err error
		admitted, err = admitBlockMoveReturns(app, job.Owner, returns[0].DestinationPageID, "notion-migration-away-"+job.ID, returns)
		if err != nil {
			return 0, err
		}
	}
	for _, block := range blocks {
		if admitted && block.move.BlockID != "" {
			block.place.MovingTo = block.move.DestinationPageID
		}
		if err := setNotionMigrationBlockPlace(app, payload.MigrationID, block.sourceID, block.place); err != nil {
			return 0, err
		}
	}
	if !admitted {
		return 0, nil
	}
	return len(returns), nil
}

func setNotionMigrationBlockPlace(app core.App, migrationID, sourceID string, place notionMigrationBlockPlace) error {
	encoded, err := json.Marshal(place)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_notion_migration_ledger}} SET [[place]]={:place}
		WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]='block'
	`).Bind(dbx.Params{"place": string(encoded), "migrationId": migrationID, "sourceId": sourceID}).Execute()
	return err
}

// notionMigrationAwaySpot is the page a block's source parent stands on now,
// and where there it goes: right after the top block holding its parent, or
// after the nearest of the blocks before it in the source that stands at
// the top there by the time the move runs (notionMigrationSiblingsBefore).
func notionMigrationAwaySpot(
	app core.App, jobID string, staged notionMigrationStagedPlace,
) (string, []string, *notionMigrationSiblingsBefore, error) {
	var native []string
	kinds := []string{"page", "row"}
	if staged.ParentKind == "block" {
		kinds = []string{"block"}
	}
	encodedKinds, err := json.Marshal(kinds)
	if err != nil {
		return "", nil, nil, err
	}
	if err := app.DB().NewQuery(`
		SELECT [[native_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]] IN (SELECT [[value]] FROM json_each({:kinds})) LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": staged.ParentSourceID, "kinds": string(encodedKinds)}).Column(&native); err != nil || len(native) == 0 {
		return "", nil, nil, err
	}
	if staged.ParentKind == "block" {
		// Right after the top block holding its new parent, however deep;
		// each block is climbed once, so a chain that comes back on itself ends.
		holder := []struct {
			ID       string `db:"id"`
			PageID   string `db:"page_id"`
			ParentID string `db:"parent_id"`
		}{}
		climbed := map[string]bool{}
		for id := native[0]; id != "" && !climbed[id]; {
			climbed[id] = true
			holder = holder[:0]
			if err := app.DB().NewQuery(`SELECT [[id]],[[page_id]],[[parent_id]] FROM {{leafen_blocks}} WHERE [[id]]={:id}`).
				Bind(dbx.Params{"id": id}).All(&holder); err != nil || len(holder) == 0 {
				return "", nil, nil, err
			}
			if holder[0].ParentID == "" {
				return holder[0].PageID, []string{holder[0].ID}, nil, nil
			}
			id = holder[0].ParentID
		}
		return "", nil, nil, nil
	}
	return native[0], nil, &notionMigrationSiblingsBefore{
		JobID: jobID, ParentID: staged.ParentSourceID, ParentKind: staged.ParentKind, Position: staged.Position,
	}, nil
}

// notionMigrationSiblingsBefore names the blocks before a block in its source
// parent, which a move of it away from its page puts it after. They are read
// when the move runs, nearest first, a window at a time to the first one, so
// those moved there before it count, and however many before it are gone.
type notionMigrationSiblingsBefore struct {
	JobID      string `json:"jobId"`
	ParentID   string `json:"parentId"`
	ParentKind string `json:"parentKind"`
	Position   int    `json:"position"`
}

const notionMigrationSiblingWindow = 50

// nearestIn is the nearest of the blocks before it standing in order (a
// page's top blocks), and whether one does.
func (before notionMigrationSiblingsBefore) nearestIn(app core.App, order []string) (string, bool, error) {
	standing := make(map[string]bool, len(order))
	for _, id := range order {
		standing[id] = true
	}
	for position := before.Position; ; {
		siblings := []struct {
			SourceID string `db:"source_id"`
			Position int    `db:"position"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[source_id]], [[position]] FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[parent_source_id]]={:parent} AND [[parent_kind]]={:parentKind}
				AND [[position]]<{:position}
			ORDER BY [[position]] DESC LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": before.JobID, "parent": before.ParentID, "parentKind": before.ParentKind,
			"position": position, "limit": notionMigrationSiblingWindow,
		}).All(&siblings); err != nil {
			return "", false, err
		}
		for _, sibling := range siblings {
			native := []string{}
			if err := app.DB().NewQuery(`
				SELECT [[native_id]] FROM {{leafen_notion_import_mappings}}
				WHERE [[job_id]]={:jobId} AND [[source_id]] IN ({:sourceId}, {:sourceId} || ':block') AND [[kind]] IN ('block','page')
			`).Bind(dbx.Params{"jobId": before.JobID, "sourceId": sibling.SourceID}).Column(&native); err != nil {
				return "", false, err
			}
			for _, id := range native {
				if standing[id] {
					return id, true, nil
				}
			}
		}
		if len(siblings) < notionMigrationSiblingWindow {
			return "", false, nil
		}
		position = siblings[len(siblings)-1].Position
	}
}
