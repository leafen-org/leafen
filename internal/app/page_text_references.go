package app

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's text lives in its collaborative document, so the page mentions
// and page links typed there reach the page reference table through the
// page's text projection: each block's text targets are kept beside the
// block, and the reference table holds a block's content targets and text
// targets together, as its triggers rebuild them on every block change.

// linkedPageID reads the page a link opens, in the forms the reference table
// reads: an app link or a /p/ path.
func linkedPageID(link string) string {
	link = strings.TrimSpace(link)
	remainder := ""
	if prefix := "leafen://page/"; len(link) >= len(prefix) && strings.EqualFold(link[:len(prefix)], prefix) {
		remainder = link[len(prefix):]
	}
	if remainder == "" {
		index := strings.Index(link, "/p/")
		if upper := strings.Index(link, "/P/"); upper >= 0 && (index < 0 || upper < index) {
			index = upper
		}
		if index >= 0 {
			remainder = link[index+3:]
		}
	}
	if end := strings.IndexAny(remainder, "/?#"); end >= 0 {
		remainder = remainder[:end]
	}
	remainder = strings.TrimSpace(remainder)
	// A page's public ID, as page Markdown keeps a link at the app's address
	// (notionMarkdownAppAddress), names the page whose ID it encodes.
	if _, public := normalizeNotionUUIDRequest(remainder); public {
		if pageID, err := resolvePageOwnPublicID(remainder); err == nil {
			return pageID
		}
	}
	return remainder
}

// blockTextPageTargets lists the pages one block's text mentions or links to.
func blockTextPageTargets(text *crdt.YText) []string {
	if text == nil {
		return nil
	}
	targets := []string{}
	for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
		if delta.Op != crdt.DeltaOpInsert {
			continue
		}
		target := ""
		if mention, _ := delta.Attributes["mention"].(string); mention == "page" {
			target, _ = delta.Attributes["pageId"].(string)
			target = strings.TrimSpace(target)
		} else if link, _ := delta.Attributes["link"].(string); link != "" && mention == "" {
			target = linkedPageID(link)
		}
		if target != "" && !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	sort.Strings(targets)
	return targets
}

// projectPageTextReferences keeps the text targets of a page's blocks, and
// rebuilds the reference table rows of each block whose targets changed.
func projectPageTextReferences(ctx context.Context, app core.App, pageID string, targets map[string][]string) error {
	stored := []struct {
		BlockID string `db:"block_id"`
		Target  string `db:"target_page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[text]].[[block_id]], [[text]].[[target_page_id]]
		FROM {{leafen_block_text_page_references}} [[text]]
		JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[text]].[[block_id]]
		WHERE [[block]].[[page_id]] = {:pageId}
		ORDER BY [[text]].[[block_id]], [[text]].[[target_page_id]]
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID}).All(&stored); err != nil {
		return err
	}
	previous := map[string][]string{}
	for _, row := range stored {
		previous[row.BlockID] = append(previous[row.BlockID], row.Target)
	}
	changed := []string{}
	for blockID, list := range targets {
		if !slices.Equal(previous[blockID], list) {
			changed = append(changed, blockID)
		}
	}
	for blockID := range previous {
		if _, kept := targets[blockID]; !kept {
			changed = append(changed, blockID)
		}
	}
	sort.Strings(changed)
	for _, blockID := range changed {
		params := dbx.Params{"blockId": blockID}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_block_text_page_references}} WHERE [[block_id]] = {:blockId}
		`).WithContext(ctx).Bind(params).Execute(); err != nil {
			return err
		}
		for _, target := range targets[blockID] {
			if _, err := app.DB().NewQuery(`
				INSERT OR IGNORE INTO {{leafen_block_text_page_references}} ([[block_id]], [[target_page_id]])
				VALUES ({:blockId}, {:target})
			`).WithContext(ctx).Bind(dbx.Params{"blockId": blockID, "target": target}).Execute(); err != nil {
				return err
			}
		}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_block_page_references}} WHERE [[block_id]] = {:blockId};
		`).WithContext(ctx).Bind(params).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT OR REPLACE INTO {{leafen_block_page_references}}
				([[workspace_id]], [[source_page_id]], [[block_id]], [[source_position]], [[target_page_id]])
			SELECT [[workspace_id]], [[source_page_id]], [[block_id]], [[source_position]], [[target_page_id]]
			FROM [[leafen_block_page_reference_source]] WHERE [[block_id]] = {:blockId}
		`).WithContext(ctx).Bind(params).Execute(); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT OR REPLACE INTO {{leafen_block_page_references}}
				([[workspace_id]], [[source_page_id]], [[block_id]], [[source_position]], [[target_page_id]])
			SELECT [[block]].[[workspace_id]], [[block]].[[page_id]], [[block]].[[id]], [[block]].[[position]],
				[[text]].[[target_page_id]]
			FROM {{leafen_block_text_page_references}} [[text]]
			JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[text]].[[block_id]]
			WHERE [[text]].[[block_id]] = {:blockId}
		`).WithContext(ctx).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}
