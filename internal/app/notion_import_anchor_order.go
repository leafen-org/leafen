package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Pages hanging on one field — a page's title, a line, a table row's cells, a
// caption, a database page's cell — stand in their parent's sidebar in the
// order the field mentions them, as Notion's sidebar lists a title's and a
// line's (seen 2026-11-18 twice: a line mentioning pages made second, first
// and third lists them second, first, third, and one mentioning pages made
// second, first, third lists them so too; a title mentioning the second made
// before the first lists the second first), whatever order they were made in.
// Notion lists a table row's in no sidebar; Leafen's stand where the row
// does (childPageBodyOrder), in the order its cells read. The import gives
// each the place its mention has among those already hanging there when it is
// made: Notion gives them all the same place in their block, and a run again
// brings a new one among those an earlier run made. alignChildPageSidebar
// keeps that order.

// notionImportAnchoredPagePosition is a sub-page's first place among the
// pages already hanging on its anchor, those this run or an earlier one made:
// right after the one its field mentions last before it, or right before the
// one mentioned first after it; false when its field doesn't say, or none
// hangs there yet.
func notionImportAnchoredPagePosition(
	app core.App, jobID string, source storedNotionSource, mappings *notionApplyMappings, parentPageID, anchor string,
) (float64, bool, error) {
	if source.ParentSourceID == "" {
		return 0, false, nil
	}
	// A callout's words are its first line's (notionImportBlockWords).
	_, field, found, err := notionImportBlockWords(app, jobID, source.ParentSourceID)
	if err != nil || !found {
		return 0, false, err
	}
	field = strings.ReplaceAll(field, "-", "")
	at := notionImportMentionAt(field, source.SourceID)
	if at < 0 {
		return 0, false, nil
	}
	// Those made in this window are in its mappings, not yet stored.
	hanging := []struct {
		ID       string  `db:"id"`
		Position float64 `db:"position"`
		SourceID string  `db:"source_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]], [[page]].[[position]], COALESCE([[mapping]].[[source_id]], '') AS [[source_id]]
		FROM {{leafen_pages}} [[page]]
		LEFT JOIN {{leafen_notion_import_mappings}} [[mapping]]
			ON [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[kind]] = 'page' AND [[mapping]].[[native_id]] = [[page]].[[id]]
		WHERE [[page]].[[parent_id]] = {:parent} AND [[page]].[[anchor_block_id]] = {:anchor} AND [[page]].[[in_trash]] = FALSE
	`).Bind(dbx.Params{"jobId": jobID, "parent": parentPageID, "anchor": anchor}).All(&hanging); err != nil {
		return 0, false, err
	}
	for index := range hanging {
		if hanging[index].SourceID == "" && mappings != nil {
			hanging[index].SourceID = mappings.pendingSource(hanging[index].ID, "page")
		}
	}
	before, after := -1, -1
	for index, page := range hanging {
		mentioned := notionImportMentionAt(field, page.SourceID)
		switch {
		case mentioned < 0 || mentioned == at:
		case mentioned < at && (before < 0 || mentioned > notionImportMentionAt(field, hanging[before].SourceID)):
			before = index
		case mentioned > at && (after < 0 || mentioned < notionImportMentionAt(field, hanging[after].SourceID)):
			after = index
		}
	}
	switch {
	case before >= 0 && after >= 0:
		return hanging[before].Position + (hanging[after].Position-hanging[before].Position)/2, true, nil
	case before >= 0:
		return hanging[before].Position + 1, true, nil
	case after >= 0:
		return hanging[after].Position - 1, true, nil
	}
	return 0, false, nil
}

// notionImportMentionAt is where a field's source text, its dashes left out,
// first names a Notion object; -1 when it doesn't.
func notionImportMentionAt(field, sourceID string) int {
	id := strings.ReplaceAll(strings.TrimSpace(sourceID), "-", "")
	if id == "" {
		return -1
	}
	return strings.Index(field, id)
}

// pendingSource is the Notion object a window made an object from, while the
// window hasn't stored it yet; "" for one it didn't make.
func (mappings *notionApplyMappings) pendingSource(nativeID, kind string) string {
	for _, mapping := range mappings.pending {
		if mapping.NativeID == nativeID && mapping.Kind == kind {
			return mapping.SourceID
		}
	}
	return ""
}
