package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A wiki's page stands in the body of the page above it in the wiki as a link
// marked as the wiki's (wiki_page_links.go), which Notion's page Markdown
// writes as the page block it is there: <page url="…">title</page> (seen
// 2026-11-19, Notion-Version 2025-09-03: a page of a wiki holding another
// page of it reads so; notion_page_body_wiki_items.go). So the public API's
// Markdown and MCP's fetch name it with a <page> tag, and Markdown written
// back keeps it where its tag stands and gives the page the title its tag
// gives, as a sub-page's; Leafen's own Markdown keeps writing it as the link
// it is (<link-to-page url="…" />).

// notionMarkdownBlockReference starts a block's reference, and names the page
// it stands for: a sub-page's block its page, a link marked as a wiki's its
// live page of the wiki, none for any other block.
func notionMarkdownBlockReference(
	app core.App, owner string, block workspaceExportMarkdownBlock,
) (notionMarkdownReference, string, error) {
	content, err := json.Marshal(block.Content)
	if err != nil {
		return notionMarkdownReference{}, "", err
	}
	reference := notionMarkdownReference{BlockID: block.ID, ParentID: block.ParentID, Type: block.Type, Content: content}
	if isStoredChildPageBlock(block.ID, block.Type, string(content)) {
		return reference, block.ID, nil
	}
	if block.Type != graphBlockTypeLinkToPage || block.Content[wikiPageLinkMark] != true {
		return reference, "", nil
	}
	pageID, err := wikiPageOfLinkBlock(app, storedNotionBlock{ID: block.ID, Owner: owner, Type: block.Type})
	return reference, pageID, err
}

// pageID is the page a <page> tag's reference names: a sub-page's block is
// its page, and a wiki's link names its page.
func (reference notionMarkdownReference) pageID() string {
	if reference.Type != graphBlockTypeLinkToPage {
		return reference.BlockID
	}
	content := struct {
		ChildPageID string `json:"childPageId"`
	}{}
	_ = json.Unmarshal(reference.Content, &content)
	return content.ChildPageID
}

// renameNotionMarkdownTagPages gives the pages tags named the titles their
// tags gave: a wiki's page through its title property, as its database's
// page takes one, any other page as a sub-page (renameNotionMarkdownSubPages);
// and an inline database of several sources its name of its own
// (nameNotionMarkdownInlineDatabase).
func (p *pocketBaseYgoPersistence) renameNotionMarkdownTagPages(
	ctx context.Context, app core.App, principalID string, renames []notionMarkdownRename,
) error {
	pages := []notionMarkdownRename{}
	for _, rename := range renames {
		if rename.DatabaseBlockID != "" {
			if err := p.nameNotionMarkdownInlineDatabase(ctx, principalID, rename); err != nil {
				return err
			}
			continue
		}
		page, err := app.FindRecordById(pagesCollection, rename.PageID)
		if err != nil {
			return err
		}
		if rename.SourceID != "" || page.GetString("data_source_id") == "" {
			pages = append(pages, rename)
			continue
		}
		if err := p.renameNotionMarkdownWikiPage(ctx, principalID, rename); err != nil {
			return err
		}
	}
	return renameNotionMarkdownSubPages(app, principalID, pages)
}

// renameNotionMarkdownWikiPage writes a wiki's page's title as its database
// page's title value, as the page's own title field does.
func (p *pocketBaseYgoPersistence) renameNotionMarkdownWikiPage(
	ctx context.Context, principalID string, rename notionMarkdownRename,
) error {
	authority, _, err := databaseRowPageMutationAuthorization(p.app, principalID, rename.PageID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	title, err := json.Marshal(rename.Title)
	if err != nil {
		return err
	}
	_, err = p.applyContentGraphMutationContext(ctx, principalID, authority.ContainerPageID,
		graphMutationInput{MutationID: "markdown-wiki-page-title:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			EnforcePageLock: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, page, err := databaseRowPageMutationAuthorization(txApp, principalID, rename.PageID, permissionWorkspaceWrite)
				if err == nil && current.ContainerPageID != authority.ContainerPageID {
					err = errDatabaseRowPageConflict
				}
				return page, err
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, _, err := databaseRowPageMutationAuthorization(txApp, principalID, rename.PageID, permissionWorkspaceWrite)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				ids, err := loadWikiDatabaseIDs(txApp, current.DataSourceID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID: current.RowID, DataSourceID: current.DataSourceID,
						SubitemParentID: optionalDatabaseRowParent(current.SubitemParentID), Position: current.Position,
						Values: map[string]graphRowValue{ids.Title: {Type: graphValueTypeTitle, Value: title, Spans: rename.Spans}},
					},
				}}}, nil
			},
		},
	)
	return err
}

// A wiki is a database in the Markdown of the page holding it, as its block
// is a child database there (notion_wiki_identity.go): Notion's writes it
// <database url="…/p/<wiki>" inline="false" data-source-url="collection://…"
// wiki="true">title</database>, and its "v2" with data-source-url before
// inline and the title on its own lines (seen 2026-11-20, Notion-Version
// 2025-09-03). Written back, the tag keeps the wiki where it stands, and a new
// title in it renames the wiki's page and its data source, as a full-page
// database's.

// notionMarkdownPageDatabase is the database a page's block reads as in the
// Markdown of the page holding it: a full-page database's, or a wiki's, which
// wiki says. found is false for any other page.
func notionMarkdownPageDatabase(
	app core.App, owner string, page *core.Record, readable func(string) (bool, error),
) (notionMarkdownDatabase, bool, bool, error) {
	if isFullPageDatabase(page) {
		database, found, err := loadNotionMarkdownDatabase(app, owner, page.Id, "", readable)
		return database, false, found, err
	}
	containerID, err := notionMarkdownWikiOfPage(app, page.Id)
	if err != nil || containerID == "" {
		return notionMarkdownDatabase{}, false, false, err
	}
	blockID := ""
	if err := app.DB().NewQuery(`SELECT [[block_id]] FROM {{leafen_database_containers}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": containerID}).Row(&blockID); err != nil {
		return notionMarkdownDatabase{}, false, false, err
	}
	database, found, err := loadNotionMarkdownDatabase(app, owner, page.Id, blockID, readable)
	return database, true, found && database.ContainerID == containerID, err
}

// wikiAttribute is a wiki's database's tag's wiki="true", nothing for any
// other database.
func (reference notionMarkdownReference) wikiAttribute() string {
	if !reference.Wiki {
		return ""
	}
	return ` wiki="true"`
}
