package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's block follows its page in a person's change, as in Notion:
// deleting it sends the page to the trash, putting it back restores the page
// (child_page_blocks.go). A wiki's page's block is a link marked as the
// wiki's in the body of the page above it in the wiki (wiki_page_links.go),
// and it follows the page the same way: Notion's page Markdown replaced
// without a wiki's page's <page> tag, deleting content allowed, sends that
// page to the trash, under the page it was in, and out of the wiki's rows,
// as it does an ordinary sub-page (seen 2026-11-20, Notion-Version
// 2025-09-03). A wiki's page is its database's page, so it goes to the trash
// and back as its row does, once the change is made.

// setChildPageTrash sends a sub-page to the trash or brings it back, as the
// page's own trash change does.
func setChildPageTrash(app core.App, actorID, pageID string, trash bool, mutationID string) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if page.GetBool("in_trash") == trash {
		return nil
	}
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: trash,
		MutationID: "page-block:" + childPageBlockDigest(mutationID, pageID), BaseCursor: page.GetInt("cursor"),
	})
	_, status, err := applyPageMetadataTransaction(app, actorID, pageID, input, pageMetadataRequestHash(pageID, input))
	if err != nil {
		return err
	}
	if status != pageMetadataApplied {
		return errChildPageBlockPageChange
	}
	return nil
}

// wikiPageLinkChange is a link standing for a wiki's page that a person's
// change deletes (trash) or adds.
type wikiPageLinkChange struct {
	pageID string
	trash  bool
}

// noteWikiPageLink adds a block a change deletes or adds to changes when it
// is a link marked as a wiki's; content is the block's stored map or its
// written JSON.
func noteWikiPageLink(changes []wikiPageLinkChange, blockType string, content any, trash bool) []wikiPageLinkChange {
	if blockType != graphBlockTypeLinkToPage {
		return changes
	}
	fields, ok := content.(map[string]any)
	if raw, written := content.(json.RawMessage); written {
		ok = json.Unmarshal(raw, &fields) == nil
	}
	pageID, _ := fields["childPageId"].(string)
	if !ok || fields[wikiPageLinkMark] != true || pageID == "" {
		return changes
	}
	return append(changes, wikiPageLinkChange{pageID: pageID, trash: trash})
}

// settleChildPageWikiJobs finishes what a change's page blocks left for after
// it is made: pages made in a wiki's page become the wiki's, as one made
// there from the sidebar does (page_creation.go); then each wiki's page whose
// link the change took from, or put in, the body of the page above it in
// the wiki goes to the trash or comes back, unless its link is there or gone
// again. It returns the change's error, or the first of its own.
func (p *pocketBaseYgoPersistence) settleChildPageWikiJobs(
	ctx context.Context, principalID, pageID string, jobs childPageBlockJobs, err error,
) error {
	for _, made := range jobs.wiki {
		if err != nil {
			return err
		}
		_, err = p.adoptPageTreeIntoWiki(ctx, principalID, made)
	}
	for _, change := range jobs.wikiLinks {
		if err != nil {
			return err
		}
		if followErr := p.followWikiPageLink(ctx, principalID, pageID, change); followErr != nil {
			p.app.Logger().Warn("leafen wiki page did not follow its link", "page", change.pageID, "error", followErr)
		}
	}
	return err
}

// followWikiPageLink sends a wiki's page to the trash when the body of the
// page above it in the wiki no longer holds its link, or brings it back when
// that body holds one again.
func (p *pocketBaseYgoPersistence) followWikiPageLink(
	ctx context.Context, principalID, bodyID string, change wikiPageLinkChange,
) error {
	place := struct {
		Holder  string `db:"holder"`
		InTrash bool   `db:"in_trash"`
		Owner   string `db:"owner"`
	}{}
	// A wiki's page is in the trash as its row is (database_row_trash.go).
	err := p.app.DB().NewQuery(`SELECT COALESCE(` + pageWikiParentSQL("[[page]]") + `, '') AS [[holder]],
		NOT ` + databaseRowNotTrashedSQL("[[page]].[[id]]") + ` AS [[in_trash]], [[page]].[[owner]]
		FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": change.pageID}).One(&place)
	if errors.Is(err, sql.ErrNoRows) || err == nil && place.Holder != bodyID || err == nil && place.InTrash == change.trash {
		return nil
	}
	if err != nil {
		return err
	}
	links, err := wikiPageLinkBlocks(p.app, place.Owner, change.pageID)
	if err != nil {
		return err
	}
	held := false
	for _, link := range links {
		held = held || link.PageID == bodyID
	}
	if held == change.trash {
		return nil
	}
	authority, err := authorizeDatabaseRowPageInTrash(p.app, principalID, change.pageID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	_, err = p.changeDatabaseRowTrash(ctx, principalID, change.pageID, databaseRowPageActionInput{
		RequestID: "wiki-page-block:" + newDiagnosticReference(), ExpectedAuthorityRevision: authority.AuthorityRevision,
	}, change.trash)
	return err
}
