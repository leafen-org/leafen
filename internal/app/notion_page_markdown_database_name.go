package app

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A <database> tag written back to a page with a new title renames what the
// tag names by (notionMarkdownInlineDatabaseTitle): an inline database of
// two or more sources of its own goes by a name of its own
// (databaseNameFrom), so the title becomes that name, its sources keeping
// theirs, as a rename through the API gives it (updateNotionDatabaseSource):
// kept on its block with its formatting, plain words as one piece; a title
// of spaces alone leaves it unnamed, named by its sources' names.

// nameNotionMarkdownInlineDatabase gives an inline database of several
// sources the name its tag gave, once the page's body is written.
func (p *pocketBaseYgoPersistence) nameNotionMarkdownInlineDatabase(
	ctx context.Context, principalID string, rename notionMarkdownRename,
) error {
	container := struct {
		Owner  string `db:"owner"`
		PageID string `db:"page_id"`
	}{}
	if err := p.app.DB().NewQuery(`
		SELECT [[owner]], [[page_id]] FROM {{leafen_database_containers}} WHERE [[id]] = {:id} AND [[kind]] = 'source'
	`).Bind(dbx.Params{"id": rename.DatabaseBlockID}).One(&container); err != nil {
		return err
	}
	spans := rename.Spans
	if len(spans) == 0 && rename.Title != "" {
		spans = []graphTextSpan{{Text: rename.Title}}
	}
	_, err := p.applyContentGraphMutationContext(ctx, principalID, container.PageID, graphMutationInput{
		MutationID: "markdown-database-name:" + newDiagnosticReference(),
	}, contentGraphMutationHooks{
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			return authorizePageBody(txApp, principalID, container.PageID, permissionWorkspaceWrite)
		},
		Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
			return databaseBlockContentChange(txApp, doc, container.Owner, container.PageID, rename.DatabaseBlockID,
				databasePageNameSpansChange(spans))
		},
	})
	return err
}
