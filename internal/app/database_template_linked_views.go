package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A linked database in a body another page takes — a template's applied to
// a database page, a template or page copied — comes with its views, as
// Notion's templates do (seen 2026-10-14): each view is copied under the
// copied block, and a filter naming the page the body comes from names the
// page that takes it, so a template's "this page" filter is each page's own.
// A page made from a template, or one it is applied to, takes Leafen's own
// "this page" value as its own ID too, as Notion's does (seen 2026-11-12:
// the filter's chip there is the page's name and icon, the page ticked
// among its database's); a template's copy keeps it, its own "this page".
// A template made of a page's body — an import's, of a Notion template it
// read as a row — takes a filter naming that page as its own "this page",
// the value the screen's choice saves in a template: Notion's API gives a
// template's "this page" as the template's own ID, which reads as its row
// (seen 2026-11-16: the template's chip is its name, and a page made from
// it filters by the new page, in Notion's app and its API alike).
// A linked database of a database a page's copy takes with the body shows
// the copy, as a page's body does (pageDuplicationBodyRewrite), one of the
// body's own database once that database's copy is made
// (nameRowPageDatabaseCopy); one of a source the taking page's owner hasn't
// keeps the block alone, as before.

// copiedLinkedDatabaseViews gives a copied body's linked database blocks
// their views on the target page, under the IDs viewID gives: each block
// names its first view's copy, and the views are returned by block.
// sourceBlock gives a copied block's ID in the source.
func copiedLinkedDatabaseViews(
	app core.App, sourcePageID, targetPageID, targetOwner string, body []newRowPageBlock,
	sourceBlock func(string) string, viewID func(string) string,
) (map[string][]graphDatabaseView, error) {
	copies := map[string][]graphDatabaseView{}
	var thisPage *bool
	madeOfPage := false
	for index, block := range body {
		if block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		if linked, _ := block.Content["linkedDatabaseSource"].(bool); !linked {
			continue
		}
		stored := []struct {
			ID           string `db:"id"`
			DataSourceID string `db:"data_source_id"`
			Type         string `db:"type"`
			Name         string `db:"name"`
			Position     string `db:"position"`
			Config       string `db:"config"`
			Owner        string `db:"owner"`
			SourcePageID string `db:"source_page_id"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[view]].[[id]], [[view]].[[data_source_id]], [[view]].[[type]], [[view]].[[name]], [[view]].[[position]],
				[[view]].[[config]], COALESCE([[source]].[[owner]], '') AS [[owner]],
				COALESCE([[source]].[[page_id]], '') AS [[source_page_id]]
			FROM {{leafen_database_views}} [[view]]
			LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[view]].[[data_source_id]]
			WHERE [[view]].[[page_id]] = {:pageId} AND [[view]].[[container_id]] = {:blockId}
			ORDER BY [[view]].[[position]], [[view]].[[id]]
		`).Bind(dbx.Params{"pageId": sourcePageID, "blockId": sourceBlock(block.ID)}).All(&stored); err != nil {
			return nil, err
		}
		if thisPage == nil && len(stored) > 0 {
			bound, err := templateThisPageBound(app, sourcePageID, targetPageID)
			if err != nil {
				return nil, err
			}
			thisPage = &bound
			made, err := templateMadeOfPageBody(app, sourcePageID, targetPageID)
			if err != nil {
				return nil, err
			}
			madeOfPage = made
		}
		views := []graphDatabaseView{}
		for _, view := range stored {
			config := map[string]any{}
			if err := json.Unmarshal([]byte(view.Config), &config); err != nil {
				return nil, err
			}
			if madeOfPage {
				for _, key := range []string{"filterGroup", "quickFilters"} {
					if part, found := config[key]; found {
						config[key] = replacedPageID(part, sourcePageID, map[string]any{"kind": "leafen.current_page"})
					}
				}
			}
			settings := replacedPageID(config, sourcePageID, targetPageID)
			if *thisPage {
				settings = bindDatabaseViewCurrentPage(settings, targetPageID)
			}
			encoded, err := json.Marshal(settings)
			if err != nil {
				return nil, err
			}
			taken := graphDatabaseView{
				ID: viewID(view.ID), DataSourceID: view.DataSourceID, ContainerID: block.ID,
				Type: view.Type, Name: view.Name, Position: view.Position, Config: encoded,
			}
			if block.LinkedViews != nil {
				if taken, err = block.LinkedViews(taken); err != nil {
					return nil, err
				}
			}
			// One still of a source the taking page's owner hasn't keeps
			// the block alone; one of the body's own database stays, to show
			// the database's copy once it is made (nameRowPageDatabaseCopy).
			if taken.DataSourceID == view.DataSourceID && view.Owner != targetOwner && view.SourcePageID != sourcePageID {
				views = nil
				break
			}
			views = append(views, taken)
		}
		if len(views) == 0 {
			continue
		}
		content := make(map[string]any, len(block.Content))
		for key, value := range block.Content {
			content[key] = value
		}
		if current, _ := content["viewId"].(string); current != "" {
			content["viewId"] = viewID(current)
		}
		body[index].Content = content
		copies[block.ID] = views
	}
	return copies, nil
}

// templateThisPageBound reports a body going from a template to a page
// that isn't one, whose views' "this page" values become that page's ID.
func templateThisPageBound(app core.App, sourcePageID, targetPageID string) (bool, error) {
	fromTemplate, err := isDatabaseTemplatePageID(app, sourcePageID)
	if err != nil || !fromTemplate {
		return false, err
	}
	toTemplate, err := isDatabaseTemplatePageID(app, targetPageID)
	return !toTemplate, err
}

// templateMadeOfPageBody reports a body going from a page that isn't a
// template to a template made of it, whose views' values naming the page
// become the template's "this page".
func templateMadeOfPageBody(app core.App, sourcePageID, targetPageID string) (bool, error) {
	fromTemplate, err := isDatabaseTemplatePageID(app, sourcePageID)
	if err != nil || fromTemplate {
		return false, err
	}
	return isDatabaseTemplatePageID(app, targetPageID)
}

// replacedPageID is a view's settings with each value naming one page
// naming another, or what to gives in its place.
func replacedPageID(value any, from string, to any) any {
	switch typed := value.(type) {
	case string:
		if typed == from {
			return to
		}
	case []any:
		for index, item := range typed {
			typed[index] = replacedPageID(item, from, to)
		}
	case map[string]any:
		for key, item := range typed {
			typed[key] = replacedPageID(item, from, to)
		}
	}
	return value
}

// writeCopiedLinkedDatabaseViews keeps, for a body written straight to a new
// page, its linked databases' views and the databases' places, as a change
// on the page keeps them.
func writeCopiedLinkedDatabaseViews(
	app core.App, owner, workspaceID, pageID string, body []newRowPageBlock, copies map[string][]graphDatabaseView,
) error {
	now := time.Now().UTC().UnixMilli()
	for _, block := range body {
		views := copies[block.ID]
		if len(views) == 0 {
			continue
		}
		for _, view := range views {
			if err := writeDatabaseViewRecord(app, nil, owner, workspaceID, pageID, "", view, now); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(block.Content)
		if err != nil {
			return err
		}
		content, err := parseDatabaseContainerBlockContent(encoded)
		if err != nil {
			return err
		}
		if err := projectDatabaseContainerBlock(app, owner, workspaceID, pageID, block.ID, block.ParentID, block.Position, content); err != nil {
			return err
		}
	}
	return nil
}
