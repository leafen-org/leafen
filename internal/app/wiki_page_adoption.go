package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page made, moved or restored under a wiki's page, or under a page of a
// wiki, becomes the wiki database's page with the live pages under it, as a
// Notion wiki's pages are its database's pages: owned by whoever put it
// there, under the page it sits under.

// wikiPlace is where a page under a given parent sits in a wiki: the wiki's
// page, its database's data source, and the wiki's page above it, empty
// when that is the wiki's page itself.
type wikiPlace struct {
	WikiPageID string `db:"wiki_page_id"`
	SourceID   string `db:"source_id"`
	AboveID    string `db:"above_id"`
}

// loadWikiPlace reports the wiki a page under parentID sits in, if any.
func loadWikiPlace(app core.App, parentID string) (wikiPlace, bool, error) {
	if parentID == "" {
		return wikiPlace{}, false, nil
	}
	place := wikiPlace{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[page_id]] AS [[wiki_page_id]], [[source]].[[id]] AS [[source_id]], '' AS [[above_id]]
		FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[page_id]] = {:parent} AND [[source]].[[wiki]] = 1
		UNION ALL
		SELECT [[source]].[[page_id]], [[source]].[[id]], [[row]].[[id]]
		FROM {{leafen_database_rows}} AS [[row]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]] AND [[source]].[[wiki]] = 1
		WHERE [[row]].[[id]] = {:parent}
		LIMIT 1
	`).Bind(dbx.Params{"parent": parentID}).One(&place)
	if errors.Is(err, sql.ErrNoRows) {
		return wikiPlace{}, false, nil
	}
	return place, err == nil, err
}

// loadWikiDatabaseIDs reads the parts of a wiki's database a page of it
// takes values in.
func loadWikiDatabaseIDs(app core.App, sourceID string) (wikiDatabaseIDs, error) {
	ids := wikiDatabaseIDs{Source: sourceID}
	properties, wiki, err := loadWikiSourceProperties(app, sourceID)
	if err != nil || !wiki {
		return ids, errors.Join(err, errWikiConflict)
	}
	ids.Owner, ids.Verification = properties.Owner, properties.Verification
	err = app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:source} AND [[type]] = 'title' ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"source": sourceID}).Row(&ids.Title)
	return ids, err
}

// adoptPageTreeIntoWiki makes a page under a wiki's page, or under a page of
// a wiki, the wiki database's page, owned by the person who put it there,
// and then the live pages under it, a window at a time from the top, each
// under the page above it. A page that is a wiki's page already — one moved
// into a wiki's data source — takes the live pages under it into the wiki
// so. Another database's page, a page outside every wiki, one in the trash
// or a wiki of its own is left as it is, with the pages under it; so is a
// page whose wiki a wiki job is changing.
func (p *pocketBaseYgoPersistence) adoptPageTreeIntoWiki(ctx context.Context, actorID, pageID string) (bool, error) {
	page, err := p.app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return false, err
	}
	if page.GetBool("in_trash") || page.GetBool("is_wiki") || page.GetBool("notion_import_staging") {
		return false, nil
	}
	// A database's page brings in the pages under it, not itself.
	asRow, above := page.GetString("data_source_id") != "", page.GetString("parent_id")
	if asRow {
		above = pageID
	}
	place, inWiki, err := loadWikiPlace(p.app, above)
	if err != nil || !inWiki {
		return false, err
	}
	if fenced, err := wikiMutationFenceActive(p.app, place.WikiPageID); err != nil || fenced {
		return false, err
	}
	ids, err := loadWikiDatabaseIDs(p.app, place.SourceID)
	if err != nil {
		return false, err
	}
	workspaceID := page.GetString("workspace")
	window, frontier := []wikiConversionItem{{PageID: pageID, ParentID: above}}, []string{}
	if asRow {
		if window, frontier, err = nextWikiAdoptionWindow(p.app, workspaceID, []string{pageID}); err != nil {
			return false, err
		}
	}
	for len(window) > 0 {
		if err := p.adoptWikiWindow(ctx, actorID, pageID, workspaceID, place, ids, window); err != nil {
			return false, err
		}
		for _, item := range window {
			frontier = append(frontier, item.PageID)
		}
		window, frontier, err = nextWikiAdoptionWindow(p.app, workspaceID, frontier)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// nextWikiAdoptionWindow reads the next live pages under the pages already
// made the wiki's, keeping the pages that still may have some.
func nextWikiAdoptionWindow(app core.App, workspaceID string, frontier []string) ([]wikiConversionItem, []string, error) {
	for len(frontier) > 0 {
		parents := frontier[:min(len(frontier), maxWikiMutationItemsPerTurn)]
		encoded, err := json.Marshal(parents)
		if err != nil {
			return nil, nil, err
		}
		window := []wikiConversionItem{}
		if err := app.DB().NewQuery(`
			SELECT [[id]] AS [[page_id]], [[parent_id]] FROM {{leafen_pages}}
			WHERE [[workspace]] = {:workspaceId} AND [[parent_id]] IN (SELECT [[value]] FROM json_each({:parents}))
				AND [[data_source_id]] = '' AND [[in_trash]] = 0 AND [[is_wiki]] = 0 AND [[notion_import_staging]] = 0
				AND [[duplication_target_job_id]] = ''
			ORDER BY [[parent_id]], [[position]], [[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "parents": string(encoded), "limit": maxWikiMutationItemsPerTurn,
		}).All(&window); err != nil {
			return nil, nil, err
		}
		if len(window) > 0 {
			return window, frontier, nil
		}
		frontier = frontier[len(parents):]
	}
	return nil, nil, nil
}

// adoptWikiWindow makes a window of pages the wiki database's pages in one
// change of the wiki's page. Whoever may change the page the tree starts at
// puts it in the wiki.
func (p *pocketBaseYgoPersistence) adoptWikiWindow(
	ctx context.Context, actorID, treeID, workspaceID string, place wikiPlace, ids wikiDatabaseIDs,
	window []wikiConversionItem,
) error {
	pageIDs := make([]string, len(window))
	for index, item := range window {
		pageIDs[index] = item.PageID
	}
	digest := sha256.Sum256([]byte("wiki-adopt\x00" + strings.Join(pageIDs, "\x00")))
	ownerID := ""
	_, err := p.applyContentGraphMutationContext(ctx, actorID, place.WikiPageID,
		graphMutationInput{MutationID: "wiki-adopt:" + pageIDs[0] + ":" + newDiagnosticReference()},
		contentGraphMutationHooks{
			SuppressAutomationEvents: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				if _, err := authorizePageBody(txApp, actorID, treeID, permissionWorkspaceWrite); err != nil {
					return pageAuthorization{}, err
				}
				workspace, err := authorizeWorkspace(txApp, actorID, workspaceID, permissionWorkspaceWrite)
				if err != nil {
					return pageAuthorization{}, err
				}
				wikiPage, err := txApp.FindRecordById(pagesCollection, place.WikiPageID)
				if err != nil {
					return pageAuthorization{}, err
				}
				return pageAuthorization{Workspace: workspace, Page: wikiPage}, nil
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, authorization pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				ownerID = authorization.Workspace.OwnerID
				// A page put under a wiki is owned by whoever put it there.
				operations, err := wikiConversionRows(
					txApp, func(*core.Record) string { return actorID }, workspaceID, authorization.Workspace.OwnerID, authorization.Page, ids, window,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				// A wiki's pages are its database's; their page blocks become links to them.
				links, err := childPageBlockWikiLinks(doc, pageIDs)
				return contentGraphMutationPreparation{Operations: append(operations, links...)}, err
			},
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				for _, pageID := range pageIDs {
					if err := turnPageIntoWikiRow(txApp, workspaceID, pageID, place.SourceID, place.WikiPageID); err != nil {
						return err
					}
				}
				return nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if err := settleWikiRows(ctx, txApp, workspaceID, place.WikiPageID, pageIDs); err != nil {
					return err
				}
				if err := swapChildPageBlocksForWikiLinks(txApp, ownerID, workspaceID, actorID, pageIDs); err != nil {
					return err
				}
				recheckCollaborationAccessAfterCommit(txApp)
				return nil
			},
		})
	return err
}
