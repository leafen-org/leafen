package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A wiki's database is a database on the wiki's page, as a Notion wiki is a
// database with one data source: its pages are the data source's pages, with
// Notion's default wiki properties in Notion's order — Owner, Verification,
// Tags and Last edited time — and its views All pages and Pages I own, named
// in the language of the person who makes the wiki, each with its newest
// edits first (Notion's, seen 2026-11-11). Its block sits on the wiki's page,
// where the wiki's Home shows the page itself instead.

// wikiDatabaseIDs names the parts of a wiki's database, fixed by the job
// that turns the page into a wiki so that a job running its step again finds
// what it made.
type wikiDatabaseIDs struct {
	Block, Source, Title, Owner, Tags, Verification, LastEdited, AllPages, PagesIOwn string
}

func newWikiDatabaseIDs(jobID string) wikiDatabaseIDs {
	id := func(part string) string {
		digest := sha256.Sum256([]byte("leafen:wiki-database\x00" + jobID + "\x00" + part))
		return "wiki-" + part + "-" + hex.EncodeToString(digest[:12])
	}
	return wikiDatabaseIDs{
		Block: id("block"), Source: id("source"), Title: id("title"), Owner: id("owner"), Tags: id("tags"),
		Verification: id("verification"), LastEdited: id("edited"), AllPages: id("all"), PagesIOwn: id("mine"),
	}
}

// A wiki's Home in its updates, as one of its views (recordWorkspaceDatabaseActivities):
// its address's view (the web's WIKI_HOME_VIEW_ID) and its kind.
const (
	wikiHomeViewID   = "home"
	wikiHomeViewKind = "wiki_home"
)

// isWikiTagsPropertyID reports whether a property is the Tags a wiki's
// database was made with.
func isWikiTagsPropertyID(id string) bool {
	return strings.HasPrefix(id, "wiki-tags-")
}

// wikiPropertyIDs names the properties a wiki's database was made with; one
// taken away since is empty.
type wikiPropertyIDs struct {
	Title, Owner, Verification, Tags, LastEdited string
}

// wikiViewColumns is the column order and widths of a wiki's two views, as
// Notion's (seen 2026-11-13 in both views: Page 300, Owner, Verification and
// Tags 150, Last edited time 250), less the properties not there.
func wikiViewColumns(ids wikiPropertyIDs) ([]string, map[string]int) {
	columns := []struct {
		id    string
		width int
	}{{ids.Title, 300}, {ids.Owner, 150}, {ids.Verification, 150}, {ids.Tags, 150}, {ids.LastEdited, 250}}
	order, widths := []string{}, map[string]int{}
	for _, column := range columns {
		if column.id == "" {
			continue
		}
		order = append(order, column.id)
		widths[column.id] = column.width
	}
	return order, widths
}

// wikiDatabaseOperations lays out a wiki's database: its data source named as
// the wiki's page, its properties and views by the names given, and its block
// after the page's last block.
func wikiDatabaseOperations(
	app core.App, ids wikiDatabaseIDs, wikiPageID, title string, names wikiDatabaseNames,
) ([]graphOperation, error) {
	var tail string
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId} AND [[parent_id]] = ''
	`).Bind(dbx.Params{"pageId": wikiPageID}).Row(&tail); err != nil {
		return nil, err
	}
	blockPosition, err := databasePositionAfter(tail)
	if err != nil {
		return nil, err
	}
	property := func(id, kind, name, position string) graphOperation {
		return graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: id, DataSourceID: ids.Source, Type: kind, Name: name, Position: position, Config: json.RawMessage(`{}`),
		}}
	}
	// Last edited time shows its dates relative to today ("오늘 오전 3:52",
	// an older one in full), as a new Notion wiki's does (its date format
	// "상대", seen 2026-11-18 on a wiki made then and one made 2026-11-01).
	lastEdited := property(ids.LastEdited, graphValueTypeLastEditedTime, names.LastEditedTime, "a4")
	lastEdited.Property.Config = json.RawMessage(`{"dateFormat":"relative"}`)
	// Both views sort by Last edited time, newest first, their bar folded until
	// a sort or filter is opened; Pages I own keeps its owner's pages. Their
	// columns stand in the properties' order at Notion's widths.
	newestFirst := []any{map[string]any{"propertyId": ids.LastEdited, "direction": "desc"}}
	columnOrder, columnWidths := wikiViewColumns(wikiPropertyIDs{
		Title: ids.Title, Owner: ids.Owner, Verification: ids.Verification, Tags: ids.Tags, LastEdited: ids.LastEdited,
	})
	allPagesConfig, err := json.Marshal(map[string]any{"sorts": newestFirst, "filterBarHidden": true,
		"propertyOrder": columnOrder, "propertyWidths": columnWidths})
	if err != nil {
		return nil, err
	}
	ownedConfig, err := json.Marshal(map[string]any{"sorts": newestFirst, "filterBarHidden": true,
		"propertyOrder": columnOrder, "propertyWidths": columnWidths, "filterGroup": map[string]any{
			"conjunction": "and", "groups": []any{},
			"filters": []any{map[string]any{
				"propertyId": ids.Owner, "operator": "contains", "value": map[string]any{"kind": "leafen.current_user"},
			}},
		}})
	if err != nil {
		return nil, err
	}
	blockContent, err := json.Marshal(map[string]any{"dataSourceId": ids.Source, "viewId": ids.AllPages})
	if err != nil {
		return nil, err
	}
	return []graphOperation{
		{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{ID: ids.Source, Title: title}},
		property(ids.Title, graphValueTypeTitle, names.Page, "a0"),
		property(ids.Owner, graphValueTypePerson, names.Owner, "a1"),
		property(ids.Verification, graphValueTypeVerification, names.Verification, "a2"),
		property(ids.Tags, graphValueTypeMultiSelect, names.Tags, "a3"),
		lastEdited,
		{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: ids.AllPages, DataSourceID: ids.Source, Type: graphViewTypeTable, Name: names.AllPages,
			Position: "a0", Config: allPagesConfig,
		}},
		{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: ids.PagesIOwn, DataSourceID: ids.Source, Type: graphViewTypeTable, Name: names.PagesIOwn,
			Position: "a1", Config: ownedConfig,
		}},
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: ids.Block, Position: blockPosition, Type: graphBlockTypeInlineDatabase, Content: blockContent,
		}},
	}, nil
}

// createWikiDatabase puts a wiki's database on the wiki's page and marks its
// data source as the wiki's: a page under another page in the wiki keeps its
// place under it by its row's own parent, not by a relation pair, so the
// wiki's Sub-pages start off, as Notion's do (seen 2026-11-11), and turning
// them on gives it a pair of its own. The job turning the page into a wiki
// authorizes the change on the page it fences.
func (p *pocketBaseYgoPersistence) createWikiDatabase(
	ctx context.Context, actorID, wikiPageID, jobID string, authorize func(core.App) (pageAuthorization, error),
) (wikiDatabaseIDs, error) {
	ids := newWikiDatabaseIDs(jobID)
	_, err := p.applyContentGraphMutationContext(ctx, actorID, wikiPageID,
		graphMutationInput{MutationID: "wiki-database:" + jobID},
		contentGraphMutationHooks{
			Authorize:    authorize,
			WikiHomeMade: ids.Source,
			// The job's step, run again, is the same change.
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				digest := sha256.Sum256([]byte("wiki-database\x00" + jobID + "\x00" + wikiPageID))
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				names, err := loadWikiDatabaseNames(txApp, jobID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				operations, err := wikiDatabaseOperations(txApp, ids, wikiPageID, authorization.Page.GetString("title"), names)
				return contentGraphMutationPreparation{Operations: operations}, err
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				_, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_data_sources}}
					SET [[wiki]] = 1, [[wiki_owner_property_id]] = {:owner}, [[wiki_verification_property_id]] = {:verification}
					WHERE [[id]] = {:source}
				`).Bind(dbx.Params{
					"source": ids.Source, "owner": ids.Owner, "verification": ids.Verification,
				}).Execute()
				return err
			},
		})
	return ids, err
}
