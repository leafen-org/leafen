package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A full-page database is a kind of page, as in Notion, rather than a page
// that happens to hold one database: its page records the kind, and its
// body is its database. The pages that held only their own database when
// the kind arrived are full-page databases; one that held more, or had
// sub-pages, stays a page. A wiki's page stays a page whose database is
// shown as its page, and another database's rows and templates are pages;
// a page of a wiki may be a full-page database, as a Notion wiki holds
// databases among its pages (seen 2026-10-29: one made in a wiki's page
// shows the wiki's owner, verification and tags over its views).

const (
	pageKindPage     = "page"
	pageKindDatabase = "database"
)

var (
	errFullPageDatabaseBody  = graphInputError(errorCodeGraphInvalid, "A full-page database holds only its database.")
	errFullPageDatabaseStart = graphInputError(errorCodeGraphInvalid, "Only a page of its own with no sub-pages can become a full-page database.")
)

// isFullPageDatabase reports whether a page is a full-page database.
func isFullPageDatabase(page *core.Record) bool {
	return page != nil && page.GetString("kind") == pageKindDatabase
}

// pageRecordKind is a page's kind as the app reads it.
func pageRecordKind(page *core.Record) string {
	return readPageKind(page.GetString("kind"))
}

// readPageKind reads a stored kind; a page from before kinds is a page.
func readPageKind(kind string) string {
	if kind == pageKindDatabase {
		return pageKindDatabase
	}
	return pageKindPage
}

// changePageKind makes a page a full-page database, as a new page in Notion
// starts as one, or a full-page database a page again, as a database turned
// inline leaves its page. A page that becomes a full-page database is a page
// of its own or a wiki's, not another database's row, a template or a wiki,
// with no sub-pages; the change
// that makes it one gives it its database, or finds it there, and the body
// rule then holds it to it. A page again keeps its database as a block.
func changePageKind(app core.App, authorization pageAuthorization, input graphMutationInput) error {
	page := authorization.Page
	kind := *input.PageKind
	if kind != pageKindDatabase && kind != pageKindPage {
		return graphInputError(errorCodeGraphInvalid, "Check the content change details.")
	}
	if pageRecordKind(page) == kind {
		return nil
	}
	if kind == pageKindPage {
		if err := recordDatabasePageSignpost(app, page); err != nil {
			return err
		}
		return savePageKind(app, authorization, pageKindPage)
	}
	if page.GetBool(databaseTemplatePageField) || page.GetBool("is_wiki") {
		return errFullPageDatabaseStart
	}
	if source := page.GetString("data_source_id"); source != "" {
		if _, wiki, err := loadWikiSourceProperties(app, source); err != nil || !wiki {
			if err != nil {
				return err
			}
			return errFullPageDatabaseStart
		}
	}
	var subPages int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[parent_id]] = {:id} AND [[data_source_id]] = ''
	`).Bind(dbx.Params{"id": page.Id}).Row(&subPages); err != nil {
		return err
	}
	if subPages > 0 {
		return errFullPageDatabaseStart
	}
	// A person's start on the page takes its place, the page as it was going
	// to the trash (database_start_page_trash.go).
	if input.ReplacesPage != nil && *input.ReplacesPage {
		if err := trashPageAsItWas(app, authorization); err != nil {
			return err
		}
	}
	return savePageKind(app, authorization, pageKindDatabase)
}

// savePageKind keeps a page's new kind; the page list learns it as it
// learns any change.
func savePageKind(app core.App, authorization pageAuthorization, kind string) error {
	page := authorization.Page
	cursor, err := nextWorkspaceUserStateCursor(app, authorization.Workspace.OwnerID)
	if err != nil {
		return err
	}
	page.Set("kind", kind)
	page.Set("cursor", cursor)
	page.Set("revision", page.GetInt("revision")+1)
	return app.Save(page)
}

// storedPageKind reads a page's kind. A room that is no page, or a page from
// before kinds were stored, is a page.
func storedPageKind(app core.App, pageID string) (string, error) {
	pages, err := app.FindCachedCollectionByNameOrId(pagesCollection)
	if err != nil {
		return "", err
	}
	if pages.Fields.GetByName("kind") == nil {
		return pageKindPage, nil
	}
	var kind string
	err = app.DB().NewQuery(`SELECT [[kind]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&kind)
	if errors.Is(err, sql.ErrNoRows) || err == nil && kind != pageKindDatabase {
		return pageKindPage, nil
	}
	return kind, err
}

// fullPageDatabaseBodySQL holds for a page of leafen_pages, in a statement on
// leafen_pages itself, whose stored body is the one keepFullPageDatabaseBody
// keeps: one top-level block, and that block a database, its own or linked.
const fullPageDatabaseBodySQL = `((SELECT COUNT(*) FROM {{leafen_blocks}} AS [[top]]
		WHERE [[top]].[[owner]] = {{leafen_pages}}.[[owner]] AND [[top]].[[page_id]] = {{leafen_pages}}.[[id]]
			AND [[top]].[[parent_id]] = '') = 1
	AND EXISTS (SELECT 1 FROM {{leafen_blocks}} AS [[top]]
		WHERE [[top]].[[owner]] = {{leafen_pages}}.[[owner]] AND [[top]].[[page_id]] = {{leafen_pages}}.[[id]]
			AND [[top]].[[parent_id]] = '' AND [[top]].[[type]] = 'inline_database'))`

// keepFullPageDatabaseBody refuses a change that leaves a full-page
// database's body holding anything but its database: one top-level block,
// either its own database or a linked database with separately owned views.
// A page an import is still building is left to finish first: its kind comes
// with it while its body arrives over several changes.
func keepFullPageDatabaseBody(app core.App, doc *crdt.Doc, pageID string) error {
	kind, err := storedPageKind(app, pageID)
	if err != nil || kind != pageKindDatabase {
		return err
	}
	var staging bool
	if err := app.DB().NewQuery(`SELECT [[notion_import_staging]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&staging); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if staging {
		return nil
	}
	roots, err := structuralOrder(doc, "")
	if err != nil {
		return err
	}
	if len(roots) != 1 {
		return errFullPageDatabaseBody
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	block, ok := metadata[roots[0]]
	if !ok || block.Type != graphBlockTypeInlineDatabase {
		return errFullPageDatabaseBody
	}
	return nil
}

// Starting a database may replace an empty paragraph, never writing that
// arrived while its chooser was open. The room lock and transaction cover
// this check and the deletion, including text received from collaborators.
func keepDatabaseStartWriting(app core.App, doc *crdt.Doc, page *core.Record, input graphMutationInput) error {
	if input.PageKind == nil || *input.PageKind != pageKindDatabase || isFullPageDatabase(page) {
		return nil
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	// Structure and collaborative text have separate stored streams. The
	// structural document cannot tell whether a paragraph holds writing.
	var writing *crdt.Doc
	for _, operation := range input.Operations {
		if operation.Kind != graphOperationKindDeleteBlock || operation.Deletion == nil {
			continue
		}
		id := operation.Deletion.BlockID
		block, exists := metadata[id]
		if !exists {
			continue
		}
		if writing == nil {
			loaded, _, err := loadYgoDoc(app, page.Id)
			if err != nil {
				return err
			}
			writing = loaded
		}
		children, err := structuralOrder(doc, id)
		if err != nil {
			return err
		}
		if block.Type != graphBlockTypeParagraph || block.ParentID != "" || len(children) != 0 ||
			writing.GetText(ygoBlockTextRootPrefix+id).Len() != 0 || len(input.TextUpdates) != 0 {
			return graphInputError(errorCodeGraphInvalid, "Only an empty page can start a database.")
		}
	}
	return nil
}

// fullPageDatabase is the database a full-page database is, with the title
// the database object gives it: its page's, which a database of several
// sources keeps apart from its first source's name, as Notion's child
// database does (seen 2026-11-10; one source shares its name with the page,
// full_page_database_name.go, but while it shows one from another database).
type fullPageDatabase struct {
	ContainerID string `db:"id"`
	Title       string `db:"title"`
	Kind        string `db:"kind"`
}

// loadFullPageDatabase reads the database a full-page database is.
func loadFullPageDatabase(app core.App, page *core.Record) (fullPageDatabase, bool, error) {
	database := fullPageDatabase{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], {:title} AS [[title]], [[container]].[[kind]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_blocks}} AS [[own]] ON [[own]].[[owner]] = [[container]].[[owner]]
			AND [[own]].[[page_id]] = [[container]].[[page_id]] AND [[own]].[[id]] = [[container]].[[block_id]]
			AND [[own]].[[parent_id]] = ''
		WHERE [[container]].[[page_id]] = {:page} AND [[container]].[[owner]] = {:owner}
			AND [[container]].[[kind]] IN ('source', 'linked') AND [[container]].[[parent_block_id]] = ''
		ORDER BY [[own]].[[position]], [[own]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"page": page.Id, "owner": page.GetString("owner"), "title": page.GetString("title")}).One(&database)
	if errors.Is(err, sql.ErrNoRows) {
		return fullPageDatabase{}, false, nil
	}
	// A database of its own reads as the database object names it, by the
	// one rule (notionDatabaseBlockTitle): by its page's title while two or
	// more sources of its own name it, by its one source's name while one
	// does — which its page's title keeps apart from while it shows one from
	// another database beside it (seen 2026-11-25: child_database "F2" under
	// a page titled "F") — and unnamed by its sources' names listed when it
	// has two or more (seen 2026-11-13: "A and B"), else "New database" (seen
	// 2026-11-12).
	if err == nil && database.Kind == "source" {
		var named string
		var found bool
		if named, found, err = notionDatabaseBlockTitle(app, database.ContainerID, notionNameAsWritten); found && named != "" {
			database.Title = named
		} else if strings.TrimSpace(database.Title) == "" {
			database.Title = notionDatabaseShownTitle(database.Title)
		}
	}
	return database, err == nil, err
}
